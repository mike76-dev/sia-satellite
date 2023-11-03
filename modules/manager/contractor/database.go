package contractor

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
	"go.sia.tech/renterd/object"
)

// initDB initializes the sync state.
func (c *Contractor) initDB() error {
	var count int
	err := c.db.QueryRow("SELECT COUNT(*) FROM ctr_info").Scan(&count)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = c.db.Exec(`
		INSERT INTO ctr_info (height, last_change, synced)
		VALUES (?, ?, ?)
	`, 0, modules.ConsensusChangeBeginning[:], false)
	return err
}

// loadState loads the sync state of the contractor.
func (c *Contractor) loadState() error {
	cc := make([]byte, 32)
	var height uint64
	var synced bool
	err := c.db.QueryRow(`
		SELECT height, last_change, synced
		FROM ctr_info
		WHERE id = 1
	`).Scan(&height, &cc, &synced)
	if err != nil {
		return err
	}

	c.blockHeight = height
	copy(c.lastChange[:], cc)
	c.synced = make(chan struct{})
	if synced {
		close(c.synced)
	}

	return nil
}

// updateState saves the sync state of the contractor.
func (c *Contractor) updateState() error {
	synced := false
	select {
	case <-c.synced:
		synced = true
	default:
	}
	_, err := c.db.Exec(`
		UPDATE ctr_info
		SET height = ?, last_change = ?, synced = ?
		WHERE id = 1
	`, c.blockHeight, c.lastChange[:], synced)
	return err
}

// UpdateRenter updates the renter record in the database.
// The record must have already been created.
func (c *Contractor) UpdateRenter(renter modules.Renter) error {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	renter.Allowance.EncodeTo(e)
	e.Flush()
	_, err := c.db.Exec(`
		UPDATE ctr_renters
		SET current_period = ?, allowance = ?, private_key = ?,
		account_key = ?, auto_renew_contracts = ?,
		backup_file_metadata = ?, auto_repair_files = ?
		WHERE email = ?
	`, renter.CurrentPeriod, buf.Bytes(), renter.PrivateKey, renter.AccountKey, renter.Settings.AutoRenewContracts, renter.Settings.BackupFileMetadata, renter.Settings.AutoRepairFiles, renter.Email)
	return err
}

// updateRenewedContract updates renewed_from and renewed_to
// fields in the contracts table.
func (c *Contractor) updateRenewedContract(oldID, newID types.FileContractID) error {
	_, err := c.db.Exec("UPDATE ctr_contracts SET renewed_from = ? WHERE id = ?", oldID[:], newID[:])
	if err != nil {
		return err
	}
	_, err = c.db.Exec("UPDATE ctr_contracts SET renewed_to = ? WHERE id = ?", newID[:], oldID[:])
	return err
}

// managedFindRenter tries to find a renter by the contract ID.
func (c *Contractor) managedFindRenter(fcid types.FileContractID) (modules.Renter, error) {
	key := make([]byte, 32)
	err := c.db.QueryRow("SELECT renter_pk FROM ctr_contracts WHERE id = ?", fcid[:]).Scan(&key)
	if err != nil {
		return modules.Renter{}, ErrRenterNotFound
	}

	var rpk types.PublicKey
	copy(rpk[:], key)
	renter, exists := c.renters[rpk]
	if exists {
		return renter, nil
	}

	return modules.Renter{}, ErrRenterNotFound
}

// loadDoubleSpent loads the double-spent contracts map from the database.
func (c *Contractor) loadDoubleSpent() error {
	rows, err := c.db.Query("SELECT id, height FROM ctr_dspent")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		id := make([]byte, 32)
		var height uint64
		var fcid types.FileContractID
		if err := rows.Scan(&id, &height); err != nil {
			return err
		}
		copy(fcid[:], id)
		c.doubleSpentContracts[fcid] = height
	}

	return nil
}

