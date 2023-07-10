package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"
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
)

// managerRateHandlerGET handles the API call to /manager/rate.
func (api *API) managerRateHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	currency := strings.ToUpper(ps.ByName("currency"))
	if currency == "" {
		WriteError(w, Error{"currency not specified"}, http.StatusBadRequest)
		return
	}

	var rate float64
	var err error
	if currency == "SC" {
		// Special case.
		rate, err = api.manager.GetSiacoinRate("USD")
		if err != nil {
			WriteError(w, Error{fmt.Sprintf("couldn't get SC rate: %v", err)}, http.StatusInternalServerError)
			return
		}
	} else {
		rate, err = api.manager.GetExchangeRate(currency)
		if err != nil {
			WriteError(w, Error{fmt.Sprintf("couldn't get exchange rate: %v", err)}, http.StatusInternalServerError)
			return
		}
	}

	er := ExchangeRate{
		Currency: currency,
		Rate:     rate,
	}

	WriteJSON(w, er)
}

// managerAveragesHandlerGET handles the API call to /manager/averages.
func (api *API) managerAveragesHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	currency := strings.ToUpper(ps.ByName("currency"))
	if currency == "" {
		currency = "SC"
	}

	rate := float64(1.0)
	var err error
	if currency != "SC" {
		rate, err = api.manager.GetExchangeRate(currency)
		if err != nil {
			WriteError(w, Error{fmt.Sprintf("couldn't get exchange rate: %v", err)}, http.StatusInternalServerError)
			return
		}
	}

	ha := HostAverages{api.manager.GetAverages(), rate}

	WriteJSON(w, ha)
}
