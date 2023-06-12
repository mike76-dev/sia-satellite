package main

import (
	"fmt"
	//"math"
	"os"
	"reflect"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
	"github.com/mike76-dev/sia-satellite/node/api/client"
	"github.com/spf13/cobra"

	"go.sia.tech/siad/build"
)

var (
	// General Flags.
	alertSuppress bool
	verbose       bool   // Display additional information.

	// Module Specific Flags.
	//
	// Wallet Flags.
	initForce            bool   // Destroy and re-encrypt the wallet on init if it already exists.
	initPassword         bool   // Supply a custom password when creating a wallet.
	walletRawTxn         bool   // Encode/decode transactions in base64-encoded binary.
	walletStartHeight    uint64 // Start height for transaction search.
	walletEndHeight      uint64 // End height for transaction search.
	walletTxnFeeIncluded bool   // Include the fee in the balance being sent.
	insecureInput        bool   // Insecure password/seed input. Disables the shoulder-surfing and Mac secure input feature.
)

var (
	// Globals.
	rootCmd    *cobra.Command // Root command cobra object, used by bash completion cmd.
	httpClient client.Client
)

// Exit codes.
// Inspired by sysexits.h
const (
	exitCodeGeneral = 1  // Not in sysexits.h, but is standard practice.
	exitCodeUsage   = 64 // EX_USAGE in sysexits.h
)

// wrap wraps a generic command with a check that the command has been
// passed the correct number of arguments. The command must take only strings
// as arguments.
func wrap(fn interface{}) func(*cobra.Command, []string) {
	fnVal, fnType := reflect.ValueOf(fn), reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		panic("wrapped function has wrong type signature")
	}
	for i := 0; i < fnType.NumIn(); i++ {
		if fnType.In(i).Kind() != reflect.String {
			panic("wrapped function has wrong type signature")
		}
	}

	return func(cmd *cobra.Command, args []string) {
		if len(args) != fnType.NumIn() {
			_ = cmd.UsageFunc()(cmd)
			os.Exit(exitCodeUsage)
		}
		argVals := make([]reflect.Value, fnType.NumIn())
		for i := range args {
			argVals[i] = reflect.ValueOf(args[i])
		}
		fnVal.Call(argVals)
	}
}

// die prints its arguments to stderr, in production exits the program with the
// default error code.
func die(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(exitCodeGeneral)
}

// statuscmd is the handler for the command `satc`.
// Prints basic information about Satellite.
func statuscmd() {
	// For UX formating.
	defer fmt.Println()

	// Consensus Info.
	cg, err := httpClient.ConsensusGet()
	if strings.Contains(err.Error(), api.ErrAPICallNotRecognized.Error()) {
		// Assume module is not loaded if status command is not recognized.
		fmt.Printf("Consensus:\n  Status: %s\n\n", moduleNotReadyStatus)
	} else if err != nil {
		die("Could not get consensus status:", err)
	} else {
		fmt.Printf(`Consensus:
  Synced: %v
  Height: %v
`, yesNo(cg.Synced), cg.Height)
	}

	// Wallet Info.
	/*walletStatus, err := httpClient.WalletGet()
	if strings.Contains(err.Error(), api.ErrAPICallNotRecognized.Error()) {
		// Assume module is not loaded if status command is not recognized.
		fmt.Printf("Wallet:\n  Status: %s\n\n", moduleNotReadyStatus)
	} else if err != nil {
		die("Could not get wallet status:", err)
	} else if walletStatus.Unlocked {
		fmt.Printf(`Wallet:
  Status:          unlocked
  Siacoin Balance: %v
`, modules.ConvertCurrency(walletStatus.ConfirmedSiacoinBalance))
	} else {
		fmt.Printf(`Wallet:
  Status: Locked
`)
	}*/

	// Satellite Info.
	/*renters, err := httpClient.SatelliteRentersGet()
	if err != nil {
		die(err)
	}
	contracts, err := httpClient.SatelliteContractsGet("")
	if err != nil {
		die(err)
	}

	fmt.Printf(`Satellite:
  Renters:          %v
  Active Contracts: %v
`, len(renters.Renters), len(contracts.ActiveContracts))*/
}

func main() {
	// Initialize commands.
	rootCmd = initCmds()

	// Initialize client.
	initClient(rootCmd, &verbose, &httpClient, &alertSuppress)

	// Perform some basic actions after cobra has initialized.
	cobra.OnInitialize(func() {
		// Set API password if it was not set.
		setAPIPasswordIfNotSet()

		// Check for Critical Alerts.
		alerts, err := httpClient.DaemonAlertsGet()
		if err == nil && len(alerts.CriticalAlerts) > 0 && !alertSuppress {
			printAlerts(alerts.CriticalAlerts, modules.SeverityCritical)
			fmt.Println("------------------")
			fmt.Printf("\n  The above %v critical alerts should be resolved ASAP\n\n", len(alerts.CriticalAlerts))
		}
	})

	// Run.
	if err := rootCmd.Execute(); err != nil {
		// Since no commands return errors (all commands set Command.Run instead of
		// Command.RunE), Command.Execute() should only return an error on an
		// invalid command or flag. Therefore Command.Usage() was called (assuming
		// Command.SilenceUsage is false) and we should exit with exitCodeUsage.
		os.Exit(exitCodeUsage)
	}
}

