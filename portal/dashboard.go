package portal

import (
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/julienschmidt/httprouter"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/fastrand"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

type (
	// userBalance holds the current balance as well as
	// the data on the chosen payment scheme.
	userBalance struct {
		IsUser     bool    `json:"isuser"`
		Subscribed bool    `json:"subscribed"`
		Balance    float64 `json:"balance"`
		Currency   string  `json:"currency"`
		SCBalance  float64 `json:"scbalance"`
		StripeID   string  `json:"stripeid"`
	}

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
)

// balanceHandlerGET handles the GET /dashboard/balance requests.
func (api *portalAPI) balanceHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code: httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
		Error{
			Code: httpErrorTokenExpired,
			Message: "token already expired",
		}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	count, cErr := api.portal.countEmails(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if count == 0 {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Retrieve the balance information from the database.
	var ub *userBalance
	if ub, cErr = api.portal.getBalance(email); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Calculate the Siacoin balance.
	if ub.IsUser {
		api.portal.mu.Lock()
		defer api.portal.mu.Unlock()

		fiatRate, ok := api.portal.exchRates[ub.Currency]
		if ok && fiatRate > 0 && api.portal.scusdRate > 0 {
			ub.SCBalance = ub.Balance / fiatRate / api.portal.scusdRate
		}
	}

	writeJSON(w, ub)
}

// averagesHandlerGET handles the GET /dashboard/averages requests.
func (api *portalAPI) averagesHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	api.portal.mu.Lock()
	defer api.portal.mu.Unlock()

	// Get the currency parameter and check it.
	currency := req.FormValue("currency")
	fiatRate, ok := api.portal.exchRates[currency]
	if !ok {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "unsupported currency",
			}, http.StatusBadRequest)
		return
	}

	// Convert the averages into human-readable values.
	rate := fiatRate * api.portal.scusdRate
	sha := convertHostAverages(api.portal.satellite.GetAverages(), rate)

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
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code: httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
		Error{
			Code: httpErrorTokenExpired,
			Message: "token already expired",
		}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	count, cErr := api.portal.countEmails(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if count == 0 {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Decode the request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data hostsRequest
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Calculate the exchange rate.
	api.portal.mu.Lock()

	fiatRate, ok := api.portal.exchRates[data.Currency]
	if !ok {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "unsupported currency",
			}, http.StatusBadRequest)
		return
	}
	rate := fiatRate * api.portal.scusdRate

	api.portal.mu.Unlock()

	// Sanity check.
	if rate == 0 {
		api.portal.log.Println("ERROR: zero exchange rate")
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "zero exchange rate",
			}, http.StatusInternalServerError)
		return
	}

	// Create an allowance.
	a := smodules.DefaultAllowance
	a.Funds = types.SiacoinPrecision.MulFloat(data.Estimation / rate)
	a.Hosts = data.Hosts
	a.Period = types.BlockHeight(data.Duration * float64(types.BlocksPerWeek))
	a.ExpectedStorage = uint64(data.Storage * (1 << 30))
	a.ExpectedUpload = uint64(data.Upload * (1 << 30))
	a.ExpectedDownload = uint64(data.Download * (1 << 30))
	a.ExpectedRedundancy = data.Redundancy
	a.MaxRPCPrice = modules.MaxRPCPrice
	a.MaxSectorAccessPrice = modules.MaxSectorAccessPrice
	a.MaxContractPrice = types.SiacoinPrecision.MulFloat(data.MaxContractPrice / rate)
	a.MaxStoragePrice = types.SiacoinPrecision.MulFloat(data.MaxStoragePrice / rate)
	a.MaxUploadBandwidthPrice = types.SiacoinPrecision.MulFloat(data.MaxUploadPrice / rate)
	a.MaxDownloadBandwidthPrice = types.SiacoinPrecision.MulFloat(data.MaxDownloadPrice / rate)

	// Pick random hosts.
	hosts, hErr := api.portal.satellite.RandomHosts(a.Hosts, a)
	if hErr != nil {
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
	// any potential missed additions
	totalPayout = totalPayout.MulFloat(1.2)

	// Apply exchange rate and round up to the whole number.
	precision, _ := types.SiacoinPrecision.Float64()
	estimation, _ := totalPayout.MulFloat(rate).Float64()
	payment := math.Ceil(estimation / precision)

	// Update the payment amount.
	pErr := api.portal.putPayment(email, payment, data.Currency, true)
	if pErr != nil {
		api.portal.log.Printf("ERROR: error recording the payment: %v\n", pErr)
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
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code: httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
		Error{
			Code: httpErrorTokenExpired,
			Message: "token already expired",
		}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	count, cErr := api.portal.countEmails(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if count == 0 {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Retrieve the payment history.
	var from, to int
	from, cErr = strconv.Atoi(req.FormValue("from"))
	if cErr != nil {
		api.portal.log.Printf("ERROR: wrong numeric value: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorBadRequest,
				Message: "wrong numeric value",
			}, http.StatusBadRequest)
		return
	}
	to, cErr = strconv.Atoi(req.FormValue("to"))
	if cErr != nil {
		api.portal.log.Printf("ERROR: wrong numeric value: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorBadRequest,
				Message: "wrong numeric value",
			}, http.StatusBadRequest)
		return
	}

	var ups []userPayment
	if ups, cErr = api.portal.getPayments(email, from, to); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
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
// paymentsHandlerGET handles the GET /dashboard/payments requests.
func (api *portalAPI) seedHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code: httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
		Error{
			Code: httpErrorTokenExpired,
			Message: "token already expired",
		}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	count, cErr := api.portal.countEmails(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if count == 0 {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Retrieve the account balance.
	var ub *userBalance
	if ub, cErr = api.portal.getBalance(email); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
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
	walletSeed, cErr := api.portal.satellite.GetWalletSeed()
	defer fastrand.Read(walletSeed[:])
	if cErr != nil {
		api.portal.log.Printf("ERROR: error retrieving wallet seed: %v\n", cErr)
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
