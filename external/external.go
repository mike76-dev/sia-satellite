// Package external contains the API of all services
// external to Sia.
package external

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
)

// currencyAPI is the network address of the currency
// exchange rate API.
const currencyAPI = "https://api.freecurrencyapi.com/v1/latest"

// ExchangeRates holds the firat currency exchange rates.
type ExchangeRates struct {
	Data map[string]float64 `json: "data"`
}

// FetchExchangeRates retrieves the fiat currency exchange
// rates using an external API request.
func FetchExchangeRates() (*ExchangeRates, error) {
	key := os.Getenv("SATD_FREECURRENCY_API_KEY")
	if key == "" {
		return &ExchangeRates{}, errors.New("unable to find API key")
	}
	client := &http.Client{}
	req, _ := http.NewRequest("GET", currencyAPI, nil)
	req.Header.Set("apikey", key)
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return &ExchangeRates{}, errors.New("falied to fetch exchange rates")
		}
		var data ExchangeRates
		dec := json.NewDecoder(resp.Body)
		dec.Decode(&data)
		return &data, nil
	}
	return &ExchangeRates{}, errors.New("falied to fetch exchange rates")
}
