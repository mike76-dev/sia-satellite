package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
	"github.com/spf13/cobra"
	"go.sia.tech/core/types"
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
		Use:   "balance [public_key]",
		Short: "Print the renter balance",
		Long:  "Print the balance of the renter with the given public key.",
		Run:   wrap(managerbalancecmd),
	}

	managerContractsCmd = &cobra.Command{
		Use:   "contracts [public_key] | contracts all",
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

	managerAveragesCmd = &cobra.Command{
		Use:   "averages [currency]",
		Short: "Display host network averages",
		Long:  "Display the host network averages in the given currency. Both fiat currencies (as a 3-letter code) and SC are supported. Default is SC.",
		Run:   wrap(manageraveragescmd),
	}

	managerPreferencesCmd = &cobra.Command{
		Use:   "preferences",
		Short: "Display email preferences",
		Long:  "Display the chosen email preferences such as the email address and the warning threshold.",
		Run:   wrap(managerpreferencescmd),
	}

	managerSetPreferencesCmd = &cobra.Command{
		Use:   "setpreferences [email] [warning_threshold]",
		Short: "Change email preferences",
		Long:  "Set the email preferences such as the email address and the warning threshold. Pass 'none' as the email address to remove it.",
		Run:   wrap(managersetpreferencescmd),
	}

	managerPricesCmd = &cobra.Command{
		Use:   "prices",
		Short: "Display current prices",
		Long:  "Display the current prices.",
		Run:   wrap(managerpricescmd),
	}

	managerSetPricesCmd = &cobra.Command{
		Use:   "setprices [type] [payment_plan] [value]",
		Short: "Change current prices",
		Long: `Change the current prices. Following types are accepted:

formcontract:     fee for forming or renewing a single contract (fraction of the contract amount),
savemetadata:     fee for backing up a single slab (in SC),
retrievemetadata: fee for retrieving a single slab (in SC),
storemetadata:    fee for storing a single slab for one month (in SC),
storepartialdata: fee for storing one MiB (also incomplete) of partial slab data (in SC/month),
migrateslab:      fee for repairing a single slab (in SC),

For payment_plan, either 'prepayment' or 'invoicing' are accepted.`,
		Run: wrap(managersetpricescmd),
	}

	managerMaintenanceCmd = &cobra.Command{
		Use:   "maintenance",
		Short: "Perform maintenance actions",
		Long:  "Start or stop a satellite maintenance.",
		Run:   wrap(managermaintenancecmd),
	}

	managerMaintenanceStartCmd = &cobra.Command{
		Use:   "start",
		Short: "Start maintenance",
		Long:  "Start a satellite maintenance.",
		Run:   wrap(managermaintenancestartcmd),
	}

	managerMaintenanceStopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop maintenance",
		Long:  "Stop a running satellite maintenance.",
		Run:   wrap(managermaintenancestopcmd),
	}
)

// manageraveragescmd is the handler for the command `satc manager averages [currency]`.
// Displays the host network averages in the given currency.
func manageraveragescmd(currency string) {
	ha, err := httpClient.ManagerAverages(currency)
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
		fmt.Printf("Storage Price: \t\t%.4f %v\n", modules.Float64(ha.StoragePrice)*ha.Rate, currency)
		fmt.Printf("Collateral: \t\t%.4f %v\n", modules.Float64(ha.Collateral)*ha.Rate, currency)
		fmt.Printf("Download Price: \t%.4f %v\n", modules.Float64(ha.DownloadBandwidthPrice)*ha.Rate, currency)
		fmt.Printf("Upload Price: \t\t%.4f %v\n", modules.Float64(ha.UploadBandwidthPrice)*ha.Rate, currency)
		fmt.Printf("Contract Price: \t%.4f %v\n", modules.Float64(ha.ContractPrice)*ha.Rate, currency)
		fmt.Printf("Base RPC Price: \t%.4f %v\n", modules.Float64(ha.BaseRPCPrice)*ha.Rate, currency)
		fmt.Printf("Sector Access Price: \t%.4f %v\n", modules.Float64(ha.SectorAccessPrice)*ha.Rate, currency)
	}
}

