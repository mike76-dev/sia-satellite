package main

import (
	"fmt"

	//"github.com/mike76-dev/sia-satellite/modules"
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
