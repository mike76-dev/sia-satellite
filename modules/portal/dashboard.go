package portal

import (
	"encoding/hex"
	"math"
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

type (
	// sensibleHostAverages contains the human-readable host network.
	// averages.
	sensibleHostAverages struct {
		NumHosts               uint64  `json:"numhosts"`
		Duration               uint64  `json:"duration"`
		StoragePrice           float64 `json:"storageprice"`
		Collateral             float64 `json:"collateral"`
		DownloadBandwidthPrice float64 `json:"downloadbandwidthprice"`
		UploadBandwidthPrice   float64 `json:"uploadbandwidthprice"`
		ContractPrice          float64 `json:"contractprice"`
		BaseRPCPrice           float64 `json:"baserpcprice"`
		SectorAccessPrice      float64 `json:"sectoraccessprice"`
		SCRate                 float64 `json:"scrate"`
	}

	// hostsRequest contains the body of a /dashboard/hosts request.
	hostsRequest struct {
		Hosts            uint64  `json:"numhosts"`
		Duration         float64 `json:"duration"`
		Storage          float64 `json:"storage"`
		Upload           float64 `json:"upload"`
		Download         float64 `json:"download"`
		Redundancy       float64 `json:"redundancy"`
		MaxContractPrice float64 `json:"maxcontractprice"`
		MaxStoragePrice  float64 `json:"maxstorageprice"`
		MaxUploadPrice   float64 `json:"maxuploadprice"`
		MaxDownloadPrice float64 `json:"maxdownloadprice"`
		Estimation       float64 `json:"estimation"`
		Currency         string  `json:"currency"`
	}

	// hostsResponse contains the response to a /dashboard/hosts request.
	hostsResponse struct {
		Hosts      uint64  `json:"numhosts"`
		Estimation float64 `json:"estimation"`
		Currency   string  `json:"currency"`
	}

	// userPayment contains the details of a payment made by the user
	// account.
	userPayment struct {
		Amount    float64 `json:"amount"`
		Currency  string  `json:"currency"`
		AmountSC  float64 `json:"amountsc"`
		Timestamp uint64  `json:"timestamp"`
	}

	// renterContract represents a contract formed by the renter.
	renterContract struct {
		DownloadSpending    string `json:"downloadspending"`
		EndHeight           uint64 `json:"endheight"`
		Fees                string `json:"fees"`
		FundAccountSpending string `json:"fundaccountspending"`
		HostPublicKey       string `json:"hostpublickey"`
		HostVersion         string `json:"hostversion"`
		ID                  string `json:"id"`
		MaintenanceSpending string `json:"maintenancespending"`
		NetAddress          string `json:"netaddress"`
		RenterFunds         string `json:"renterfunds"`
		RemainingCollateral string `json:"remainingcollateral"`
		Size                string `json:"size"`
		StartHeight         uint64 `json:"startheight"`
		Status              string `json:"status"`
		StorageSpending     string `json:"storagespending"`
		TotalCost           string `json:"totalcost"`
		UploadSpending      string `json:"uploadspending"`
		GoodForUpload       bool   `json:"goodforupload"`
		GoodForRenew        bool   `json:"goodforrenew"`
		BadContract         bool   `json:"badcontract"`
	}
)

// balanceHandlerGET handles the GET /dashboard/balance requests.
func (api *portalAPI) balanceHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Retrieve the balance information from the database.
	var ub modules.UserBalance
	if ub, err = api.portal.manager.GetBalance(email); err != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	writeJSON(w, ub)
}

// averagesHandlerGET handles the GET /dashboard/averages requests.
func (api *portalAPI) averagesHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the currency parameter and check it.
	currency := req.FormValue("currency")
	scRate, err := api.portal.manager.GetSiacoinRate(currency)
	if err != nil {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "unsupported currency",
			}, http.StatusBadRequest)
		return
	}

	// Convert the averages into human-readable values.
	sha := convertHostAverages(api.portal.manager.GetAverages(), scRate)

	writeJSON(w, sha)
}