// managerrenterscmd is the handler for the command `satc manager renters`.
// Prints the list of the renters.
func managerrenterscmd() {
	renters, err := httpClient.ManagerRenters()
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
	renter, err := httpClient.ManagerRenter(key)
	if err != nil {
		die(err)
	}
	var sk, ak string
	if renter.PrivateKey != nil {
		sk = hex.EncodeToString(renter.PrivateKey)
	}
	if renter.AccountKey != nil {
		ak = hex.EncodeToString(renter.AccountKey)
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
Account Key:          %v
Auto Renew Contracts: %v
Backup File Metadata: %v
Auto Repair Files:    %v
`, renter.PublicKey.String(), renter.Email, renter.CurrentPeriod, renter.Allowance.Period, renter.Allowance.RenewWindow, renter.Allowance.Hosts, modules.FilesizeUnits(renter.Allowance.ExpectedStorage), modules.FilesizeUnits(renter.Allowance.ExpectedUpload), modules.FilesizeUnits(renter.Allowance.ExpectedDownload),
		renter.Allowance.MinShards, renter.Allowance.TotalShards, renter.Allowance.MaxRPCPrice, renter.Allowance.MaxContractPrice, renter.Allowance.MaxDownloadBandwidthPrice, renter.Allowance.MaxSectorAccessPrice, renter.Allowance.MaxStoragePrice, renter.Allowance.MaxUploadBandwidthPrice,
		renter.Allowance.MinMaxCollateral, renter.Allowance.BlockHeightLeeway, sk, ak, renter.Settings.AutoRenewContracts, renter.Settings.BackupFileMetadata, renter.Settings.AutoRepairFiles)
}

// managerbalancecmd is the handler for the command `satc manager balance [public_key]`.
// Prints the balance of the given renter.
func managerbalancecmd(key string) {
	ub, err := httpClient.ManagerBalance(key)
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
	contracts, err := httpClient.ManagerContracts(key)
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
			fmt.Printf(`%v. ID:          %v
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
`, n+1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Passive:")
		for n, c := range contracts.PassiveContracts {
			fmt.Printf(`%v. ID:          %v
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
`, n+1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Refreshed:")
		for n, c := range contracts.RefreshedContracts {
			fmt.Printf(`%v. ID:          %v
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
`, n+1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Disabled:")
		for n, c := range contracts.DisabledContracts {
			fmt.Printf(`%v. ID:          %v
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
`, n+1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Expired:")
		for n, c := range contracts.ExpiredContracts {
			fmt.Printf(`%v. ID:          %v
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
`, n+1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}

		fmt.Println()
		fmt.Println("Expired Refreshed:")
		for n, c := range contracts.ExpiredRefreshedContracts {
			fmt.Printf(`%v. ID:          %v
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
`, n+1, c.ID.String(), c.RenterPublicKey.String(), c.HostPublicKey.String(), c.NetAddress, c.HostVersion, c.StartHeight, c.EndHeight, c.StorageSpending, c.UploadSpending, c.DownloadSpending, c.FundAccountSpending, c.MaintenanceSpending.AccountBalanceCost.Add(c.MaintenanceSpending.FundAccountCost).Add(c.MaintenanceSpending.UpdatePriceTableCost), c.Fees, c.TotalCost, c.RenterFunds, modules.FilesizeUnits(c.Size), c.GoodForUpload, c.GoodForRenew, c.BadContract)
		}
	}
}

// managercmd is the handler for the command `satc manager`.
// Prints the information about the manager.
func managercmd() {
	renters, err := httpClient.ManagerRenters()
	if err != nil {
		die(err)
	}
	contracts, err := httpClient.ManagerContracts("")
	if err != nil {
		die(err)
	}
	maintenance, err := httpClient.ManagerMaintenance()
	if err != nil {
		die(err)
	}

	fmt.Printf(`Renters:          %v
Active Contracts: %v
Maintenance:      %v
`, len(renters.Renters), len(contracts.ActiveContracts), yesNo(maintenance))
}

// managerpreferencescmd is the handler for the command `satc manager preferences`.
// Prints the email preferences.
func managerpreferencescmd() {
	ep, err := httpClient.ManagerPreferences()
	if err != nil {
		die(err)
	}

	if ep.Email == "" {
		ep.Email = "Not set"
	}
	fmt.Printf(`Email:             %v
Warning Threshold: %v
`, ep.Email, ep.WarnThreshold)
}

// managersetpreferencescmd is the handler for the command
// `satc manager setpreferences [email] [warning_threshold]`.
// Changes the email preferences.
func managersetpreferencescmd(email, wt string) {
	if email == "none" {
		email = ""
	}
	threshold, err := types.ParseCurrency(wt)
	if err != nil {
		die(err)
	}

	err = httpClient.ManagerUpdatePreferences(api.EmailPreferences{
		Email:         email,
		WarnThreshold: threshold,
	})
	if err != nil {
		die(err)
	}

	fmt.Println("Email preferences updated")
}

// managerpricescmd is the handler for the command `satc manager prices`.
// Prints the current prices.
func managerpricescmd() {
	prices, err := httpClient.ManagerPrices()
	if err != nil {
		die(err)
	}

	fmt.Printf(`Current Prices:
Form or Renew Contract (fraction of the contract amount):
  Pre-Payment:    %v
  Invoicing:      %v
Backup a Slab (in SC):
  Pre-Payment:    %v
  Invoicing:      %v
Restore a Slab (in SC):
  Pre-Payment:    %v
  Invoicing:      %v
Store a Slab (in SC per month):
  Pre-Payment:    %v
  Invoicing:      %v
Store Partial Slab Data (in SC per MiB per month):
  Pre-Payment:    %v
  Invoicing:      %v
Repair a Slab (in SC):
  Pre-Payment:    %v
  Invoicing:      %v
`,
		prices.FormContract.PrePayment,
		prices.FormContract.Invoicing,
		prices.SaveMetadata.PrePayment,
		prices.SaveMetadata.Invoicing,
		prices.RetrieveMetadata.PrePayment,
		prices.RetrieveMetadata.Invoicing,
		prices.StoreMetadata.PrePayment,
		prices.StoreMetadata.Invoicing,
		prices.StorePartialData.PrePayment,
		prices.StorePartialData.Invoicing,
		prices.MigrateSlab.PrePayment,
		prices.MigrateSlab.Invoicing,
	)
}

// managersetpricescmd is the handler for the command
// `satc manager setprices [type] [payment_plan] [value]`.
// Changes the current prices.
func managersetpricescmd(typ, plan, v string) {
	prices, err := httpClient.ManagerPrices()
	if err != nil {
		die(err)
	}

	if plan != "prepayment" && plan != "invoicing" {
		die(errors.New("invalid payment plan"))
	}

	value, err := strconv.ParseFloat(v, 64)
	if err != nil {
		die(err)
	}

	switch typ {
	case "formcontract":
		if plan == "prepayment" {
			prices.FormContract.PrePayment = value
		} else {
			prices.FormContract.Invoicing = value
		}
	case "savemetadata":
		if plan == "prepayment" {
			prices.SaveMetadata.PrePayment = value
		} else {
			prices.SaveMetadata.Invoicing = value
		}
	case "retrievemetadata":
		if plan == "prepayment" {
			prices.RetrieveMetadata.PrePayment = value
		} else {
			prices.RetrieveMetadata.Invoicing = value
		}
	case "storemetadata":
		if plan == "prepayment" {
			prices.StoreMetadata.PrePayment = value
		} else {
			prices.StoreMetadata.Invoicing = value
		}
	case "storepartialdata":
		if plan == "prepayment" {
			prices.StorePartialData.PrePayment = value
		} else {
			prices.StorePartialData.Invoicing = value
		}
	case "migrateslab":
		if plan == "prepayment" {
			prices.MigrateSlab.PrePayment = value
		} else {
			prices.MigrateSlab.Invoicing = value
		}
	default:
		die(errors.New("invalid price type"))
	}

	err = httpClient.ManagerUpdatePrices(prices)
	if err != nil {
		die(err)
	}

	fmt.Println("Prices updated successfully")
}

// managermaintenancecmd is the handler for the command `satc manager maintenance`.
// Prints the information about the satellite maintenance.
func managermaintenancecmd() {
	maintenance, err := httpClient.ManagerMaintenance()
	if err != nil {
		die(err)
	}

	fmt.Println("Maintenance:", yesNo(maintenance))
}

// managermaintenancestartcmd is the handler for the command
// `satc manager maintenance start`.
// Starts a satellite maintenance.
func managermaintenancestartcmd() {
	err := httpClient.ManagerSetMaintenance(true)
	if err != nil {
		die(err)
	}

	fmt.Println("Maintenance started")
}

// managermaintenancestopcmd is the handler for the command
// `satc manager maintenance stop`.
// Stops a running satellite maintenance.
func managermaintenancestopcmd() {
	err := httpClient.ManagerSetMaintenance(false)
	if err != nil {
		die(err)
	}

	fmt.Println("Maintenance stopped")
}
