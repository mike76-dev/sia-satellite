// provider package is the renter-facing part of the satellite module.
package provider

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/errors"

	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/persist"
	siasync "go.sia.tech/siad/sync"
)

// A Provider contains the information necessary to communicate with the
// renters.
type Provider struct {
	// Dependencies.
	g         smodules.Gateway
	satellite modules.ContractFormer

	autoAddress smodules.NetAddress // Determined using automatic tooling in network.go

	// Utilities.
	listener      net.Listener
	log           *persist.Logger
	mu            sync.RWMutex
	persist       persistence
	persistDir    string
	port          string
	threads       siasync.ThreadGroup
	staticAlerter *smodules.GenericAlerter
}

// New returns an initialized Provider.
func New(g smodules.Gateway, satelliteAddr string, persistDir string) (*Provider, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the Provider object.
	p := &Provider{
		g:             g,
		persistDir:    persistDir,
		staticAlerter: smodules.NewAlerter("provider"),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err = smodules.PeekErr(errChan); err != nil {
			errChan <- errors.Compose(p.threads.Stop(), err)
		}
	}()

	// Create the logger.
	p.log, err = persist.NewFileLogger(filepath.Join(persistDir, logFile))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Establish the closing of the logger.
	p.threads.AfterStop(func() {
		err := p.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the provider logger:", err)
		}
	})
	p.log.Println("INFO: provider created, started logging")

	// Load the provider persistence.
	if loadErr := p.load(); loadErr != nil && !os.IsNotExist(loadErr) {
		errChan <- errors.AddContext(loadErr, "unable to load provider")
		return nil, errChan
	}

	// Make sure that the provider saves on shutdown.
	p.threads.AfterStop(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		err := p.saveSync()
		if err != nil {
			p.log.Println("ERROR: Unable to save provider:", err)
		}
	})

	// Initialize the networking.
	err = p.initNetworking(satelliteAddr)
	if err != nil {
		p.log.Println("Could not initialize provider networking:", err)
		errChan <- err
		return nil, errChan
	}

	return p, errChan
}

// Close shuts down the provider.
func (p *Provider) Close() error {
	if err := p.threads.Stop(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveSync()
}

// SetSatellite sets the satellite dependency of the Provider.
func (p *Provider) SetSatellite(cf modules.ContractFormer) {
	p.satellite = cf
}
