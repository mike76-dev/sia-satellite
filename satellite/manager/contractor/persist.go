package contractor

import (
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	"go.sia.tech/siad/types"
)

var (
	persistMeta = persist.Metadata{
		Header:  "Contractor Persistence",
		Version: "0.1.0",
	}

	// PersistFilename is the filename to be used when persisting contractor
	// information to a JSON file
	PersistFilename = "contractor.json"
)

// saveFrequency determines how often the Contractor will be saved.
const saveFrequency = 2 * time.Minute

// contractorPersist defines what Contractor data persists across sessions.
type contractorPersist struct {
	BlockHeight          types.BlockHeight               `json:"blockheight"`
	LastChange           smodules.ConsensusChangeID      `json:"lastchange"`
	DoubleSpentContracts map[string]types.BlockHeight    `json:"doublespentcontracts"`
	Synced               bool                            `json:"synced"`

	// Subsystem persistence:
	WatchdogData watchdogPersist `json:"watchdogdata"`
}

// renterData holds the MySQL entry of modules.Renter.
type renterData struct {
	Email         string
	PublicKey     string
	CurrentPeriod uint64
	Funds         string
	Hosts         uint64
	Period        uint64
	RenewWindow   uint64

	ExpectedStorage    uint64
	ExpectedUpload    uint64
	ExpectedDownload   uint64
	ExpectedRedundancy float64

	MaxRPCPrice               string
	MaxContractPrice          string
	MaxDownloadBandwidthPrice string
	MaxSectorAccessPrice      string
	MaxStoragePrice           string
	MaxUploadBandwidthPrice   string
	MinMaxCollateral          string
}

// persistData returns the data in the Contractor that will be saved to disk.
func (c *Contractor) persistData() contractorPersist {
	synced := false
	select {
	case <-c.synced:
		synced = true
	default:
	}
	data := contractorPersist{
		BlockHeight:          c.blockHeight,
		LastChange:           c.lastChange,
		DoubleSpentContracts: make(map[string]types.BlockHeight),
		Synced:               synced,
	}
	for fcID, height := range c.doubleSpentContracts {
		data.DoubleSpentContracts[fcID.String()] = height
	}
	data.WatchdogData = c.staticWatchdog.callPersistData()
	return data
}

