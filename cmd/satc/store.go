package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/internal/utils"
)

type settingsStatus struct {
	GougingSettingsSaved  bool `json:"gougingSettingsSaved"`
	UploadSettingsSaved   bool `json:"uploadSettingsSaved"`
	ContractSettingsSaved bool `json:"contractSettingsSaved"`
}

type persistData struct {
	Settings settingsStatus `json:"settings"`
}

func loadFromStore(dir string) (persistData, error) {
	var data persistData
	if js, err := os.ReadFile(filepath.Join(dir, "satc.json")); os.IsNotExist(err) {
		return data, nil
	} else if err != nil {
		return data, utils.AddContext(err, "couldn't read store")
	} else if err := json.Unmarshal(js, &data); err != nil {
		return data, utils.AddContext(err, "couldn't decode data")
	}
	return data, nil
}

func saveToStore(dir string, data persistData) error {
	buf, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return utils.AddContext(err, "couldn't encode data")
	}

	file, err := os.OpenFile(filepath.Join(dir, "satc.json"), os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return utils.AddContext(err, "couldn't open file")
	}

	defer func() {
		err = utils.ComposeErrors(err, file.Close())
	}()

	if _, err = file.Write(buf); err != nil {
		return utils.AddContext(err, "couldn't write file")
	} else if err = file.Sync(); err != nil {
		return utils.AddContext(err, "couldn't sync file")
	}

	return nil
}
