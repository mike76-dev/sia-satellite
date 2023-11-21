// Package external contains the API of all services
// external to Sia.
package external

import (
	"encoding/json"
	"errors"
	"net/http"
)

const (
	// marketAPI is the endpoint of the Siacoin exchange rate API.
	marketAPI = "https://api.siacentral.com/v2/market/exchange-rate"
)

type (
	// marketResponse holds the market API response.
	marketResponse struct {
		Message string             `json:"message"`
		Type    string             `json:"type"`
		Price   map[string]float64 `json:"price"`
	}
)

// FetchSCRates retrieves the Siacoin exchange rates.
func FetchSCRates() (map[string]float64, error) {
	resp, err := http.Get(marketAPI)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("falied to fetch SC exchange rates")
		}
		var data marketResponse
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&data)
		if err != nil {
			return nil, errors.New("wrong format of SC exchange rates")
		}
		return data.Price, nil
	}
	return nil, errors.New("falied to fetch SC exchange rates")
}
