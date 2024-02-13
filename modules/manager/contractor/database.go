package contractor

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/object"
	"github.com/mike76-dev/sia-satellite/modules"
	"go.uber.org/zap"
	"lukechampine.com/frand"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
)

// bufferedFilesUploadInterval determines how often buffered files
// are uploaded to the network.
const bufferedFilesUploadInterval = 10 * time.Minute

// orphanedSlabPruneInterval determines how often orphaned slabs
// are pruned.
const orphanedSlabPruneInterval = 30 * time.Minute

// orphanedSlabPruneThreshold determines how old an orphaned slab
// has to be to get pruned.
const orphanedSlabPruneThreshold = 2 * time.Hour

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
	`, 0, []byte{}, false)
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

	c.tip.Height = height
	copy(c.tip.ID[:], cc)
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
	`, c.tip.Height, c.tip.ID[:], synced)
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
		backup_file_metadata = ?, auto_repair_files = ?, proxy_uploads = ?
		WHERE email = ?
	`,
		renter.CurrentPeriod,
		buf.Bytes(),
		renter.PrivateKey,
		renter.AccountKey,
		renter.Settings.AutoRenewContracts,
		renter.Settings.BackupFileMetadata,
		renter.Settings.AutoRepairFiles,
		renter.Settings.ProxyUploads,
		renter.Email,
	)
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
		c.log.Error("couldn't clear double-spent contracts", zap.Error(err))
		tx.Rollback()
		return err
	}

	for id, height := range c.doubleSpentContracts {
		_, err := tx.Exec("INSERT INTO ctr_dspent (id, height) VALUES (?, ?)", id[:], height)
		if err != nil {
			c.log.Error("couldn't update double-spent contracts", zap.Error(err))
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
			c.log.Error("error scanning database row", zap.Error(err))
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
		auto_renew_contracts, backup_file_metadata,
		auto_repair_files, proxy_uploads
		FROM ctr_renters
	`)
	if err != nil {
		c.log.Error("could not load the renters", zap.Error(err))
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
		var autoRenew, backupMetadata, autoRepair, proxyUploads bool
		if err := rows.Scan(
			&email,
			&pk,
			&period,
			&aBytes,
			&sk,
			&ak,
			&autoRenew,
			&backupMetadata,
			&autoRepair,
			&proxyUploads,
		); err != nil {
			c.log.Error("could not load the renter", zap.Error(err))
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
				ProxyUploads:       proxyUploads,
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
		w.contractor.log.Error("couldn't save watchdog", zap.Error(err))
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_watchdog")
	if err != nil {
		w.contractor.log.Error("couldn't clear watchdog data", zap.Error(err))
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
			w.contractor.log.Error("couldn't save watchdog", zap.Error(err))
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
		w.contractor.log.Error("couldn't load watchdog", zap.Error(err))
		return err
	}

	rows, err := tx.Query("SELECT id, bytes FROM ctr_watchdog")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		w.contractor.log.Error("couldn't load watchdog", zap.Error(err))
		tx.Rollback()
		return err
	}

	for rows.Next() {
		fcs := new(fileContractStatus)
		id := make([]byte, 32)
		var fcsBytes []byte
		if err := rows.Scan(&id, &fcsBytes); err != nil {
			w.contractor.log.Error("couldn't load watchdog", zap.Error(err))
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
			w.contractor.log.Error("couldn't load watchdog", zap.Error(err))
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
func (c *Contractor) DeleteMetadata(pk types.PublicKey) error {
	tx, err := c.db.Begin()
	if err != nil {
		c.log.Error("unable to start transaction", zap.Error(err))
		return modules.AddContext(err, "unable to start transaction")
	}

	// Retrieve slab IDs.
	var slabs []types.Hash256
	rows, err := tx.Query("SELECT enc_key FROM ctr_slabs WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Error("unable to query slabs", zap.Error(err))
		tx.Rollback()
		return modules.AddContext(err, "unable to query slabs")
	}

	for rows.Next() {
		s := make([]byte, 32)
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			c.log.Error("unable to load slab ID", zap.Error(err))
			tx.Rollback()
			return modules.AddContext(err, "unable to load slab ID")
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
			c.log.Error("unable to delete shards", zap.Error(err))
			continue
		}
	}

	// Delete slabs.
	_, err = tx.Exec("DELETE FROM ctr_slabs WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Error("unable to delete slabs", zap.Error(err))
		tx.Rollback()
		return modules.AddContext(err, "unable to delete slabs")
	}

	// Delete objects.
	_, err = tx.Exec("DELETE FROM ctr_metadata WHERE renter_pk = ?", pk[:])
	if err != nil {
		c.log.Error("unable to delete metadata", zap.Error(err))
		tx.Rollback()
		return modules.AddContext(err, "unable to delete metadata")
	}

	err = tx.Commit()
	if err != nil {
		c.log.Error("unable to commit transaction", zap.Error(err))
		return modules.AddContext(err, "unable to commit transaction")
	}

	return nil
}

// dbDeleteObject deletes a single file metadata object from
// the database.
func dbDeleteObject(tx *sql.Tx, pk types.PublicKey, bucket, path []byte) error {
	objectID := make([]byte, 32)
	err := tx.QueryRow(`
		SELECT id
		FROM ctr_metadata
		WHERE bucket = ? AND filepath = ? AND renter_pk = ?
	`, bucket, path, pk[:]).Scan(&objectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	rows, err := tx.Query(`
		SELECT enc_key
		FROM ctr_slabs
		WHERE object_id = ?
		AND orphan = FALSE
		AND enc_key IN (
			SELECT enc_key
			FROM ctr_slabs
			GROUP BY enc_key
			HAVING COUNT(DISTINCT(object_id)) = 1
		)
	`, objectID)
	if err != nil {
		return err
	}
	var slabs []types.Hash256
	for rows.Next() {
		var slab types.Hash256
		id := make([]byte, 32)
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		copy(slab[:], id)
		slabs = append(slabs, slab)
	}
	rows.Close()

	for _, slab := range slabs {
		_, err = tx.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", slab[:])
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec("DELETE FROM ctr_slabs WHERE object_id = ?", objectID)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM ctr_metadata WHERE id = ?", objectID)
	return err
}

// updateMetadata updates the file metadata in the database.
func (c *Contractor) updateMetadata(pk types.PublicKey, fm modules.FileMetadata, imported bool) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	if err := dbDeleteObject(tx, pk, fm.Bucket, fm.Path); err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to delete object")
	}

	var retrieved int64
	modified := time.Now().Unix()
	if imported {
		retrieved = time.Now().Unix()
	}
	objectID := make([]byte, 32)
	frand.Read(objectID)
	_, err = tx.Exec(`
		INSERT INTO ctr_metadata (
			id,
			enc_key,
			bucket,
			filepath,
			etag,
			mime,
			renter_pk,
			uploaded,
			modified,
			retrieved,
			encrypted
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		objectID,
		fm.Key[:],
		fm.Bucket,
		fm.Path,
		fm.ETag,
		fm.MimeType,
		pk[:],
		modified,
		modified,
		retrieved,
		fm.Encrypted,
	)
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to store object")
	}

	// Delete orphaned slabs and shards in case of a multipart uplod.
	// An empty object key is likely the sign of a multipart upload.
	// TODO: verify that.
	if fm.Key == (types.Hash256{}) {
		for _, slab := range fm.Slabs {
			_, err = tx.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", slab.Key[:])
			if err != nil {
				tx.Rollback()
				return modules.AddContext(err, "unable to delete orphaned shards")
			}
			_, err = tx.Exec("DELETE FROM ctr_slabs WHERE enc_key = ? AND orphan = TRUE", slab.Key[:])
			if err != nil {
				tx.Rollback()
				return modules.AddContext(err, "unable to delete orphaned slab")
			}
		}
	}

	// Create a map of inserted slabs. This way, the shards will only be
	// inserted once for each slab.
	insertedSlabs := make(map[types.Hash256]struct{})
	for i, slab := range fm.Slabs {
		var count int
		err = tx.QueryRow(`
			SELECT COUNT(*)
			FROM ctr_slabs
			WHERE enc_key = ?
			AND orphan = TRUE
		`, slab.Key[:]).Scan(&count)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "unable to get packed slab length")
		}
		if fm.Key != (types.Hash256{}) && count > 0 {
			_, err = tx.Exec(`
				UPDATE ctr_slabs
				SET
					object_id = ?,
					offset = ?,
					len = ?,
					num = ?,
					orphan = FALSE
				WHERE enc_key = ?
				AND orphan = TRUE
			`, objectID, slab.Offset, slab.Length, i, slab.Key[:])
			if err != nil {
				tx.Rollback()
				return modules.AddContext(err, "unable to update orphaned slab")
			}
			if fm.Key != (types.Hash256{}) {
				fm.Data = fm.Data[slab.Length:]
			}
		} else {
			var data []byte
			if slab.Partial {
				data = fm.Data[:slab.Length]
				fm.Data = fm.Data[slab.Length:]
			}
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
					orphan,
					modified,
					retrieved,
					data
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`,
				slab.Key[:],
				objectID,
				pk[:],
				slab.MinShards,
				slab.Offset,
				slab.Length,
				i,
				slab.Partial,
				false,
				modified,
				modified,
				data,
			)
			if err != nil {
				tx.Rollback()
				return modules.AddContext(err, "unable to insert slab")
			}
			if slab.Partial {
				continue
			}
			if _, exists := insertedSlabs[slab.Key]; !exists {
				for _, ss := range slab.Shards {
					_, err = tx.Exec(`
						INSERT INTO ctr_shards (slab_id, host, merkle_root)
						VALUES (?, ?, ?)
					`, slab.Key[:], ss.Host[:], ss.Root[:])
					if err != nil {
						tx.Rollback()
						return modules.AddContext(err, "unable to insert shard")
					}
				}
			}
			insertedSlabs[slab.Key] = struct{}{}
		}
	}

	return tx.Commit()
}