// updateDoubleSpent updates the double-spent contracts map in the database.
func (c *Contractor) updateDoubleSpent() error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_dspent")
	if err != nil {
		c.log.Println("ERROR: couldn't clear double-spent contracts:", err)
		tx.Rollback()
		return err
	}

	for id, height := range c.doubleSpentContracts {
		_, err := tx.Exec("INSERT INTO ctr_dspent (id, height) VALUES (?, ?)", id[:], height)
		if err != nil {
			c.log.Println("ERROR: couldn't update double-spent contracts:", err)
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// loadRenewHistory loads the renewal history from the database.
func (c *Contractor) loadRenewHistory() error {
	rows, err := c.db.Query("SELECT id, renewed_from, renewed_to FROM ctr_contracts")
	if err != nil {
		return err
	}
	defer rows.Close()

	id := make([]byte, 32)
	from := make([]byte, 32)
	to := make([]byte, 32)
	var fcid, fcidNew, fcidOld types.FileContractID
	for rows.Next() {
		if err := rows.Scan(&id, &from, &to); err != nil {
			c.log.Println("Error scanning database row:", err)
			continue
		}
		copy(fcid[:], id)
		copy(fcidOld[:], from)
		copy(fcidNew[:], to)

		if fcidOld != (types.FileContractID{}) {
			c.renewedFrom[fcid] = fcidOld
		}
		if fcidNew != (types.FileContractID{}) {
			c.renewedTo[fcid] = fcidNew
		}
	}

	return nil
}

// loadRenters load the renters from the database.
func (c *Contractor) loadRenters() error {
	rows, err := c.db.Query(`
		SELECT email, public_key, current_period,
		allowance, private_key, account_key,
		auto_renew_contracts, backup_file_metadata, auto_repair_files
		FROM ctr_renters
	`)
	if err != nil {
		c.log.Println("ERROR: could not load the renters:", err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var email string
		pk := make([]byte, 32)
		var aBytes []byte
		var period uint64
		sk := make([]byte, 64)
		ak := make([]byte, 64)
		var autoRenew, backupMetadata, autoRepair bool
		if err := rows.Scan(&email, &pk, &period, &aBytes, &sk, &ak, &autoRenew, &backupMetadata, &autoRepair); err != nil {
			c.log.Println("ERROR: could not load the renter:", err)
			continue
		}

		var a modules.Allowance
		buf := bytes.NewBuffer(aBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(aBytes))})
		a.DecodeFrom(d)
		if err := d.Err(); err != nil {
			return err
		}

		renter := modules.Renter{
			Allowance:     a,
			CurrentPeriod: period,
			Email:         email,
			PrivateKey:    types.PrivateKey(sk),
			AccountKey:    types.PrivateKey(ak),
			Settings: modules.RenterSettings{
				AutoRenewContracts: autoRenew,
				BackupFileMetadata: backupMetadata,
				AutoRepairFiles:    autoRepair,
			},
		}
		copy(renter.PublicKey[:], pk)
		c.renters[renter.PublicKey] = renter
	}

	return nil
}

// save saves the watchdog to the database.
func (w *watchdog) save() error {
	tx, err := w.contractor.db.Begin()
	if err != nil {
		w.contractor.log.Println("ERROR: couldn't save watchdog:", err)
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_watchdog")
	if err != nil {
		w.contractor.log.Println("ERROR: couldn't clear watchdog data:", err)
		tx.Rollback()
		return err
	}

	for id, fcs := range w.contracts {
		var buf bytes.Buffer
		e := types.NewEncoder(&buf)
		fcs.EncodeTo(e)
		e.Flush()
		_, err := tx.Exec("INSERT INTO ctr_watchdog (id, bytes) VALUES (?, ?)", id[:], buf.Bytes())
		if err != nil {
			w.contractor.log.Println("ERROR: couldn't save watchdog:", err)
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// load loads the watchdog from the database.
func (w *watchdog) load() error {
	tx, err := w.contractor.db.Begin()
	if err != nil {
		w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
		return err
	}

	rows, err := tx.Query("SELECT id, bytes FROM ctr_watchdog")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
		tx.Rollback()
		return err
	}

	for rows.Next() {
		fcs := new(fileContractStatus)
		id := make([]byte, 32)
		var fcsBytes []byte
		if err := rows.Scan(&id, &fcsBytes); err != nil {
			w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
			rows.Close()
			tx.Rollback()
			return err
		}
		var fcid types.FileContractID
		copy(fcid[:], id)
		buf := bytes.NewBuffer(fcsBytes)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(fcsBytes))})
		fcs.DecodeFrom(d)
		if err := d.Err(); err != nil {
			w.contractor.log.Println("ERROR: couldn't load watchdog:", err)
			rows.Close()
			tx.Rollback()
			return err
		}
		w.contracts[fcid] = fcs

		// Add all parent outputs the formation txn.
		parentOutputs := getParentOutputIDs(fcs.formationTxnSet)
		for _, oid := range parentOutputs {
			w.addOutputDependency(oid, fcid)
		}
	}

	rows.Close()

	for pk, renter := range w.contractor.renters {
		w.renewWindows[pk] = renter.Allowance.RenewWindow
	}

	return tx.Commit()
}

// DeleteMetadata deletes the renter's saved file metadata.
func (c *Contractor) DeleteMetadata(pk types.PublicKey) {
	tx, err := c.db.Begin()
	if err != nil {
		c.log.Println("ERROR: unable to start transaction:", err)
		return
	}

	// Retrieve slab IDs.
	var slabs []types.Hash256
	rows, err := tx.Query("SELECT enc_key FROM ctr_slabs WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Println("ERROR: unable to query slabs:", err)
		tx.Rollback()
		return
	}

	for rows.Next() {
		s := make([]byte, 32)
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			c.log.Println("ERROR: unable to load slab ID:", err)
			tx.Rollback()
			return
		}
		var slab types.Hash256
		copy(slab[:], s)
		slabs = append(slabs, slab)
	}
	rows.Close()

	// Delete shards.
	for _, slab := range slabs {
		_, err := tx.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", slab[:])
		if err != nil {
			c.log.Println("ERROR: unable to delete shards:", err)
			continue
		}
	}

	// Delete slabs.
	_, err = tx.Exec("DELETE FROM ctr_slabs WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Println("ERROR: unable to delete slabs:", err)
		tx.Rollback()
		return
	}

	// Delete partial slab buffer.
	_, err = tx.Exec("DELETE FROM ctr_buffers WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Println("ERROR: unable to delete buffer:", err)
		tx.Rollback()
		return
	}

	// Delete objects.
	_, err = tx.Exec("DELETE FROM ctr_metadata WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Println("ERROR: unable to delete metadata:", err)
		tx.Rollback()
		return
	}

	err = tx.Commit()
	if err != nil {
		c.log.Println("ERROR: unable to commit transaction:", err)
	}
}

