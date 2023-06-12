package node

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/consensus"
	"github.com/mike76-dev/sia-satellite/modules/gateway"
	"github.com/mike76-dev/sia-satellite/persist"
	//"github.com/mike76-dev/sia-satellite/portal"
	//"github.com/mike76-dev/sia-satellite/satellite"

	//"go.sia.tech/siad/modules/transactionpool"
	//"go.sia.tech/siad/modules/wallet"
)

// Node represents a satellite node containing all required modules.
type Node struct {
	// MySQL database.
	DB         *sql.DB

	// The modules of the node.
	ConsensusSet    modules.ConsensusSet
	Gateway         modules.Gateway
	//Portal          modules.Portal
	//Satellite       modules.Satellite
	//TransactionPool smodules.TransactionPool
	//Wallet          smodules.Wallet

	// The high level directory where all the persistence gets stored for the
	// modules.
	Dir string
}

// Close will call close on every module within the node, combining and
// returning the errors.
func (n *Node) Close() (err error) {
	/*if n.Portal != nil {
		fmt.Println("Closing portal...")
		err = modules.ComposeErrors(err, n.Portal.Close())
	}
	if n.Satellite != nil {
		fmt.Println("Closing satellite...")
		err = modules.ComposeErrors(err, n.Satellite.Close())
	}
	if n.Wallet != nil {
		fmt.Println("Closing wallet...")
		err = modules.ComposeErrors(err, n.Wallet.Close())
	}
	if n.TransactionPool != nil {
		fmt.Println("Closing transaction pool...")
		err = modules.ComposeErrors(err, n.TransactionPool.Close())
	}*/
	if n.ConsensusSet != nil {
		fmt.Println("Closing consensus...")
		err = modules.ComposeErrors(err, n.ConsensusSet.Close())
	}
	if n.Gateway != nil {
		fmt.Println("Closing gateway...")
		err = modules.ComposeErrors(err, n.Gateway.Close())
	}
	if n.DB != nil {
		fmt.Println("Closing database...")
		err = modules.ComposeErrors(err, n.DB.Close())
	}
	return err
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

	// Connect to the database.
	fmt.Println("Connecting to the SQL database...")
	cfg := mysql.Config {
		User:                 config.DBUser,
		Passwd:               dbPassword,
		Net:                  "tcp",
		Addr:                 "127.0.0.1:3306",
		DBName:               config.DBName,
		AllowNativePasswords: true,
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatalf("Could not connect to the database: %v\n", err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatalf("MySQL database not responding: %v\n", err)
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	// Load gateway.
	fmt.Println("Loading gateway...")
	g, err := gateway.New(db, config.GatewayAddr, config.Bootstrap, true, d)
	if err != nil {
		errChan <- modules.AddContext(err, "unable to create gateway")
		return nil, errChan
	}

	// Load consensus.
	fmt.Println("Loading consensus...")
	cs, errChanCS := consensus.New(db, g, config.Bootstrap, d)
	if err := modules.PeekErr(errChanCS); err != nil {
		errChan <- modules.AddContext(err, "unable to create consensus set")
		return nil, errChan
	}

	// Load transaction pool.
	/*fmt.Println("Loading transaction pool...")
	tpoolDir := filepath.Join(d, "transactionpool")
	if err := os.MkdirAll(tpoolDir, 0700); err != nil {
		return nil, errChan
	}
	tp, err := transactionpool.New(cs, g, tpoolDir)
	if err != nil {
		errChan <- modules.AddContext(err, "unable to create transaction pool")
		return nil, errChan
	}*/

	// Load wallet.
	/*fmt.Println("Loading wallet...")
	walletDir := filepath.Join(d, "wallet")
	if err := os.MkdirAll(walletDir, 0700); err != nil {
		return nil, errChan
	}
	w, err := wallet.New(cs, tp, walletDir)
	if err != nil {
		errChan <- modules.AddContext(err, "unable to create wallet")
		return nil, errChan
	}*/

	// Load satellite.
	/*fmt.Println("Loading satellite...")
	satDir := filepath.Join(d, "satellite")
	if err := os.MkdirAll(satDir, 0700); err != nil {
		return nil, errChan
	}
	s, errChanS := satellite.New(cs, g, tp, w, db, config.SatelliteAddr, satDir)
	if err := modules.PeekErr(errChanS); err != nil {
		errChan <- modules.AddContext(err, "unable to create satellite")
		return nil, errChan
	}*/

	// Load portal.
	/*fmt.Println("Loading portal...")
	portalDir := filepath.Join(d, "portal")
	if err := os.MkdirAll(portalDir, 0700); err != nil {
		return nil, errChan
	}
	p, err := portal.New(config, s, db, portalDir)
	if err != nil {
		errChan <- modules.AddContext(err, "unable to create portal")
		return nil, errChan
	}*/

	// Setup complete.
	fmt.Printf("API is now available, synchronous startup completed in %.3f seconds\n", time.Since(loadStartTime).Seconds())
	go func() {
		close(errChan)
	}()

	return &Node{
		DB: db,

		ConsensusSet:    cs,
		Gateway:         g,
		//Portal:          p,
		//Satellite:       s,
		//TransactionPool: tp,
		//Wallet:          w,

		Dir: d,
	}, errChan
}