// convertHostAverages converts modules.HostAverages into human-
// readable format. rate is the exchange rate between SC and the
// currency to display the values in.
func convertHostAverages(ha modules.HostAverages, rate float64) sensibleHostAverages {
	sp  := modules.Float64(ha.StoragePrice.Mul(modules.BlockBytesPerMonthTerabyte))
	c   := modules.Float64(ha.Collateral.Mul(modules.BlockBytesPerMonthTerabyte))
	dbp := modules.Float64(ha.DownloadBandwidthPrice.Mul64(modules.BytesPerTerabyte))
	ubp := modules.Float64(ha.UploadBandwidthPrice.Mul64(modules.BytesPerTerabyte))
	cp  := modules.Float64(ha.ContractPrice)
	brp := modules.Float64(ha.BaseRPCPrice)
	sap := modules.Float64(ha.SectorAccessPrice)

	hastings := modules.Float64(types.HastingsPerSiacoin)

	sha := sensibleHostAverages{
		NumHosts:               ha.NumHosts,
		Duration:               ha.Duration,
		StoragePrice:           sp / hastings,
		Collateral:             c / hastings,
		DownloadBandwidthPrice: dbp / hastings,
		UploadBandwidthPrice:   ubp / hastings,
		ContractPrice:          cp / hastings,
		BaseRPCPrice:           brp / hastings,
		SectorAccessPrice:      sap / hastings,
		SCRate:                 rate,
	}

	return sha
}

