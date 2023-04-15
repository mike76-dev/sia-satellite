package main

import (
	"fmt"
	"strconv"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/spf13/cobra"
)

var (
	portalCmd = &cobra.Command{
		Use:   "portal",
		Short: "Print the credit information",
		Long:  "Print the information about any running promo action.",
		Run:   wrap(portalcmd),
	}

	portalSetCmd = &cobra.Command{
		Use:   "set [number] [amount]",
		Short: "Set the credit information",
		Long:  "Start, change, or cancel a promo action.",
		Run:   wrap(portalsetcmd),
	}
)

// portalcmd is the handler for the command `satc portal`.
// Prints the credit information.
func portalcmd() {
	credits, err := httpClient.PortalCreditsGet()
	if err != nil {
		die(err)
	}

	fmt.Printf(`Amount:    %v USD
Remaining: %v credits
`, credits.Amount, credits.Remaining)
}

// portalsetcmd is the handler for the command `satc portal set
// [number] [amount]`. Sets the credit information.
func portalsetcmd(num, amt string) {
	number, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		fmt.Println("Could not parse number: ", err)
		die()
	}

	var amount float64
	if number > 0 {
		amount, err = strconv.ParseFloat(amt, 64)
		if err != nil {
			fmt.Println("Could not parse amount: ", err)
			die()
		}
	}

	credits := modules.CreditData{
		Amount:    amount,
		Remaining: number,
	}

	err = httpClient.PortalCreditsPost(credits)
	if err != nil {
		fmt.Println("Could not set credit information: ", err)
		die()
	}
	fmt.Println("Successfully updated credit information")
}
