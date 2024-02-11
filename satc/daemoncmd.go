package main

import (
	"fmt"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/spf13/cobra"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print version information.",
		Run:   wrap(versioncmd),
	}
)

// version prints the version of satc and satd.
func versioncmd() {
	fmt.Println("Satellite Client")
	fmt.Println("\tVersion " + build.NodeVersion)
	if build.GitRevision != "" {
		fmt.Println("\tGit Revision " + build.GitRevision)
		fmt.Println("\tBuild Time   " + build.BuildTime)
	}
	dvg, err := httpClient.DaemonVersion()
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
