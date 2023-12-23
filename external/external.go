// Package external contains the API of all services
// external to Sia.
package external

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mike76-dev/sia-satellite/modules"
)

const (
	// marketAPI is the endpoint of the Siacoin exchange rate API.
	marketAPI = "https://api.siacentral.com/v2/market/exchange-rate"

	// googleAPI is the endpoint for retrieving the Google public key.
	googleAPI = "https://www.googleapis.com/oauth2/v1/certs"
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
	return nil, modules.AddContext(err, "falied to fetch SC exchange rates")
}

// GetGooglePublicKey retrieves the public key from Google.
func GetGooglePublicKey(keyID string) (string, error) {
	resp, err := http.Get(googleAPI)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	keys := map[string]string{}
	err = json.Unmarshal(data, &keys)
	if err != nil {
		return "", err
	}
	key, ok := keys[keyID]
	if !ok {
		return "", errors.New("key not found")
	}
	return key, nil
}
