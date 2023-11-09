package provider

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/core/types"
)

var (
	// Nil dependency errors.
	errNilDB      = errors.New("provider cannot use a nil database")
	errNilGateway = errors.New("provider cannot use nil gateway")
	errNilManager = errors.New("provider cannot use a nil manager")
)

// A Provider contains the information necessary to communicate with the
// renters.
type Provider struct {
	// Dependencies.
	db *sql.DB
	g  modules.Gateway
	m  modules.Manager

	autoAddress modules.NetAddress // Determined using automatic tooling in network.go
	publicKey   types.PublicKey
	secretKey   types.PrivateKey

	// Utilities.
	listener      net.Listener
	mux           net.Listener
	log           *persist.Logger
	mu            sync.RWMutex
	port          string
	tg            siasync.ThreadGroup
	staticAlerter *modules.GenericAlerter
}

// New returns an initialized Provider.
func New(db *sql.DB, g modules.Gateway, m modules.Manager, satelliteAddr string, muxAddr string, dir string) (*Provider, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the Provider object.
	p := &Provider{
		db:            db,
		g:             g,
		m:             m,
		staticAlerter: modules.NewAlerter("provider"),
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err = modules.PeekErr(errChan); err != nil {
			errChan <- modules.ComposeErrors(p.tg.Stop(), err)
		}
	}()

	// Create the logger.
	p.log, err = persist.NewFileLogger(filepath.Join(dir, "provider.log"))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Establish the closing of the logger.
	p.tg.AfterStop(func() {
		err := p.log.Close()
		if err != nil {
			// The logger may or may not be working here, so use a Println
			// instead.
			fmt.Println("Failed to close the provider logger:", err)
		}
	})
	p.log.Println("INFO: provider created, started logging")

	// Load the provider persistence.
	if loadErr := p.load(); loadErr != nil {
		errChan <- modules.AddContext(loadErr, "unable to load provider")
		return nil, errChan
	}

	// Initialize the networking.
	err = p.initNetworking(satelliteAddr, muxAddr)
	if err != nil {
		p.log.Println("ERROR: could not initialize provider networking:", err)
		errChan <- err
		return nil, errChan
	}

	return p, errChan
}

// Close shuts down the provider.
func (p *Provider) Close() error {
	if err := p.tg.Stop(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.save()
}

// PublicKey returns the provider's public key.
func (p *Provider) PublicKey() types.PublicKey {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.publicKey
}

// SecretKey returns the provider's secret key.
func (p *Provider) SecretKey() types.PrivateKey {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.secretKey
}

// enforce that Provider satisfies the modules.Provider interface.
var _ modules.Provider = (*Provider)(nil)
