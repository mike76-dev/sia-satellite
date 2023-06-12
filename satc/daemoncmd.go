package main

import (
	"fmt"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/spf13/cobra"

	"go.sia.tech/siad/build"
)

var (
	alertsCmd = &cobra.Command{
		Use:   "alerts",
		Short: "view daemon alerts",
		Long:  "view daemon alerts",
		Run:   wrap(alertscmd),
	}

	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the Sia daemon",
		Long:  "Stop the Sia daemon.",
		Run:   wrap(stopcmd),
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print version information.",
		Run:   wrap(versioncmd),
	}
)

// alertscmd prints the alerts from the daemon. This will not print critical
// alerts as critical alerts are printed on every satc command.
func alertscmd() {
	const maxAlerts = 1000

	al, err := httpClient.DaemonAlertsGet()
	if err != nil {
		fmt.Println("Could not get daemon alerts:", err)
		return
	}
	if len(al.Alerts) == 0 {
		fmt.Println("There are no alerts registered.")
		return
	}
	if len(al.Alerts) == len(al.CriticalAlerts) {
		// Return since critical alerts are already displayed.
		return
	}

	remaining := maxAlerts
	for sev := modules.AlertSeverity(modules.SeverityError); sev >= modules.SeverityInfo; sev-- {
		if remaining <= 0 {
			return
		}

		var alerts []modules.Alert
		switch sev {
		case modules.SeverityError:
			alerts = al.ErrorAlerts
		case modules.SeverityWarning:
			alerts = al.WarningAlerts
		case modules.SeverityInfo:
			alerts = al.InfoAlerts
		}

		n := len(alerts)
		if n > remaining {
			n = remaining
		}

		remaining -= n
		printAlerts(alerts[:n], sev)
	}

	if len(al.Alerts) > maxAlerts {
		fmt.Printf("Only %v/%v alerts are displayed.\n", maxAlerts, len(al.Alerts))
	}
}

// version prints the version of satc and satd.
func versioncmd() {
	fmt.Println("Satellite Client")
	fmt.Println("\tVersion " + build.NodeVersion)
	if build.GitRevision != "" {
		fmt.Println("\tGit Revision " + build.GitRevision)
		fmt.Println("\tBuild Time   " + build.BuildTime)
	}
	dvg, err := httpClient.DaemonVersionGet()
	if err != nil {
		fmt.Println("Could not get daemon version:", err)
		return
	}
	fmt.Println("Satellite Daemon")
	fmt.Println("\tVersion " + dvg.Version)
	if dvg.GitRevision != "" {
		fmt.Println("\tGit Revision " + dvg.GitRevision)
		fmt.Println("\tBuild Time   " + dvg.BuildTime)
	}
}

// stopcmd is the handler for the command `satc stop`.
// Stops the daemon.
func stopcmd() {
	err := httpClient.DaemonStopGet()
	if err != nil {
		die("Could not stop daemon:", err)
	}
	fmt.Println("Satellite daemon stopped.")
}

// printAlerts is a helper function to print details of a slice of alerts
// with given severity description to command line
func printAlerts(alerts []modules.Alert, as modules.AlertSeverity) {
	fmt.Printf("\n  There are %v %s alerts\n", len(alerts), as.String())
	for _, a := range alerts {
		fmt.Printf(`
------------------
  Module:   %s
  Severity: %s
  Message:  %s
  Cause:    %s`, a.Module, a.Severity.String(), a.Msg, a.Cause)
	}
	fmt.Printf("\n------------------\n\n")
}
