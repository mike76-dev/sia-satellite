package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"golang.org/x/term"
)

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
	// Parse command line flags.
	log.SetFlags(0)
	userAgent := flag.String("agent", "Sat-Agent", "custom agent used for API calls")
	gatewayAddr := flag.String("addr", ":0", "address to listen on for peer connections")
	apiAddr := flag.String("api-addr", "localhost:10080", "address to serve API on")
	satelliteAddr := flag.String("sat-addr", ":10082", "address to listen on for renter requests")
	dir := flag.String("dir", ".", "directory to store node state in")
	bootstrap := flag.Bool("bootstrap", true, "bootstrap the gateway and consensus modules")
	flag.Parse()

	// Fetch API password.
	apiPassword := getAPIPassword()

	// Create the state directory if it does not yet exist.
	// This also checks if the provided directory parameter is valid.
	err := os.MkdirAll(*dir, 0700)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", *dir)
	}

	// Start satd. startDaemon will only return when it is shutting down.
	err = startDaemon(*userAgent, *gatewayAddr, *apiAddr, apiPassword, *satelliteAddr, *dir, *bootstrap)
	if err != nil {
		log.Fatal(err)
	}

	// Daemon seems to have closed cleanly. Print a 'closed' message.
	fmt.Println("Shutdown complete.")
}
