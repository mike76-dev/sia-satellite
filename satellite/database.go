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

	scRate, err := s.GetSiacoinRate(c)
	if err != nil {
		return nil, err
	}

	ub := &modules.UserBalance{
		IsUser:     !errors.Is(err, sql.ErrNoRows),
		Subscribed: sub,
		Balance:    b,
		Locked:     l,
		Currency:   c,
		StripeID:   id,
		SCRate:     scRate,
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
	if amountWithFee > ub.Balance {
		s.log.Println("WARN: trying to lock more than the available balance")
		amountWithFee = ub.Balance
	}

	// Calculate the new balance.
	ub.Locked += amount
	ub.Balance -= amountWithFee

	// Save the new balance.
	err = s.UpdateBalance(email, ub)
	if err != nil {
		return err
	}

	// Update the spendings.
	us, err := s.getSpendings(email)
	if err != nil {
		return err
	}
	us.CurrentLocked += amount
	us.CurrentOverhead += amountWithFee - amount

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
	unlocked := amount
	burned := totalWithFee - amount
	if totalWithFee > ub.Locked {
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
	prevMonth := s.prevMonth.BlockHeight
	currentMonth := s.currentMonth.BlockHeight
	s.mu.Unlock()
	if height < prevMonth {
		// Spending outside the reporting period.
		return nil
	}
	us, err := s.getSpendings(email)
	if err != nil {
		return err
	}
	if height < currentMonth {
		us.PrevLocked -= (unlocked + burned)
		us.PrevUsed += burned
	} else {
		us.CurrentLocked -= (unlocked + burned)
		us.CurrentUsed += burned
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
	scRate, err := s.GetSiacoinRate(currency)
	if err != nil {
		return nil, err
	}
	if scRate == 0 {
		return nil, errors.New("couldn't get exchange rate")
	}

	// Get user spendings.
	us, err := s.getSpendings(email)
	if err != nil {
		return nil, err
	}
	us.SCRate = scRate

	return us, nil
}
