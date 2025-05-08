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

	store.Settings.GougingSettingsSaved = true
	if err := saveToStore(dir, store); err != nil {
		return utils.AddContext(err, "couldn't save gouging settings status")
	}

	return nil
}

// updateUploadSettings updates the upload settings of `renterd` on the satellite.
func updateUploadSettings(body *bytes.Buffer) error {
	var rus renterd.UploadSettings
	if err := decodeRequest(body, &rus); err != nil {
		return err
	}

	us := account.UploadSettings{
		MinShards:   rus.Redundancy.MinShards,
		TotalShards: rus.Redundancy.TotalShards,
	}

	var httpError api.Error
	if err := satellite.Post("/account/settings/upload", &us, &httpError); err != nil {
		return utils.AddContext(err, "couldn't update upload settings")
	} else if httpError.Code != api.HttpErrorNone {
		return fmt.Errorf("failed to update upload settings: %s", httpError.Message)
	}

	store.Settings.UploadSettingsSaved = true
	if err := saveToStore(dir, store); err != nil {
		return utils.AddContext(err, "couldn't save upload settings status")
	}

	return nil
}

// updateContractSettings updates the contracts settings of `renterd` on the satellite.
func updateContractSettings(body *bytes.Buffer) error {
	var rac renterd.AutopilotConfig
	if err := decodeRequest(body, &rac); err != nil {
		return err
	}

	cs := account.ContractSettings{
		Count:       rac.Contracts.Amount,
		Period:      rac.Contracts.Period,
		RenewWindow: rac.Contracts.RenewWindow,
		Download:    rac.Contracts.Download,
		Upload:      rac.Contracts.Upload,
	}

	var httpError api.Error
	if err := satellite.Post("/account/settings/contracts", &cs, &httpError); err != nil {
		return utils.AddContext(err, "couldn't update contracts settings")
	} else if httpError.Code != api.HttpErrorNone {
		return fmt.Errorf("failed to update contracts settings: %s", httpError.Message)
	}

	store.Settings.ContractSettingsSaved = true
	if err := saveToStore(dir, store); err != nil {
		return utils.AddContext(err, "couldn't save contracts settings status")
	}

	return nil
}
