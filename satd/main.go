package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mike76-dev/sia-satellite/persist"
	"golang.org/x/term"
)

// Default config values.
var defaultConfig = persist.SatdConfig{
	Name:          "",
	GatewayAddr:   ":0",
	APIAddr:       "localhost:9990",
	SatelliteAddr: ":9992",
	Dir:           ".",
	DBUser:        "",
	DBName:        "satellite",
	PortalPort:    ":8080",
}

var config persist.SatdConfig

func getAPIPassword() string {
	apiPassword := os.Getenv("SATD_API_PASSWORD")
	if apiPassword != "" {
		fmt.Println("Using SATD_API_PASSWORD environment variable.")
	} else {
		fmt.Print("Enter API password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("Could not read API password: %v\n", err)
		}
		apiPassword = string(pw)
	}
	return apiPassword
}

func getDBPassword() string {
	dbPassword := os.Getenv("SATD_DB_PASSWORD")
	if dbPassword != "" {
		fmt.Println("Using SATD_DB_PASSWORD environment variable.")
	} else {
		fmt.Print("Enter database password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("Could not read database password: %v\n", err)
		}
		dbPassword = string(pw)
	}
	return dbPassword
}

func getWalletSeed() string {
	seed := os.Getenv("SATD_WALLET_SEED")
	if seed != "" {
		log.Println("Using SATD_WALLET_SEED environment variable.")
	} else {
		fmt.Print("Enter wallet seed: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("Could not read wallet seed: %v\n", err)
		}
		seed = string(pw)
	}
	return seed
}

func main() {
	log.SetFlags(0)

	// Load config file if it exists. Otherwise load the defaults.
	configDir := os.Getenv("SATD_CONFIG_DIR")
	if configDir != "" {
		fmt.Println("Using SATD_CONFIG_DIR environment variable to load config.")
	}
	ok, err := config.Load(configDir)
	if err != nil {
		log.Fatalln("Could not load config file:", err)
	}
	if !ok {
		config = defaultConfig
	}

	// Parse command line flags. If set, they override the loaded config.
	name := flag.String("name", "", "name of the satellite node")
	gatewayAddr := flag.String("addr", "", "address to listen on for peer connections")
	apiAddr := flag.String("api-addr", "", "address to serve API on")
	satelliteAddr := flag.String("sat-addr", "", "address to listen on for renter requests")
	dir := flag.String("dir", "", "directory to store node state in")
	dbUser := flag.String("db-user", "", "username for accessing the database")
	dbName := flag.String("db-name", "", "name of MYSQL database")
	portalPort := flag.String("portal", "", "port number the portal server listens at")
	flag.Parse()
	if *name != "" {
		config.Name = *name
	}
	if *gatewayAddr != "" {
		config.GatewayAddr = *gatewayAddr
	}
	if *apiAddr != "" {
		config.APIAddr = *apiAddr
	}
	if *satelliteAddr != "" {
		config.SatelliteAddr = *satelliteAddr
	}
	if *dir != "" {
		config.Dir = *dir
	}
	if *dbUser != "" {
		config.DBUser = *dbUser
	}
	if *dbName != "" {
		config.DBName = *dbName
	}
	if *portalPort != "" {
		config.PortalPort = *portalPort
	}

	// Save the configuration.
	err = config.Save(configDir)
	if err != nil {
		log.Fatalln("Unable to save config file")
	}

	// Fetch API password.
	apiPassword := getAPIPassword()

	// Fetch DB password.
	dbPassword := getDBPassword()

	// Fetch wallet seed.
	seed := getWalletSeed()

	// Create the state directory if it does not yet exist.
	// This also checks if the provided directory parameter is valid.
	err = os.MkdirAll(config.Dir, 0700)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", config.Dir)
	}

	// Start satd. startDaemon will only return when it is shutting down.
	err = startDaemon(&config, apiPassword, dbPassword, seed)
	if err != nil {
		log.Fatalln(err)
	}

	// Daemon seems to have closed cleanly. Print a 'closed' message.
	fmt.Println("Shutdown complete.")
}