// DeleteObject deletes the saved file metadata object.
func (c *Contractor) DeleteObject(pk types.PublicKey, bucket, path []byte) error {
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
			if po[string(bucket.Name)] == nil {
				po[string(bucket.Name)] = make(map[string]struct{})
			}
			po[string(bucket.Name)][string(path)] = struct{}{}
		}
	}

	rows, err := c.db.Query(`
		SELECT
			id,
			enc_key,
			bucket,
			filepath,
			etag,
			mime,
			modified,
			retrieved,
			encrypted
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
		ec := make([]byte, 32)
		var bucket, path, mimeType []byte
		var eTag, encrypted string
		var modified, retrieved uint64
		if err := rows.Scan(&objectID, &ec, &bucket, &path, &eTag, &mimeType, &modified, &retrieved, &encrypted); err != nil {
			return nil, modules.AddContext(err, "unable to retrieve object")
		}

		// If the object is present in the map and hasn't beed modified, skip it.
		if files, exists := po[string(bucket)]; exists {
			if _, exists := files[string(path)]; exists {
				if retrieved >= modified {
					continue
				}
			}
		}

		slabRows, err := c.db.Query(`
			SELECT enc_key, min_shards, offset, len, num, partial, data
			FROM ctr_slabs
			WHERE object_id = ?
			AND orphan = FALSE
			ORDER BY num ASC
		`, objectID)
		if err != nil {
			return nil, modules.AddContext(err, "unable to query slabs")
		}

		var md modules.FileMetadata
		for slabRows.Next() {
			var shards []modules.Shard
			slabID := make([]byte, 32)
			var minShards uint8
			var offset, length uint64
			var i int
			var partial bool
			var data []byte
			if err := slabRows.Scan(&slabID, &minShards, &offset, &length, &i, &partial, &data); err != nil {
				slabRows.Close()
				return nil, modules.AddContext(err, "unable to retrieve slab")
			}
			if !partial {
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
			} else {
				md.Data = append(md.Data, data...)
			}
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
		copy(md.Key[:], ec)
		md.Bucket = bucket
		md.Path = path
		md.ETag = eTag
		md.MimeType = mimeType
		md.Encrypted = encrypted
		md.Slabs = slabs
		fm = append(fm, md)

		// Update timestamp.
		_, err = c.db.Exec(`
			UPDATE ctr_metadata
			SET retrieved = ?
			WHERE id = ?
		`, time.Now().Unix(), objectID)
		if err != nil {
			return nil, modules.AddContext(err, "couldn't update timestamp")
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
				orphan,
				modified,
				retrieved
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			slab.Key[:],
			[]byte{},
			rpk[:],
			slab.MinShards,
			slab.Offset,
			slab.Length,
			0,
			false,
			true,
			time.Now().Unix(),
			time.Now().Unix(),
		)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't save packed slab")
		}

		// Convert any partial slabs into complete.
		_, err = tx.Exec(`
			UPDATE ctr_slabs
			SET min_shards = ?,
				partial = FALSE,
				data = ?
			WHERE enc_key = ?
			AND partial = TRUE
		`, slab.MinShards, []byte{}, slab.Key[:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't update partial slabs")
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

	return tx.Commit()
}

// encryptionKey is a helper function that converts a types.Hash256
// to an object.EncryptionKey.
func encryptionKey(id types.Hash256) (key object.EncryptionKey, err error) {
	err = key.UnmarshalBinary(id[:])
	return
}

// convertEncryptionKey converts an object.EncryptionKey to a
// types.Hash256.
func convertEncryptionKey(key object.EncryptionKey) (id types.Hash256, err error) {
	b, err := key.MarshalBinary()
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
		copy(shard.LatestHost[:], h)
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
	rows, err := c.db.Query(`
		SELECT enc_key, renter_pk, min_shards, offset, len, num, partial
		FROM ctr_slabs
		WHERE orphan = FALSE
		ORDER BY num ASC
	`)
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
func (c *Contractor) getObject(pk types.PublicKey, bucket, path []byte) (object.Object, types.Hash256, error) {
	// Start a transaction.
	tx, err := c.db.Begin()
	if err != nil {
		return object.Object{}, types.Hash256{}, err
	}

	// Find the object ID.
	id := make([]byte, 32)
	ec := make([]byte, 32)
	var encrypted string
	err = tx.QueryRow(`
		SELECT id, enc_key, encrypted
		FROM ctr_metadata
		WHERE bucket = ? AND filepath = ? AND renter_pk = ?
	`, bucket, path, pk[:]).Scan(&id, &ec, &encrypted)
	if err != nil {
		tx.Rollback()
		return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't find object")
	}
	var objectID, key types.Hash256
	copy(objectID[:], id)
	copy(key[:], ec)
	objectKey, err := encryptionKey(key)
	if err != nil {
		tx.Rollback()
		return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't unmarshal object key")
	}

	// Load slabs.
	rows, err := tx.Query(`
		SELECT enc_key, min_shards, offset, len, num, partial
		FROM ctr_slabs
		WHERE object_id = ?
		ORDER BY num ASC
	`, objectID[:])
	if err != nil {
		tx.Rollback()
		return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't load slabs")
	}

	var slabs []object.SlabSlice
	for rows.Next() {
		slabID := make([]byte, 32)
		var minShards uint8
		var offset, length uint64
		var n int
		var p bool
		if err = rows.Scan(&slabID, &minShards, &offset, &length, &n, &p); err != nil {
			rows.Close()
			tx.Rollback()
			return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't load slab")
		}
		copy(key[:], slabID)
		var slabKey object.EncryptionKey
		slabKey, err = encryptionKey(key)
		if err != nil {
			rows.Close()
			tx.Rollback()
			return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't unmarshal slab key")
		}
		slabs = append(slabs, object.SlabSlice{
			Slab: object.Slab{
				Key:       slabKey,
				MinShards: minShards,
			},
			Offset: uint32(offset),
			Length: uint32(length),
		})
	}
	rows.Close()

	// Load shards.
	for index := range slabs {
		var id types.Hash256
		id, err = convertEncryptionKey(slabs[index].Key)
		if err != nil {
			tx.Rollback()
			return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't marshal encryption key")
		}
		rows, err = tx.Query(`
			SELECT host, merkle_root
			FROM ctr_shards
			WHERE slab_id = ?
		`, id[:])
		if err != nil {
			tx.Rollback()
			return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't load shards")
		}

		for rows.Next() {
			host := make([]byte, 32)
			root := make([]byte, 32)
			if err = rows.Scan(&host, &root); err != nil {
				rows.Close()
				tx.Rollback()
				return object.Object{}, types.Hash256{}, modules.AddContext(err, "couldn't load shard")
			}
			var shard object.Sector
			copy(shard.LatestHost[:], host)
			copy(shard.Root[:], root)
			slabs[index].Shards = append(slabs[index].Shards, shard)
		}
		rows.Close()
	}

	// Construct the object.
	obj := object.Object{
		Key:   objectKey,
		Slabs: slabs,
	}

	tx.Commit()
	return obj, objectID, nil
}

// getPartialSlab returns the chunk of data corresponding to the
// params supplied.
func (c *Contractor) getPartialSlab(objectID types.Hash256, key object.EncryptionKey, offset, length uint64) ([]byte, error) {
	id, err := convertEncryptionKey(key)
	if err != nil {
		return nil, modules.AddContext(err, "couldn't marshal encryption key")
	}
	var data []byte
	err = c.db.QueryRow(`
		SELECT data
		FROM ctr_slabs
		WHERE enc_key = ?
		AND object_id = ?
		AND offset = ?
		AND len = ?
		AND partial = TRUE
	`,
		id[:],
		objectID[:],
		offset,
		length,
	).Scan(&data)
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
	rows, err := tx.Query(`
		SELECT enc_key, min_shards
		FROM ctr_slabs
		WHERE renter_pk = ?
		AND modified > retrieved
		AND partial = FALSE
		AND orphan = FALSE
	`, rpk[:])
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
			AND orphan = FALSE
		`, time.Now().Unix(), slab.Key[:])
		if err != nil {
			return nil, modules.AddContext(err, "couldn't update timestamp")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, modules.AddContext(err, "couldn't commit transaction")
	}

	return slabs, nil
}

