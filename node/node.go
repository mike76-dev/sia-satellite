package node

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/NebulousLabs/errors"

	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/modules/consensus"
	"go.sia.tech/siad/modules/gateway"
	"go.sia.tech/siad/modules/transactionpool"
	"go.sia.tech/siad/modules/wallet"
)

// Node represents a satellite node containing all required modules.
type Node struct {
	ConsensusSet		modules.ConsensusSet
	Gateway					modules.Gateway
	TransactionPool	modules.TransactionPool
	Wallet					modules.Wallet

	// The high level directory where all the persistence gets stored for the
	// modules.
	Dir string
}

// Close will call close on every module within the node, combining and
// returning the errors.
func (n *Node) Close() (err error) {
	if n.Wallet != nil {
		fmt.Println("Closing wallet...")
		err = errors.Compose(err, n.Wallet.Close())
	}
	if n.TransactionPool != nil {
		fmt.Println("Closing transaction pool...")
		err = errors.Compose(err, n.TransactionPool.Close())
	}
	if n.ConsensusSet != nil {
		fmt.Println("Closing consensus...")
		err = errors.Compose(err, n.ConsensusSet.Close())
	}
	if n.Gateway != nil {
		fmt.Println("Closing gateway...")
		err = errors.Compose(err, n.Gateway.Close())
	}
	return nil
}

// New will create a new node.
func New(gatewayAddr string, dir string, bootstrap bool, loadStartTime time.Time) (*Node, <-chan error) {
	// Make sure the path is an absolute one.
	d, err := filepath.Abs(dir)
	errChan := make(chan error, 1)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Load gateway.
	fmt.Println("Loading gateway...")
	gatewayDir := filepath.Join(d, "gateway")
	if err := os.MkdirAll(gatewayDir, 0700); err != nil {
		return nil, errChan
	}
	g, err := gateway.New(gatewayAddr, bootstrap, gatewayDir)
	if err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create gateway"))
		return nil, errChan
	}

	// Load consensus.
	fmt.Println("Loading consensus...")
	consensusDir := filepath.Join(d, "consensus")
	if err := os.MkdirAll(consensusDir, 0700); err != nil {
		return nil, errChan
	}
	cs, errChanCS := consensus.New(g, bootstrap, consensusDir)
	if err := modules.PeekErr(errChanCS); err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create consensus set"))
		return nil, errChan
	}

	// Load transaction pool.
	fmt.Println("Loading transaction pool...")
	tpoolDir := filepath.Join(d, "transactionpool")
	if err := os.MkdirAll(tpoolDir, 0700); err != nil {
		return nil, errChan
	}
	tp, err := transactionpool.New(cs, g, tpoolDir)
	if err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create transaction pool"))
		return nil, errChan
	}

	// Load wallet.
	fmt.Println("Loading wallet...")
	walletDir := filepath.Join(d, "wallet")
	if err := os.MkdirAll(walletDir, 0700); err != nil {
		return nil, errChan
	}
	w, err := wallet.New(cs, tp, walletDir)
	if err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create wallet"))
		return nil, errChan
	}


	// Setup complete.
	fmt.Printf("API is now available, synchronous startup completed in %.3f seconds\n", time.Since(loadStartTime).Seconds())
	go func() {
		close(errChan)
	}()

	return &Node{
		ConsensusSet:			cs,
		Gateway:					g,
		TransactionPool:	tp,
		Wallet:						w,

		Dir:							d,
	}, errChan
}
