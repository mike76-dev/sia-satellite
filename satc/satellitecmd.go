package main

import (
	"fmt"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/spf13/cobra"

	smodules "go.sia.tech/siad/modules"
)

var (
	satelliteRentersCmd = &cobra.Command{
		Use:   "renters",
		Short: "Print the list of the renters",
		Long:  "Print the list of the current renters.",
		Run:   wrap(satelliterenterscmd),
	}

	satelliteRenterCmd = &cobra.Command{
		Use:   "renter [public_key]",
		Short: "Print the renter settings",
		Long:  "Print the settings of the renter with the given public key.",
		Run:   wrap(satelliterentercmd),
	}

	satelliteBalanceCmd = &cobra.Command{
		Use:   "balance [public_key}",
		Short: "Print the renter balance",
		Long:  "Print the balance of the renter with the given public key.",
		Run:   wrap(satellitebalancecmd),
	}

	satelliteContractsCmd = &cobra.Command{
		Use:   "contracts [public_key] or contracts all",
		Short: "Print the list of the contracts",
		Long:  "Print the list of the contracts. A renter public key may be provided.",
		Run:   wrap(satellitecontractscmd),
	}

	satelliteCmd = &cobra.Command{
		Use:   "satellite",
		Short: "View satellite info",
		Long:  "View satellite information.",
		Run:   wrap(satellitecmd),
	}
)

// satelliterenterscmd is the handler for the command `satc satellite renters`.
// Prints the list of the renters.
func satelliterenterscmd() {
	renters, err := httpClient.SatelliteRentersGet()
	if err != nil {
		die(err)
	}

	for _, renter := range renters.Renters {
		fmt.Printf("%v %v\n", renter.PublicKey.String(), renter.Email)
	}
}

// satelliterentercmd is the handler for the command `satc satellite renter [public_key]`.
// Prints the settings of the given renter.
func satelliterentercmd(key string) {
	renter, err := httpClient.SatelliteRenterGet(key)
	if err != nil {
		die(err)
	}
	fmt.Printf(`Public Key: %v
Email:      %v

Current Period: %v
New Period:     %v
Renew Window:   %v
Hosts:          %v

Expected Storage:    %v
Expected Upload:     %v
Expected Download:   %v
Min Shards:          %v
Total Shards:        %v

Max RPC Price:           %v
Max Contract Price:      %v
Max Download Price:      %v
Max Sector Access Price: %v
Max Storage Price:       %v
Max Upload Price:        %v
Min Max Collateral:      %v
Block Height Leeway:     %v
`, renter.PublicKey.String(), renter.Email, renter.CurrentPeriod, renter.Allowance.Period, renter.Allowance.RenewWindow, renter.Allowance.Hosts, smodules.FilesizeUnits(renter.Allowance.ExpectedStorage), smodules.FilesizeUnits(renter.Allowance.ExpectedUpload), smodules.FilesizeUnits(renter.Allowance.ExpectedDownload),
renter.Allowance.MinShards, renter.Allowance.TotalShards, modules.CurrencyUnits(renter.Allowance.MaxRPCPrice), modules.CurrencyUnits(renter.Allowance.MaxContractPrice), modules.CurrencyUnits(renter.Allowance.MaxDownloadBandwidthPrice), modules.CurrencyUnits(renter.Allowance.MaxSectorAccessPrice), modules.CurrencyUnits(renter.Allowance.MaxStoragePrice), modules.CurrencyUnits(renter.Allowance.MaxUploadBandwidthPrice),
modules.CurrencyUnits(renter.Allowance.MinMaxCollateral), renter.Allowance.BlockHeightLeeway)
}

