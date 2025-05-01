package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/mike76-dev/sia-satellite/account"
	public "github.com/mike76-dev/sia-satellite/api/public"
	"github.com/mike76-dev/sia-satellite/hostdb"
	"github.com/mike76-dev/sia-satellite/internal/syncerutil"
	"github.com/mike76-dev/sia-satellite/mail"
	"github.com/mike76-dev/sia-satellite/persist"
	"github.com/mike76-dev/sia-satellite/wallet"
	"go.sia.tech/core/consensus"
	"go.sia.tech/core/gateway"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/syncer"
	"go.uber.org/zap/zapcore"
)

type node struct {
	chain    *chain.Manager
	syncer   *syncer.Syncer
	wallet   *wallet.Wallet
	hostDB   *hostdb.HostDB
	accounts *account.AccountManager

	Start func() (stop func())
}

// newNode created a new satd node.
func newNode(config *persist.SatdConfig, dbPassword, seed string) *node {
	// Make sure the path is an absolute one.
	dir, err := filepath.Abs(config.Dir)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", config.Dir)
	}

	// Create the state directory if it does not yet exist.
	// This also checks if the provided directory parameter is valid.
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", dir)
	}

	// Extract network parameters.
	var network *consensus.Network
	var genesisBlock types.Block
	var bootstrap []string
	if config.Test {
		network, genesisBlock = chain.TestnetZen()
		bootstrap = syncer.ZenBootstrapPeers
	} else {
		network, genesisBlock = chain.Mainnet()
		bootstrap = syncer.MainnetBootstrapPeers
	}

	// Initialize consensus.
	bdb, err := coreutils.OpenBoltChainDB(filepath.Join(dir, "consensus.db"))
	if err != nil {
		log.Fatalf("Could not initialize consensus: %v\n", err)
	}

	cmLogger, cmCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "cm.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Could not initialize consensus logger: %v\n", err)
	}

	dbstore, tipState, err := chain.NewDBStore(bdb, network, genesisBlock, chain.NewZapMigrationLogger(cmLogger))
	if err != nil {
		log.Fatalf("Could not initialize consensus store: %v\n", err)
	}

	cm := chain.NewManager(dbstore, tipState)
	chain.WithLog(cmLogger)(cm)

	// Initialize syncer.
	syncerListener, err := net.Listen("tcp", config.GatewayAddr)
	if err != nil {
		log.Fatalf("Could not start syncer listener: %v\n", err)
	}

	// Peers will reject us if our hostname is empty or unspecified, so use loopback.
	syncerAddr := syncerListener.Addr().String()
	host, port, _ := net.SplitHostPort(syncerAddr)
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		syncerAddr = net.JoinHostPort("127.0.0.1", port)
	}

	ps, err := syncerutil.NewJSONPeerStore(filepath.Join(dir, "peers.json"))
	if err != nil {
		log.Fatalf("Could not initialize peer store: %v\n", err)
	}

	for _, peer := range bootstrap {
		if err := ps.AddPeer(peer); err != nil {
			log.Fatalf("Could not add %s to peers: %v\n", peer, err)
		}
	}

	header := gateway.Header{
		GenesisID:  genesisBlock.ID(),
		UniqueID:   gateway.GenerateUniqueID(),
		NetAddress: syncerAddr,
	}

	syncerLogger, syncerCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "syncer.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Could not initialize syncer logger: %v\n", err)
	}

	s := syncer.New(syncerListener, cm, ps, header, syncer.WithLogger(syncerLogger))

	// Initialize MySQL database.
	log.Println("Connecting to the SQL database...")
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
		log.Fatalf("Could not connect to the database: %v\n", err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatalf("MySQL database not responding: %v\n", err)
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	// Initialize wallet.
	walletLogger, walletCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "wallet.log"), zapcore.InfoLevel)
	if err != nil {
		log.Fatalf("Could not initialize wallet logger: %v\n", err)
	}

	w, err := wallet.New(cm, s, db, seed, walletLogger)
	if err != nil {
		log.Fatalf("Could not initialize wallet: %v\n", err)
	}

	// Initialize host database.
	hdbLogger, hdbCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "hostdb.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Could not initialize hostDB logger: %v\n", err)
	}

	hdb, err := hostdb.New(db, hdbLogger, config.Test)
	if err != nil {
		log.Fatalf("Could not initialize hostDB: %v\n", err)
	}

	// Initialize accounts.
	am, err := account.New(db, w)
	if err != nil {
		log.Fatalf("Couldn't initialize account manager: %v\n", err)
	}

	// Initialize mail client.
	log.Println("Creating mail client...")
	mc, err := mail.New(dir)
	if err != nil {
		log.Fatalf("Could not create mail client: %v\n", err)
	}

	// Initialize public API.
	httpListener, err := net.Listen("tcp", config.HTTPAddr)
	if err != nil {
		log.Fatalf("Could not start HTTP listener: %v\n", err)
	}

	apiLogger, apiCloseFn, err := persist.NewFileLogger(filepath.Join(dir, "api.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Could not initialize API logger: %v\n", err)
	}

	apiServer := public.NewServer(am, mc, apiLogger)
	srv := &http.Server{Handler: apiServer}
	go srv.Serve(httpListener)
	log.Printf("Public API: listening on %s\n", httpListener.Addr())

	return &node{
		chain:    cm,
		syncer:   s,
		wallet:   w,
		hostDB:   hdb,
		accounts: am,
		Start: func() func() {
			ch := make(chan struct{})
			go func() {
				s.Run()
				close(ch)
			}()
			return func() {
				apiServer.Close()
				srv.Shutdown(context.Background())
				httpListener.Close()
				hdb.Close()
				w.Close()
				syncerListener.Close()
				<-ch
				bdb.Close()
				apiCloseFn()
				hdbCloseFn()
				walletCloseFn()
				syncerCloseFn()
				cmCloseFn()
				db.Close()
			}
		},
	}
}
