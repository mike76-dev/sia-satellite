package account

import (
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

// ContractPreferences lists the contract preferences of an account.
type ContractPreferences struct {
	Count       uint64 `json:"count"`
	Period      uint64 `json:"period"`
	RenewWindow uint64 `json:"renewWindow"`
	Download    uint64 `json:"download"`
	Upload      uint64 `json:"upload"`
	UploadSettings
}

// SatelliteSettings lists the satellite preferences of an account.
type SatelliteSettings struct {
	ManageContracts bool `json:"manageContracts"`
	BackupMetadata  bool `json:"backupMetadata"`
	AutoRepair      bool `json:"autoRepair"`
}

// GetGougingSettings retrieves the account's gouging settings.
func (am *AccountManager) GetGougingSettings(acc *Account) (GougingSettings, error) {
	var msp, mip, mep, mcp []byte
	if err := am.db.QueryRow(`
		SELECT
			max_storage_price,
			max_ingress_price,
			max_egress_price,
			max_contract_price
		FROM am_settings
		WHERE email = ?
	`, acc.Email).Scan(&msp, &mip, &mep, &mcp); err != nil {
		return GougingSettings{}, utils.AddContext(err, "couldn't query gouging settings")
	}

	var gs GougingSettings
	d := types.NewBufDecoder(msp)
	(*types.V2Currency)(&gs.MaxStoragePrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return GougingSettings{}, utils.AddContext(err, "couldn't decode max storage price")
	}
	d = types.NewBufDecoder(mip)
	(*types.V2Currency)(&gs.MaxIngressPrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return GougingSettings{}, utils.AddContext(err, "couldn't decode max ingress price")
	}
	d = types.NewBufDecoder(mep)
	(*types.V2Currency)(&gs.MaxEgressPrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return GougingSettings{}, utils.AddContext(err, "couldn't decode max egress price")
	}
	d = types.NewBufDecoder(mcp)
	(*types.V2Currency)(&gs.MaxContractPrice).DecodeFrom(d)
	if err := d.Err(); err != nil {
		return GougingSettings{}, utils.AddContext(err, "couldn't decode max contract price")
	}

	return gs, nil
}

// UpdateGougingSettings updates the account's gouging settings.
func (am *AccountManager) UpdateGougingSettings(acc *Account, gs GougingSettings) error {
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
func (am *AccountManager) GetUploadSettings(acc *Account) (UploadSettings, error) {
	var ms, ts int
	if err := am.db.QueryRow(`
		SELECT
			min_shards,
			total_shards
		FROM am_settings
		WHERE email = ?
	`, acc.Email).Scan(&ms, &ts); err != nil {
		return UploadSettings{}, utils.AddContext(err, "couldn't query upload settings")
	}

	return UploadSettings{
		MinShards:   ms,
		TotalShards: ts,
	}, nil
}

// UpdateUploadSettings updates the account's upload settings.
func (am *AccountManager) UpdateUploadSettings(acc *Account, us UploadSettings) error {
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
func (am *AccountManager) GetSatelliteSettings(acc *Account) (SatelliteSettings, error) {
	var mc, bm, ar bool
	if err := am.db.QueryRow(`
		SELECT
			manage_contracts,
			backup_metadata,
			auto_repair
		FROM am_settings
		WHERE email = ?
	`, acc.Email).Scan(&mc, &bm, &ar); err != nil {
		return SatelliteSettings{}, utils.AddContext(err, "couldn't query satellite settings")
	}

	return SatelliteSettings{
		ManageContracts: mc,
		BackupMetadata:  bm,
		AutoRepair:      ar,
	}, nil
}

// UpdateSatelliteSettings updates the account's satellite settings.
func (am *AccountManager) UpdateSatelliteSettings(acc *Account, ss SatelliteSettings) error {
	_, err := am.db.Exec(`
		UPDATE am_settings
		SET
			manage_contracts = ?,
			backup_metadata = ?,
			auto_repair = ?
		WHERE email = ?
	`,
		ss.ManageContracts,
		ss.BackupMetadata,
		ss.AutoRepair,
		acc.Email,
	)
	if err != nil {
		return utils.AddContext(err, "couldn't update satellite settings")
	}

	return nil
}
