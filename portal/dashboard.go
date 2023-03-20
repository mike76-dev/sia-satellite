package portal

import (
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/fastrand"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

type (
	// sensibleHostAverages contains the human-readable host network
	// averages.
	sensibleHostAverages struct {
		NumHosts               uint64  `json:"numhosts"`
		Duration               string  `json:"duration"`
		StoragePrice           float64 `json:"storageprice"`
		Collateral             float64 `json:"collateral"`
		DownloadBandwidthPrice float64 `json:"downloadbandwidthprice"`
		UploadBandwidthPrice   float64 `json:"uploadbandwidthprice"`
		ContractPrice          float64 `json:"contractprice"`
		BaseRPCPrice           float64 `json:"baserpcprice"`
		SectorAccessPrice      float64 `json:"sectoraccessprice"`
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
		AmountUSD float64 `json:"amountusd"`
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
	var ub *modules.UserBalance
	if ub, err = api.portal.satellite.GetBalance(email); err != nil {
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
	scRate, err := api.portal.satellite.GetSiacoinRate(currency)
	if err != nil {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "unsupported currency",
			}, http.StatusBadRequest)
		return
	}

	// Convert the averages into human-readable values.
	sha := convertHostAverages(api.portal.satellite.GetAverages(), scRate)

	writeJSON(w, sha)
}

// convertHostAverages converts modules.HostAverages into human-
// readable format. rate is the exchange rate between SC and the
// currency to display the values in.
func convertHostAverages(ha modules.HostAverages, rate float64) sensibleHostAverages {
	var d string
	switch {
	case ha.Duration < types.BlocksPerWeek:
		d = fmt.Sprintf("%.1f days", float64(ha.Duration) / float64(types.BlocksPerDay))
		break
	case ha.Duration < types.BlocksPerMonth:
		d = fmt.Sprintf("%.1f weeks", float64(ha.Duration) / float64(types.BlocksPerWeek))
		break
	default:
		d = fmt.Sprintf("%.1f months", float64(ha.Duration) / float64(types.BlocksPerMonth))
		break
	}

	sp, _  := ha.StoragePrice.Mul(smodules.BlockBytesPerMonthTerabyte).Float64()
	c, _   := ha.Collateral.Mul(smodules.BlockBytesPerMonthTerabyte).Float64()
	dbp, _ := ha.DownloadBandwidthPrice.Mul(smodules.BytesPerTerabyte).Float64()
	ubp, _ := ha.UploadBandwidthPrice.Mul(smodules.BytesPerTerabyte).Float64()
	cp, _  := ha.ContractPrice.Float64()
	brp, _ := ha.BaseRPCPrice.Float64()
	sap, _ := ha.SectorAccessPrice.Float64()

	precision, _ := types.SiacoinPrecision.Float64()

	sha := sensibleHostAverages{
		NumHosts:               ha.NumHosts,
		Duration:               d,
		StoragePrice:           sp * rate / precision,
		Collateral:             c * rate / precision,
		DownloadBandwidthPrice: dbp * rate / precision,
		UploadBandwidthPrice:   ubp * rate / precision,
		ContractPrice:          cp * rate / precision,
		BaseRPCPrice:           brp * rate / precision,
		SectorAccessPrice:      sap * rate / precision,
	}

	return sha
}

// hostsHandlerPOST handles the POST /dashboard/hosts requests.
func (api *portalAPI) hostsHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
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
	scRate, err := api.portal.satellite.GetSiacoinRate(data.Currency)
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
	a.Funds = types.SiacoinPrecision.MulFloat(data.Estimation / scRate)
	a.Hosts = data.Hosts
	a.Period = types.BlockHeight(data.Duration * float64(types.BlocksPerWeek))
	a.ExpectedStorage = uint64(data.Storage * (1 << 30))
	a.ExpectedUpload = uint64(data.Upload * (1 << 30))
	a.ExpectedDownload = uint64(data.Download * (1 << 30))
	a.ExpectedRedundancy = data.Redundancy
	a.MaxRPCPrice = modules.MaxRPCPrice
	a.MaxSectorAccessPrice = modules.MaxSectorAccessPrice
	a.MaxContractPrice = types.SiacoinPrecision.MulFloat(data.MaxContractPrice / scRate)
	a.MaxStoragePrice = types.SiacoinPrecision.MulFloat(data.MaxStoragePrice / scRate)
	a.MaxUploadBandwidthPrice = types.SiacoinPrecision.MulFloat(data.MaxUploadPrice / scRate)
	a.MaxDownloadBandwidthPrice = types.SiacoinPrecision.MulFloat(data.MaxDownloadPrice / scRate)

	// Pick random hosts.
	hosts, err := api.portal.satellite.RandomHosts(a.Hosts, a)
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
		totalContractCost = totalContractCost.Add(host.ContractPrice)
		totalDownloadCost = totalDownloadCost.Add(host.DownloadBandwidthPrice)
		totalStorageCost = totalStorageCost.Add(host.StoragePrice)
		totalUploadCost = totalUploadCost.Add(host.UploadBandwidthPrice)
		totalCollateral = totalCollateral.Add(host.Collateral)
	}

	// Account for the expected data size and duration.
	totalDownloadCost = totalDownloadCost.Mul64(a.ExpectedDownload)
	totalStorageCost = totalStorageCost.Mul64(a.ExpectedStorage * uint64(a.Period))
	totalUploadCost = totalUploadCost.Mul64(a.ExpectedUpload)
	totalCollateral = totalCollateral.Mul64(a.ExpectedStorage * uint64(a.Period))

	// Factor in redundancy.
	totalStorageCost = totalStorageCost.MulFloat(a.ExpectedRedundancy)
	totalUploadCost = totalUploadCost.MulFloat(a.ExpectedRedundancy)
	totalCollateral = totalCollateral.MulFloat(a.ExpectedRedundancy)

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
	_, feePerByte := api.portal.satellite.FeeEstimation()
	txnsFees := feePerByte.Mul64(smodules.EstimatedFileContractTransactionSetSize).Mul64(uint64(a.Hosts))
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
	siafundFee := taxableAmount.MulTax().RoundDown(types.SiafundCount)
	totalPayout := taxableAmount.Add(siafundFee)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions.
	totalPayout = totalPayout.MulFloat(1.2)

	// Apply exchange rate and round up to the whole number.
	precision, _ := types.SiacoinPrecision.Float64()
	estimation, _ := totalPayout.MulFloat(scRate).Float64()
	payment := math.Ceil(estimation / precision)

	// Update the payment amount.
	err = api.portal.putPayment(email, payment, data.Currency, true)
	if err != nil {
		api.portal.log.Printf("ERROR: error recording the payment: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Send the response.
	resp := hostsResponse{
		Hosts:      uint64(len(hosts)),
		Estimation: payment,
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
	var ub *modules.UserBalance
	if ub, err = api.portal.satellite.GetBalance(email); err != nil {
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
	walletSeed, err := api.portal.satellite.GetWalletSeed()
	defer fastrand.Read(walletSeed[:])
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
	defer fastrand.Read(renterSeed[:])
	
	w.Header().Set("Renter-Seed", hex.EncodeToString(renterSeed[:]))
	writeSuccess(w)
}

// keyHandlerGET handles the GET /dashboard/key requests.
func (api *portalAPI) keyHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	writeJSON(w, struct{Key string `json:"key"`}{Key: api.portal.satellite.PublicKey().String()})
}

// contractsHandlerGet handles the GET /dashboard/contracts requests.
func (api *portalAPI) contractsHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Fetch request params.
	var active, passive, refreshed, disabled, expired, expiredRefreshed bool
	ac := req.FormValue("active")
	ps := req.FormValue("passive")
	rf := req.FormValue("refreshed")
	ds := req.FormValue("disabled")
	ex := req.FormValue("expired")
	er := req.FormValue("expired-refreshed")
	if ac != "" {
		active, err = strconv.ParseBool(ac)
		if err != nil {
			active = false
		}
	}
	if ps != "" {
		passive, err = strconv.ParseBool(ps)
		if err != nil {
			passive = false
		}
	}
	if rf != "" {
		refreshed, err = strconv.ParseBool(rf)
		if err != nil {
			refreshed = false
		}
	}
	if ds != "" {
		disabled, err = strconv.ParseBool(ds)
		if err != nil {
			disabled = false
		}
	}
	if ex != "" {
		expired, err = strconv.ParseBool(ex)
		if err != nil {
			expired = false
		}
	}
	if er != "" {
		expiredRefreshed, err = strconv.ParseBool(er)
		if err != nil {
			expiredRefreshed = false
		}
	}

	// Get the renter.
	var renter modules.Renter
	renters := api.portal.satellite.Renters()
	for _, r := range renters {
		if r.Email == email {
			renter = r
			break
		}
	}

	// Filter the contracts.
	contracts := api.getContracts(renter, active, passive, refreshed, disabled, expired, expiredRefreshed)

	writeJSON(w, contracts)
}

// getContracts filters the satellite contracts by the given parameters.
func (api *portalAPI) getContracts(renter modules.Renter, ac, ps, rf, ds, ex, er bool) []renterContract {
	var rc []renterContract
	currentBlockHeight := api.portal.satellite.BlockHeight()

	for _, c := range api.portal.satellite.Contracts() {
		// Skip contracts that don't belong to the renter.
		if renter.PublicKey.String() != c.RenterPublicKey.String() {
			continue
		}

		// Fetch host address.
		var netAddress smodules.NetAddress
		hdbe, exists, _ := api.portal.satellite.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.NetAddress
		}

		// Build the contract.
		maintenanceSpending := c.MaintenanceSpending.AccountBalanceCost
		maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.FundAccountCost)
		maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.UpdatePriceTableCost)
		contract := renterContract{
			BadContract:         c.Utility.BadContract,
			DownloadSpending:    modules.CurrencyUnits(c.DownloadSpending),
			EndHeight:           uint64(c.EndHeight),
			Fees:                modules.CurrencyUnits(c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee)),
			FundAccountSpending: modules.CurrencyUnits(c.FundAccountSpending),
			GoodForUpload:       c.Utility.GoodForUpload,
			GoodForRenew:        c.Utility.GoodForRenew,
			HostPublicKey:       c.HostPublicKey.String(),
			HostVersion:         hdbe.Version,
			ID:                  c.ID.String(),
			NetAddress:          string(netAddress),
			MaintenanceSpending: modules.CurrencyUnits(maintenanceSpending),
			RenterFunds:         modules.CurrencyUnits(c.RenterFunds),
			Size:                smodules.FilesizeUnits(c.Size()),
			StartHeight:         uint64(c.StartHeight),
			StorageSpending:     modules.CurrencyUnits(c.StorageSpending),
			TotalCost:           modules.CurrencyUnits(c.TotalCost),
			UploadSpending:      modules.CurrencyUnits(c.UploadSpending),
		}

		// Determine contract status.
		refreshed := api.portal.satellite.RefreshedContract(c.ID)
		active := c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
		passive := !c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
		disabledContract := !active && !passive && !refreshed

		// A contract can either be active, passive, refreshed, or disabled.
		statusErr := active && passive && refreshed || active && refreshed || active && passive || passive && refreshed
		if statusErr {
			fmt.Println("CRITICAL: Contract has multiple status types, this should never happen")
		} else if active && ac {
			contract.Status = "active"
			rc = append(rc, contract)
		} else if passive && ps {
			contract.Status = "passive"
			rc = append(rc, contract)
		} else if refreshed && rf {
			contract.Status = "refreshed"
			rc = append(rc, contract)
		} else if disabledContract && ds {
			contract.Status = "disabled"
			rc = append(rc, contract)
		}
	}

	// Process old contracts.
	for _, c := range api.portal.satellite.OldContracts() {
		// Skip contracts that don't belong to the renter.
		if renter.PublicKey.String() != c.RenterPublicKey.String() {
			continue
		}
		var size uint64
		if len(c.Transaction.FileContractRevisions) != 0 {
			size = c.Transaction.FileContractRevisions[0].NewFileSize
		}

		// Fetch host address.
		var netAddress smodules.NetAddress
		hdbe, exists, _ := api.portal.satellite.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.NetAddress
		}

		// Build the contract.
		maintenanceSpending := c.MaintenanceSpending.AccountBalanceCost
		maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.FundAccountCost)
		maintenanceSpending = maintenanceSpending.Add(c.MaintenanceSpending.UpdatePriceTableCost)
		contract := renterContract{
			BadContract:         c.Utility.BadContract,
			DownloadSpending:    modules.CurrencyUnits(c.DownloadSpending),
			EndHeight:           uint64(c.EndHeight),
			Fees:                modules.CurrencyUnits(c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee)),
			FundAccountSpending: modules.CurrencyUnits(c.FundAccountSpending),
			GoodForUpload:       c.Utility.GoodForUpload,
			GoodForRenew:        c.Utility.GoodForRenew,
			HostPublicKey:       c.HostPublicKey.String(),
			HostVersion:         hdbe.Version,
			ID:                  c.ID.String(),
			NetAddress:          string(netAddress),
			MaintenanceSpending: modules.CurrencyUnits(maintenanceSpending),
			RenterFunds:         modules.CurrencyUnits(c.RenterFunds),
			Size:                smodules.FilesizeUnits(size),
			StartHeight:         uint64(c.StartHeight),
			StorageSpending:     modules.CurrencyUnits(c.StorageSpending),
			TotalCost:           modules.CurrencyUnits(c.TotalCost),
			UploadSpending:      modules.CurrencyUnits(c.UploadSpending),
		}

		// Determine contract status.
		refreshed := api.portal.satellite.RefreshedContract(c.ID)
		endHeightInPast := c.EndHeight < currentBlockHeight || c.StartHeight < renter.CurrentPeriod
		expiredContract := endHeightInPast && !refreshed
		expiredRefreshed := endHeightInPast && refreshed
		refreshedContract := refreshed && !endHeightInPast
		disabledContract := !refreshed && !endHeightInPast

		// A contract can only be refreshed, disabled, expired, or expired refreshed.
		if expiredContract && ex {
			contract.Status = "expired"
			rc = append(rc, contract)
		} else if expiredRefreshed && er {
			contract.Status = "expired-refreshed"
			rc = append(rc, contract)
		} else if refreshedContract && rf {
			contract.Status = "refreshed"
			rc = append(rc, contract)
		} else if disabledContract && ds {
			contract.Status = "disabled"
			rc = append(rc, contract)
		}
	}

	return rc
}

// blockHeightHandlerGET handles the GET /dashboard/blockheight requests.
func (api *portalAPI) blockHeightHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	writeJSON(w, struct{Height uint64 `json:"height"`}{Height: uint64(api.portal.satellite.BlockHeight())})
}
