// Package external contains the API of all services
// external to Sia.
package external

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
)

const (
	// currencyAPI is the endpoint of the currency exchange rate API.
	currencyAPI = "https://api.freecurrencyapi.com/v1/latest"

	// scusdRateAPI is the endpoint of the SC-USD rate API.
	scusdRateAPI = "https://api.bittrex.com/v3/markets/SC-USD/ticker"
)

type (
	// exchangeRates holds the fiat currency exchange rates.
	exchangeRates struct {
		Data map[string]float64 `json: "data"`
	}

	// scusdRate holds the SC-USD market data.
	scusdRate struct {
		Symbol        string `json: "symbol"`
		LastTradeRate string `json: "lastTradeRate"`
		BidRate       string `json: "bidRate"`
		AskRate       string `json: "askRate"`
	}
)

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

// FetchSCUSDRate retrieves the SC-USD trade rate.
func FetchSCUSDRate() (float64, error) {
	resp, err := http.Get(scusdRateAPI)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, errors.New("falied to fetch SC-USD rate")
		}
		var data scusdRate
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&data)
		if err != nil {
			return 0, errors.New("wrong format of SC-USD market data")
		}
		return strconv.ParseFloat(data.LastTradeRate, 64)
	}
	return 0, errors.New("falied to fetch SC-USD rate")
}
