package contractor

import (
	"encoding/hex"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
)

// UpdateRenter updates the renter record in the database.
// The record must have already been created.
func (c *Contractor) UpdateRenter(renter modules.Renter) error {
	var sk string
	if (renter.PrivateKey != crypto.SecretKey{}) {
		sk = hex.EncodeToString(renter.PrivateKey[:])
	}
	_, err := c.db.Exec(`
		UPDATE renters
		SET current_period = ?, funds = ?, hosts = ?, period = ?, renew_window = ?,
			expected_storage = ?, expected_upload = ?, expected_download = ?,
			min_shards = ?, total_shards = ?, max_rpc_price = ?, max_contract_price = ?,
			max_download_bandwidth_price = ?, max_sector_access_price = ?,
			max_storage_price = ?, max_upload_bandwidth_price = ?, min_max_collateral = ?,
			blockheight_leeway = ?, private_key = ?, auto_renew_contracts = ?
		WHERE email = ?
	`, uint64(renter.CurrentPeriod), renter.Allowance.Funds.String(), renter.Allowance.Hosts, uint64(renter.Allowance.Period), uint64(renter.Allowance.RenewWindow), renter.Allowance.ExpectedStorage, renter.Allowance.ExpectedUpload, renter.Allowance.ExpectedDownload, renter.Allowance.MinShards, renter.Allowance.TotalShards, renter.Allowance.MaxRPCPrice.String(), renter.Allowance.MaxContractPrice.String(), renter.Allowance.MaxDownloadBandwidthPrice.String(), renter.Allowance.MaxSectorAccessPrice.String(), renter.Allowance.MaxStoragePrice.String(), renter.Allowance.MaxUploadBandwidthPrice.String(), renter.Allowance.MinMaxCollateral.String(), uint64(renter.Allowance.BlockHeightLeeway), sk, renter.Settings.AutoRenewContracts, renter.Email)
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

// managedFindRenter tries to find a renter by the contract ID.
func (c *Contractor) managedFindRenter(fcid types.FileContractID) (modules.Renter, error) {
	var key string
	id := hex.EncodeToString(fcid[:])
	err := c.db.QueryRow(`
		SELECT renter_pk
		FROM contracts
		WHERE contract_id = ?
	`, id).Scan(&key)
	if err != nil {
		return modules.Renter{}, ErrRenterNotFound
	}

	renter, exists := c.renters[key]
	if exists {
		return renter, nil
	}

	return modules.Renter{}, ErrRenterNotFound
}
