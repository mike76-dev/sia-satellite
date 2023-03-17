package contractor

import (
	"encoding/hex"
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/siad/types"
)

// UpdateRenter updates the renter record in the database.
// The record must have already been created.
func (c *Contractor) UpdateRenter(renter modules.Renter) error {
	_, err := c.db.Exec(`
		UPDATE renters
		SET current_period = ?, funds = ?, hosts = ?, period = ?, renew_window = ?,
			expected_storage = ?, expected_upload = ?, expected_download = ?,
			expected_redundancy = ?, max_rpc_price = ?, max_contract_price = ?,
			max_download_bandwidth_price = ?, max_sector_access_price = ?,
			max_storage_price = ?, max_upload_bandwidth_price = ?
		WHERE email = ?
	`, uint64(renter.CurrentPeriod), renter.Allowance.Funds.String(), renter.Allowance.Hosts, uint64(renter.Allowance.Period), uint64(renter.Allowance.RenewWindow), renter.Allowance.ExpectedStorage, renter.Allowance.ExpectedUpload, renter.Allowance.ExpectedDownload, renter.Allowance.ExpectedRedundancy, renter.Allowance.MaxRPCPrice.String(), renter.Allowance.MaxContractPrice.String(), renter.Allowance.MaxDownloadBandwidthPrice.String(), renter.Allowance.MaxSectorAccessPrice.String(), renter.Allowance.MaxStoragePrice.String(), renter.Allowance.MaxUploadBandwidthPrice.String(), renter.Email)
	return err
}

// updateRenewedContract updates renewed_from and renewed_to
// fields in the contracts table.
func (c *Contractor) updateRenewedContract(oldID, newID types.FileContractID) error {
	_, err := c.db.Exec("UPDATE contracts SET renewed_from = ? WHERE contract_id = ?", oldID.String(), newID.String())
	if err != nil {
		return err
	}
	_, err = c.db.Exec("UPDATE contracts SET renewed_to = ? WHERE contract_id = ?", newID.String(), oldID.String())
	return err
}

// updateOldContract updates an expired contract with the new revision.
func (c *Contractor) updateOldContract(rev types.FileContractRevision, sigs []types.TransactionSignature, uploads, downloads, fundAccount types.Currency) error {
	// Retrieve the contract.
	c.mu.Lock()
	defer c.mu.Unlock()
	contract, exists := c.oldContracts[rev.ParentID]
	if !exists {
		return errors.New("contract not found in old contracts")
	}
	contract.Transaction.FileContractRevisions[0] = rev
	contract.Transaction.TransactionSignatures = sigs
	id := hex.EncodeToString(contract.ID[:])

	if !uploads.Equals(types.ZeroCurrency) || !downloads.Equals(types.ZeroCurrency) || !fundAccount.Equals(types.ZeroCurrency) {
		// Update the contract metadata.
		contract.UploadSpending.Add(uploads)
		contract.DownloadSpending.Add(downloads)
		contract.FundAccountSpending.Add(fundAccount)
		_, err := c.db.Exec(`
			UPDATE contracts
			SET download_spending = ?, fund_account_spending = ?, upload_spending = ?
			WHERE contract_id = ?
		`, contract.DownloadSpending.String(), contract.FundAccountSpending.String(), contract.UploadSpending.String(), id)
		if err != nil {
			return err
		}
	}

	c.oldContracts[rev.ParentID] = contract

	// Update the contract transaction.
	var value, hash string
	if len(rev.NewMissedProofOutputs) > 2 {
		value = rev.NewMissedProofOutputs[2].Value.String()
		hash = hex.EncodeToString(rev.NewMissedProofOutputs[2].UnlockHash[:])
	}
	_, err := c.db.Exec(`
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
	`, hex.EncodeToString(rev.ParentID[:]), rev.UnlockConditions.Timelock, rev.UnlockConditions.PublicKeys[0].String(), rev.UnlockConditions.PublicKeys[1].String(), rev.UnlockConditions.SignaturesRequired, rev.NewRevisionNumber, rev.NewFileSize, hex.EncodeToString(rev.NewFileMerkleRoot[:]), rev.NewWindowStart, rev.NewWindowEnd, rev.NewValidProofOutputs[0].Value.String(), hex.EncodeToString(rev.NewValidProofOutputs[0].UnlockHash[:]), rev.NewValidProofOutputs[1].Value.String(), hex.EncodeToString(rev.NewValidProofOutputs[1].UnlockHash[:]), rev.NewMissedProofOutputs[0].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[0].UnlockHash[:]), rev.NewMissedProofOutputs[1].Value.String(), hex.EncodeToString(rev.NewMissedProofOutputs[1].UnlockHash[:]), value, hash, hex.EncodeToString(rev.NewUnlockHash[:]), hex.EncodeToString(sigs[0].ParentID[:]), sigs[0].PublicKeyIndex, sigs[0].Timelock, hex.EncodeToString(sigs[0].Signature), hex.EncodeToString(sigs[1].ParentID[:]), sigs[1].PublicKeyIndex, sigs[1].Timelock, hex.EncodeToString(sigs[1].Signature), id)

	return err
}