// hostsHandlerPOST handles the POST /dashboard/hosts requests.
func (api *portalAPI) hostsHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	_, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Decode the request body.
	dec, err := prepareDecoder(w, req)
	if err != nil {
		return
	}

	var data hostsRequest
	hdErr, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, hdErr, code)
		return
	}

	// Calculate the exchange rate.
	scRate, err := api.portal.manager.GetSiacoinRate(data.Currency)
	if err != nil {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "unsupported currency",
			}, http.StatusBadRequest)
		return
	}

	// Sanity check.
	if scRate == 0 {
		api.portal.log.Println("ERROR: zero exchange rate")
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "zero exchange rate",
			}, http.StatusInternalServerError)
		return
	}

	// Create an allowance.
	a := modules.DefaultAllowance
	a.Funds = modules.FromFloat(data.Estimation)
	a.Hosts = data.Hosts
	a.Period = uint64(data.Duration * float64(modules.BlocksPerWeek))
	a.ExpectedStorage = uint64(data.Storage * (1 << 30))
	a.ExpectedUpload = uint64(data.Upload * (1 << 30))
	a.ExpectedDownload = uint64(data.Download * (1 << 30))
	a.MinShards = uint64(10)
	a.TotalShards = uint64(10 * data.Redundancy)
	a.MaxRPCPrice = modules.MaxRPCPrice
	a.MaxSectorAccessPrice = modules.MaxSectorAccessPrice
	a.MaxContractPrice = modules.FromFloat(data.MaxContractPrice / scRate)
	a.MaxStoragePrice = modules.FromFloat(data.MaxStoragePrice / scRate)
	a.MaxUploadBandwidthPrice = modules.FromFloat(data.MaxUploadPrice / scRate)
	a.MaxDownloadBandwidthPrice = modules.FromFloat(data.MaxDownloadPrice / scRate)

	// Pick random hosts.
	hosts, err := api.portal.manager.RandomHosts(a.Hosts, a)
	if err != nil {
		api.portal.log.Println("ERROR: could not get random hosts", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "could not get hosts",
			}, http.StatusInternalServerError)
		return
	}

	// Check if there are zero hosts, which means no estimation can be made.
	if len(hosts) == 0 {
		writeJSON(w, hostsResponse{ Currency: data.Currency, })
		return
	}

	// Calculate the price estimation.
	// Add up the costs for each host.
	var totalContractCost types.Currency
	var totalDownloadCost types.Currency
	var totalStorageCost types.Currency
	var totalUploadCost types.Currency
	var totalCollateral types.Currency
	for _, host := range hosts {
		totalContractCost = totalContractCost.Add(host.Settings.ContractPrice)
		totalDownloadCost = totalDownloadCost.Add(host.Settings.DownloadBandwidthPrice)
		totalStorageCost = totalStorageCost.Add(host.Settings.StoragePrice)
		totalUploadCost = totalUploadCost.Add(host.Settings.UploadBandwidthPrice)
		totalCollateral = totalCollateral.Add(host.Settings.Collateral)
	}

	// Account for the expected data size and duration.
	totalDownloadCost = totalDownloadCost.Mul64(a.ExpectedDownload)
	totalStorageCost = totalStorageCost.Mul64(a.ExpectedStorage * uint64(a.Period))
	totalUploadCost = totalUploadCost.Mul64(a.ExpectedUpload)
	totalCollateral = totalCollateral.Mul64(a.ExpectedStorage * uint64(a.Period))

	// Factor in redundancy.
	totalStorageCost = totalStorageCost.Mul64(a.TotalShards).Div64(a.MinShards)
	totalUploadCost = totalUploadCost.Mul64(a.TotalShards).Div64(a.MinShards)
	totalCollateral = totalCollateral.Mul64(a.TotalShards).Div64(a.MinShards)

	// Perform averages.
	totalContractCost = totalContractCost.Div64(uint64(len(hosts)))
	totalDownloadCost = totalDownloadCost.Div64(uint64(len(hosts)))
	totalStorageCost = totalStorageCost.Div64(uint64(len(hosts)))
	totalUploadCost = totalUploadCost.Div64(uint64(len(hosts)))
	totalCollateral = totalCollateral.Div64(uint64(len(hosts)))

	// Take the average of the host set to estimate the overall cost of the
	// contract forming. This is to protect against the case where less hosts
	// were gathered for the estimate that the allowance requires.
	totalContractCost = totalContractCost.Mul64(a.Hosts)

	// Add the cost of paying the transaction fees and then double the contract
	// costs to account for renewing a full set of contracts.
	_, feePerByte := api.portal.manager.FeeEstimation()
	txnsFees := feePerByte.Mul64(modules.EstimatedFileContractTransactionSetSize).Mul64(uint64(a.Hosts))
	totalContractCost = totalContractCost.Add(txnsFees)
	totalContractCost = totalContractCost.Mul64(2)

	// Add in siafund fee. which should be around 10%. The 10% siafund fee
	// accounts for paying 3.9% siafund on transactions and host collateral. We
	// estimate the renter to spend all of it's allowance so the siafund fee
	// will be calculated on the sum of the allowance and the hosts collateral.
	taxableAmount := totalContractCost
	taxableAmount = taxableAmount.Add(totalDownloadCost)
	taxableAmount = taxableAmount.Add(totalStorageCost)
	taxableAmount = taxableAmount.Add(totalUploadCost)
	taxableAmount = taxableAmount.Add(totalCollateral)
	siafundFee := modules.Tax(api.portal.manager.BlockHeight(), taxableAmount)
	totalPayout := taxableAmount.Add(siafundFee)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions.
	totalPayout = totalPayout.Mul64(12).Div64(10)

	// Apply exchange rate and round up to the whole number.
	hastings := modules.Float64(types.HastingsPerSiacoin)
	estimation := modules.Float64(totalPayout)
	toPay := math.Ceil(estimation / hastings * scRate)

	// Send the response.
	resp := hostsResponse{
		Hosts:      uint64(len(hosts)),
		Estimation: toPay,
		Currency:   data.Currency,
	}

	writeJSON(w, resp)
}

// paymentsHandlerGET handles the GET /dashboard/payments requests.
func (api *portalAPI) paymentsHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Retrieve the payment history.
	var ups []userPayment
	if ups, err = api.portal.getPayments(email); err != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	writeJSON(w, ups)
}