// satellitebalancecmd is the handler for the command `satc satellite balance [public_key]`.
// Prints the balance of the given renter.
func satellitebalancecmd(key string) {
	ub, err := httpClient.SatelliteBalanceGet(key)
	if err != nil {
		die(err)
	}

	var lockedSC float64
	if ub.Balance > 0 {
		lockedSC = ub.SCBalance * ub.Locked / ub.Balance
	}

	fmt.Printf(`Is User:    %v
Subscribed: %v

Available Balance: %.4f SC (%.2f %v)
Locked Balance:    %.4f SC (%.2f %v)
`, ub.IsUser, ub.Subscribed, ub.SCBalance, ub.Balance, ub.Currency, lockedSC, ub.Locked, ub.Currency)
}

// satellitecontractscmd is the handler for the commands `satc satellite contracts all`
// and `satc satellite contracts [public_key]`. Prints the satellite contracts.
func satellitecontractscmd(key string) {
	if key == "all" {
		key = ""
	}
	contracts, err := httpClient.SatelliteContractsGet(key)
	if err != nil {
		die(err)
	}

	fmt.Printf(`Active:            %v
Passive:           %v
Refreshed:         %v
Disabled:          %v
Expired:           %v
Expired Refreshed: %v
`, len(contracts.ActiveContracts), len(contracts.PassiveContracts), len(contracts.RefreshedContracts), len(contracts.DisabledContracts), len(contracts.ExpiredContracts), len(contracts.ExpiredRefreshedContracts))

	if verbose {
		fmt.Println()
		fmt.Println("Active:")
		for n, c := range contracts.ActiveContracts {
			fmt.Printf(`%v. ID:           %v
  Renter:       %v
  Host:         %v
  Host Address: %v
  Host Version: %v
  Start Height: %v
  End Height:   %v

  Storage Spending:      %v
  Upload Spending:       %v
  Download Spending:     %v
  Fund Account Spending: %v
  Maintenance Spending:  %v
  Fees:                  %v
  Total Cost:            %v
  Remaining Funds:       %v

  Size:            %v
  Good For Upload: %v
  Good For Renew:  %v
  Bad Contract:    %v
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, modules.CurrencyUnits(c.StorageSpending), modules.CurrencyUnits(c.UploadSpending), modules.CurrencyUnits(c.DownloadSpending), modules.CurrencyUnits(c.FundAccountSpending), modules.CurrencyUnits(c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost)), modules.CurrencyUnits(c.Fees), modules.CurrencyUnits(c.TotalCost), modules.CurrencyUnits(c.RenterFunds), smodules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Passive:")
		for n, c := range contracts.PassiveContracts {
			fmt.Printf(`%v. ID:           %v
  Renter:       %v
  Host:         %v
  Host Address: %v
  Host Version: %v
  Start Height: %v
  End Height:   %v

  Storage Spending:      %v
  Upload Spending:       %v
  Download Spending:     %v
  Fund Account Spending: %v
  Maintenance Spending:  %v
  Fees:                  %v
  Total Cost:            %v
  Remaining Funds:       %v

  Size:            %v
  Good For Upload: %v
  Good For Renew:  %v
  Bad Contract:    %v
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, modules.CurrencyUnits(c.StorageSpending), modules.CurrencyUnits(c.UploadSpending), modules.CurrencyUnits(c.DownloadSpending), modules.CurrencyUnits(c.FundAccountSpending), modules.CurrencyUnits(c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost)), modules.CurrencyUnits(c.Fees), modules.CurrencyUnits(c.TotalCost), modules.CurrencyUnits(c.RenterFunds), smodules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Refreshed:")
		for n, c := range contracts.RefreshedContracts {
			fmt.Printf(`%v. ID:           %v
  Renter:       %v
  Host:         %v
  Host Address: %v
  Host Version: %v
  Start Height: %v
  End Height:   %v

  Storage Spending:      %v
  Upload Spending:       %v
  Download Spending:     %v
  Fund Account Spending: %v
  Maintenance Spending:  %v
  Fees:                  %v
  Total Cost:            %v
  Remaining Funds:       %v

  Size:            %v
  Good For Upload: %v
  Good For Renew:  %v
  Bad Contract:    %v
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, modules.CurrencyUnits(c.StorageSpending), modules.CurrencyUnits(c.UploadSpending), modules.CurrencyUnits(c.DownloadSpending), modules.CurrencyUnits(c.FundAccountSpending), modules.CurrencyUnits(c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost)), modules.CurrencyUnits(c.Fees), modules.CurrencyUnits(c.TotalCost), modules.CurrencyUnits(c.RenterFunds), smodules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Disabled:")
		for n, c := range contracts.DisabledContracts {
			fmt.Printf(`%v. ID:           %v
  Renter:       %v
  Host:         %v
  Host Address: %v
  Host Version: %v
  Start Height: %v
  End Height:   %v

  Storage Spending:      %v
  Upload Spending:       %v
  Download Spending:     %v
  Fund Account Spending: %v
  Maintenance Spending:  %v
  Fees:                  %v
  Total Cost:            %v
  Remaining Funds:       %v

  Size:            %v
  Good For Upload: %v
  Good For Renew:  %v
  Bad Contract:    %v
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, modules.CurrencyUnits(c.StorageSpending), modules.CurrencyUnits(c.UploadSpending), modules.CurrencyUnits(c.DownloadSpending), modules.CurrencyUnits(c.FundAccountSpending), modules.CurrencyUnits(c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost)), modules.CurrencyUnits(c.Fees), modules.CurrencyUnits(c.TotalCost), modules.CurrencyUnits(c.RenterFunds), smodules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Expired:")
		for n, c := range contracts.ExpiredContracts {
			fmt.Printf(`%v. ID:           %v
  Renter:       %v
  Host:         %v
  Host Address: %v
  Host Version: %v
  Start Height: %v
  End Height:   %v

  Storage Spending:      %v
  Upload Spending:       %v
  Download Spending:     %v
  Fund Account Spending: %v
  Maintenance Spending:  %v
  Fees:                  %v
  Total Cost:            %v
  Remaining Funds:       %v

  Size:            %v
  Good For Upload: %v
  Good For Renew:  %v
  Bad Contract:    %v
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, modules.CurrencyUnits(c.StorageSpending), modules.CurrencyUnits(c.UploadSpending), modules.CurrencyUnits(c.DownloadSpending), modules.CurrencyUnits(c.FundAccountSpending), modules.CurrencyUnits(c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost)), modules.CurrencyUnits(c.Fees), modules.CurrencyUnits(c.TotalCost), modules.CurrencyUnits(c.RenterFunds), smodules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Expired Refreshed:")
		for n, c := range contracts.ExpiredRefreshedContracts {
			fmt.Printf(`%v. ID:           %v
  Renter:       %v
  Host:         %v
  Host Address: %v
  Host Version: %v
  Start Height: %v
  End Height:   %v

  Storage Spending:      %v
  Upload Spending:       %v
  Download Spending:     %v
  Fund Account Spending: %v
  Maintenance Spending:  %v
  Fees:                  %v
  Total Cost:            %v
  Remaining Funds:       %v

  Size:            %v
  Good For Upload: %v
  Good For Renew:  %v
  Bad Contract:    %v
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, modules.CurrencyUnits(c.StorageSpending), modules.CurrencyUnits(c.UploadSpending), modules.CurrencyUnits(c.DownloadSpending), modules.CurrencyUnits(c.FundAccountSpending), modules.CurrencyUnits(c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost)), modules.CurrencyUnits(c.Fees), modules.CurrencyUnits(c.TotalCost), modules.CurrencyUnits(c.RenterFunds), smodules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}
	}
}
		
// satellitecmd is the handler for the command `satc satellite`.
// Prints the information about the satellite.
func satellitecmd() {
	renters, err := httpClient.SatelliteRentersGet()
	if err != nil {
		die(err)
	}
	contracts, err := httpClient.SatelliteContractsGet("")
	if err != nil {
		die(err)
	}

	fmt.Printf(`Renters:          %v
Active Contracts: %v
`, len(renters.Renters), len(contracts.ActiveContracts))
}
