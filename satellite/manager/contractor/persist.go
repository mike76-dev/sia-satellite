package contractor

import (
	"path/filepath"

	"go.sia.tech/siad/modules"
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

// contractorPersist defines what Contractor data persists across sessions.
type contractorPersist struct {
	Allowance            modules.Allowance               `json:"allowance"`
	BlockHeight          types.BlockHeight               `json:"blockheight"`
	CurrentPeriod        types.BlockHeight               `json:"currentperiod"`
	LastChange           modules.ConsensusChangeID       `json:"lastchange"`
	OldContracts         []modules.RenterContract        `json:"oldcontracts"`
	DoubleSpentContracts map[string]types.BlockHeight    `json:"doublespentcontracts"`
	RecoverableContracts []modules.RecoverableContract   `json:"recoverablecontracts"`
	RenewedFrom          map[string]types.FileContractID `json:"renewedfrom"`
	RenewedTo            map[string]types.FileContractID `json:"renewedto"`
	Synced               bool                            `json:"synced"`
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
		Allowance:            c.allowance,
		BlockHeight:          c.blockHeight,
		CurrentPeriod:        c.currentPeriod,
		LastChange:           c.lastChange,
		RenewedFrom:          make(map[string]types.FileContractID),
		RenewedTo:            make(map[string]types.FileContractID),
		DoubleSpentContracts: make(map[string]types.BlockHeight),
		Synced:               synced,
	}
	for k, v := range c.renewedFrom {
		data.RenewedFrom[k.String()] = v
	}
	for k, v := range c.renewedTo {
		data.RenewedTo[k.String()] = v
	}
	for _, contract := range c.oldContracts {
		data.OldContracts = append(data.OldContracts, contract)
	}
	for fcID, height := range c.doubleSpentContracts {
		data.DoubleSpentContracts[fcID.String()] = height
	}
	for _, contract := range c.recoverableContracts {
		data.RecoverableContracts = append(data.RecoverableContracts, contract)
	}
	return data
}

// load loads the Contractor persistence data from disk.
func (c *Contractor) load() error {
	var data contractorPersist
	err := persist.LoadJSON(persistMeta, &data, filepath.Join(c.persistDir, PersistFilename))
	if err != nil {
		return err
	}

	c.allowance = data.Allowance
	c.blockHeight = data.BlockHeight
	c.currentPeriod = data.CurrentPeriod
	c.lastChange = data.LastChange
	c.synced = make(chan struct{})
	if data.Synced {
		close(c.synced)
	}
	var fcid types.FileContractID
	for k, v := range data.RenewedFrom {
		if err := fcid.LoadString(k); err != nil {
			return err
		}
		c.renewedFrom[fcid] = v
	}
	for k, v := range data.RenewedTo {
		if err := fcid.LoadString(k); err != nil {
			return err
		}
		c.renewedTo[fcid] = v
	}
	for _, contract := range data.OldContracts {
		c.oldContracts[contract.ID] = contract
	}
	for fcIDString, height := range data.DoubleSpentContracts {
		if err := fcid.LoadString(fcIDString); err != nil {
			return err
		}
		c.doubleSpentContracts[fcid] = height
	}
	for _, contract := range data.RecoverableContracts {
		c.recoverableContracts[contract.ID] = contract
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
