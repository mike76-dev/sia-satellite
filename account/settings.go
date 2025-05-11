package account

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
)

// ScoreBases list the pre-defined scoring bases.
var ScoreBases = []string{"global", "eu", "us", "ap"}

// GougingSettings lists the gouging settings of an account.
type GougingSettings struct {
	MaxStoragePrice  types.Currency `json:"maxStoragePrice"`
	MaxIngressPrice  types.Currency `json:"maxIngressPrice"`
	MaxEgressPrice   types.Currency `json:"maxEgressPrice"`
	MaxContractPrice types.Currency `json:"maxContractPrice"`
}

// UploadSettings lists the upload settings of an account.
type UploadSettings struct {
	MinShards   int `json:"minShards"`
	TotalShards int `json:"totalShards"`
}

// HostPreferences lists the host preferences of an account.
type HostPreferences struct {
	GougingSettings
	MaxLatency       int64    `json:"maxLatency"`
	MinUploadSpeed   uint64   `json:"minUploadSpeed"`
	MinDownloadSpeed uint64   `json:"minDownloadSpeed"`
	Basis            string   `json:"basis"`
	Countries        []string `json:"countries"`
}

// ContractSettings lists the contract settings of an account.
type ContractSettings struct {
	Count       uint64 `json:"count"`
	Period      uint64 `json:"period"`
	RenewWindow uint64 `json:"renewWindow"`
	Download    uint64 `json:"download"`
	Upload      uint64 `json:"upload"`
}

// ContractPreferences combines lists the contract and upload settings.
type ContractPreferences struct {
	ContractSettings
	UploadSettings
}

// SatelliteSettings lists the satellite preferences of an account.
type SatelliteSettings struct {
	ManageContracts bool `json:"manageContracts"`
	BackupMetadata  bool `json:"backupMetadata"`
	AutoRepair      bool `json:"autoRepair"`

	RenterKey types.PrivateKey `json:"renterKey,omitempty"`
}

// RenterSettings combines all settings of an account.
type RenterSettings struct {
	HostPreferences
	ContractPreferences
	SatelliteSettings
}

// loadSettings loads the settings from the database.
func (am *AccountManager) loadSettings(acc *Account) error {
	var msp, mip, mep, mcp, rk []byte
	var countries string
	if err := am.db.QueryRow(`
		SELECT
			max_storage_price,
			max_ingress_price,
			max_egress_price,
			max_contract_price,
			max_latency,
			min_upload_speed,
			min_download_speed,
			basis,
			countries,
			contract_count,
			contract_period,
			renew_window,
			ingress,
			egress,
			min_shards,
			total_shards,
			manage_contracts,
			backup_metadata,
			auto_repair,
			renter_key
		FROM am_settings
		WHERE email = ?
	`, acc.Email).Scan(
		&msp,
		&mip,
		&mep,
		&mcp,
		&acc.settings.MaxLatency,
		&acc.settings.MinUploadSpeed,
		&acc.settings.MinDownloadSpeed,
		&acc.settings.Basis,
		&countries,
		&acc.settings.Count,
		&acc.settings.Period,
		&acc.settings.RenewWindow,
		&acc.settings.Download,
		&acc.settings.Upload,
		&acc.settings.MinShards,
		&acc.settings.TotalShards,
		&acc.settings.ManageContracts,
		&acc.settings.BackupMetadata,
		&acc.settings.AutoRepair,
		&rk,
	); err != nil && errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return utils.AddContext(err, "couldn't query settings")
	}

	d := types.NewBufDecoder(msp)
	(*types.V2Currency)(&acc.settings.MaxStoragePrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return utils.AddContext(err, "couldn't decode max storage price")
	}
	d = types.NewBufDecoder(mip)
	(*types.V2Currency)(&acc.settings.MaxIngressPrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return utils.AddContext(err, "couldn't decode max ingress price")
	}
	d = types.NewBufDecoder(mep)
	(*types.V2Currency)(&acc.settings.MaxEgressPrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return utils.AddContext(err, "couldn't decode max egress price")
	}
	d = types.NewBufDecoder(mcp)
	(*types.V2Currency)(&acc.settings.MaxContractPrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return utils.AddContext(err, "couldn't decode max contract price")
	}

	acc.settings.Countries = strings.Split(countries, ",")
	if rk != nil {
		acc.settings.RenterKey = rk
	}

	return nil
}

