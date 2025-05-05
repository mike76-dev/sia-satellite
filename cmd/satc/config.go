package main

import (
	"os"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"gopkg.in/yaml.v3"
)

// apiConfig determines the access to `satc`.
type apiConfig struct {
	Address string `yaml:"address"`
}

// renterdConfig determines the access to `renterd`.
type renterdConfig struct {
	Address  string `yaml:"address"`
	Password string `yaml:"password"`
}

// satelliteConfig determines the access to a satellite.
type satelliteConfig struct {
	Address  string `yaml:"address"`
	APIToken string `yaml:"apiToken"`
}

// config combines various config parameters.
type config struct {
	APIConfig       apiConfig       `yaml:"api"`
	RenterdConfig   renterdConfig   `yaml:"renterd"`
	SatelliteConfig satelliteConfig `yaml:"satellite"`
}

// loadConfig loads the `satc` config.
func loadConfig(dir string) (*config, error) {
	path := filepath.Join(dir, "satc.yml")
	f, err := os.Open(path)
	if err != nil {
		return nil, utils.AddContext(err, "couldn't open config file")
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	cfg := &config{}
	if err := dec.Decode(cfg); err != nil {
		return nil, utils.AddContext(err, "couldn't decode config")
	}

	return cfg, nil
}
