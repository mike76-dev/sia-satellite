package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/persist"
	"golang.org/x/term"
	"lukechampine.com/flagg"
)

// Default config values.
var defaultConfig = persist.SatdConfig{
	Name:        "localhost",
	GatewayAddr: ":9981",
	APIAddr:     ":9980",
	HTTPAddr:    ":8080",
	Dir:         ".",
	DBUser:      "",
	DBName:      "satellite",
	Test:        false,
}

var config persist.SatdConfig
var configDir string

func getAPIPassword() string {
	apiPassword := os.Getenv("SATD_API_PASSWORD")
	if apiPassword != "" {
		log.Println("Using SATD_API_PASSWORD environment variable.")
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
		log.Println("Using SATD_DB_PASSWORD environment variable.")
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

var (
	rootUsage = `Usage:
    satd [flags] [action]

Run 'satd' with no arguments to start the blockchain node and API server.

Actions:
    version     print hsd version
`
	versionUsage = `Usage:
    satd version

Prints the version of the satd binary.
`
)

func main() {
	log.SetFlags(0)

	// Load config file if it exists. Otherwise load the defaults.
	configDir = os.Getenv("SATD_CONFIG_DIR")
	if configDir != "" {
		log.Println("Using SATD_CONFIG_DIR environment variable to load config.")
	}
	ok, err := config.Load(configDir)
	if err != nil {
		log.Fatalf("Could not load config file: %v\n", err)
	}
	if !ok {
		config = defaultConfig
	}

	var gatewayAddr,
		apiAddr,
		httpAddr,
		dir,
		dbUser,
		dbName string

	var test bool

	rootCmd := flagg.Root
	rootCmd.Usage = flagg.SimpleUsage(rootCmd, rootUsage)
	rootCmd.StringVar(&gatewayAddr, "gateway-addr", "", "p2p address to listen on")
	rootCmd.StringVar(&apiAddr, "api-addr", "", "address to serve API on")
	rootCmd.StringVar(&httpAddr, "http-addr", "", "address to serve public API on")
	rootCmd.StringVar(&dir, "dir", "", "directory to store node state in")
	rootCmd.StringVar(&dbUser, "db-user", "", "username for accessing the database")
	rootCmd.StringVar(&dbName, "db-name", "", "name of MYSQL database")
	rootCmd.BoolVar(&test, "test", false, "enable test mode")
	versionCmd := flagg.New("version", versionUsage)

	cmd := flagg.Parse(flagg.Tree{
		Cmd: rootCmd,
		Sub: []flagg.Tree{
			{Cmd: versionCmd},
		},
	})

	switch cmd {
	case rootCmd:
		if len(cmd.Args()) != 0 {
			cmd.Usage()
			return
		}

		// Parse command line flags. If set, they override the loaded config.
		if gatewayAddr != "" {
			config.GatewayAddr = gatewayAddr
		}
		if apiAddr != "" {
			config.APIAddr = apiAddr
		}
		if httpAddr != "" {
			config.HTTPAddr = httpAddr
		}
		if dir != "" {
			config.Dir = dir
		}
		if dbUser != "" {
			config.DBUser = dbUser
		}
		if dbName != "" {
			config.DBName = dbName
		}
		if test {
			config.Test = true
		}

		// Save the configuration.
		err = config.Save(configDir)
		if err != nil {
			log.Fatalf("Unable to save config file: %v\n", err)
		}

		// Fetch API password.
		apiPassword := getAPIPassword()

		// Fetch DB password.
		dbPassword := getDBPassword()

		// Fetch wallet seed.
		seed := getWalletSeed()

		// Start satd. startDaemon will only return when it is shutting down.
		startDaemon(&config, apiPassword, dbPassword, seed)

		// Daemon seems to have closed cleanly. Print a 'closed' message.
		log.Println("Shutdown complete.")

	case versionCmd:
		if len(cmd.Args()) != 0 {
			cmd.Usage()
			return
		}
		fmt.Printf("%s v%v\n", build.NodeBinaryName, build.NodeVersion)
		if build.GitRevision != "" {
			fmt.Println("Git Revision " + build.GitRevision)
		}
	}
}
