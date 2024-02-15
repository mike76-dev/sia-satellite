package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	syncerCmd = &cobra.Command{
		Use:   "syncer",
		Short: "Perform syncer actions",
		Long:  "View and manage the syncer's connected peers.",
		Run:   wrap(syncercmd),
	}

	syncerConnectCmd = &cobra.Command{
		Use:   "connect [address]",
		Short: "Connect to a peer",
		Long:  "Connect to a peer and add it to the node list.",
		Run:   wrap(syncerconnectcmd),
	}

	syncerPeersCmd = &cobra.Command{
		Use:   "peers",
		Short: "View a list of peers",
		Long:  "View the current peer list.",
		Run:   wrap(syncerpeerscmd),
	}
)

// syncerconnectcmd is the handler for the command `satc syncer connect [address]`.
// Adds a new peer to the peer list.
func syncerconnectcmd(addr string) {
	err := httpClient.SyncerConnect(addr)
	if err != nil {
		die("Could not add peer:", err)
	}
	fmt.Println("Added", addr, "to peer list.")
}

// syncercmd is the handler for the command `satc syncer`.
// Prints the number of peers.
func syncercmd() {
	peers, err := httpClient.SyncerPeers()
	if err != nil {
		die("Could not get syncer info:", err)
	}
	fmt.Println("Active peers:", len(peers))
}

// syncerpeerscmd is the handler for the command `satc syncer peers`.
// Prints a list of all peers.
func syncerpeerscmd() {
	peers, err := httpClient.SyncerPeers()
	if err != nil {
		die("Could not get peer list:", err)
	}
	if len(peers) == 0 {
		fmt.Println("No peers to show.")
		return
	}
	fmt.Println(len(peers), "active peers:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Version\tOutbound\tAddress")
	for _, peer := range peers {
		fmt.Fprintf(w, "%v\t%v\t%v\n", peer.Version, yesNo(!peer.Inbound), peer.Address)
	}
	if err := w.Flush(); err != nil {
		die("failed to flush writer")
	}
}
