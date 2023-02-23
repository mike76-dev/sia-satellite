package api

import (
	"fmt"
	"net/http"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/julienschmidt/httprouter"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

type (
	// Renter contains information about the renter.
	Renter struct {
		Email     string             `json:"email"`
		PublicKey types.SiaPublicKey `json:"publickey"`
	}

	// RentersGET contains the list of the renters.
	RentersGET struct {
		Renters []Renter `json:"renters"`
	}

	// RenterContract represents a contract formed by the renter.
	RenterContract struct {
		// Amount of contract funds that have been spent on downloads.
		DownloadSpending types.Currency `json:"downloadspending"`
		// Block height that the file contract ends on.
		EndHeight types.BlockHeight `json:"endheight"`
		// Fees paid in order to form the file contract.
		Fees types.Currency `json:"fees"`
		// Amount of contract funds that have been spent on funding an ephemeral
		// account on the host.
		FundAccountSpending types.Currency `json:"fundaccountspending"`
		// Public key of the renter that formed the contract.
		RenterPublicKey types.SiaPublicKey `json:"renterpublickey"`
		// Public key of the host the contract was formed with.
		HostPublicKey types.SiaPublicKey `json:"hostpublickey"`
		// HostVersion is the version of Sia that the host is running.
		HostVersion string `json:"hostversion"`
		// ID of the file contract.
		ID types.FileContractID `json:"id"`
		// A signed transaction containing the most recent contract revision.
		LastTransaction types.Transaction `json:"lasttransaction"`
		// Amount of contract funds that have been spent on maintenance tasks
		// such as updating the price table or syncing the ephemeral account
		// balance.
		MaintenanceSpending smodules.MaintenanceSpending `json:"maintenancespending"`
		// Address of the host the file contract was formed with.
		NetAddress smodules.NetAddress `json:"netaddress"`
		// Remaining funds left to spend on uploads & downloads.
		RenterFunds types.Currency `json:"renterfunds"`
		// Size of the file contract, which is typically equal to the number of
		// bytes that have been uploaded to the host.
		Size uint64 `json:"size"`
		// Block height that the file contract began on.
		StartHeight types.BlockHeight `json:"startheight"`
		// Amount of contract funds that have been spent on storage.
		StorageSpending types.Currency `json:"storagespending"`
		// Total cost to the wallet of forming the file contract.
		TotalCost types.Currency `json:"totalcost"`
		// Amount of contract funds that have been spent on uploads.
		UploadSpending types.Currency `json:"uploadspending"`
		// Signals if contract is good for uploading data.
		GoodForUpload bool `json:"goodforupload"`
		// Signals if contract is good for a renewal.
		GoodForRenew bool `json:"goodforrenew"`
		// Signals if a contract has been marked as bad.
		BadContract bool `json:"badcontract"`
	}

	// RenterContracts contains the renter's contracts.
	RenterContracts struct {
		ActiveContracts           []RenterContract `json:"activecontracts"`
		PassiveContracts          []RenterContract `json:"passivecontracts"`
		RefreshedContracts        []RenterContract `json:"refreshedcontracts"`
		DisabledContracts         []RenterContract `json:"disabledcontracts"`
		ExpiredContracts          []RenterContract `json:"expiredcontracts"`
		ExpiredRefreshedContracts []RenterContract `json:"expiredrefreshedcontracts"`
	}
)

// satelliteRentersHandlerGET handles the API call to /satellite/renters.
func (api *API) satelliteRentersHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	renters := api.satellite.Renters()

	r := RentersGET{
		Renters: make([]Renter, 0, len(renters)),
	}

	for _, renter := range renters {
		r.Renters = append(r.Renters, Renter{
			Email:     renter.Email,
			PublicKey: renter.PublicKey,
		})
	}

	WriteJSON(w, r)
}

// satelliteRenterHandlerGET handles the API call to /satellite/renter.
func (api *API) satelliteRenterHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	pk := ps.ByName("publickey")
	if pk == "" {
		WriteError(w, Error{"public key not specified"}, http.StatusBadRequest)
		return
	}

	key := modules.ReadPublicKey(pk)
	renter, err := api.satellite.GetRenter(key)
	if err != nil {
		WriteError(w, Error{"renter not found: " + err.Error()}, http.StatusBadRequest)
		return
	}

	WriteJSON(w, renter)
}

// satelliteBalanceHandlerGET handles the API call to /satellite/balance.
func (api *API) satelliteBalanceHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	pk := ps.ByName("publickey")
	if pk == "" {
		WriteError(w, Error{"public key not specified"}, http.StatusBadRequest)
		return
	}

	key := modules.ReadPublicKey(pk)
	renter, err := api.satellite.GetRenter(key)
	if err != nil {
		WriteError(w, Error{"renter not found: " + err.Error()}, http.StatusBadRequest)
		return
	}

	ub, err := api.satellite.GetBalance(renter.Email)
	if err != nil {
		WriteError(w, Error{"unable to get balance: " + err.Error()}, http.StatusInternalServerError)
		return
	}

	WriteJSON(w, ub)
}