// seedHandlerGET handles the GET /dashboard/seed requests.
func (api *portalAPI) seedHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Retrieve the account balance.
	var ub modules.UserBalance
	if ub, err = api.portal.manager.GetBalance(email); err != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No balance yet.
	if !ub.IsUser {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Generate the seed and wipe it after use.
	walletSeed, err := api.portal.manager.GetWalletSeed()
	if err != nil {
		api.portal.log.Printf("ERROR: error retrieving wallet seed: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	renterSeed := modules.DeriveRenterSeed(walletSeed, email)
	defer frand.Read(renterSeed)
	
	w.Header().Set("Renter-Seed", hex.EncodeToString(renterSeed))
	writeSuccess(w)
}

// keyHandlerGET handles the GET /dashboard/key requests.
func (api *portalAPI) keyHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	key := api.portal.provider.PublicKey()
	writeJSON(w, struct{Key string `json:"key"`}{Key: hex.EncodeToString(key[:])})
}

// contractsHandlerGET handles the GET /dashboard/contracts requests.
func (api *portalAPI) contractsHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Fetch request params.
	var current, old bool
	cu := req.FormValue("current")
	ol := req.FormValue("old")
	if cu != "" {
		current, err = strconv.ParseBool(cu)
		if err != nil {
			current = false
		}
	}
	if ol != "" {
		old, err = strconv.ParseBool(ol)
		if err != nil {
			old = false
		}
	}

	// Get the renter.
	var renter modules.Renter
	renters := api.portal.manager.Renters()
	for _, r := range renters {
		if r.Email == email {
			renter = r
			break
		}
	}

	// Filter the contracts.
	contracts := api.getContracts(renter, current, old)

	writeJSON(w, contracts)
}

