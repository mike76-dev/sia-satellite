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

// exchangeRates holds the firat currency exchange rates.
type exchangeRates struct {
	Data map[string]float64 `json: "data"`
}

// FetchExchangeRates retrieves the fiat currency exchange
// rates using an external API request.
func FetchExchangeRates() (map[string]float64, error) {
	key := os.Getenv("SATD_FREECURRENCY_API_KEY")
	if key == "" {
		return nil, errors.New("unable to find API key")
	}
	client := &http.Client{}
	req, _ := http.NewRequest("GET", currencyAPI, nil)
	req.Header.Set("apikey", key)
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("falied to fetch exchange rates")
		}
		var data exchangeRates
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&data)
		if err != nil {
			return nil, errors.New("wrong format of the exchange rates")
		}
		return data.Data, nil
	}
	return nil, errors.New("falied to fetch exchange rates")
}