// uploadPackedSlabs checks if there is any partial slab data
// that is large enough to form a slab and uploads it.
func (c *Contractor) uploadPackedSlabs(rpk types.PublicKey) error {
	// Fetch the renter.
	c.mu.Lock()
	renter, exists := c.renters[rpk]
	c.mu.Unlock()
	if !exists {
		return ErrRenterNotFound
	}

	// Skip if the renter hasn't opted in.
	if !renter.Settings.ProxyUploads {
		return nil
	}

	// Start a transaction.
	tx, err := c.db.Begin()
	if err != nil {
		return modules.AddContext(err, "couldn't start transaction")
	}

	// Create a list of the partial slabs.
	rows, err := tx.Query(`
		SELECT enc_key, object_id, len, data
		FROM ctr_slabs
		WHERE renter_pk = ?
		AND partial = TRUE
	`, renter.PublicKey[:])
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "couldn't query partial slab data")
	}

	type partialSlab struct {
		key      []byte
		objectID []byte
		offset   uint64
		length   uint64
	}
	var slabs []partialSlab
	slabSize := rhpv2.SectorSize * renter.Allowance.MinShards
	var slabData []byte
	var incomplete int
	var offset uint64
	for rows.Next() {
		key := make([]byte, 32)
		id := make([]byte, 32)
		var length uint64
		var data []byte
		if err := rows.Scan(&key, &id, &length, &data); err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't scan partial slab data")
		}
		slabs = append(slabs, partialSlab{key, id, offset, length})
		offset += length
		slabData = append(slabData, data...)
		if uint64(len(slabData)) >= slabSize {
			if uint64(len(slabData)) > slabSize {
				diff := uint64(len(slabData)) - slabSize
				slabs[len(slabs)-1].length -= diff
				slabs = append(slabs, partialSlab{
					key:      slabs[len(slabs)-1].key,
					objectID: slabs[len(slabs)-1].objectID,
					offset:   0,
					length:   diff,
				})
				incomplete = 1
			}
			break
		}
	}
	rows.Close()

	// Return early if there are no slabs to upload.
	if uint64(len(slabData)) < slabSize {
		tx.Commit()
		return nil
	}

	// Upload the slab.
	slabKey := object.GenerateEncryptionKey()
	slab, err := c.managedUploadPackedSlab(renter.PublicKey, slabData[:slabSize], slabKey, 0)
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "unable to upload slab")
	}

	// Save the shards in the database.
	slabID, err := convertEncryptionKey(slabKey)
	if err != nil {
		tx.Rollback()
		return modules.AddContext(err, "couldn't marshal encryption key")
	}
	for _, shard := range slab.Shards {
		_, err := tx.Exec(`
			INSERT INTO ctr_shards (slab_id, host, merkle_root)
			VALUES (?, ?, ?)
		`, slabID[:], shard.Host[:], shard.Root[:])
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't insert shard")
		}
	}

	// Delete partial slabs.
	for i := 0; i < len(slabs)-incomplete; i++ {
		_, err := tx.Exec(`
			DELETE FROM ctr_slabs
			WHERE enc_key = ?
			AND object_id = ?
			AND partial = TRUE
		`, slabs[i].key, slabs[i].objectID)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't delete partial slabs")
		}
	}

	// Add new (complete) slabs.
	for i := 0; i < len(slabs)-incomplete; i++ {
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
				orphan,
				modified,
				retrieved
			)
			VALUES (?, ?, ?, ?, ?, ?, (
				SELECT MAX(num) FROM (
					SELECT object_id, num
					FROM ctr_slabs
				) AS num_slabs
				WHERE num_slabs.object_id = ?
			) + 1, ?, ?, ?, ?)
		`,
			slabID[:],
			slabs[i].objectID,
			rpk[:],
			renter.Allowance.MinShards,
			slabs[i].offset,
			slabs[i].length,
			slabs[i].objectID,
			false,
			false,
			time.Now().Unix(),
			time.Now().Unix(),
		)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't insert slab")
		}
	}

	// If there was an incomplete slab left, add it.
	if incomplete > 0 {
		key := make([]byte, 32)
		frand.Read(key)
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
				orphan,
				modified,
				retrieved,
				data
			)
			VALUES (?, ?, ?, ?, ?, ?, (
				SELECT MAX(num) FROM (
					SELECT object_id, num
					FROM ctr_slabs
				) AS num_slabs
				WHERE num_slabs.object_id = ?
			) + 1, ?, ?, ?, ?, ?)
		`,
			key,
			slabs[len(slabs)-1].objectID[:],
			rpk[:],
			0,
			0,
			uint64(len(slabData))-slabSize,
			slabs[len(slabs)-1].objectID[:],
			true,
			false,
			time.Now().Unix(),
			time.Now().Unix(),
			slabData[slabSize:],
		)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't save partial slab")
		}
	}

	// Update the timestamps.
	for _, slab := range slabs {
		_, err := tx.Exec(`
			UPDATE ctr_metadata
			SET modified = ?
			WHERE id = ?
		`, time.Now().Unix(), slab.objectID)
		if err != nil {
			tx.Rollback()
			return modules.AddContext(err, "couldn't update timestamp")
		}
	}

	err = tx.Commit()
	if err != nil {
		return modules.AddContext(err, "couldn't commit transaction")
	}

	return nil
}