// initCmds initializes root command and its subcommands
func initCmds() *cobra.Command {
	root := &cobra.Command{
		Use:   os.Args[0],
		Short: "satc v" + build.NodeVersion,
		Long:  "satc v" + build.NodeVersion,
		Run:   wrap(statuscmd),
	}

	// Daemon Commands.
	root.AddCommand(alertsCmd, stopCmd, versionCmd)


	// Create command tree (alphabetized by root command).
	root.AddCommand(consensusCmd)

	root.AddCommand(gatewayCmd)
	gatewayCmd.AddCommand(gatewayAddressCmd, gatewayBlocklistCmd, gatewayConnectCmd, gatewayDisconnectCmd, gatewayListCmd)
	gatewayBlocklistCmd.AddCommand(gatewayBlocklistAppendCmd, gatewayBlocklistClearCmd, gatewayBlocklistRemoveCmd, gatewayBlocklistSetCmd)

	//root.AddCommand(hostdbCmd)
	//hostdbCmd.AddCommand(hostdbFiltermodeCmd, hostdbSetFiltermodeCmd, hostdbViewCmd)
	//hostdbCmd.Flags().IntVarP(&hostdbNumHosts, "numhosts", "n", 0, "Number of hosts to display from the hostdb")

	//root.AddCommand(portalCmd)
	//portalCmd.AddCommand(portalSetCmd)

	//root.AddCommand(satelliteCmd)
	//satelliteCmd.AddCommand(satelliteRentersCmd, satelliteRenterCmd, satelliteBalanceCmd, satelliteContractsCmd)

	//root.AddCommand(walletCmd)
	//walletCmd.AddCommand(walletAddressCmd, walletAddressesCmd, walletBalanceCmd, walletBroadcastCmd, walletChangepasswordCmd,
		//walletInitCmd, walletInitSeedCmd, walletLoadCmd, walletLockCmd, walletSeedsCmd, walletSendCmd,
		//walletSignCmd, walletSweepCmd, walletTransactionsCmd, walletUnlockCmd)
	//walletInitCmd.Flags().BoolVarP(&initPassword, "password", "p", false, "Prompt for a custom password")
	//walletInitCmd.Flags().BoolVarP(&initForce, "force", "", false, "destroy the existing wallet and re-encrypt")
	//walletInitSeedCmd.Flags().BoolVarP(&initForce, "force", "", false, "destroy the existing wallet")
	//walletInitSeedCmd.Flags().BoolVarP(&insecureInput, "insecure-input", "", false, "Disable shoulder-surf protection (echoing passwords and seeds)")
	//walletLoadCmd.AddCommand(walletLoadSeedCmd)
	//walletSendCmd.AddCommand(walletSendSiacoinsCmd)
	//walletSendSiacoinsCmd.Flags().BoolVarP(&walletTxnFeeIncluded, "fee-included", "", false, "Take the transaction fee out of the balance being submitted instead of the fee being additional")
	//walletUnlockCmd.Flags().BoolVarP(&insecureInput, "insecure-input", "", false, "Disable shoulder-surf protection (echoing passwords and seeds)")
	//walletUnlockCmd.Flags().BoolVarP(&initPassword, "password", "p", false, "Display interactive password prompt even if SATD_WALLET_PASSWORD is set")
	//walletBroadcastCmd.Flags().BoolVarP(&walletRawTxn, "raw", "", false, "Decode transaction as base64 instead of JSON")
	//walletSignCmd.Flags().BoolVarP(&walletRawTxn, "raw", "", false, "Encode signed transaction as base64 instead of JSON")
	//walletTransactionsCmd.Flags().Uint64Var(&walletStartHeight, "startheight", 0, " Height of the block where transaction history should begin.")
	//walletTransactionsCmd.Flags().Uint64Var(&walletEndHeight, "endheight", math.MaxUint64, " Height of the block where transaction history should end.")

	return root
}

// initClient initializes client cmd flags and default values
func initClient(root *cobra.Command, verbose *bool, client *client.Client, alertSuppress *bool) {
	root.PersistentFlags().BoolVarP(verbose, "verbose", "v", false, "Display additional information")
	root.PersistentFlags().StringVarP(&client.Address, "addr", "a", "localhost:9990", "which host/port to communicate with (i.e. the host/port satd is listening on)")
	root.PersistentFlags().StringVarP(&client.Password, "apipassword", "", "", "the password for the API's http authentication")
	root.PersistentFlags().StringVarP(&client.UserAgent, "useragent", "", "Sat-Agent", "the useragent used by satc to connect to the daemon's API")
	root.PersistentFlags().BoolVarP(alertSuppress, "alert-suppress", "s", false, "suppress satc alerts")
}

// setAPIPasswordIfNotSet sets API password if it was not set
func setAPIPasswordIfNotSet() {
	// Check if the API Password is set
	if httpClient.Password == "" {
		// No password passed in, fetch the API Password
		pwd := os.Getenv("SATD_API_PASSWORD")
		if pwd == "" {
			fmt.Println("Exiting: Error getting API Password")
			os.Exit(exitCodeGeneral)
		}
		httpClient.Password = pwd
	}
}
