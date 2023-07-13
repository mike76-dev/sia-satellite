package main

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/spf13/cobra"
)

var (
	managerRentersCmd = &cobra.Command{
		Use:   "renters",
		Short: "Print the list of the renters",
		Long:  "Print the list of the current renters.",
		Run:   wrap(managerrenterscmd),
	}

	managerRenterCmd = &cobra.Command{
		Use:   "renter [public_key]",
		Short: "Print the renter settings",
		Long:  "Print the settings of the renter with the given public key.",
		Run:   wrap(managerrentercmd),
	}

	managerBalanceCmd = &cobra.Command{
		Use:   "balance [public_key}",
		Short: "Print the renter balance",
		Long:  "Print the balance of the renter with the given public key.",
		Run:   wrap(managerbalancecmd),
	}

	managerContractsCmd = &cobra.Command{
		Use:   "contracts [public_key] or contracts all",
		Short: "Print the list of the contracts",
		Long:  "Print the list of the contracts. A renter public key may be provided.",
		Run:   wrap(managercontractscmd),
	}

	managerCmd = &cobra.Command{
		Use:   "manager",
		Short: "Perform manager actions",
		Long:  "Perform manager actions.",
		Run:   wrap(managercmd),
	}

	managerRateCmd = &cobra.Command{
		Use:   "rate [currency]",
		Short: "Display exchange rate",
		Long:  "Display the exchange rate of the given currency. Both fiat currencies (as a 3-letter code) and SC are supported.",
		Run:   wrap(managerratecmd),
	}

	managerAveragesCmd = &cobra.Command{
		Use:   "averages [currency]",
		Short: "Display host network averages",
		Long:  "Display the host network averages in the given currency. Both fiat currencies (as a 3-letter code) and SC are supported. Default is SC.",
		Run:   wrap(manageraveragescmd),
	}
)

// managerratecmd is the handler for the command `satc manager rate [currency]`.
// Displays the exchange rate of the given currency.
func managerratecmd(currency string) {
	er, err := httpClient.ManagerRateGet(currency)
	if err != nil {
		die(err)
	}

	if er.Currency == "SC" {
		fmt.Printf("1 %v = %v USD\n", er.Currency, er.Rate)
	} else {
		fmt.Printf("1 USD = %v %v\n", er.Rate, er.Currency)
	}
}

// manageraveragescmd is the handler for the command `satc manager averages [currency]`.
// Displays the host network averages in the given currency.
func manageraveragescmd(currency string) {
	ha, err := httpClient.ManagerAveragesGet(currency)
	if err != nil {
		die(err)
	}

	currency = strings.ToUpper(currency)
	fmt.Printf("Number of Hosts: \t%v\n", ha.NumHosts)
	fmt.Printf("Contract Duration: \t%v\n", ha.Duration)
	if ha.Rate == 1.0 { // SC
		fmt.Printf("Storage Price: \t\t%v\n", ha.StoragePrice)
		fmt.Printf("Collateral: \t\t%v\n", ha.Collateral)
		fmt.Printf("Download Price: \t%v\n", ha.DownloadBandwidthPrice)
		fmt.Printf("Upload Price: \t\t%v\n", ha.UploadBandwidthPrice)
		fmt.Printf("Contract Price: \t%v\n", ha.ContractPrice)
		fmt.Printf("Base RPC Price: \t%v\n", ha.BaseRPCPrice)
		fmt.Printf("Sector Access Price: \t%v\n", ha.SectorAccessPrice)
	} else {
		fmt.Printf("Storage Price: \t\t%.4f %v\n", modules.Float64(ha.StoragePrice) * ha.Rate, currency)
		fmt.Printf("Collateral: \t\t%.4f %v\n", modules.Float64(ha.Collateral) * ha.Rate, currency)
		fmt.Printf("Download Price: \t%.4f %v\n", modules.Float64(ha.DownloadBandwidthPrice) * ha.Rate, currency)
		fmt.Printf("Upload Price: \t\t%.4f %v\n", modules.Float64(ha.UploadBandwidthPrice) * ha.Rate, currency)
		fmt.Printf("Contract Price: \t%.4f %v\n", modules.Float64(ha.ContractPrice) * ha.Rate, currency)
		fmt.Printf("Base RPC Price: \t%.4f %v\n", modules.Float64(ha.BaseRPCPrice) * ha.Rate, currency)
		fmt.Printf("Sector Access Price: \t%.4f %v\n", modules.Float64(ha.SectorAccessPrice) * ha.Rate, currency)
	}
}

// managerrenterscmd is the handler for the command `satc manager renters`.
// Prints the list of the renters.
func managerrenterscmd() {
	renters, err := httpClient.ManagerRentersGet()
	if err != nil {
		die(err)
	}

	if len(renters.Renters) == 0 {
		fmt.Println("No renters")
		return
	}

	for _, renter := range renters.Renters {
		fmt.Printf("%v %v\n", renter.PublicKey.String(), renter.Email)
	}
}

// managerrentercmd is the handler for the command `satc manager renter [public_key]`.
// Prints the settings of the given renter.
func managerrentercmd(key string) {
	renter, err := httpClient.ManagerRenterGet(key)
	if err != nil {
		die(err)
	}
	var sk string
	if renter.PrivateKey != nil {
		sk = hex.EncodeToString(renter.PrivateKey)
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

Private Key:          %v
Auto Renew Contracts: %v
`, renter.PublicKey.String(), renter.Email, renter.CurrentPeriod, renter.Allowance.Period, renter.Allowance.RenewWindow, renter.Allowance.Hosts, modules.FilesizeUnits(renter.Allowance.ExpectedStorage), modules.FilesizeUnits(renter.Allowance.ExpectedUpload), modules.FilesizeUnits(renter.Allowance.ExpectedDownload),
renter.Allowance.MinShards, renter.Allowance.TotalShards, renter.Allowance.MaxRPCPrice, renter.Allowance.MaxContractPrice, renter.Allowance.MaxDownloadBandwidthPrice, renter.Allowance.MaxSectorAccessPrice, renter.Allowance.MaxStoragePrice, renter.Allowance.MaxUploadBandwidthPrice,
renter.Allowance.MinMaxCollateral, renter.Allowance.BlockHeightLeeway, sk, renter.Settings.AutoRenewContracts)
}

// managerbalancecmd is the handler for the command `satc manager balance [public_key]`.
// Prints the balance of the given renter.
func managerbalancecmd(key string) {
	ub, err := httpClient.ManagerBalanceGet(key)
	if err != nil {
		die(err)
	}

	fmt.Printf(`Is User:    %v
Subscribed: %v

Available Balance: %.4f SC
Locked Balance:    %.4f SC
`, ub.IsUser, ub.Subscribed, ub.Balance, ub.Locked)
}

// managercontractscmd is the handler for the commands `satc manager contracts all`
// and `satc manager contracts [public_key]`. Prints the Satellite contracts.
func managercontractscmd(key string) {
	if strings.ToLower(key) == "all" {
		key = ""
	}
	contracts, err := httpClient.ManagerContractsGet(key)
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
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
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
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
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
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
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
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
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
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
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
`, n + 1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}
	}
}
		
// managercmd is the handler for the command `satc manager`.
// Prints the information about the manager.
func managercmd() {
	renters, err := httpClient.ManagerRentersGet()
	if err != nil {
		die(err)
	}
	contracts, err := httpClient.ManagerContractsGet("")
	if err != nil {
		die(err)
	}

	fmt.Printf(`Renters:          %v
Active Contracts: %v
`, len(renters.Renters), len(contracts.ActiveContracts))
}
