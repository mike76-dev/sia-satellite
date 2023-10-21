package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/spf13/cobra"
)

var (
	portalCmd = &cobra.Command{
		Use:   "portal",
		Short: "Print the portal information",
		Long:  "Print the information related to the portal.",
		Run:   wrap(portalcmd),
	}

	portalCreditsCmd = &cobra.Command{
		Use:   "credits",
		Short: "Print the credit information",
		Long:  "Print the information about any running promo action.",
		Run:   wrap(portalcreditscmd),
	}

	portalCreditsSetCmd = &cobra.Command{
		Use:   "set [number] [amount]",
		Short: "Set the credit information",
		Long:  "Start, change, or cancel a promo action.",
		Run:   wrap(portalcreditssetcmd),
	}

	portalAnnouncementCmd = &cobra.Command{
		Use:   "announcement",
		Short: "Print the current announcement",
		Long:  "Print the current portal announcement.",
		Run:   wrap(portalannouncementcmd),
	}

	portalAnnouncementSetCmd = &cobra.Command{
		Use:   "set [path]",
		Short: "Set portal announcement",
		Long:  "Set a new portal announcement.",
		Run:   wrap(portalannouncementsetcmd),
	}

	portalAnnouncementRemoveCmd = &cobra.Command{
		Use:   "remove",
		Short: "Removes portal announcement",
		Long:  "Removes the current portal announcement.",
		Run:   wrap(portalannouncementremovecmd),
	}
)

// portalcmd is the handler for the command `satc portal`.
// Prints the portal information.
func portalcmd() {
	credits, err := httpClient.PortalCreditsGet()
	if err != nil {
		die(err)
	}
	a, err := httpClient.PortalAnnouncementGet()
	if err != nil {
		die(err)
	}
	if a == "" {
		a = "Announcement not set"
	} else {
		a = "Announcement set"
	}

	fmt.Printf(`Amount:    %v USD
Remaining: %v credits
%v
`, credits.Amount, credits.Remaining, a)
}

// portalcreditscmd is the handler for the command `satc portal credits`.
// Prints the credit information.
func portalcreditscmd() {
	credits, err := httpClient.PortalCreditsGet()
	if err != nil {
		die(err)
	}

	fmt.Printf(`Amount:    %v USD
Remaining: %v credits
`, credits.Amount, credits.Remaining)
}

// portalcreditssetcmd is the handler for the command `satc portal credits set
// [number] [amount]`. Sets the credit information.
func portalcreditssetcmd(num, amt string) {
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

// portalannouncementcmd is the handler for the command `satc portal announcement`.
// Prints the current portal announcement.
func portalannouncementcmd() {
	a, err := httpClient.PortalAnnouncementGet()
	if err != nil {
		die(err)
	}

	if a == "" {
		fmt.Println("Announcement not set")
		return
	}
	fmt.Println("Current Announcement:")
	fmt.Printf("%s\n", a)
}

// portalannouncementsetcmd is the handler for the command `satc portal announcement set
// [path]`. Sets a new portal announcement.
func portalannouncementsetcmd(path string) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		die(errors.New("file not found"))
	}
	if err != nil {
		die(err)
	}
	defer file.Close()
	b, err := io.ReadAll(file)
	if err != nil {
		die(err)
	}

	err = httpClient.PortalAnnouncementPost(string(b))
	if err != nil {
		die(err)
	}
	fmt.Println("Successfully posted the announcement")
}

// portalannouncementremovecmd is the handler for the command `satc portal announcement
// remove`. Clears the portal announcement.
func portalannouncementremovecmd() {
	err := httpClient.PortalAnnouncementPost("")
	if err != nil {
		die(err)
	}
	fmt.Println("Announcement removed")
}