// managedUploadBufferedFiles uploads temporary files to the network.
func (c *Contractor) managedUploadBufferedFiles() {
	// Skip if a satellite maintenance is running.
	if c.m.Maintenance() {
		c.log.Info("skipping file uploads because satellite maintenance is running")
		return
	}

	// No file uploads unless contractor is synced.
	if !c.managedSynced() {
		c.log.Info("skipping file uploads since consensus isn't synced yet")
		return
	}

	c.mu.Lock()
	if c.uploadingBufferedFiles {
		c.mu.Unlock()
		c.log.Info("skipping file uploads since another thread is running already")
		return
	}
	c.uploadingBufferedFiles = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.uploadingBufferedFiles = false
		c.mu.Unlock()
		pending, err := c.uploadPending()
		if err != nil {
			c.log.Error("couldn't check files pending upload", zap.Error(err))
			return
		}
		if pending {
			go c.managedUploadBufferedFiles()
		}
	}()

	c.log.Info("uploading buffered files")

	// Sort the files by the upload timestamp, the older come first.
	rows, err := c.db.Query(`
		SELECT filename, bucket, filepath, mime, renter_pk, encrypted
		FROM ctr_uploads
		WHERE ready = TRUE
		ORDER BY filename ASC
	`)
	if err != nil {
		c.log.Error("couldn't query buffered files", zap.Error(err))
		return
	}
	defer rows.Close()

	for rows.Next() {
		select {
		case <-c.tg.StopChan():
			return
		default:
		}
		var n, encrypted string
		pk := make([]byte, 32)
		var bucket, path, mimeType []byte
		if err := rows.Scan(&n, &bucket, &path, &mimeType, &pk, &encrypted); err != nil {
			rows.Close()
			c.log.Error("couldn't scan file record", zap.Error(err))
			return
		}
		var rpk types.PublicKey
		copy(rpk[:], pk)

		// Read the file.
		err = func() error {
			name := filepath.Join(c.m.BufferedFilesDir(), n)
			file, err := os.Open(name)
			if err != nil {
				c.log.Error("couldn't open file", zap.Error(err))
				return err
			}
			defer func() {
				file.Close()
				if err == nil {
					err = os.Remove(name)
					if err != nil {
						c.log.Error("couldn't delete file", zap.Error(err))
						return
					}
					_, err = c.db.Exec(`
						DELETE FROM ctr_uploads
						WHERE renter_pk = ?
						AND filename = ?
						AND bucket = ?
						AND filepath = ?
					`, pk, n, bucket, path)
					if err != nil {
						c.log.Error("couldn't delete file record", zap.Error(err))
						return
					}
				}
			}()

			// Upload the data.
			fm, err := c.managedUploadObject(file, rpk, bucket, path, mimeType, encrypted)
			if err != nil {
				c.log.Error("couldn't upload object", zap.Error(err))
				return err
			}

			// Store the object in the database.
			if err := c.updateMetadata(rpk, fm, false); err != nil {
				c.log.Error("couldn't save object", zap.Error(err))
				return err
			}

			// Upload any complete slabs.
			if err := c.uploadPackedSlabs(rpk); err != nil {
				c.log.Error("couldn't upload packed slabs", zap.Error(err))
				return err
			}

			return nil
		}()
		if err != nil {
			return
		}
	}
}

