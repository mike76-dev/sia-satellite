package main

import (
	"fmt"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/spf13/cobra"
)

var (
	managerCmd = &cobra.Command{
		Use:   "manager",
		Short: "Perform manager actions",
		Long:  "Perform manager actions.",
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
