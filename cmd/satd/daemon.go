package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/persist"
)

// startDaemon starts the satd server.
func startDaemon(config *persist.SatdConfig, apiPassword, dbPassword string, seed string) {
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

	// Start the node.
	n := newNode(config, dbPassword, seed)
	stop := n.Start()

	log.Println("API: Listening on", l.Addr())
	go startWeb(l, n, apiPassword)

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)

	<-signalCh
	log.Println("Shutting down...")
	stop()
}