// dbDeleteObject deletes a single file metadata object from
// the database.
func dbDeleteObject(tx *sql.Tx, pk types.PublicKey, bucket, path string) error {
	objectID := make([]byte, 32)
	err := tx.QueryRow(`
		SELECT enc_key
		FROM ctr_metadata
		WHERE bucket = ? AND filepath = ? AND renter_pk = ?
	`, bucket, path, pk[:]).Scan(&objectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	rows, err := tx.Query("SELECT enc_key FROM ctr_slabs WHERE object_id = ?", objectID)
	if err != nil {
		return err
	}
	slabs := make(map[types.Hash256]int)
	for rows.Next() {
		var slab types.Hash256
		id := make([]byte, 32)
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		copy(slab[:], id)
		slabs[slab]++
	}
	rows.Close()

	for slab, num := range slabs {
		if num == 1 {
			_, err = tx.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", slab[:])
			if err != nil {
				return err
			}
		}
	}

	_, err = tx.Exec("DELETE FROM ctr_slabs WHERE object_id = ?", objectID)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_buffers WHERE object_id = ?", objectID)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_metadata WHERE enc_key = ?", objectID)
	return err
}

// updateMetadata updates the file metadata in the database.
func (c *Contractor) updateMetadata(pk types.PublicKey, fm modules.FileMetadata) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	if err := dbDeleteObject(tx, pk, fm.Bucket, fm.Path); err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to delete object")
	}

	_, err = tx.Exec(`
		INSERT INTO ctr_metadata (
			enc_key,
			bucket,
			filepath,
			etag,
			mime,
			renter_pk,
			uploaded,
			modified,
			retrieved
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		fm.Key[:],
		fm.Bucket,
		fm.Path,
		fm.ETag,
		fm.MimeType,
		pk[:],
		uint64(time.Now().Unix()),
		uint64(time.Now().Unix()),
		uint64(time.Now().Unix()),
	)
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to store object")
	}

	var packedLength uint64
	emptyID := make([]byte, 32)
	for i, s := range fm.Slabs {
		var count int
		err = tx.QueryRow(`
			SELECT COUNT(*)
			FROM ctr_slabs
			WHERE enc_key = ?
			AND object_id = ?
		`, s.Key[:], emptyID[:]).Scan(&count)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "unable to get packed slab length")
		}
		if count > 0 {
			packedLength = s.Length
			_, err = tx.Exec(`
				UPDATE ctr_slabs
				SET
					object_id = ?,
					offset = ?,
					len = ?,
					num = ?
				WHERE enc_key = ?
				AND object_id = ?
			`, fm.Key[:], s.Offset, s.Length, i, s.Key[:], emptyID[:])
			if err != nil {
				tx.Rollback()
				return modules.AddContext(err, "unable to update orphaned slab")
			}
		} else {
			_, err = tx.Exec(`
				INSERT INTO ctr_slabs (
					enc_key,
					object_id,
					renter_pk,
					min_shards,
					offset,
					len,
					num,
					partial,
					modified,
					retrieved
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, s.Key[:], fm.Key[:], pk[:], s.MinShards, s.Offset, s.Length, i, s.Partial, time.Now().Unix(), time.Now().Unix())
			if err != nil {
				tx.Rollback()
				return modules.AddContext(err, "unable to insert slab")
			}
			for _, ss := range s.Shards {
				_, err = tx.Exec(`
					INSERT INTO ctr_shards (slab_id, host, merkle_root)
					VALUES (?, ?, ?)
				`, s.Key[:], ss.Host[:], ss.Root[:])
				if err != nil {
					tx.Rollback()
					return modules.AddContext(err, "unable to insert shard")
				}
			}
		}
	}

	if len(fm.Data) > int(packedLength) {
		_, err = tx.Exec(`
			INSERT INTO ctr_buffers (object_id, renter_pk, len, data)
			VALUES (?, ?, ?, ?)
		`, fm.Key[:], pk[:], len(fm.Data)-int(packedLength), fm.Data[packedLength:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "unable to save partial data")
		}
	} else {
		_, err = tx.Exec("DELETE FROM ctr_buffers WHERE object_id = ?", fm.Key[:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "unable to delete partial data")
		}
	}

	return tx.Commit()
}

