package api

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type (
	// PaymentCurrency lists the properties of a payment currency.
	PaymentCurrency struct {
		Name          string  `json:"name"`
		MinimumAmount float32 `json:"minimum"`
		ZeroDecimal   bool    `json:"zeroDecimal"`
	}
)

// allowedCurrencies lists all available currencies.
var allowedCurrencies = []PaymentCurrency{
	{
		Name:          "USD",
		MinimumAmount: 0.5,
		ZeroDecimal:   false,
	},
	{
		Name:          "EUR",
		MinimumAmount: 0.5,
		ZeroDecimal:   false,
	},
	{
		Name:          "GBP",
		MinimumAmount: 0.3,
		ZeroDecimal:   false,
	},
	{
		Name:          "CAD",
		MinimumAmount: 0.5,
		ZeroDecimal:   false,
	},
}

// stripeCurrenciesHandlerGET handles the GET /stripe/currencies requests.
func (s *Server) stripeCurrenciesHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	s.writeJSON(w, allowedCurrencies)
}
