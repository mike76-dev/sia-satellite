package node

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/siamux"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"github.com/mike76-dev/sia-satellite/portal"
	"github.com/mike76-dev/sia-satellite/satellite"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/modules/consensus"
	"go.sia.tech/siad/modules/gateway"
	"go.sia.tech/siad/modules/transactionpool"
	"go.sia.tech/siad/modules/wallet"
)

// Node represents a satellite node containing all required modules.
type Node struct {
	// The mux of the node.
	Mux    *siamux.SiaMux
	muxLog *os.File

	// The modules of the node.
	ConsensusSet    smodules.ConsensusSet
	Gateway         smodules.Gateway
	Portal          modules.Portal
	Satellite       modules.Satellite
	TransactionPool smodules.TransactionPool
	Wallet          smodules.Wallet

	// The high level directory where all the persistence gets stored for the
	// modules.
	Dir string
}

// Close will call close on every module within the node, combining and
// returning the errors.
func (n *Node) Close() (err error) {
	if n.Portal != nil {
		fmt.Println("Closing portal...")
		err = errors.Compose(err, n.Portal.Close())
	}
	if n.Satellite != nil {
		fmt.Println("Closing satellite...")
		err = errors.Compose(err, n.Satellite.Close())
	}
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
	if n.Mux != nil {
		fmt.Println("Closing siamux...")
		err = errors.Compose(err, n.Mux.Close(), n.muxLog.Close())
	}
	return nil
}

// New will create a new node.
func New(config *persist.SatdConfig, dbPassword string, loadStartTime time.Time) (*Node, <-chan error) {
	// Make sure the path is an absolute one.
	d, err := filepath.Abs(config.Dir)
	errChan := make(chan error, 1)
	if err != nil {
		errChan <- err
		return nil, errChan
	}

	// Create the siamux.
	mux, muxLog, err := smodules.NewSiaMux(filepath.Join(d, "siamux"), d, config.SiamuxAddr, config.SiamuxWSAddr)
	if err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create siamux"))
		return nil, errChan
	}

	// Load gateway.
	fmt.Println("Loading gateway...")
	gatewayDir := filepath.Join(d, "gateway")
	if err := os.MkdirAll(gatewayDir, 0700); err != nil {
		return nil, errChan
	}
	g, err := gateway.New(config.GatewayAddr, config.Bootstrap, gatewayDir)
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
	cs, errChanCS := consensus.New(g, config.Bootstrap, consensusDir)
	if err := smodules.PeekErr(errChanCS); err != nil {
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

	// Load satellite.
	fmt.Println("Loading satellite...")
	satDir := filepath.Join(d, "satellite")
	if err := os.MkdirAll(satDir, 0700); err != nil {
		return nil, errChan
	}
	s, err := satellite.New(cs, g, tp, w, config.SatelliteAddr, satDir)
	if err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create satellite"))
		return nil, errChan
	}

	// Load portal.
	fmt.Println("Loading portal...")
	portalDir := filepath.Join(d, "portal")
	if err := os.MkdirAll(portalDir, 0700); err != nil {
		return nil, errChan
	}
	p, err := portal.New(config, s, dbPassword, portalDir)
	if err != nil {
		errChan <- errors.Extend(err, errors.New("unable to create portal"))
		return nil, errChan
	}

	// Setup complete.
	fmt.Printf("API is now available, synchronous startup completed in %.3f seconds\n", time.Since(loadStartTime).Seconds())
	go func() {
		close(errChan)
	}()

	return &Node{
		Mux:    mux,
		muxLog: muxLog,

		ConsensusSet:    cs,
		Gateway:         g,
		Portal:          p,
		Satellite:       s,
		TransactionPool: tp,
		Wallet:          w,

		Dir: d,
	}, errChan
}
