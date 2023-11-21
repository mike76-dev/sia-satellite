package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

type (
	// ExchangeRate contains the exchange rate of a given currency.
	ExchangeRate struct {
		Currency string  `json:"currency"`
		Rate     float64 `json:"rate"`
	}

	// HostAverages contains the host network averages.
	HostAverages struct {
		modules.HostAverages
		Rate float64
	}

	// Renter contains information about the renter.
	Renter struct {
		Email     string          `json:"email"`
		PublicKey types.PublicKey `json:"publickey"`
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
		EndHeight uint64 `json:"endheight"`
		// Fees paid in order to form the file contract.
		Fees types.Currency `json:"fees"`
		// Amount of contract funds that have been spent on funding an ephemeral
		// account on the host.
		FundAccountSpending types.Currency `json:"fundaccountspending"`
		// Public key of the renter that formed the contract.
		RenterPublicKey types.PublicKey `json:"renterpublickey"`
		// Public key of the host the contract was formed with.
		HostPublicKey types.PublicKey `json:"hostpublickey"`
		// HostVersion is the version of Sia that the host is running.
		HostVersion string `json:"hostversion"`
		// ID of the file contract.
		ID types.FileContractID `json:"id"`
		// A signed transaction containing the most recent contract revision.
		LastTransaction types.Transaction `json:"lasttransaction"`
		// Amount of contract funds that have been spent on maintenance tasks
		// such as updating the price table or syncing the ephemeral account
		// balance.
		MaintenanceSpending modules.MaintenanceSpending `json:"maintenancespending"`
		// Address of the host the file contract was formed with.
		NetAddress string `json:"netaddress"`
		// Remaining funds left to spend on uploads & downloads.
		RenterFunds types.Currency `json:"renterfunds"`
		// Size of the file contract, which is typically equal to the number of
		// bytes that have been uploaded to the host.
		Size uint64 `json:"size"`
		// Block height that the file contract began on.
		StartHeight uint64 `json:"startheight"`
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

	// EmailPreferences contains the email preferences.
	EmailPreferences struct {
		Email         string         `json:"email"`
		WarnThreshold types.Currency `json:"threshold"`
	}
)

// managerAveragesHandlerGET handles the API call to /manager/averages.
func (api *API) managerAveragesHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	currency := strings.ToUpper(ps.ByName("currency"))
	if currency == "" {
		currency = "SC"
	}

	rate := float64(1.0)
	var err error
	if currency != "SC" {
		rate, err = api.manager.GetSiacoinRate(currency)
		if err != nil {
			WriteError(w, Error{fmt.Sprintf("couldn't get exchange rate: %v", err)}, http.StatusInternalServerError)
			return
		}
	}

	ha := HostAverages{api.manager.GetAverages(), rate}

	WriteJSON(w, ha)
}

// managerRentersHandlerGET handles the API call to /manager/renters.
func (api *API) managerRentersHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	renters := api.manager.Renters()

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

// managerRenterHandlerGET handles the API call to /manager/renter.
func (api *API) managerRenterHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	pk := ps.ByName("publickey")
	if pk == "" {
		WriteError(w, Error{"public key not specified"}, http.StatusBadRequest)
		return
	}

	var key types.PublicKey
	err := key.UnmarshalText([]byte(pk))
	if err != nil {
		WriteError(w, Error{"couldn't unmarshal public key: " + err.Error()}, http.StatusBadRequest)
		return
	}

	renter, err := api.manager.GetRenter(key)
	if err != nil {
		WriteError(w, Error{"renter not found: " + err.Error()}, http.StatusBadRequest)
		return
	}

	WriteJSON(w, renter)
}

// managerBalanceHandlerGET handles the API call to /manager/balance.
func (api *API) managerBalanceHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	pk := ps.ByName("publickey")
	if pk == "" {
		WriteError(w, Error{"public key not specified"}, http.StatusBadRequest)
		return
	}

	var key types.PublicKey
	err := key.UnmarshalText([]byte(pk))
	if err != nil {
		WriteError(w, Error{"couldn't unmarshal public key: " + err.Error()}, http.StatusBadRequest)
		return
	}

	renter, err := api.manager.GetRenter(key)
	if err != nil {
		WriteError(w, Error{"renter not found: " + err.Error()}, http.StatusBadRequest)
		return
	}

	ub, err := api.manager.GetBalance(renter.Email)
	if err != nil {
		WriteError(w, Error{"unable to get balance: " + err.Error()}, http.StatusInternalServerError)
		return
	}

	WriteJSON(w, ub)
}

