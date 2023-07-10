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