// getContracts filters the satellite contracts by the given parameters.
func (api *portalAPI) getContracts(renter modules.Renter, current, old bool) []renterContract {
	var rc []renterContract
	currentBlockHeight := api.portal.manager.BlockHeight()

	if current {
		contracts := api.portal.manager.ContractsByRenter(renter.PublicKey)
		for _, c := range contracts {
			// Fetch host address.
			cp := types.ZeroCurrency
			var netAddress string
			hdbe, exists, _ := api.portal.manager.Host(c.HostPublicKey)
			if exists {
				netAddress = hdbe.Settings.NetAddress
				cp = hdbe.Settings.ContractPrice
			}

			// Build the contract.
			maintenanceSpending := c.MaintenanceSpending.AccountBalanceCost
			maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.FundAccountCost)
			maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.UpdatePriceTableCost)
			remainingCollateral := c.Transaction.FileContractRevisions[0].MissedProofOutputs[1].Value
			contract := renterContract{
				BadContract:         c.Utility.BadContract,
				DownloadSpending:    c.DownloadSpending.String(),
				EndHeight:           c.EndHeight,
				Fees:                c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee).String(),
				FundAccountSpending: c.FundAccountSpending.String(),
				GoodForUpload:       c.Utility.GoodForUpload,
				GoodForRenew:        c.Utility.GoodForRenew,
				HostPublicKey:       c.HostPublicKey.String(),
				HostVersion:         hdbe.Settings.Version,
				ID:                  c.ID.String(),
				NetAddress:          netAddress,
				MaintenanceSpending: maintenanceSpending.String(),
				RenterFunds:         c.RenterFunds.String(),
				RemainingCollateral: remainingCollateral.Sub(cp).String(),
				Size:                modules.FilesizeUnits(c.Size()),
				StartHeight:         c.StartHeight,
				StorageSpending:     c.StorageSpending.String(),
				TotalCost:           c.TotalCost.String(),
				UploadSpending:      c.UploadSpending.String(),
			}

			// Determine contract status.
			refreshed := api.portal.manager.RefreshedContract(c.ID)
			active := c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
			passive := !c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
			disabledContract := !active && !passive && !refreshed

			// A contract can either be active, passive, refreshed, or disabled.
			statusErr := active && passive && refreshed || active && refreshed || active && passive || passive && refreshed
			if statusErr {
				api.portal.log.Println("CRITICAL: Contract has multiple status types, this should never happen")
			} else if active {
				contract.Status = "active"
				rc = append(rc, contract)
			} else if passive {
				contract.Status = "passive"
				rc = append(rc, contract)
			} else if refreshed {
				contract.Status = "refreshed"
				rc = append(rc, contract)
			} else if disabledContract {
				contract.Status = "disabled"
				rc = append(rc, contract)
			}
		}
	}

	// Process old contracts.
	if old {
		contracts := api.portal.manager.OldContractsByRenter(renter.PublicKey)
		for _, c := range contracts {
			var size uint64
			if len(c.Transaction.FileContractRevisions) != 0 {
				size = c.Transaction.FileContractRevisions[0].Filesize
			}

			// Fetch host address.
			cp := types.ZeroCurrency
			var netAddress string
			hdbe, exists, _ := api.portal.manager.Host(c.HostPublicKey)
			if exists {
				netAddress = hdbe.Settings.NetAddress
				cp = hdbe.Settings.ContractPrice
			}

			// Build the contract.
			maintenanceSpending := c.MaintenanceSpending.AccountBalanceCost
			maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.FundAccountCost)
			maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.UpdatePriceTableCost)
			remainingCollateral := c.Transaction.FileContractRevisions[0].MissedProofOutputs[1].Value
			contract := renterContract{
				BadContract:         c.Utility.BadContract,
				DownloadSpending:    c.DownloadSpending.String(),
				EndHeight:           c.EndHeight,
				Fees:                c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee).String(),
				FundAccountSpending: c.FundAccountSpending.String(),
				GoodForUpload:       c.Utility.GoodForUpload,
				GoodForRenew:        c.Utility.GoodForRenew,
				HostPublicKey:       c.HostPublicKey.String(),
				HostVersion:         hdbe.Settings.Version,
				ID:                  c.ID.String(),
				NetAddress:          netAddress,
				MaintenanceSpending: maintenanceSpending.String(),
				RenterFunds:         c.RenterFunds.String(),
				RemainingCollateral: remainingCollateral.Sub(cp).String(),
				Size:                modules.FilesizeUnits(size),
				StartHeight:         c.StartHeight,
				StorageSpending:     c.StorageSpending.String(),
				TotalCost:           c.TotalCost.String(),
				UploadSpending:      c.UploadSpending.String(),
			}

			// Determine contract status.
			refreshed := api.portal.manager.RefreshedContract(c.ID)
			endHeightInPast := c.EndHeight < currentBlockHeight || c.StartHeight < renter.CurrentPeriod
			expiredContract := endHeightInPast && !refreshed
			expiredRefreshed := endHeightInPast && refreshed
			refreshedContract := refreshed && !endHeightInPast
			disabledContract := !refreshed && !endHeightInPast

			// A contract can only be refreshed, disabled, expired, or expired refreshed.
			if expiredContract {
				contract.Status = "expired"
				rc = append(rc, contract)
			} else if expiredRefreshed {
				contract.Status = "expired-refreshed"
				rc = append(rc, contract)
			} else if refreshedContract {
				contract.Status = "refreshed"
				rc = append(rc, contract)
			} else if disabledContract {
				contract.Status = "disabled"
				rc = append(rc, contract)
			}
		}
	}

	return rc
}

// blockHeightHandlerGET handles the GET /dashboard/blockheight requests.
func (api *portalAPI) blockHeightHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	writeJSON(w, struct{Height uint64 `json:"height"`}{Height: api.portal.manager.BlockHeight()})
}

// spendingsHandlerGET handles the GET /dashboard/spendings requests.
func (api *portalAPI) spendingsHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Retrieve the spendings from the database.
	currency := req.FormValue("currency")
	if currency == "" {
		currency = "USD"
	}
	var us modules.UserSpendings
	if us, err = api.portal.manager.RetrieveSpendings(email, currency); err != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	writeJSON(w, us)
}

// settingsHandlerGET handles the GET /dashboard/settings requests.
func (api *portalAPI) settingsHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Get the renter.
	var renter modules.Renter
	renters := api.portal.manager.Renters()
	for _, r := range renters {
		if r.Email == email {
			renter = r
			break
		}
	}

	writeJSON(w, renter.Settings)
}
