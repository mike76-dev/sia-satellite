package portal

import (
	"fmt"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"

	"github.com/mike76-dev/sia-satellite/modules"

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

	sp, _  := ha.StoragePrice.Mul64(uint64(types.BlocksPerMonth)).Mul64(1 << 40).Float64()
	c, _   := ha.Collateral.Mul64(uint64(types.BlocksPerMonth)).Mul64(1 << 40).Float64()
	dbp, _ := ha.DownloadBandwidthPrice.Mul64(1 << 40).Float64()
	ubp, _ := ha.UploadBandwidthPrice.Mul64(1 << 40).Float64()
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
