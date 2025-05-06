package main

import (
	"bytes"
	"fmt"

	"github.com/mike76-dev/sia-satellite/account"
	api "github.com/mike76-dev/sia-satellite/api/public"
	"github.com/mike76-dev/sia-satellite/internal/utils"
	renterd "go.sia.tech/renterd/api"
)

// updateGougingSettings updates the gouging settings of `renterd` on the satellite.
func updateGougingSettings(body *bytes.Buffer) error {
	var rgs renterd.GougingSettings
	if err := decodeRequest(body, &rgs); err != nil {
		return err
	}

	gs := account.GougingSettings{
		MaxStoragePrice:  rgs.MaxStoragePrice,
		MaxIngressPrice:  rgs.MaxUploadPrice,
		MaxEgressPrice:   rgs.MaxDownloadPrice,
		MaxContractPrice: rgs.MaxContractPrice,
	}

	var httpError api.Error
	if err := satellite.Post("/account/settings/gouging", &gs, &httpError); err != nil {
		return utils.AddContext(err, "couldn't update gouging settings")
	} else if httpError.Code != api.HttpErrorNone {
		return fmt.Errorf("failed to update gouging settings: %s", httpError.Message)
	}

	return nil
}
