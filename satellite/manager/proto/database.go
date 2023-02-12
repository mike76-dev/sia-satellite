package proto

import (
	"database/sql"
	"encoding/hex"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

type (
	// contractPersist holds the contract data in the database.
	contractPersist struct {
		StartHeight          uint64
		SecretKey            string
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
		NewRevisionNumber  uint64
		NewFileSize        uint64
		NewFileMerkleRoot  string
		NewWindowStart     uint64
		NewWindowEnd       uint64
		ValidValue0        string
		ValidUnlockHash0   string
		ValidValue1        string
		ValidUnlockHash1   string
		MissedValue0       string
		MissedUnlockHash0  string
		MissedValue1       string
		MissedUnlockHash1  string
		MissedValue2       string
		MissedUnlockHash2  string
		NewUnlockHash      string
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

// saveContract saves the FileContract in the database.
func (fc *FileContract) saveContract() error {
	// Prepare the necessary variables.
	fc.mu.Lock()
	h := fc.header
	fc.mu.Unlock()
	rev := h.LastRevision()
	hid := h.ID()
	id := hex.EncodeToString(hid[:])
	var ts0, ts1 types.TransactionSignature
	if len(h.Transaction.TransactionSignatures) > 0 {
		copy(ts0.ParentID[:], h.Transaction.TransactionSignatures[0].ParentID[:])
		ts0.PublicKeyIndex = h.Transaction.TransactionSignatures[0].PublicKeyIndex
		ts0.Timelock = h.Transaction.TransactionSignatures[0].Timelock
		copy(ts0.Signature[:], h.Transaction.TransactionSignatures[0].Signature[:])
	}
	if len(h.Transaction.TransactionSignatures) > 1 {
		copy(ts1.ParentID[:], h.Transaction.TransactionSignatures[1].ParentID[:])
		ts1.PublicKeyIndex = h.Transaction.TransactionSignatures[1].PublicKeyIndex
		ts1.Timelock = h.Transaction.TransactionSignatures[1].Timelock
		copy(ts1.Signature[:], h.Transaction.TransactionSignatures[1].Signature[:])
	}

	// Check if the contract is already in the database.
	var count int
	err := fc.db.QueryRow("SELECT COUNT(*) FROM contracts WHERE contract_id = ?", id).Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		// Update contract.
		_, err := fc.db.Exec(`
			UPDATE contracts
			SET start_height = ?, secret_key = ?, download_spending = ?,
				fund_account_spending = ?, storage_spending = ?, upload_spending = ?,
				total_cost = ?, contract_fee = ?, txn_fee = ?, siafund_fee = ?,
				account_balance_cost = ?, fund_account_cost = ?,
				update_price_table_cost = ?, good_for_upload = ?, good_for_renew = ?,
				bad_contract = ?, last_oos_err = ?, locked = ?
			WHERE contract_id = ?
		`, h.StartHeight, hex.EncodeToString(h.SecretKey[:]), h.DownloadSpending.String(), h.FundAccountSpending.String(), h.StorageSpending.String(), h.UploadSpending.String(), h.TotalCost.String(), h.ContractFee.String(), h.TxnFee.String(), h.SiafundFee.String(), h.MaintenanceSpending.AccountBalanceCost.String(), h.MaintenanceSpending.FundAccountCost.String(), h.MaintenanceSpending.UpdatePriceTableCost.String(), h.Utility.GoodForUpload, h.Utility.GoodForRenew, h.Utility.BadContract, h.Utility.LastOOSErr, h.Utility.Locked, id)
		if err != nil {
			return err
		}

		// Update transaction.
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
		`, hex.EncodeToString(rev.ParentID[:]), rev.UnlockConditions.Timelock, rev.UnlockConditions.PublicKeys[0].String(), rev.UnlockConditions.PublicKeys[1].String(), rev.UnlockConditions.SignaturesRequired, rev.NewRevisionNumber, rev.NewFileSize, hex.EncodeToString(rev.NewFileMerkleRoot[:]), rev.NewWindowStart, rev.NewWindowEnd, rev.NewValidProofOutputs[0].Value.String(), hex.EncodeToString(rev.NewValidProofOutputs[0].UnlockHash[:]), rev.NewValidProofOutputs[1].Value.String(), hex.EncodeToString(rev.NewValidProofOutputs[1].UnlockHash[:]), rev.NewMissedProofOutputs[0].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[0].UnlockHash[:]), rev.NewMissedProofOutputs[1].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[1].UnlockHash[:]), rev.NewMissedProofOutputs[2].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[2].UnlockHash[:]), hex.EncodeToString(rev.NewUnlockHash[:]), hex.EncodeToString(ts0.ParentID[:]), ts0.PublicKeyIndex, ts0.Timelock, hex.EncodeToString(ts0.Signature), hex.EncodeToString(ts1.ParentID[:]), ts1.PublicKeyIndex, ts1.Timelock, hex.EncodeToString(ts1.Signature), id)

		return err
	}

	// Insert new contract.
	_, err = fc.db.Exec(`
		INSERT INTO contracts
			(contract_id, start_height, secret_key, download_spending,
			fund_account_spending, storage_spending, upload_spending, total_cost,
			contract_fee, txn_fee, siafund_fee, account_balance_cost,
			fund_account_cost, update_price_table_cost, good_for_upload,
			good_for_renew, bad_contract, last_oos_err, locked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, h.StartHeight, hex.EncodeToString(h.SecretKey[:]), h.DownloadSpending.String(), h.FundAccountSpending.String(), h.StorageSpending.String(), h.UploadSpending.String(), h.TotalCost.String(), h.ContractFee.String(), h.TxnFee.String(), h.SiafundFee.String(), h.MaintenanceSpending.AccountBalanceCost.String(), h.MaintenanceSpending.FundAccountCost.String(), h.MaintenanceSpending.UpdatePriceTableCost.String(), h.Utility.GoodForUpload, h.Utility.GoodForRenew, h.Utility.BadContract, h.Utility.LastOOSErr, h.Utility.Locked)
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
	`, id, hex.EncodeToString(rev.ParentID[:]), rev.UnlockConditions.Timelock, rev.UnlockConditions.PublicKeys[0].String(), rev.UnlockConditions.PublicKeys[1].String(), rev.UnlockConditions.SignaturesRequired, rev.NewRevisionNumber, rev.NewFileSize, hex.EncodeToString(rev.NewFileMerkleRoot[:]), rev.NewWindowStart, rev.NewWindowEnd, rev.NewValidProofOutputs[0].Value.String(), hex.EncodeToString(rev.NewValidProofOutputs[0].UnlockHash[:]), rev.NewValidProofOutputs[1].Value.String(), hex.EncodeToString(rev.NewValidProofOutputs[1].UnlockHash[:]), rev.NewMissedProofOutputs[0].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[0].UnlockHash[:]), rev.NewMissedProofOutputs[1].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[1].UnlockHash[:]), rev.NewMissedProofOutputs[2].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[2].UnlockHash[:]), hex.EncodeToString(rev.NewUnlockHash[:]), hex.EncodeToString(ts0.ParentID[:]), ts0.PublicKeyIndex, ts0.Timelock, hex.EncodeToString(ts0.Signature), hex.EncodeToString(ts1.ParentID[:]), ts1.PublicKeyIndex, ts1.Timelock, hex.EncodeToString(ts1.Signature))

	return err
}

// loadContract loads the contract from the database.
func loadContract(fcid types.FileContractID, db *sql.DB) (contractHeader, error) {
	var cp contractPersist
	var tp transactionPersist
	id := hex.EncodeToString(fcid[:])

	// Load contract data.
	err := db.QueryRow(`
		SELECT start_height, secret_key, download_spending, fund_account_spending,
			storage_spending, upload_spending, total_cost, contract_fee, txn_fee,
			siafund_fee, account_balance_cost, fund_account_cost,
			update_price_table_cost, good_for_upload, good_for_renew, bad_contract,
			last_oos_err, locked
		FROM contracts
		WHERE contract_id = ?
	`, id).Scan(&cp.StartHeight, &cp.SecretKey, &cp.DownloadSpending, &cp.FundAccountSpending, &cp.StorageSpending, &cp.UploadSpending, &cp.TotalCost, &cp.ContractFee, &cp.TxnFee, &cp.SiafundFee, &cp.AccountBalanceCost, &cp.FundAccountCost, &cp.UpdatePriceTableCost, &cp.GoodForUpload, &cp.GoodForRenew, &cp.BadContract, &cp.LastOOSErr, &cp.Locked)
	if err != nil {
		return contractHeader{}, err
	}

	// Load transaction data.
	err = db.QueryRow(`
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
	`, id).Scan(&tp.ParentID, &tp.Timelock, &tp.PublicKey0, &tp.PublicKey1, &tp.SignaturesRequired, &tp.NewRevisionNumber, &tp.NewFileSize, &tp.NewFileMerkleRoot, &tp.NewWindowStart, &tp.NewWindowEnd, &tp.ValidValue0, &tp.ValidUnlockHash0, &tp.ValidValue1, &tp.ValidUnlockHash1, &tp.MissedValue0, &tp.MissedUnlockHash0, &tp.MissedValue1, &tp.MissedUnlockHash1, &tp.MissedValue2, &tp.MissedUnlockHash2, &tp.NewUnlockHash, &tp.ParentID0, &tp.PublicKeyIndex0, &tp.Timelock0, &tp.Signature0, &tp.ParentID1, &tp.PublicKeyIndex1, &tp.Timelock1, &tp.Signature1)
	if err != nil {
		return contractHeader{}, err
	}

	// Construct the transaction.
	var t types.Transaction
	t.FileContractRevisions = make([]types.FileContractRevision, 1)
	numSignatures := 0
	if tp.Signature0 != "" {
		numSignatures++
	}
	if tp.Signature1 != "" {
		numSignatures++
	}
	t.TransactionSignatures = make([]types.TransactionSignature, numSignatures)
	var b []byte
	b, _ = hex.DecodeString(tp.ParentID)
	copy(t.FileContractRevisions[0].ParentID[:], b)
	t.FileContractRevisions[0].UnlockConditions = types.UnlockConditions{
		Timelock:           types.BlockHeight(tp.Timelock),
		PublicKeys:         make([]types.SiaPublicKey, 2),
		SignaturesRequired: tp.SignaturesRequired,
	}
	_ = t.FileContractRevisions[0].UnlockConditions.PublicKeys[0].LoadString(tp.PublicKey0)
	_ = t.FileContractRevisions[0].UnlockConditions.PublicKeys[1].LoadString(tp.PublicKey1)
	t.FileContractRevisions[0].NewRevisionNumber = tp.NewRevisionNumber
	t.FileContractRevisions[0].NewFileSize = tp.NewFileSize
	b, _ = hex.DecodeString(tp.NewFileMerkleRoot)
	copy(t.FileContractRevisions[0].NewFileMerkleRoot[:], b)
	t.FileContractRevisions[0].NewWindowStart = types.BlockHeight(tp.NewWindowStart)
	t.FileContractRevisions[0].NewWindowEnd = types.BlockHeight(tp.NewWindowEnd)
	t.FileContractRevisions[0].NewValidProofOutputs = make([]types.SiacoinOutput, 2)
	t.FileContractRevisions[0].NewMissedProofOutputs = make([]types.SiacoinOutput, 3)
	t.FileContractRevisions[0].NewValidProofOutputs[0].Value = modules.ReadCurrency(tp.ValidValue0)
	b, _ = hex.DecodeString(tp.ValidUnlockHash0)
	copy(t.FileContractRevisions[0].NewValidProofOutputs[0].UnlockHash[:], b)
	t.FileContractRevisions[0].NewValidProofOutputs[1].Value = modules.ReadCurrency(tp.ValidValue1)
	b, _ = hex.DecodeString(tp.ValidUnlockHash1)
	copy(t.FileContractRevisions[0].NewValidProofOutputs[1].UnlockHash[:], b)
	t.FileContractRevisions[0].NewMissedProofOutputs[0].Value = modules.ReadCurrency(tp.MissedValue0)
	b, _ = hex.DecodeString(tp.MissedUnlockHash0)
	copy(t.FileContractRevisions[0].NewMissedProofOutputs[0].UnlockHash[:], b)
	t.FileContractRevisions[0].NewMissedProofOutputs[1].Value = modules.ReadCurrency(tp.MissedValue1)
	b, _ = hex.DecodeString(tp.MissedUnlockHash1)
	copy(t.FileContractRevisions[0].NewMissedProofOutputs[1].UnlockHash[:], b)
	t.FileContractRevisions[0].NewMissedProofOutputs[2].Value = modules.ReadCurrency(tp.MissedValue2)
	b, _ = hex.DecodeString(tp.MissedUnlockHash2)
	copy(t.FileContractRevisions[0].NewMissedProofOutputs[2].UnlockHash[:], b)
	b, _ = hex.DecodeString(tp.NewUnlockHash)
	copy(t.FileContractRevisions[0].NewUnlockHash[:], b)
	if numSignatures > 0 {
		b, _ = hex.DecodeString(tp.ParentID0)
		copy(t.TransactionSignatures[0].ParentID[:], b)
		t.TransactionSignatures[0].PublicKeyIndex = tp.PublicKeyIndex0
		t.TransactionSignatures[0].Timelock = types.BlockHeight(tp.Timelock0)
		b, _ = hex.DecodeString(tp.Signature0)
		copy(t.TransactionSignatures[0].Signature[:], b)
		t.TransactionSignatures[0].CoveredFields = types.CoveredFields{
			FileContractRevisions: []uint64{0},
		}
	}
	if numSignatures > 1 {
		b, _ = hex.DecodeString(tp.ParentID1)
		copy(t.TransactionSignatures[1].ParentID[:], b)
		t.TransactionSignatures[1].PublicKeyIndex = tp.PublicKeyIndex1
		t.TransactionSignatures[1].Timelock = types.BlockHeight(tp.Timelock1)
		b, _ = hex.DecodeString(tp.Signature1)
		copy(t.TransactionSignatures[1].Signature[:], b)
		t.TransactionSignatures[1].CoveredFields = types.CoveredFields{
			FileContractRevisions: []uint64{0},
		}
	}

	// Construct the contract header.
	var h contractHeader
	h.Transaction = t
	h.StartHeight = types.BlockHeight(cp.StartHeight)
	b, _ = hex.DecodeString(cp.SecretKey)
	copy(h.SecretKey[:], b)
	h.DownloadSpending = modules.ReadCurrency(cp.DownloadSpending)
	h.FundAccountSpending = modules.ReadCurrency(cp.FundAccountSpending)
	h.StorageSpending = modules.ReadCurrency(cp.StorageSpending)
	h.UploadSpending = modules.ReadCurrency(cp.UploadSpending)
	h.TotalCost = modules.ReadCurrency(cp.TotalCost)
	h.ContractFee = modules.ReadCurrency(cp.ContractFee)
	h.TxnFee = modules.ReadCurrency(cp.TxnFee)
	h.SiafundFee = modules.ReadCurrency(cp.SiafundFee)
	h.MaintenanceSpending = smodules.MaintenanceSpending{
		AccountBalanceCost:   modules.ReadCurrency(cp.AccountBalanceCost),
		FundAccountCost:      modules.ReadCurrency(cp.FundAccountCost),
		UpdatePriceTableCost: modules.ReadCurrency(cp.UpdatePriceTableCost),
	}
	h.Utility = smodules.ContractUtility{
		GoodForUpload: cp.GoodForUpload,
		GoodForRenew:  cp.GoodForRenew,
		BadContract:   cp.BadContract,
		LastOOSErr:    types.BlockHeight(cp.LastOOSErr),
		Locked:        cp.Locked,
	}

	return h, nil
}

// deleteContract deletes the contract from the database.
func deleteContract(fcid types.FileContractID, db *sql.DB) error {
	id := hex.EncodeToString(fcid[:])
	_, err0 := db.Exec("DELETE FROM contractkeys WHERE contract_id = ?", id)
	_, err1 := db.Exec("DELETE FROM transactions WHERE contract_id = ?", id)
	_, err2 := db.Exec("DELETE FROM contracts WHERE contract_id = ?", id)
	return errors.Compose(err0, err1, err2)
}

// loadContracts loads the map[pubkey]contractID from the database.
func loadContracts(db *sql.DB) (map[string]types.FileContractID, error) {
	rows, err := db.Query("SELECT renter_pk, host_pk, contract_id FROM contractkeys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make(map[string]types.FileContractID)
	var rpk, hpk, id string
	var b []byte
	var fcid types.FileContractID

	for rows.Next() {
		if err := rows.Scan(&rpk, &hpk, &id); err != nil {
			continue
		}
		b, _ = hex.DecodeString(id)
		copy(fcid[:], b)
		keys[rpk + hpk] = fcid
	}

	return keys, nil
}
