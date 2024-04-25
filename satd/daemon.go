package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/node"
	"github.com/mike76-dev/sia-satellite/node/api/server"
	"github.com/mike76-dev/sia-satellite/persist"
)

// startDaemon starts the satd server.
func startDaemon(config *persist.SatdConfig, apiPassword, dbPassword, seed string) error {
	loadStart := time.Now()

	fmt.Printf("satd v%v\n", build.NodeVersion)
	if build.GitRevision == "" {
		fmt.Println("WARN: compiled without build commit or version. To compile correctly, please use the makefile")
	} else {
		fmt.Println("Git Revision " + build.GitRevision)
	}
	fmt.Println("Loading...")

	// Start listening to the API requests.
	l, err := net.Listen("tcp", config.APIAddr)
	if err != nil {
		log.Fatal(err)
	}
	n, err := node.New(config, dbPassword, seed, loadStart)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("p2p: Listening on", n.Syncer.Addr())
	stop := n.Start()
	log.Println("api: Listening on", l.Addr())
	go server.StartWeb(l, n, apiPassword)
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)
	<-signalCh
	log.Println("Shutting down...")
	stop()

	return nil
}
