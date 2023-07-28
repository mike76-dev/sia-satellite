package manager

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// dbGetBlockTimestamps retrieves the block timestamps from the database.
func dbGetBlockTimestamps(tx *sql.Tx) (curr blockTimestamp, prev blockTimestamp, err error) {
	var height, timestamp uint64
	err = tx.QueryRow("SELECT height, time FROM mg_timestamp WHERE id = 1").Scan(&height, &timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return blockTimestamp{}, blockTimestamp{}, nil
	}
	if err != nil {
		return blockTimestamp{}, blockTimestamp{}, err
	}
	curr.BlockHeight = height
	curr.Timestamp = time.Unix(int64(timestamp), 0)

	err = tx.QueryRow("SELECT height, time FROM mg_timestamp WHERE id = 2").Scan(&height, &timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return curr, blockTimestamp{}, nil
	}
	if err != nil {
		return curr, blockTimestamp{}, err
	}
	prev.BlockHeight = height
	prev.Timestamp = time.Unix(int64(timestamp), 0)

	return
}

// dbPutBlockTimestamps saves the block timestamps in the database.
func dbPutBlockTimestamps(tx *sql.Tx, curr blockTimestamp, prev blockTimestamp) error {
	var ct, pt uint64
	if curr.BlockHeight > 0 {
		ct = uint64(curr.Timestamp.Unix())
	}
	_, err := tx.Exec(`
		REPLACE INTO mg_timestamp (id, height, time)
		VALUES (1, ?, ?)
	`, curr.BlockHeight, ct)
	if err != nil {
		return err
	}

	if prev.BlockHeight > 0 {
		pt = uint64(prev.Timestamp.Unix())
	}
	_, err = tx.Exec(`
		REPLACE INTO mg_timestamp (id, height, time)
		VALUES (2, ?, ?)
	`, prev.BlockHeight, pt)

	return err
}

// dbGetAverages retrieves the host network averages from the database.
func dbGetAverages(tx *sql.Tx) (avg modules.HostAverages, err error) {
	var avgBytes []byte
	err = tx.QueryRow("SELECT bytes FROM mg_averages WHERE id = 1").Scan(&avgBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return
	}

	d := types.NewDecoder(io.LimitedReader{R: bytes.NewBuffer(avgBytes), N: int64(len(avgBytes))})
	avg.DecodeFrom(d)
	err = d.Err()

	return
}

// dbPutAverages retrieves the host network averages from the database.
func dbPutAverages(tx *sql.Tx, avg modules.HostAverages) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	avg.EncodeTo(e)
	e.Flush()

	_, err := tx.Exec("REPLACE INTO mg_averages (id, bytes) VALUES (1, ?)", buf.Bytes())

	return err
}

// GetBalance retrieves the balance information on the account.
// An empty struct is returned when there is no data.
func (m *Manager) GetBalance(email string) (modules.UserBalance, error) {
	var sub bool
	var b, l float64
	var c, id string
	err := m.db.QueryRow(`
		SELECT subscribed, sc_balance, sc_locked, currency, stripe_id
		FROM mg_balances WHERE email = ?
	`, email).Scan(&sub, &b, &l, &c, &id)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return modules.UserBalance{}, nil
	}
	if err != nil {
		return modules.UserBalance{}, err
	}

	scRate, err := m.GetSiacoinRate(c)
	if err != nil {
		return modules.UserBalance{}, err
	}

	ub := modules.UserBalance{
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
func (m *Manager) UpdateBalance(email string, ub modules.UserBalance) error {
	_, err := m.db.Exec(`
		REPLACE INTO mg_balances
		(email, subscribed, sc_balance, sc_locked, currency, stripe_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, email, ub.Subscribed, ub.Balance, ub.Locked, ub.Currency, ub.StripeID)

	return err
}

// getSpendings retrieves the user's spendings.
func (m *Manager) getSpendings(email string) (modules.UserSpendings, error) {
	var currLocked, currUsed, currOverhead float64
	var prevLocked, prevUsed, prevOverhead float64
	var currFormed, currRenewed, prevFormed, prevRenewed uint64

	err := m.db.QueryRow(`
		SELECT current_locked, current_used, current_overhead,
			prev_locked, prev_used, prev_overhead,
			current_formed, current_renewed,
			prev_formed, prev_renewed
		FROM mg_spendings
		WHERE email = ?`, email).Scan(&currLocked, &currUsed, &currOverhead, &prevLocked, &prevUsed, &prevOverhead, &currFormed, &currRenewed, &prevFormed, &prevRenewed)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return modules.UserSpendings{}, err
	}

	us := modules.UserSpendings{
		CurrentLocked:   currLocked,
		CurrentUsed:     currUsed,
		CurrentOverhead: currOverhead,
		PrevLocked:      prevLocked,
		PrevUsed:        prevUsed,
		PrevOverhead:    prevOverhead,
		CurrentFormed:   currFormed,
		CurrentRenewed:  currRenewed,
		PrevFormed:      prevFormed,
		PrevRenewed:     prevRenewed,
	}

	return us, nil
}

// updateSpendings updates the user's spendings.
func (m *Manager) updateSpendings(email string, us modules.UserSpendings) error {
	_, err := m.db.Exec(`
		REPLACE INTO mg_spendings
		(email, current_locked, current_used, current_overhead,
		prev_locked, prev_used, prev_overhead,
		current_formed, current_renewed,
		prev_formed, prev_renewed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, email, us.CurrentLocked, us.CurrentUsed, us.CurrentOverhead, us.PrevLocked, us.PrevUsed, us.PrevOverhead, us.CurrentFormed, us.CurrentRenewed, us.PrevFormed, us.PrevRenewed)

	return err
}

// IncrementStats increments the number of formed or renewed contracts.
func (m *Manager) IncrementStats(email string, renewed bool) (err error) {
	if renewed {
		_, err = m.db.Exec(`
			UPDATE mg_spendings
			SET current_renewed = current_renewed + 1
			WHERE email = ?
		`, email)
	} else {
		_, err = m.db.Exec(`
			UPDATE mg_spendings
			SET current_formed = current_formed + 1
			WHERE email = ?
		`, email)
	}
	return
}

// numSlabs returns the count of file slab metadata objects stored for
// the specified renter.
func (m *Manager) numSlabs(pk types.PublicKey) (count int, err error) {
	var items []types.Hash256
	rows, err := m.db.Query("SELECT id FROM ctr_metadata WHERE renter_pk = ?", pk[:])
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item types.Hash256
		id := make([]byte, 32)
		if err = rows.Scan(&id); err != nil {
			return
		}
		copy(item[:], id)
		items = append(items, item)
	}

	for _, item := range items {
		var c int
		err = m.db.QueryRow("SELECT COUNT(*) FROM ctr_slabs WHERE object_id = ?", item[:]).Scan(&c)
		count += c
	}

	return
}