// load loads the Contractor persistence data from disk.
func (c *Contractor) load() error {
	var data contractorPersist
	err := persist.LoadJSON(persistMeta, &data, filepath.Join(c.persistDir, PersistFilename))
	if err != nil {
		return err
	}

	c.blockHeight = data.BlockHeight
	c.lastChange = data.LastChange
	c.synced = make(chan struct{})
	if data.Synced {
		close(c.synced)
	}
	var fcid types.FileContractID
	for fcIDString, height := range data.DoubleSpentContracts {
		if err := fcid.LoadString(fcIDString); err != nil {
			return err
		}
		c.doubleSpentContracts[fcid] = height
	}
	err = c.loadRenewHistory()
	if err != nil {
		return err
	}

	// Load the renters from the database.
	rows, err := c.db.Query(`
		SELECT email, public_key, current_period, funds, hosts, period, renew_window,
			expected_storage, expected_upload, expected_download, expected_redundancy,
			max_rpc_price, max_contract_price, max_download_bandwidth_price,
			max_sector_access_price, max_storage_price, max_upload_bandwidth_price,
			min_max_collateral
		FROM renters`)
	if err != nil {
		c.log.Println("ERROR: could not load the renters:", err)
		return err
	}
	defer rows.Close()

	var entry renterData
	for rows.Next() {
		if err := rows.Scan(&entry.Email, &entry.PublicKey, &entry.CurrentPeriod, &entry.Funds, &entry.Hosts, &entry.Period, &entry.RenewWindow, &entry.ExpectedStorage, &entry.ExpectedUpload, &entry.ExpectedDownload, &entry.ExpectedRedundancy, &entry.MaxRPCPrice, &entry.MaxContractPrice, &entry.MaxDownloadBandwidthPrice, &entry.MaxSectorAccessPrice, &entry.MaxStoragePrice, &entry.MaxUploadBandwidthPrice, &entry.MinMaxCollateral); err != nil {
			c.log.Println("ERROR: could not load the renter:", err)
			continue
		}

		c.renters[entry.PublicKey] = modules.Renter{
			Allowance: modules.Allowance{
				Funds:       modules.ReadCurrency(entry.Funds),
				Hosts:       entry.Hosts,
				Period:      types.BlockHeight(entry.Period),
				RenewWindow: types.BlockHeight(entry.RenewWindow),

				ExpectedStorage:    entry.ExpectedStorage,
				ExpectedUpload:     entry.ExpectedUpload,
				ExpectedDownload:   entry.ExpectedDownload,
				ExpectedRedundancy: entry.ExpectedRedundancy,

				MaxRPCPrice:               modules.ReadCurrency(entry.MaxRPCPrice),
				MaxContractPrice:          modules.ReadCurrency(entry.MaxContractPrice),
				MaxDownloadBandwidthPrice: modules.ReadCurrency(entry.MaxDownloadBandwidthPrice),
				MaxSectorAccessPrice:      modules.ReadCurrency(entry.MaxSectorAccessPrice),
				MaxStoragePrice:           modules.ReadCurrency(entry.MaxStoragePrice),
				MaxUploadBandwidthPrice:   modules.ReadCurrency(entry.MaxUploadBandwidthPrice),
				MinMaxCollateral:          modules.ReadCurrency(entry.MinMaxCollateral),
			},
			CurrentPeriod: types.BlockHeight(entry.CurrentPeriod),
			PublicKey:     modules.ReadPublicKey(entry.PublicKey),
			Email:         entry.Email,
		}
	}

	c.staticWatchdog, err = newWatchdogFromPersist(c, data.WatchdogData)
	if err != nil {
		return err
	}
	c.staticWatchdog.blockHeight = data.BlockHeight

	return nil
}

// loadRenewHistory loads the renewal history from the database.
func (c *Contractor) loadRenewHistory() error {
	rows, err := c.db.Query("SELECT contract_id, renewed_from, renewed_to FROM contracts")
	if err != nil {
		return err
	}
	defer rows.Close()

	var id, from, to string
	var fcid, fcidNew, fcidOld types.FileContractID
	for rows.Next() {
		if err := rows.Scan(&id, &from, &to); err != nil {
			c.log.Println("Error scanning database row:", err)
			continue
		}
		if err := fcid.LoadString(id); err != nil {
			c.log.Println("ERROR: wrong contract ID:", err)
			continue
		}
		if from != "" {
			if err := fcidOld.LoadString(from); err != nil {
				c.log.Println("ERROR: wrong contract ID:", err)
				continue
			}
			c.renewedFrom[fcid] = fcidOld
		}
		if to != "" {
			if err := fcidNew.LoadString(to); err != nil {
				c.log.Println("ERROR: wrong contract ID:", err)
				continue
			}
			c.renewedTo[fcid] = fcidNew
		}
	}

	return nil
}

// save saves the Contractor persistence data to disk.
func (c *Contractor) save() error {
	// c.persistData is broken out because stack traces will not include the
	// function call otherwise.
	persistData := c.persistData()
	filename := filepath.Join(c.persistDir, PersistFilename)
	return persist.SaveJSON(persistMeta, persistData, filename)
}

// threadedSaveLoop periodically saves the Contractor persistence.
func (c *Contractor) threadedSaveLoop() {
	err := c.tg.Add()
	if err != nil {
		return
	}
	defer c.tg.Done()

	for {
		select {
		case <-c.tg.StopChan():
			return
		case <-time.After(saveFrequency):
			c.mu.Lock()
			err = c.save()
			c.mu.Unlock()
			if err != nil {
				c.log.Println("Difficulties saving the Contractor:", err)
			}
		}
	}
}