// satelliteContractsHandlerGET handles the API call to /satellite/contracts.
//
// Active contracts are contracts that are actively being used to store data
// and can upload, download, and renew. These contracts are GoodForUpload
// and GoodForRenew.
//
// Refreshed contracts are contracts that are in the current period and were
// refreshed due to running out of funds. A new contract that replaced a
// refreshed contract can either be in Active or Disabled contracts. These
// contracts are broken out as to not double count the data recorded in the
// contract.
//
// Disabled Contracts are contracts that are no longer active as there are Not
// GoodForUpload and Not GoodForRenew but still have endheights in the current
// period.
//
// Expired contracts are contracts who's endheights are in the past.
//
// ExpiredRefreshed contracts are refreshed contracts who's endheights are in
// the past.
func (api *API) satelliteContractsHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	pk := ps.ByName("publickey")
	var rc RenterContracts
	currentBlockHeight := api.cs.Height()

	// Fetch the renter, if provided.
	var renter modules.Renter
	var err error
	if pk != "" {
		key := modules.ReadPublicKey(pk)
		renter, err = api.satellite.GetRenter(key)
		if err != nil {
			pk = ""
		}
	}

	for _, c := range api.satellite.Contracts() {
		// Skip contracts that don't belong to the renter.
		if pk != "" && c.RenterPublicKey.String() != pk {
			continue
		}

		// Fetch host address.
		var netAddress smodules.NetAddress
		hdbe, exists, _ := api.satellite.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.NetAddress
		}

		// Build the contract.
		contract := RenterContract{
			BadContract:         c.Utility.BadContract,
			DownloadSpending:    c.DownloadSpending,
			EndHeight:           c.EndHeight,
			Fees:                c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee),
			FundAccountSpending: c.FundAccountSpending,
			GoodForUpload:       c.Utility.GoodForUpload,
			GoodForRenew:        c.Utility.GoodForRenew,
			RenterPublicKey:     c.RenterPublicKey,
			HostPublicKey:       c.HostPublicKey,
			HostVersion:         hdbe.Version,
			ID:                  c.ID,
			LastTransaction:     c.Transaction,
			NetAddress:          netAddress,
			MaintenanceSpending: c.MaintenanceSpending,
			RenterFunds:         c.RenterFunds,
			Size:                c.Size(),
			StartHeight:         c.StartHeight,
			StorageSpending:     c.StorageSpending,
			TotalCost:           c.TotalCost,
			UploadSpending:      c.UploadSpending,
		}

		// Determine contract status.
		refreshed := api.satellite.RefreshedContract(c.ID)
		active := c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
		passive := !c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
		disabledContract := !active && !passive && !refreshed

		// A contract can either be active, passive, refreshed, or disabled.
		statusErr := active && passive && refreshed || active && refreshed || active && passive || passive && refreshed
		if statusErr {
			fmt.Println("CRITICAL: Contract has multiple status types, this should never happen")
		} else if active {
			rc.ActiveContracts = append(rc.ActiveContracts, contract)
		} else if passive {
			rc.PassiveContracts = append(rc.PassiveContracts, contract)
		} else if refreshed {
			rc.RefreshedContracts = append(rc.RefreshedContracts, contract)
		} else if disabledContract {
			rc.DisabledContracts = append(rc.DisabledContracts, contract)
		}
	}

	// Process old contracts.
	for _, c := range api.satellite.OldContracts() {
		// Skip contracts that don't belong to the renter.
		if pk != "" && c.RenterPublicKey.String() != pk {
			continue
		}
		var size uint64
		if len(c.Transaction.FileContractRevisions) != 0 {
			size = c.Transaction.FileContractRevisions[0].NewFileSize
		}

		// Fetch host address.
		var netAddress smodules.NetAddress
		hdbe, exists, _ := api.satellite.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.NetAddress
		}

		// Build the contract.
		contract := RenterContract{
			BadContract:         c.Utility.BadContract,
			DownloadSpending:    c.DownloadSpending,
			EndHeight:           c.EndHeight,
			Fees:                c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee),
			FundAccountSpending: c.FundAccountSpending,
			GoodForUpload:       c.Utility.GoodForUpload,
			GoodForRenew:        c.Utility.GoodForRenew,
			RenterPublicKey:     c.RenterPublicKey,
			HostPublicKey:       c.HostPublicKey,
			HostVersion:         hdbe.Version,
			ID:                  c.ID,
			LastTransaction:     c.Transaction,
			MaintenanceSpending: c.MaintenanceSpending,
			NetAddress:          netAddress,
			RenterFunds:         c.RenterFunds,
			Size:                size,
			StartHeight:         c.StartHeight,
			StorageSpending:     c.StorageSpending,
			TotalCost:           c.TotalCost,
			UploadSpending:      c.UploadSpending,
		}

		// Determine contract status.
		refreshed := api.satellite.RefreshedContract(c.ID)
		endHeightInPast := c.EndHeight < currentBlockHeight
		if pk != "" {
			endHeightInPast = endHeightInPast || c.StartHeight < renter.CurrentPeriod
		}
		expiredContract := endHeightInPast && !refreshed
		expiredRefreshed := endHeightInPast && refreshed
		refreshedContract := refreshed && !endHeightInPast
		disabledContract := !refreshed && !endHeightInPast

		// A contract can only be refreshed, disabled, expired, or expired refreshed.
		if expiredContract {
			rc.ExpiredContracts = append(rc.ExpiredContracts, contract)
		} else if expiredRefreshed {
			rc.ExpiredRefreshedContracts = append(rc.ExpiredRefreshedContracts, contract)
		} else if refreshedContract {
			rc.RefreshedContracts = append(rc.RefreshedContracts, contract)
		} else if disabledContract {
			rc.DisabledContracts = append(rc.DisabledContracts, contract)
		}
	}

	WriteJSON(w, rc)
}
