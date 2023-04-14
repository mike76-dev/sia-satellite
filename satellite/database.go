package satellite

import (
	"database/sql"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/siad/types"
)

// GetBalance retrieves the balance information on the account.
// An empty struct is returned when there is no data.
func (s *Satellite) GetBalance(email string) (*modules.UserBalance, error) {
	var sub bool
	var b, l float64
	var c, id string
	err := s.db.QueryRow(`
		SELECT subscribed, balance, locked, currency, stripe_id
		FROM balances WHERE email = ?
	`, email).Scan(&sub, &b, &l, &c, &id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var scBalance float64
	scRate, _ := s.GetSiacoinRate(c)
	if scRate > 0 {
		scBalance = b / scRate
	}

	ub := &modules.UserBalance{
		IsUser:     !errors.Is(err, sql.ErrNoRows),
		Subscribed: sub,
		Balance:    b,
		Locked:     l,
		Currency:   c,
		StripeID:   id,
		SCBalance:  scBalance,
	}

	return ub, nil
}

// UpdateBalance updates the balance information on the account.
func (s *Satellite) UpdateBalance(email string, ub *modules.UserBalance) error {
	// Check if there is a record already.
	var c int
	err := s.db.QueryRow("SELECT COUNT(*) FROM balances WHERE email = ?", email).Scan(&c)
	if err != nil {
		return err
	}

	// There is a record.
	if c > 0 {
		_, err := s.db.Exec(`
			UPDATE balances
			SET subscribed = ?, balance = ?, locked = ?, currency = ?, stripe_id = ?
			WHERE email = ?
		`, ub.Subscribed, ub.Balance, ub.Locked, ub.Currency, ub.StripeID, email)
		return err
	}

	// No records found.
	_, err = s.db.Exec(`
		INSERT INTO balances (email, subscribed, balance, locked, currency, stripe_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, email, ub.Subscribed, ub.Balance, ub.Locked, ub.Currency, ub.StripeID)

	return err
}

// LockSiacoins implements FundLocker interface.
func (s *Satellite) LockSiacoins(email string, amount float64) error {
	// Sanity check.
	if amount <= 0 {
		return errors.New("wrong amount")
	}

	// Retrieve the user balance.
	ub, err := s.GetBalance(email)
	if err != nil {
		return err
	}

	// Include the Satellite fee.
	amountWithFee := amount * modules.SatelliteOverhead
	if amountWithFee > ub.SCBalance {
		s.log.Println("WARN: trying to lock more than the available balance")
		amountWithFee = ub.SCBalance
	}

	// Calculate the new balance.
	scRate, _ := s.GetSiacoinRate(ub.Currency)
	if scRate == 0 {
		return errors.New("unable to fetch SC rate")
	}
	locked := amountWithFee * scRate
	ub.Locked += locked
	ub.Balance -= locked

	// Save the new balance.
	err = s.UpdateBalance(email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	s.mu.Lock()
	rate := s.scusdRate
	s.mu.Unlock()
	us, err := s.getSpendings(email)
	if err != nil {
		return err
	}
	us.CurrentLocked += amount * rate
	us.CurrentOverhead += (modules.SatelliteOverhead - 1) * amount * rate

	return s.updateSpendings(email, *us)
}

// UnlockSiacoins implements FundLocker interface.
func (s *Satellite) UnlockSiacoins(email string, amount, total float64, height types.BlockHeight) error {
	// Sanity check.
	if amount <= 0 || total <= 0 || amount > total {
		return errors.New("wrong amount")
	}

	// Retrieve the user balance.
	ub, err := s.GetBalance(email)
	if err != nil {
		return err
	}

	// Include the Satellite fee.
	totalWithFee := total * modules.SatelliteOverhead

	// Calculate the new balance.
	scRate, _ := s.GetSiacoinRate(ub.Currency)
	if scRate == 0 {
		return errors.New("unable to fetch SC rate")
	}
	unlocked := amount * scRate
	burned := (totalWithFee - amount) * scRate
	if unlocked + burned > ub.Locked {
		s.log.Println("WARN: trying to unlock more than the locked balance")
		if burned < ub.Locked {
			unlocked = ub.Locked - burned
		} else {
			burned = ub.Locked
			unlocked = 0
		}
	}
	ub.Locked -= (unlocked + burned)
	ub.Balance += unlocked

	// Save the new balance.
	err = s.UpdateBalance(email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	s.mu.Lock()
	rate := s.scusdRate
	prevMonth := s.prevMonth.BlockHeight
	currentMonth := s.currentMonth.BlockHeight
	s.mu.Unlock()
	if height < prevMonth {
		// Spending outside the reporting period.
		return nil
	}
	unlockedUSD := amount * rate
	burnedUSD := (totalWithFee - amount) * rate
	us, err := s.getSpendings(email)
	if err != nil {
		return err
	}
	if height < currentMonth {
		if unlockedUSD + burnedUSD > us.PrevLocked {
			s.log.Println("WARN: trying to unlock more than the locked balance")
			if burnedUSD < us.PrevLocked {
				unlockedUSD = us.PrevLocked - burnedUSD
			} else {
				burnedUSD = us.PrevLocked
				unlockedUSD = 0
			}
		}
		us.PrevLocked -= (unlockedUSD + burnedUSD)
		us.PrevUsed += burnedUSD
	} else {
		if unlockedUSD + burnedUSD > us.CurrentLocked {
			s.log.Println("WARN: trying to unlock more than the locked balance")
			if burnedUSD < us.CurrentLocked {
				unlockedUSD = us.CurrentLocked - burnedUSD
			} else {
				burnedUSD = us.CurrentLocked
				unlockedUSD = 0
			}
		}
		us.CurrentLocked -= (unlockedUSD + burnedUSD)
		us.CurrentUsed += burnedUSD
	}

	return s.updateSpendings(email, *us)
}

// getSpendings retrieves the user's spendings.
func (s *Satellite) getSpendings(email string) (*modules.UserSpendings, error) {
	var currLocked, currUsed, currOverhead float64
	var prevLocked, prevUsed, prevOverhead float64

	err := s.db.QueryRow(`
		SELECT current_locked, current_used, current_overhead,
		prev_locked, prev_used, prev_overhead
		FROM spendings
		WHERE email = ?`, email).Scan(&currLocked, &currUsed, &currOverhead, &prevLocked, &prevUsed, &prevOverhead)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	us := &modules.UserSpendings{
		CurrentLocked:   currLocked,
		CurrentUsed:     currUsed,
		CurrentOverhead: currOverhead,
		PrevLocked:      prevLocked,
		PrevUsed:        prevUsed,
		PrevOverhead:    prevOverhead,
	}

	return us, nil
}

// updateSpendings updates the user's spendings.
func (s *Satellite) updateSpendings(email string, us modules.UserSpendings) error {
	// Check if there is a record already.
	var c int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM spendings
		WHERE email = ?
	`, email).Scan(&c)
	if err != nil {
		return err
	}

	if c > 0 {
		_, err = s.db.Exec(`
			UPDATE spendings
			SET current_locked = ?, current_used = ?, current_overhead = ?,
			prev_locked = ?, prev_used = ?, prev_overhead = ?
			WHERE email = ?
		`, us.CurrentLocked, us.CurrentUsed, us.CurrentOverhead, us.PrevLocked, us.PrevUsed, us.PrevOverhead, email)
	} else {
		_, err = s.db.Exec(`
			INSERT INTO spendings
			 (email, current_locked, current_used, current_overhead,
			 prev_locked, prev_used, prev_overhead)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, email, us.CurrentLocked, us.CurrentUsed, us.CurrentOverhead, us.PrevLocked, us.PrevUsed, us.PrevOverhead)
	}

	return err
}

// RetrieveSpendings retrieves the user's spendings in the given currency.
func (s *Satellite) RetrieveSpendings(email string, currency string) (*modules.UserSpendings, error) {
	// Get exchange rate.
	rate, err := s.GetExchangeRate(currency)
	if err != nil {
		return nil, err
	}
	if rate == 0 {
		return nil, errors.New("couldn't get exchange rate")
	}

	// Get user spendings.
	us, err := s.getSpendings(email)
	if err != nil {
		return nil, err
	}
	us.CurrentLocked /= rate
	us.CurrentUsed /= rate
	us.CurrentOverhead /= rate
	us.PrevLocked /= rate
	us.PrevUsed /= rate
	us.PrevOverhead /= rate

	return us, nil
}