// GetGougingSettings retrieves the account's gouging settings.
func (acc *Account) GetGougingSettings() GougingSettings {
	return acc.settings.GougingSettings
}

// UpdateGougingSettings updates the account's gouging settings.
func (am *AccountManager) UpdateGougingSettings(acc *Account, gs GougingSettings) error {
	acc.settings.GougingSettings = gs
	_, err := am.db.Exec(`
		UPDATE am_settings
		SET
			max_storage_price = ?,
			max_ingress_price = ?,
			max_egress_price = ?,
			max_contract_price = ?
		WHERE email = ?
	`,
		utils.EncodeCurrency(gs.MaxStoragePrice),
		utils.EncodeCurrency(gs.MaxIngressPrice),
		utils.EncodeCurrency(gs.MaxEgressPrice),
		utils.EncodeCurrency(gs.MaxContractPrice),
		acc.Email,
	)
	if err != nil {
		return utils.AddContext(err, "couldn't update gouging settings")
	}

	return nil
}

// GetUploadSettings retrieves the account's upload settings.
func (acc *Account) GetUploadSettings() UploadSettings {
	return acc.settings.UploadSettings
}

// UpdateUploadSettings updates the account's upload settings.
func (am *AccountManager) UpdateUploadSettings(acc *Account, us UploadSettings) error {
	acc.settings.UploadSettings = us
	_, err := am.db.Exec(`
		UPDATE am_settings
		SET
			min_shards = ?,
			total_shards = ?
		WHERE email = ?
	`,
		us.MinShards,
		us.TotalShards,
		acc.Email,
	)
	if err != nil {
		return utils.AddContext(err, "couldn't update upload settings")
	}

	return nil
}

// GetSatelliteSettings retrieves the account's satellite settings.
func (acc *Account) GetSatelliteSettings() SatelliteSettings {
	return acc.settings.SatelliteSettings
}

// UpdateSatelliteSettings updates the account's satellite settings.
func (am *AccountManager) UpdateSatelliteSettings(acc *Account, ss SatelliteSettings) error {
	acc.settings.SatelliteSettings = ss
	_, err := am.db.Exec(`
		UPDATE am_settings
		SET
			manage_contracts = ?,
			backup_metadata = ?,
			auto_repair = ?,
			renter_key = ?
		WHERE email = ?
	`,
		ss.ManageContracts,
		ss.BackupMetadata,
		ss.AutoRepair,
		ss.RenterKey,
		acc.Email,
	)
	if err != nil {
		return utils.AddContext(err, "couldn't update satellite settings")
	}

	return nil
}

// GetContractSettings retrieves the account's contract settings.
func (acc *Account) GetContractSettings() ContractSettings {
	return acc.settings.ContractSettings
}

// UpdateContractSettings updates the account's contract settings.
func (am *AccountManager) UpdateContractSettings(acc *Account, cs ContractSettings) error {
	acc.settings.ContractSettings = cs
	_, err := am.db.Exec(`
		UPDATE am_settings
		SET
			contract_count = ?,
			contract_period = ?,
			renew_window = ?,
			ingress = ?,
			egress = ?
		WHERE email = ?
	`,
		cs.Count,
		cs.Period,
		cs.RenewWindow,
		cs.Download,
		cs.Upload,
		acc.Email,
	)
	if err != nil {
		return utils.AddContext(err, "couldn't update contract settings")
	}

	return nil
}
