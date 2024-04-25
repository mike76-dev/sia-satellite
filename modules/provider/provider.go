package provider

import (
	"database/sql"
	"net"
	"path/filepath"
	"sync"

	siasync "github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.uber.org/zap"

	"go.sia.tech/core/types"
)

// A Provider contains the information necessary to communicate with the
// renters.
type Provider struct {
	// Dependencies.
	db *sql.DB
	s  modules.Syncer
	m  modules.Manager

	autoAddress modules.NetAddress // Determined using automatic tooling in network.go
	publicKey   types.PublicKey
	secretKey   types.PrivateKey

	// Utilities.
	listener net.Listener
	mux      net.Listener
	log      *zap.Logger
	mu       sync.RWMutex
	port     string
	tg       siasync.ThreadGroup
}

// New returns an initialized Provider.
func New(db *sql.DB, s modules.Syncer, m modules.Manager, satelliteAddr string, muxAddr string, dir string) (*Provider, <-chan error) {
	errChan := make(chan error, 1)
	var err error

	// Create the Provider object.
	p := &Provider{
		db: db,
		s:  s,
		m:  m,
	}

	// Call stop in the event of a partial startup.
	defer func() {
		if err = modules.PeekErr(errChan); err != nil {
			errChan <- modules.ComposeErrors(p.tg.Stop(), err)
		}
	}()

	// Create the logger.
	logger, closeFn, err := persist.NewFileLogger(filepath.Join(dir, "provider.log"))
	if err != nil {
		errChan <- err
		return nil, errChan
	}
	// Establish the closing of the logger.
	p.tg.AfterStop(func() {
		closeFn()
	})
	p.log = logger
	p.log.Info("provider created, started logging")

	// Load the provider persistence.
	if loadErr := p.load(); loadErr != nil {
		errChan <- modules.AddContext(loadErr, "unable to load provider")
		return nil, errChan
	}

	// Initialize the networking.
	err = p.initNetworking(satelliteAddr, muxAddr)
	if err != nil {
		p.log.Error("could not initialize provider networking", zap.Error(err))
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
