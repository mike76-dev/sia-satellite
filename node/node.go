package node

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mike76-dev/sia-satellite/mail"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager"
	"github.com/mike76-dev/sia-satellite/modules/portal"
	"github.com/mike76-dev/sia-satellite/modules/provider"
	"github.com/mike76-dev/sia-satellite/modules/syncer"
	"github.com/mike76-dev/sia-satellite/modules/wallet"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/coreutils"
	"go.sia.tech/coreutils/chain"
)

// Node represents a satellite node containing all required modules.
type Node struct {
	// Databases.
	db  *sql.DB
	bdb *coreutils.BoltChainDB

	// The modules of the node.
	ChainManager *chain.Manager
	Syncer       modules.Syncer
	Manager      modules.Manager
	Portal       modules.Portal
	Provider     modules.Provider
	Wallet       modules.Wallet

	// The start function.
	Start func() (stop func())
}

// Close will call close on every module within the node, combining and
// returning the errors.
func (n *Node) Close() (err error) {
	if n.Portal != nil {
		fmt.Println("Closing portal...")
		err = modules.ComposeErrors(err, n.Portal.Close())
	}
	if n.Provider != nil {
		fmt.Println("Closing provider...")
		err = modules.ComposeErrors(err, n.Provider.Close())
	}
	if n.Manager != nil {
		fmt.Println("Closing manager...")
		err = modules.ComposeErrors(err, n.Manager.Close())
	}
	if n.Wallet != nil {
		fmt.Println("Closing wallet...")
		err = modules.ComposeErrors(err, n.Wallet.Close())
	}
	if n.Syncer != nil {
		fmt.Println("Closing syncer...")
		err = modules.ComposeErrors(err, n.Syncer.Close())
	}
	if n.db != nil && n.bdb != nil {
		fmt.Println("Closing databases...")
		err = modules.ComposeErrors(err, n.db.Close(), n.bdb.Close())
	}
	return err
}

// New will create a new node.
func New(config *persist.SatdConfig, dbPassword, seed string, loadStartTime time.Time) (*Node, error) {
	// Make sure the path is an absolute one.
	d, err := filepath.Abs(config.Dir)
	if err != nil {
		return nil, err
	}

	// Create a mail client.
	fmt.Println("Creating mail client...")
	ms, err := mail.New(d)
	if err != nil {
		log.Fatalf("ERROR: could not create mail client: %v\n", err)
	}

	// Connect to the MySQL database.
	fmt.Println("Connecting to the SQL database...")
	cfg := mysql.Config{
		User:                 config.DBUser,
		Passwd:               dbPassword,
		Net:                  "tcp",
		Addr:                 "127.0.0.1:3306",
		DBName:               config.DBName,
		AllowNativePasswords: true,
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatalf("Could not connect to MySQL database: %v\n", err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatalf("MySQL database not responding: %v\n", err)
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	// Connect to the BoltDB database.
	fmt.Println("Connecting to the BoltDB database...")
	bdb, err := coreutils.OpenBoltChainDB(filepath.Join(d, "consensus.db"))
	if err != nil {
		log.Fatalf("Could not connect to BoltDB database: %v\n", err)
	}

	// Create chain manager.
	fmt.Println("Loading chain manager...")
	network, genesisBlock := chain.Mainnet()
	dbstore, tipState, err := chain.NewDBStore(bdb, network, genesisBlock)
	if err != nil {
		log.Fatalf("Unable to create chain manager store: %v\n", err)
	}
	cm := chain.NewManager(dbstore, tipState)

	// Load syncer.
	fmt.Println("Loading syncer...")
	s, err := syncer.New(cm, config.GatewayAddr, d)
	if err != nil {
		log.Fatalf("Unable to create syncer: %v\n", err)
	}

	// Load wallet.
	fmt.Println("Loading wallet...")
	w, err := wallet.New(db, cm, s, seed, d)
	if err != nil {
		return nil, modules.AddContext(err, "unable to create wallet")
	}

	// Load manager.
	fmt.Println("Loading manager...")
	m, errChanM := manager.New(db, ms, cm, s, w, d, config.Name)
	if err := modules.PeekErr(errChanM); err != nil {
		return nil, modules.AddContext(err, "unable to create manager")
	}

	// Load provider.
	fmt.Println("Loading provider...")
	p, errChanP := provider.New(db, s, m, config.SatelliteAddr, config.MuxAddr, d)
	if err := modules.PeekErr(errChanP); err != nil {
		return nil, modules.AddContext(err, "unable to create provider")
	}

	// Load portal.
	fmt.Println("Loading portal...")
	pt, err := portal.New(config, db, ms, cm, w, m, p, d)
	if err != nil {
		return nil, modules.AddContext(err, "unable to create portal")
	}

	// Setup complete.
	fmt.Printf("API is now available, synchronous startup completed in %.3f seconds\n", time.Since(loadStartTime).Seconds())

	n := &Node{
		db:  db,
		bdb: bdb,

		ChainManager: cm,
		Syncer:       s,
		Manager:      m,
		Portal:       pt,
		Provider:     p,
		Wallet:       w,
	}

	n.Start = func() func() {
		ch := make(chan struct{})
		go func() {
			s.Run()
			close(ch)
		}()
		return func() {
			n.Close()
			<-ch
		}
	}

	return n, nil
}