// managerContractsHandlerGET handles the API call to /manager/contracts.
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
func (api *API) managerContractsHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	pk := strings.ToLower(ps.ByName("publickey"))
	var rc RenterContracts
	currentBlockHeight := api.cs.Height()

	// Fetch the contracts.
	var renter modules.Renter
	var contracts, oldContracts []modules.RenterContract
	if pk != "" {
		var key types.PublicKey
		err := key.UnmarshalText([]byte(pk))
		if err != nil {
			WriteError(w, Error{"couldn't unmarshal public key: " + err.Error()}, http.StatusBadRequest)
			return
		}
		contracts = api.manager.ContractsByRenter(key)
		oldContracts = api.manager.OldContractsByRenter(key)
		renter, _ = api.manager.GetRenter(key)
	} else {
		contracts = api.manager.Contracts()
		oldContracts = api.manager.OldContracts()
	}

	for _, c := range contracts {
		// Fetch host address.
		var netAddress string
		hdbe, exists, _ := api.manager.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.Settings.NetAddress
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
			HostVersion:         hdbe.Settings.Version,
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
		refreshed := api.manager.RefreshedContract(c.ID)
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
	for _, c := range oldContracts {
		var size uint64
		if len(c.Transaction.FileContractRevisions) != 0 {
			size = c.Transaction.FileContractRevisions[0].Filesize
		}

		// Fetch host address.
		var netAddress string
		hdbe, exists, _ := api.manager.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.Settings.NetAddress
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
			HostVersion:         hdbe.Settings.Version,
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
		refreshed := api.manager.RefreshedContract(c.ID)
		endHeightInPast := c.EndHeight < uint64(currentBlockHeight)
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

// managerPreferencesHandlerGET handles the API call to /manager/preferences.
func (api *API) managerPreferencesHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	email, threshold := api.manager.GetEmailPreferences()
	ep := EmailPreferences{
		Email:         email,
		WarnThreshold: threshold,
	}

	WriteJSON(w, ep)
}

// managerPreferencesHandlerPOST handles the API call to /manager/preferences.
func (api *API) managerPreferencesHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse parameters.
	var ep EmailPreferences
	err := json.NewDecoder(req.Body).Decode(&ep)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}

	// Set the preferences.
	if err := api.manager.SetEmailPreferences(ep.Email, ep.WarnThreshold); err != nil {
		WriteError(w, Error{"failed to change the preferences: " + err.Error()}, http.StatusInternalServerError)
		return
	}

	WriteSuccess(w)
}

// managerPricesHandlerGET handles the API call to /manager/prices.
func (api *API) managerPricesHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	WriteJSON(w, modules.StaticPricing)
}

// managerPricesHandlerPOST handles the API call to /manager/prices.
func (api *API) managerPricesHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse parameters.
	var prices modules.Pricing
	err := json.NewDecoder(req.Body).Decode(&prices)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}

	// Set the prices.
	if err := api.manager.UpdatePrices(prices); err != nil {
		WriteError(w, Error{"failed to change the prices: " + err.Error()}, http.StatusInternalServerError)
		return
	}

	WriteSuccess(w)
}

// managerMaintenanceHandlerPOST handles the API call to /manager/maintenance.
func (api *API) managerMaintenanceHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var start struct {
		Start bool `json:"start"`
	}
	err := json.NewDecoder(req.Body).Decode(&start)
	if err != nil {
		WriteError(w, Error{"invalid parameter: " + err.Error()}, http.StatusBadRequest)
		return
	}

	err = api.manager.StartMaintenance(start.Start)
	if err != nil {
		WriteError(w, Error{"couldn't set maintenance flag: " + err.Error()}, http.StatusInternalServerError)
		return
	}

	WriteSuccess(w)
}

// managerMaintenanceHandlerGET handles the API call to /manager/maintenance.
func (api *API) managerMaintenanceHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	WriteJSON(w, struct {
		Maintenance bool `json:"maintenance"`
	}{Maintenance: api.manager.Maintenance()})
}
