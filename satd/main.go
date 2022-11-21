package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mike76-dev/sia-satellite/node"

	"golang.org/x/term"
)

// Default config values.
var defaultConfig = node.SatdConfig{
	UserAgent: "Sat-Agent",
	GatewayAddr: ":0",
	APIAddr: "localhost:10080",
	SatelliteAddr: ":10082",
	Dir: ".",
	Bootstrap: true,
}

var config node.SatdConfig

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

func main() {
	log.SetFlags(0)

	// Load config file if it exists. Otherwise load the defaults.
	ok, err := config.Load()
	if err != nil {
		log.Fatalln("Could not load config file")
	}
	if !ok {
		config = defaultConfig
	}

	// Parse command line flags. If set, they override the loaded config.
	userAgent := flag.String("agent", "", "custom agent used for API calls")
	gatewayAddr := flag.String("addr", "", "address to listen on for peer connections")
	apiAddr := flag.String("api-addr", "", "address to serve API on")
	satelliteAddr := flag.String("sat-addr", "", "address to listen on for renter requests")
	dir := flag.String("dir", "", "directory to store node state in")
	bootstrap := flag.Bool("bootstrap", true, "bootstrap the gateway and consensus modules")
	flag.Parse()
	if *userAgent != "" {
		config.UserAgent = *userAgent
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
	config.Bootstrap = *bootstrap

	// Save the configuration.
	err = config.Save()
	if err != nil {
		log.Fatalln("Unable to save config file")
	}

	// Fetch API password.
	apiPassword := getAPIPassword()

	// Create the state directory if it does not yet exist.
	// This also checks if the provided directory parameter is valid.
	err = os.MkdirAll(config.Dir, 0700)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", config.Dir)
	}

	// Start satd. startDaemon will only return when it is shutting down.
	err = startDaemon(&config, apiPassword)
	if err != nil {
		log.Fatalln(err)
	}

	// Daemon seems to have closed cleanly. Print a 'closed' message.
	fmt.Println("Shutdown complete.")
}
