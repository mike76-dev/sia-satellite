package proto

import (
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

type (
	// contractPersist holds the contract data in the database.
	contractPersist struct {
		RenterPublicKey      string
		StartHeight          uint64
		DownloadSpending     string
		FundAccountSpending  string
		StorageSpending      string
		UploadSpending       string
		TotalCost            string
		ContractFee          string
		TxnFee               string
		SiafundFee           string
		AccountBalanceCost   string
		FundAccountCost      string
		UpdatePriceTableCost string
		GoodForUpload        bool
		GoodForRenew         bool
		BadContract          bool
		LastOOSErr           uint64
		Locked               bool
	}

	// transactionPersist holds the transaction data in the database.
	transactionPersist struct {
		ParentID           string
		Timelock           uint64
		PublicKey0         string
		PublicKey1         string
		SignaturesRequired uint64
		RevisionNumber     uint64
		Filesize           uint64
		FileMerkleRoot     string
		WindowStart        uint64
		WindowEnd          uint64
		ValidValue0        string
		ValidAddress0      string
		ValidValue1        string
		ValidAddress1      string
		MissedValue0       string
		MissedAddress0     string
		MissedValue1       string
		MissedAddress1     string
		MissedValue2       string
		MissedAddress2     string
		UnlockHash         string
		ParentID0          string
		PublicKeyIndex0    uint64
		Timelock0          uint64
		Signature0         string
		ParentID1          string
		PublicKeyIndex1    uint64
		Timelock1          uint64
		Signature1         string
	}
)

// saveContract saves the FileContract in the database. A lock must be acquired
// on the contract.
func (fc *FileContract) saveContract(rpk types.PublicKey) error {
	// Prepare the necessary variables.
	h := fc.header
	rev := h.LastRevision()
	hid := h.ID()
	id := hex.EncodeToString(hid[:])
	var ts0, ts1 types.TransactionSignature
	if len(h.Transaction.TransactionSignatures) > 0 {
		copy(ts0.ParentID[:], h.Transaction.TransactionSignatures[0].ParentID[:])
		ts0.PublicKeyIndex = h.Transaction.TransactionSignatures[0].PublicKeyIndex
		ts0.Timelock = h.Transaction.TransactionSignatures[0].Timelock
		ts0.Signature = make([]byte, len(h.Transaction.TransactionSignatures[0].Signature))
		copy(ts0.Signature, h.Transaction.TransactionSignatures[0].Signature)
	}
	if len(h.Transaction.TransactionSignatures) > 1 {
		copy(ts1.ParentID[:], h.Transaction.TransactionSignatures[1].ParentID[:])
		ts1.PublicKeyIndex = h.Transaction.TransactionSignatures[1].PublicKeyIndex
		ts1.Timelock = h.Transaction.TransactionSignatures[1].Timelock
		ts1.Signature = make([]byte, len(h.Transaction.TransactionSignatures[1].Signature))
		copy(ts1.Signature, h.Transaction.TransactionSignatures[1].Signature)
	}

	// Check if the contract is already in the database. In this case, renter
	// public key must be non-empty.
	var renterKey string
	err := fc.db.QueryRow("SELECT renter_pk FROM contracts WHERE contract_id = ?", id).Scan(&renterKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if renterKey != "" {
		// Update contract.
		_, err := fc.db.Exec(`
			UPDATE contracts
			SET renter_pk = ?, start_height = ?, download_spending = ?,
				fund_account_spending = ?, storage_spending = ?, upload_spending = ?,
				total_cost = ?, contract_fee = ?, txn_fee = ?, siafund_fee = ?,
				account_balance_cost = ?, fund_account_cost = ?,
				update_price_table_cost = ?, good_for_upload = ?, good_for_renew = ?,
				bad_contract = ?, last_oos_err = ?, locked = ?
			WHERE contract_id = ?
		`, renterKey, h.StartHeight, h.DownloadSpending.ExactString(), h.FundAccountSpending.ExactString(), h.StorageSpending.ExactString(), h.UploadSpending.ExactString(), h.TotalCost.ExactString(), h.ContractFee.ExactString(), h.TxnFee.ExactString(), h.SiafundFee.ExactString(), h.MaintenanceSpending.AccountBalanceCost.ExactString(), h.MaintenanceSpending.FundAccountCost.ExactString(), h.MaintenanceSpending.UpdatePriceTableCost.ExactString(), h.Utility.GoodForUpload, h.Utility.GoodForRenew, h.Utility.BadContract, h.Utility.LastOOSErr, h.Utility.Locked, id)
		if err != nil {
			return err
		}

		// Update transaction. It may contain a variable number of missed proof
		// outputs, so check that first.
		var value, hash string
		if len(rev.MissedProofOutputs) > 2 {
			value = rev.MissedProofOutputs[2].Value.ExactString()
			hash = hex.EncodeToString(rev.NewMissedProofOutputs[2].Address[:])
		}

		_, err = fc.db.Exec(`
			UPDATE transactions
			SET parent_id = ?, uc_timelock = ?, uc_renter_pk = ?, uc_host_pk = ?,
				signatures_required = ?, new_revision_number = ?, new_file_size = ?,
				new_file_merkle_root = ?, new_window_start = ?, new_window_end = ?,
				new_valid_proof_output_0 = ?, new_valid_proof_output_uh_0 = ?,
				new_valid_proof_output_1 = ?, new_valid_proof_output_uh_1 = ?,
				new_missed_proof_output_0 = ?, new_missed_proof_output_uh_0 = ?,
				new_missed_proof_output_1 = ?, new_missed_proof_output_uh_1 = ?,
				new_missed_proof_output_2 = ?, new_missed_proof_output_uh_2 = ?,
				new_unlock_hash = ?, t_parent_id_0 = ?, pk_index_0 = ?, timelock_0 = ?,
				signature_0 = ?, t_parent_id_1 = ?, pk_index_1 = ?, timelock_1 = ?,
				signature_1 = ?
			WHERE contract_id = ?
		`, hex.EncodeToString(rev.ParentID[:]), rev.UnlockConditions.Timelock, rev.UnlockConditions.PublicKeys[0].Key.String(), rev.UnlockConditions.PublicKeys[1].Key.String(), rev.UnlockConditions.SignaturesRequired, rev.RevisionNumber, rev.Filesize, hex.EncodeToString(rev.FileMerkleRoot[:]), rev.WindowStart, rev.WindowEnd, rev.ValidProofOutputs[0].Value.ExactString(), hex.EncodeToString(rev.ValidProofOutputs[0].Address[:]), rev.ValidProofOutputs[1].Value.ExactString(), hex.EncodeToString(rev.ValidProofOutputs[1].Address[:]), rev.MissedProofOutputs[0].Value.ExactString(), hex.EncodeToString(rev.MissedProofOutputs[0].Address[:]), rev.MissedProofOutputs[1].Value.ExactString(), hex.EncodeToString(rev.MissedProofOutputs[1].Address[:]), value, hash, hex.EncodeToString(rev.UnlockHash[:]), hex.EncodeToString(ts0.ParentID[:]), ts0.PublicKeyIndex, ts0.Timelock, hex.EncodeToString(ts0.Signature), hex.EncodeToString(ts1.ParentID[:]), ts1.PublicKeyIndex, ts1.Timelock, hex.EncodeToString(ts1.Signature), id)

		return err
	}

	// Insert new contract.
	_, err = fc.db.Exec(`
		INSERT INTO contracts
			(contract_id, renter_pk, start_height, download_spending,
			fund_account_spending, storage_spending, upload_spending, total_cost,
			contract_fee, txn_fee, siafund_fee, account_balance_cost,
			fund_account_cost, update_price_table_cost, good_for_upload,
			good_for_renew, bad_contract, last_oos_err, locked, renewed_from, renewed_to)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, rpk.String(), h.StartHeight, h.DownloadSpending.ExactString(), h.FundAccountSpending.ExactString(), h.StorageSpending.ExactString(), h.UploadSpending.ExactString(), h.TotalCost.ExactString(), h.ContractFee.ExactString(), h.TxnFee.ExactString(), h.SiafundFee.ExactString(), h.MaintenanceSpending.AccountBalanceCost.ExactString(), h.MaintenanceSpending.FundAccountCost.ExactString(), h.MaintenanceSpending.UpdatePriceTableCost.ExactString(), h.Utility.GoodForUpload, h.Utility.GoodForRenew, h.Utility.BadContract, h.Utility.LastOOSErr, h.Utility.Locked, "", "")
	if err != nil {
		return err
	}

	// Insert new transaction.
	_, err = fc.db.Exec(`
		INSERT INTO transactions
			(contract_id, parent_id, uc_timelock, uc_renter_pk, uc_host_pk,
			signatures_required, new_revision_number, new_file_size,
			new_file_merkle_root, new_window_start, new_window_end,
			new_valid_proof_output_0, new_valid_proof_output_uh_0,
			new_valid_proof_output_1, new_valid_proof_output_uh_1,
			new_missed_proof_output_0, new_missed_proof_output_uh_0,
			new_missed_proof_output_1, new_missed_proof_output_uh_1,
			new_missed_proof_output_2, new_missed_proof_output_uh_2,
			new_unlock_hash, t_parent_id_0, pk_index_0, timelock_0, signature_0,
			t_parent_id_1, pk_index_1, timelock_1, signature_1)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, hex.EncodeToString(rev.ParentID[:]), rev.UnlockConditions.Timelock, rev.UnlockConditions.PublicKeys[0].Key.String(), rev.UnlockConditions.PublicKeys[1].Key.String(), rev.UnlockConditions.SignaturesRequired, rev.RevisionNumber, rev.Filesize, hex.EncodeToString(rev.FileMerkleRoot[:]), rev.WindowStart, rev.WindowEnd, rev.ValidProofOutputs[0].Value.ExactString(), hex.EncodeToString(rev.ValidProofOutputs[0].Address[:]), rev.ValidProofOutputs[1].Value.ExactString(), hex.EncodeToString(rev.ValidProofOutputs[1].Address[:]), rev.MissedProofOutputs[0].Value.ExactString(), hex.EncodeToString(rev.MissedProofOutputs[0].Address[:]), rev.MissedProofOutputs[1].Value.ExactString(), hex.EncodeToString(rev.MissedProofOutputs[1].Address[:]), rev.MissedProofOutputs[2].Value.ExactString(), hex.EncodeToString(rev.MissedProofOutputs[2].Address[:]), hex.EncodeToString(rev.UnlockHash[:]), hex.EncodeToString(ts0.ParentID[:]), ts0.PublicKeyIndex, ts0.Timelock, hex.EncodeToString(ts0.Signature), hex.EncodeToString(ts1.ParentID[:]), ts1.PublicKeyIndex, ts1.Timelock, hex.EncodeToString(ts1.Signature))

	return err
}

// deleteContract deletes the contract from the database.
func deleteContract(fcid types.FileContractID, db *sql.DB) error {
	id := hex.EncodeToString(fcid[:])
	_, err := db.Exec("DELETE FROM transactions WHERE contract_id = ?", id)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM contracts WHERE contract_id = ?", id)
	return err
}

// loadContracts loads the entire contracts table into the contract set.
func (cs *ContractSet) loadContracts(height uint64) error {
	// Load the contracts.
	rows, err := cs.db.Query(`
		SELECT contract_id, start_height, download_spending,
			fund_account_spending, storage_spending, upload_spending, total_cost,
			contract_fee, txn_fee, siafund_fee, account_balance_cost, fund_account_cost,
			update_price_table_cost, good_for_upload, good_for_renew, bad_contract,
			last_oos_err, locked, renewed_to
		FROM contracts
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Iterate through each contract.
	var cp contractPersist
	var tp transactionPersist
	var id, renewedTo string
	for rows.Next() {
		if err := rows.Scan(&id, &cp.StartHeight, &cp.DownloadSpending, &cp.FundAccountSpending, &cp.StorageSpending, &cp.UploadSpending, &cp.TotalCost, &cp.ContractFee, &cp.TxnFee, &cp.SiafundFee, &cp.AccountBalanceCost, &cp.FundAccountCost, &cp.UpdatePriceTableCost, &cp.GoodForUpload, &cp.GoodForRenew, &cp.BadContract, &cp.LastOOSErr, &cp.Locked, &renewedTo); err != nil {
			cs.log.Println("ERROR: unable to load file contract:", err)
			continue
		}

		// Load the transaction.
		err = cs.db.QueryRow(`
			SELECT parent_id, uc_timelock, uc_renter_pk, uc_host_pk,
				signatures_required, new_revision_number, new_file_size,
				new_file_merkle_root, new_window_start, new_window_end,
				new_valid_proof_output_0, new_valid_proof_output_uh_0,
				new_valid_proof_output_1, new_valid_proof_output_uh_1,
				new_missed_proof_output_0, new_missed_proof_output_uh_0,
				new_missed_proof_output_1, new_missed_proof_output_uh_1,
				new_missed_proof_output_2, new_missed_proof_output_uh_2,
				new_unlock_hash, t_parent_id_0, pk_index_0, timelock_0, signature_0,
				t_parent_id_1, pk_index_1, timelock_1, signature_1
			FROM transactions
			WHERE contract_id = ?
		`, id).Scan(&tp.ParentID, &tp.Timelock, &tp.PublicKey0, &tp.PublicKey1, &tp.SignaturesRequired, &tp.RevisionNumber, &tp.Filesize, &tp.FileMerkleRoot, &tp.WindowStart, &tp.WindowEnd, &tp.ValidValue0, &tp.ValidAddress0, &tp.ValidValue1, &tp.ValidAddress1, &tp.MissedValue0, &tp.MissedAddress0, &tp.MissedValue1, &tp.MissedAddress1, &tp.MissedValue2, &tp.MissedAddress2, &tp.NewUnlockHash, &tp.ParentID0, &tp.PublicKeyIndex0, &tp.Timelock0, &tp.Signature0, &tp.ParentID1, &tp.PublicKeyIndex1, &tp.Timelock1, &tp.Signature1)
		if err != nil {
			cs.log.Println("ERROR: unable to load transaction:", err)
			continue
		}

		// Construct the transaction.
		var t types.Transaction
		t.FileContractRevisions = make([]types.FileContractRevision, 1)
		t.TransactionSignatures = make([]types.TransactionSignature, tp.SignaturesRequired)
		var b []byte
		b, _ = hex.DecodeString(tp.ParentID)
		copy(t.FileContractRevisions[0].ParentID[:], b)
		t.FileContractRevisions[0].UnlockConditions = types.UnlockConditions{
			Timelock:           tp.Timelock,
			PublicKeys:         {
				modules.ReadPublicKey(tp.PublicKey0).UnlockKey(),
				modules.ReadPublicKey(tp.PublicKey1).UnlockKey(),
			},
			SignaturesRequired: tp.SignaturesRequired,
		}
		t.FileContractRevisions[0].RevisionNumber = tp.RevisionNumber
		t.FileContractRevisions[0].Filesize = tp.Filesize
		b, _ = hex.DecodeString(tp.FileMerkleRoot)
		copy(t.FileContractRevisions[0].FileMerkleRoot[:], b)
		t.FileContractRevisions[0].WindowStart = tp.WindowStart
		t.FileContractRevisions[0].WindowEnd = tp.WindowEnd
		t.FileContractRevisions[0].ValidProofOutputs = make([]types.SiacoinOutput, 2)
		if tp.MissedValue2 != "" && tp.MissedAddress2 != "" {
			t.FileContractRevisions[0].MissedProofOutputs = make([]types.SiacoinOutput, 3)
		} else {
			t.FileContractRevisions[0].MissedProofOutputs = make([]types.SiacoinOutput, 2)
		}
		t.FileContractRevisions[0].ValidProofOutputs[0].Value = modules.ReadCurrency(tp.ValidValue0)
		b, _ = hex.DecodeString(tp.ValidAddress0)
		copy(t.FileContractRevisions[0].ValidProofOutputs[0].Address[:], b)
		t.FileContractRevisions[0].ValidProofOutputs[1].Value = modules.ReadCurrency(tp.ValidValue1)
		b, _ = hex.DecodeString(tp.ValidAddress1)
		copy(t.FileContractRevisions[0].ValidProofOutputs[1].Address[:], b)
		t.FileContractRevisions[0].MissedProofOutputs[0].Value = modules.ReadCurrency(tp.MissedValue0)
		b, _ = hex.DecodeString(tp.MissedAddress0)
		copy(t.FileContractRevisions[0].MissedProofOutputs[0].Address[:], b)
		t.FileContractRevisions[0].MissedProofOutputs[1].Value = modules.ReadCurrency(tp.MissedValue1)
		b, _ = hex.DecodeString(tp.MissedAddress1)
		copy(t.FileContractRevisions[0].MissedProofOutputs[1].Address[:], b)
		if tp.MissedValue2 != "" && tp.MissedAddress2 != "" {
			t.FileContractRevisions[0].MissedProofOutputs[2].Value = modules.ReadCurrency(tp.MissedValue2)
			b, _ = hex.DecodeString(tp.MissedAddress2)
			copy(t.FileContractRevisions[0].MissedProofOutputs[2].Address[:], b)
		}
		b, _ = hex.DecodeString(tp.UnlockHash)
		copy(t.FileContractRevisions[0].UnlockHash[:], b)
		if tp.SignaturesRequired > 0 {
			b, _ = hex.DecodeString(tp.ParentID0)
			copy(t.TransactionSignatures[0].ParentID[:], b)
			t.TransactionSignatures[0].PublicKeyIndex = tp.PublicKeyIndex0
			t.TransactionSignatures[0].Timelock = tp.Timelock0
			b, _ = hex.DecodeString(tp.Signature0)
			t.TransactionSignatures[0].Signature = make([]byte, len(b))
			copy(t.TransactionSignatures[0].Signature, b)
			t.TransactionSignatures[0].CoveredFields = types.CoveredFields{
				FileContractRevisions: []uint64{0},
			}
		}
		if tp.SignaturesRequired > 1 {
			b, _ = hex.DecodeString(tp.ParentID1)
			copy(t.TransactionSignatures[1].ParentID[:], b)
			t.TransactionSignatures[1].PublicKeyIndex = tp.PublicKeyIndex1
			t.TransactionSignatures[1].Timelock = tp.Timelock1
			b, _ = hex.DecodeString(tp.Signature1)
			t.TransactionSignatures[1].Signature = make([]byte, len(b))
			copy(t.TransactionSignatures[1].Signature, b)
			t.TransactionSignatures[1].CoveredFields = types.CoveredFields{
				FileContractRevisions: []uint64{0},
			}
		}

		// Construct the contract header.
		var h contractHeader
		h.Transaction = t
		h.StartHeight = cp.StartHeight
		h.DownloadSpending = modules.ReadCurrency(cp.DownloadSpending)
		h.FundAccountSpending = modules.ReadCurrency(cp.FundAccountSpending)
		h.StorageSpending = modules.ReadCurrency(cp.StorageSpending)
		h.UploadSpending = modules.ReadCurrency(cp.UploadSpending)
		h.TotalCost = modules.ReadCurrency(cp.TotalCost)
		h.ContractFee = modules.ReadCurrency(cp.ContractFee)
		h.TxnFee = modules.ReadCurrency(cp.TxnFee)
		h.SiafundFee = modules.ReadCurrency(cp.SiafundFee)
		h.MaintenanceSpending = modules.MaintenanceSpending{
			AccountBalanceCost:   modules.ReadCurrency(cp.AccountBalanceCost),
			FundAccountCost:      modules.ReadCurrency(cp.FundAccountCost),
			UpdatePriceTableCost: modules.ReadCurrency(cp.UpdatePriceTableCost),
		}
		h.Utility = modules.ContractUtility{
			GoodForUpload: cp.GoodForUpload,
			GoodForRenew:  cp.GoodForRenew,
			BadContract:   cp.BadContract,
			LastOOSErr:    cp.LastOOSErr,
			Locked:        cp.Locked,
		}

		// Create the file contract.
		fc := &FileContract{
			header: h,
			db:     cs.db,
		}

		// Check if the contract has expired.
		fcid := h.ID()
		if renewedTo == "" && h.EndHeight() >= height {
			cs.contracts[fcid] = fc
			cs.pubKeys[tp.PublicKey0 + tp.PublicKey1] = fcid
		} else {
			cs.oldContracts[fcid] = fc
		}
	}

	return nil
}

// managedFindIDs returns a list of contract IDs belonging to the given renter.
// NOTE: this function also returns the old contracts.
func (cs *ContractSet) managedFindIDs(rpk types.PublicKey) []types.FileContractID {
	rows, err := cs.db.Query(`
		SELECT contract_id
		FROM contracts
		WHERE renter_pk = ?
	`, rpk.String())
	if err != nil {
		cs.log.Println("ERROR: couldn't query database:", err)
		return nil
	}
	defer rows.Close()

	var ids []types.FileContractID
	var id string
	for rows.Next() {
		if err := rows.Scan(&id); err != nil {
			cs.log.Println("ERROR: unable to get contract ID:", err)
			continue
		}
		var fcid types.FileContractID
		b, _ := hex.DecodeString(id)
		copy(fcid[:], b)
		ids = append(ids, fcid)
	}

	return ids
}
