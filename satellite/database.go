package satellite

import (
	"database/sql"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"
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
