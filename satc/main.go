package main

import (
	"fmt"
	"os"
	"reflect"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/node/api/client"
	"github.com/spf13/cobra"
)

var (
	verbose bool // Display additional information.
)

var (
	// Globals.
	rootCmd    *cobra.Command // Root command cobra object, used by bash completion cmd.
	httpClient *client.Client
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
	cg, err := httpClient.ConsensusTip()
	if err != nil {
		die("Could not get consensus status:", err)
	}
	fmt.Printf(`Consensus:
  Synced: %v
  Height: %v
`, yesNo(cg.Synced), cg.Height)

	// TxPool Info.
	fee, err := httpClient.TxpoolFee()
	if err != nil {
		die("Could not get mining fee:", err)
	}
	fmt.Printf(`Recommended fee:
  %v / KB
`, fee.Mul64(1e3))

	// Wallet Info.
	walletStatus, err := httpClient.WalletBalance()
	if err != nil {
		die("Could not get wallet status:", err)
	}
	fmt.Printf(`Wallet:
  Siacoin Balance: %v
  Siafund Balance: %v
	`, walletStatus.Siacoins, walletStatus.Siafunds)

	// Manager Info.
	/*renters, err := httpClient.ManagerRentersGet()
		if err != nil {
			die(err)
		}
		contracts, err := httpClient.ManagerContractsGet("")
		if err != nil {
			die(err)
		}

		fmt.Printf(`Manager:
	  Renters:          %v
	  Active Contracts: %v
	`, len(renters.Renters), len(contracts.ActiveContracts))*/
}

func main() {
	// Initialize commands.
	rootCmd = initCmds()

	// Initialize client.
	httpClient = client.NewClient()
	initClient(rootCmd)

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
	root.AddCommand(versionCmd)

	// Create command tree (alphabetized by root command).
	root.AddCommand(consensusCmd)

	root.AddCommand(hostdbCmd)
	hostdbCmd.AddCommand(hostdbFiltermodeCmd, hostdbSetFiltermodeCmd, hostdbViewCmd)
	hostdbCmd.Flags().IntVarP(&hostdbNumHosts, "numhosts", "n", 0, "Number of hosts to display from the hostdb")

	root.AddCommand(managerCmd)
	managerCmd.AddCommand(managerAveragesCmd)
	managerCmd.AddCommand(managerRentersCmd, managerRenterCmd, managerBalanceCmd, managerContractsCmd)
	managerCmd.AddCommand(managerPreferencesCmd)
	managerCmd.AddCommand(managerSetPreferencesCmd)
	managerCmd.AddCommand(managerPricesCmd)
	managerCmd.AddCommand(managerSetPricesCmd)
	managerCmd.AddCommand(managerMaintenanceCmd)
	managerMaintenanceCmd.AddCommand(managerMaintenanceStartCmd)
	managerMaintenanceCmd.AddCommand(managerMaintenanceStopCmd)

	root.AddCommand(portalCmd)
	portalCmd.AddCommand(portalCreditsCmd)
	portalCmd.AddCommand(portalAnnouncementCmd)
	portalCreditsCmd.AddCommand(portalCreditsSetCmd)
	portalAnnouncementCmd.AddCommand(portalAnnouncementSetCmd)
	portalAnnouncementCmd.AddCommand(portalAnnouncementRemoveCmd)

	root.AddCommand(syncerCmd)
	syncerCmd.AddCommand(syncerConnectCmd, syncerPeersCmd)

	root.AddCommand(walletCmd)
	walletCmd.AddCommand(walletAddressCmd, walletAddressesCmd, walletBalanceCmd, walletSendCmd)
	walletSendCmd.AddCommand(walletSendSiacoinsCmd)

	return root
}

// initClient initializes client cmd flags and default values.
func initClient(root *cobra.Command) {
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Display additional information")
	root.PersistentFlags().StringVarP(&httpClient.Client().BaseURL, "addr", "a", "http://localhost:9990/api", "which host/port to communicate with (i.e. the host/port satd is listening on)")
	root.PersistentFlags().StringVarP(&httpClient.Client().Password, "apipassword", "", "", "the password for the API's http authentication")
}