// DeleteObject deletes the saved file metadata object.
func (c *Contractor) DeleteObject(pk types.PublicKey, bucket, path string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	if err = dbDeleteObject(tx, pk, bucket, path); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// retrieveMetadata retrieves the file metadata from the database.
func (c *Contractor) retrieveMetadata(pk types.PublicKey, present []modules.BucketFiles) (fm []modules.FileMetadata, err error) {
	// Create a map of the present objects for convenience.
	po := make(map[string]map[string]struct{})
	for _, bucket := range present {
		for _, path := range bucket.Paths {
			if po[bucket.Name] == nil {
				po[bucket.Name] = make(map[string]struct{})
			}
			po[bucket.Name][path] = struct{}{}
		}
	}

	rows, err := c.db.Query(`
		SELECT
			enc_key,
			bucket,
			filepath,
			etag,
			mime,
			modified,
			retrieved
		FROM ctr_metadata
		WHERE renter_pk = ?
	`, pk[:])
	if err != nil {
		return nil, modules.AddContext(err, "unable to query objects")
	}
	defer rows.Close()

	for rows.Next() {
		var slabs []modules.Slab
		objectID := make([]byte, 32)
		var bucket, path, eTag, mime string
		var modified, retrieved uint64
		if err := rows.Scan(&objectID, &bucket, &path, &eTag, &mime, &modified, &retrieved); err != nil {
			return nil, modules.AddContext(err, "unable to retrieve object")
		}

		// If the object is present in the map and hasn't been updated
		// since the last retrieval, skip it.
		if files, exists := po[bucket]; exists {
			if _, exists := files[path]; exists {
				if modified <= retrieved {
					continue
				}
			}
		}

		slabRows, err := c.db.Query(`
			SELECT enc_key, min_shards, offset, len, num, partial
			FROM ctr_slabs
			WHERE object_id = ?
			ORDER BY num ASC
		`, objectID)
		if err != nil {
			return nil, modules.AddContext(err, "unable to query slabs")
		}

		for slabRows.Next() {
			var shards []modules.Shard
			slabID := make([]byte, 32)
			var minShards uint8
			var offset, length uint64
			var i int
			var partial bool
			if err := slabRows.Scan(&slabID, &minShards, &offset, &length, &i, &partial); err != nil {
				slabRows.Close()
				return nil, modules.AddContext(err, "unable to retrieve slab")
			}
			shardRows, err := c.db.Query("SELECT host, merkle_root FROM ctr_shards WHERE slab_id = ?", slabID[:])
			if err != nil {
				slabRows.Close()
				return nil, modules.AddContext(err, "unable to query slabs")
			}
			for shardRows.Next() {
				host := make([]byte, 32)
				root := make([]byte, 32)
				if err := shardRows.Scan(&host, &root); err != nil {
					shardRows.Close()
					slabRows.Close()
					return nil, modules.AddContext(err, "unable to retrieve shard")
				}
				var shard modules.Shard
				copy(shard.Host[:], host)
				copy(shard.Root[:], root)
				shards = append(shards, shard)
			}

			shardRows.Close()
			var slab modules.Slab
			copy(slab.Key[:], slabID)
			slab.MinShards = minShards
			slab.Offset = offset
			slab.Length = length
			slab.Partial = partial
			slab.Shards = shards
			slabs = append(slabs, slab)
		}

		slabRows.Close()
		var md modules.FileMetadata
		copy(md.Key[:], objectID)
		md.Bucket = bucket
		md.Path = path
		md.ETag = eTag
		md.MimeType = mime
		md.Slabs = slabs
		fm = append(fm, md)

		// Update timestamp.
		_, err = c.db.Exec(`
			UPDATE ctr_metadata
			SET retrieved = ?
			WHERE enc_key = ?
		`, uint64(time.Now().Unix()), objectID[:])
		if err != nil {
			return nil, modules.AddContext(err, "unable to update timestamp")
		}
	}

	return
}

// updateSlab updates a file slab after a successful migration.
func (c *Contractor) updateSlab(rpk types.PublicKey, slab modules.Slab, packed bool) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	rows, err := tx.Query(`
		SELECT object_id
		FROM ctr_slabs
		WHERE enc_key = ?
	`, slab.Key[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "couldn't query slabs")
	}

	var ids []types.Hash256
	for rows.Next() {
		b := make([]byte, 32)
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			tx.Rollback()
			return modules.AddContext(err, "couldn't get object ID")
		}
		var id types.Hash256
		copy(id[:], b)
		ids = append(ids, id)
	}
	rows.Close()

	_, err = tx.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", slab.Key[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "couldn't delete shards")
	}

	for _, shard := range slab.Shards {
		_, err = tx.Exec(`
			INSERT INTO ctr_shards (slab_id, host, merkle_root)
			VALUES (?, ?, ?)
		`, slab.Key[:], shard.Host[:], shard.Root[:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't insert shard")
		}
	}

	if packed {
		_, err = tx.Exec(`
			INSERT INTO ctr_slabs (
				enc_key,
				object_id,
				renter_pk,
				min_shards,
				offset,
				len,
				num,
				partial,
				modified,
				retrieved
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, slab.Key[:], []byte{}, rpk[:], slab.MinShards, slab.Offset, slab.Length, 0, false, time.Now().Unix(), time.Now().Unix())
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't save packed slab")
		}
	} else {
		_, err = tx.Exec(`
			UPDATE ctr_slabs
			SET modified = ?
			WHERE enc_key = ?
		`, time.Now().Unix(), slab.Key[:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't update slab")
		}
	}

	for _, id := range ids {
		_, err = tx.Exec(`
			UPDATE ctr_metadata
			SET modified = ?
			WHERE enc_key = ?
		`, uint64(time.Now().Unix()), id[:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't update object")
		}
	}

	return tx.Commit()
}

// encryptionKey is a helper function that converts a types.Hash256
// to an object.EncryptionKey.
func encryptionKey(id types.Hash256) (key object.EncryptionKey, err error) {
	keyStr := hex.EncodeToString(id[:])
	err = key.UnmarshalText([]byte(keyStr))
	if err != nil {
		return object.EncryptionKey{}, err
	}
	return
}

// convertEncryptionKey converts an object.EncryptionKey to a
// types.Hash256.
func convertEncryptionKey(key object.EncryptionKey) (id types.Hash256, err error) {
	s := strings.TrimPrefix(key.String(), "key:")
	b, err := hex.DecodeString(s)
	copy(id[:], b)
	return
}

// getSlab tries to find a slab by the encryption key.
func (c *Contractor) getSlab(id types.Hash256) (slab object.Slab, offset, length uint32, err error) {
	// Unmarshal encryption key first.
	key, err := encryptionKey(id)
	if err != nil {
		return object.Slab{}, 0, 0, modules.AddContext(err, "couldn't unmarshal key")
	}

	// Start a transaction.
	tx, err := c.db.Begin()
	if err != nil {
		return
	}

	// Load shards.
	rows, err := tx.Query("SELECT host, merkle_root FROM ctr_shards WHERE slab_id = ?", id[:])
	if err != nil {
		tx.Rollback()
		return object.Slab{}, 0, 0, modules.AddContext(err, "couldn't query shards")
	}

	var shards []object.Sector
	for rows.Next() {
		h := make([]byte, 32)
		r := make([]byte, 32)
		if err := rows.Scan(&h, &r); err != nil {
			rows.Close()
			tx.Rollback()
			return object.Slab{}, 0, 0, modules.AddContext(err, "couldn't retrieve shard")
		}
		var shard object.Sector
		copy(shard.Host[:], h)
		copy(shard.Root[:], r)
		shards = append(shards, shard)
	}
	rows.Close()

	// Load the slab.
	var minShards uint8
	err = tx.QueryRow(`
		SELECT min_shards, offset, len
		FROM ctr_slabs
		WHERE enc_key = ?
	`, id[:]).Scan(&minShards, &offset, &length)
	if err != nil {
		tx.Rollback()
		return object.Slab{}, 0, 0, modules.AddContext(err, "couldn't load slab")
	}

	slab = object.Slab{
		Key:       key,
		MinShards: minShards,
		Shards:    shards,
	}

	tx.Commit()
	return
}

// getSlabs loads the slabs from the database.
func (c *Contractor) getSlabs() (slabs []slabInfo, err error) {
	// Load slabs.
	emptyID := make([]byte, 32)
	rows, err := c.db.Query(`
		SELECT enc_key, renter_pk, min_shards, offset, len, num, partial
		FROM ctr_slabs
		WHERE object_id <> ?
		ORDER BY num ASC
	`, emptyID[:])
	if err != nil {
		return nil, modules.AddContext(err, "unable to query slabs")
	}

	for rows.Next() {
		key := make([]byte, 32)
		rpk := make([]byte, 32)
		var minShards uint8
		var offset, length uint64
		var i int
		var partial bool
		if err := rows.Scan(&key, &rpk, &minShards, &offset, &length, &i, &partial); err != nil {
			rows.Close()
			return nil, modules.AddContext(err, "unable to retrieve slab")
		}
		slab := modules.Slab{
			MinShards: minShards,
			Offset:    offset,
			Length:    length,
			Partial:   partial,
		}
		copy(slab.Key[:], key)
		si := slabInfo{
			Slab: slab,
		}
		copy(si.renterKey[:], rpk)
		slabs = append(slabs, si)
	}
	rows.Close()

	// Load shards.
	for index := range slabs {
		rows, err := c.db.Query("SELECT host, merkle_root FROM ctr_shards WHERE slab_id = ?", slabs[index].Key[:])
		if err != nil {
			return nil, modules.AddContext(err, "unable to query shards")
		}

		for rows.Next() {
			host := make([]byte, 32)
			root := make([]byte, 32)
			if err := rows.Scan(&host, &root); err != nil {
				rows.Close()
				return nil, modules.AddContext(err, "unable to retrieve shard")
			}
			var shard modules.Shard
			copy(shard.Host[:], host)
			copy(shard.Root[:], root)
			slabs[index].Shards = append(slabs[index].Shards, shard)
		}
		rows.Close()
	}

	return
}

// getObject tries to find an object by its path.
func (c *Contractor) getObject(pk types.PublicKey, bucket, path string) (object.Object, error) {
	// Start a transaction.
	tx, err := c.db.Begin()
	if err != nil {
		return object.Object{}, err
	}

	// Find the object ID.
	objectID := make([]byte, 32)
	err = tx.QueryRow(`
		SELECT enc_key
		FROM ctr_metadata
		WHERE bucket = ? AND filepath = ? AND renter_pk = ?
	`, bucket, path, pk[:]).Scan(&objectID)
	if err != nil {
		tx.Rollback()
		return object.Object{}, modules.AddContext(err, "couldn't find object")
	}
	var key types.Hash256
	copy(key[:], objectID)
	objectKey, err := encryptionKey(key)
	if err != nil {
		tx.Rollback()
		return object.Object{}, modules.AddContext(err, "couldn't unmarshal object key")
	}

	// Load slabs.
	rows, err := tx.Query(`
		SELECT enc_key, min_shards, offset, len, num, partial
		FROM ctr_slabs
		WHERE object_id = ?
		ORDER BY num ASC
	`, objectID)
	if err != nil {
		tx.Rollback()
		return object.Object{}, modules.AddContext(err, "couldn't load slabs")
	}

	var slabs []object.SlabSlice
	var partialSlabs []object.PartialSlab
	for rows.Next() {
		slabID := make([]byte, 32)
		var minShards uint8
		var offset, length uint64
		var n int
		var p bool
		if err = rows.Scan(&slabID, &minShards, &offset, &length, &n, &p); err != nil {
			rows.Close()
			tx.Rollback()
			return object.Object{}, modules.AddContext(err, "couldn't load slab")
		}
		copy(key[:], slabID)
		var slabKey object.EncryptionKey
		slabKey, err = encryptionKey(key)
		if err != nil {
			rows.Close()
			tx.Rollback()
			return object.Object{}, modules.AddContext(err, "couldn't unmarshal slab key")
		}
		if p {
			partialSlabs = append(partialSlabs, object.PartialSlab{
				Key:    slabKey,
				Offset: uint32(offset),
				Length: uint32(length),
			})
		} else {
			slabs = append(slabs, object.SlabSlice{
				Slab: object.Slab{
					Key:       slabKey,
					MinShards: minShards,
				},
				Offset: uint32(offset),
				Length: uint32(length),
			})
		}
	}
	rows.Close()

	// Load shards.
	for index := range slabs {
		var id types.Hash256
		id, err = convertEncryptionKey(slabs[index].Key)
		if err != nil {
			tx.Rollback()
			return object.Object{}, modules.AddContext(err, "couldn't marshal encryption key")
		}
		rows, err = tx.Query(`
			SELECT host, merkle_root
			FROM ctr_shards
			WHERE slab_id = ?
		`, id[:])
		if err != nil {
			tx.Rollback()
			return object.Object{}, modules.AddContext(err, "couldn't load shards")
		}

		for rows.Next() {
			host := make([]byte, 32)
			root := make([]byte, 32)
			if err = rows.Scan(&host, &root); err != nil {
				rows.Close()
				tx.Rollback()
				return object.Object{}, modules.AddContext(err, "couldn't load shard")
			}
			var shard object.Sector
			copy(shard.Host[:], host)
			copy(shard.Root[:], root)
			slabs[index].Shards = append(slabs[index].Shards, shard)
		}
		rows.Close()
	}

	// Construct the object.
	obj := object.Object{
		Key:          objectKey,
		Slabs:        slabs,
		PartialSlabs: partialSlabs,
	}

	tx.Commit()
	return obj, nil
}

// getPartialSlab returns the chunk of data corresponding to the
// params supplied.
func (c *Contractor) getPartialSlab(ec object.EncryptionKey) ([]byte, error) {
	key, err := convertEncryptionKey(ec)
	if err != nil {
		return nil, modules.AddContext(err, "couldn't unmarshal key")
	}

	var data []byte
	err = c.db.QueryRow("SELECT data FROM ctr_buffers WHERE object_id = ?", key[:]).Scan(&data)
	if err != nil {
		return nil, modules.AddContext(err, "couldn't retrieve partial slab data")
	}

	return data, nil
}

// GetModifiedSlabs returns all slabs modified since the last retrieval.
func (c *Contractor) GetModifiedSlabs(rpk types.PublicKey) ([]modules.Slab, error) {
	tx, err := c.db.Begin()
	if err != nil {
		return nil, modules.AddContext(err, "couldn't start transaction")
	}

	// Make a list of slabs.
	emptyID := make([]byte, 32)
	rows, err := tx.Query(`
		SELECT enc_key, min_shards
		FROM ctr_slabs
		WHERE renter_pk = ?
		AND modified > retrieved
		AND partial = FALSE
		AND object_id <> ?
	`, rpk[:], emptyID[:])
	if err != nil {
		tx.Rollback()
		return nil, modules.AddContext(err, "couldn't query slabs")
	}

	// Make a map first in order to exclude duplicate slabs.
	slabsMap := make(map[types.Hash256]modules.Slab)
	for rows.Next() {
		key := make([]byte, 32)
		var minShards uint8
		if err := rows.Scan(&key, &minShards); err != nil {
			rows.Close()
			tx.Rollback()
			return nil, modules.AddContext(err, "couldn't retrieve slab")
		}
		slab := modules.Slab{
			MinShards: minShards,
		}
		copy(slab.Key[:], key)
		slabsMap[slab.Key] = slab
	}
	rows.Close()

	// Convert map to a slice.
	var slabs []modules.Slab
	for _, slab := range slabsMap {
		slabs = append(slabs, slab)
	}

	// Retrieve shards.
	for i, slab := range slabs {
		rows, err := tx.Query(`
			SELECT host, merkle_root
			FROM ctr_shards
			WHERE slab_id = ?
		`, slab.Key[:])
		if err != nil {
			tx.Rollback()
			return nil, modules.AddContext(err, "couldn't query shards")
		}

		for rows.Next() {
			host := make([]byte, 32)
			root := make([]byte, 32)
			if err := rows.Scan(&host, &root); err != nil {
				rows.Close()
				tx.Rollback()
				return nil, modules.AddContext(err, "couldn't retrieve shard")
			}
			var shard modules.Shard
			copy(shard.Host[:], host)
			copy(shard.Root[:], root)
			slabs[i].Shards = append(slabs[i].Shards, shard)
		}
		rows.Close()
	}

	// Update retrieved timestamp.
	for _, slab := range slabs {
		_, err = tx.Exec(`
			UPDATE ctr_slabs
			SET retrieved = ?
			WHERE enc_key = ?
			AND object_id <> ?
		`, time.Now().Unix(), slab.Key[:], emptyID[:])
		if err != nil {
			return nil, modules.AddContext(err, "couldn't update timestamp")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, modules.AddContext(err, "couldn't commit transaction")
	}

	return slabs, nil
}