// threadedUploadBufferedFiles performs the periodic upload of the
// buffered files.
func (c *Contractor) threadedUploadBufferedFiles() {
	if err := c.tg.Add(); err != nil {
		return
	}
	defer c.tg.Done()

	for {
		select {
		case <-c.tg.StopChan():
			return
		case <-time.After(bufferedFilesUploadInterval):
		}
		c.managedUploadBufferedFiles()
	}
}

// managedPruneOrphanedSlabs deletes any orphaned slabs that are older
// than the threshold together with their shards.
func (c *Contractor) managedPruneOrphanedSlabs() {
	tx, err := c.db.Begin()
	if err != nil {
		c.log.Error("unable to start transaction", zap.Error(err))
		return
	}

	// Make a list of orphaned slabs that don't have non-orphaned twins.
	// The shards of these is safe to delete.
	rows, err := tx.Query(`
		SELECT enc_key
		FROM ctr_slabs
		WHERE orphan = TRUE
		AND modified < ?
		AND enc_key IN (
			SELECT enc_key
			FROM ctr_slabs
			GROUP BY enc_key
			HAVING COUNT(*) = 1
		)
	`, uint64(time.Now().Add(-orphanedSlabPruneThreshold).Unix()))
	if err != nil {
		c.log.Error("unable to query slabs", zap.Error(err))
		tx.Rollback()
		return
	}

	var ids []types.Hash256
	for rows.Next() {
		key := make([]byte, 32)
		if err := rows.Scan(&key); err != nil {
			c.log.Error("unable to get slab ID", zap.Error(err))
			rows.Close()
			tx.Rollback()
			return
		}
		var id types.Hash256
		copy(id[:], key)
		ids = append(ids, id)
	}
	rows.Close()

	// Delete the shards.
	for _, id := range ids {
		_, err := tx.Exec("DELETE FROM ctr_shards WHERE slab_id = ?", id[:])
		if err != nil {
			c.log.Error("unable to delete shards", zap.Error(err))
			tx.Rollback()
			return
		}
	}

	// Delete the slabs.
	res, err := tx.Exec("DELETE FROM ctr_slabs WHERE orphan = TRUE")
	if err != nil {
		c.log.Error("unable to delete orphaned slabs", zap.Error(err))
		tx.Rollback()
		return
	}
	num, _ := res.RowsAffected()
	if num > 0 {
		c.log.Info(fmt.Sprintf("deleted %d orphaned slabs", num))
	}

	// Delete orphaned shards.
	res, err = tx.Exec(`
		DELETE FROM ctr_shards
		WHERE NOT slab_id IN (
			SELECT enc_key
			FROM ctr_slabs
		)
	`)
	if err != nil {
		c.log.Error("unable to delete orphaned shards", zap.Error(err))
		tx.Rollback()
		return
	}
	num, _ = res.RowsAffected()
	if num > 0 {
		c.log.Info(fmt.Sprintf("deleted %d orphaned shards", num))
	}

	if err := tx.Commit(); err != nil {
		c.log.Error("unable to commit transaction", zap.Error(err))
	}
}

// threadedPruneOrphanedSlabs prunes the orphaned slabs with the
// certain interval.
func (c *Contractor) threadedPruneOrphanedSlabs() {
	if err := c.tg.Add(); err != nil {
		return
	}
	defer c.tg.Done()

	for {
		select {
		case <-c.tg.StopChan():
			return
		case <-time.After(orphanedSlabPruneInterval):
		}
		c.managedPruneOrphanedSlabs()
	}
}

// uploadPending returns true if there is any file pending upload.
func (c *Contractor) uploadPending() (pending bool, err error) {
	var count int
	err = c.db.QueryRow("SELECT COUNT(*) FROM ctr_uploads").Scan(&count)
	pending = count > 0
	return
}
