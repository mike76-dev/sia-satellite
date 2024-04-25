package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.sia.tech/core/consensus"
	"go.sia.tech/coreutils/chain"
)

var (
	consensusCmd = &cobra.Command{
		Use:   "consensus",
		Short: "Print the current state of consensus",
		Long:  "Print the current state of consensus such as current block and block height.",
		Run:   wrap(consensuscmd),
	}
)

// consensuscmd is the handler for the command `satc consensus`.
// Prints the current state of consensus.
func consensuscmd() {
	tip, err := httpClient.ConsensusTip()
	if err != nil {
		die("Could not get current consensus state:", err)
	}

	if tip.Synced {
		fmt.Printf(`Synced: %v
Block:      %v
Height:     %v
`, yesNo(tip.Synced), tip.BlockID, tip.Height)
	} else {
		_, genesisBlock := chain.Mainnet()
		estimatedHeight := (time.Now().Unix() - genesisBlock.Timestamp.Unix()) / int64(consensus.State{}.BlockInterval().Seconds())
		estimatedProgress := float64(tip.Height) / float64(estimatedHeight) * 100
		if estimatedProgress > 100 {
			estimatedProgress = 99.9
		}
		fmt.Printf(`Synced: %v
Height: %v
Progress (estimated): %.1f%%
`, yesNo(tip.Synced), tip.Height, estimatedProgress)
	}
}
