package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	//"github.com/mike76-dev/sia-satellite/modules"

	//"go.sia.tech/core/types"
)

type (
	// ExchangeRate contains the exchange rate of a given currency.
	ExchangeRate struct {
		Currency string  `json:"currency"`
		Rate     float64 `json:"rate"`
	}
)

// managerRateHandlerGET handles the API call to /manager/rate.
func (api *API) managerRateHandlerGET(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	currency := ps.ByName("currency")
	if currency == "" {
		WriteError(w, Error{"currency not specified"}, http.StatusBadRequest)
		return
	}

	currency = strings.ToUpper(currency)
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
