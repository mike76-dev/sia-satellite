package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/mike76-dev/sia-satellite/node/api/server"
	"github.com/mike76-dev/sia-satellite/persist"

	"go.sia.tech/siad/build"
)

// tryAutoUnlock will try to automatically unlock the wallet if the
// environment variable is set.
func tryAutoUnlock(srv *server.Server) {
	password := os.Getenv("SATD_WALLET_PASSWORD")
	if password != "" {
		fmt.Println("Wallet Password found, attempting to auto-unlock wallet...")
		if err := srv.Unlock(password); err != nil {
			fmt.Println("Auto-unlock failed:", err)
		} else {
			fmt.Println("Auto-unlock successful.")
		}
	}
}

// startDaemon starts the satd server.
func startDaemon(config *persist.SatdConfig, apiPassword string, dbPassword string) error {
	loadStart := time.Now()

	fmt.Printf("satd v%v\n", build.NodeVersion)
	if build.GitRevision == "" {
		fmt.Println("WARN: compiled without build commit or version. To compile correctly, please use the makefile")
	} else {
		fmt.Println("Git Revision " + build.GitRevision)
	}
	fmt.Println("Loading...")

	// Start and run the server.
	srv, err := server.New(config, apiPassword, dbPassword, loadStart)
	if err != nil {
		return err
	}

	// Attempt to auto-unlock the wallet using the SATD_WALLET_PASSWORD env variable.
	tryAutoUnlock(srv)

	// Listen for kill signals.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	startupTime := time.Since(loadStart)
	fmt.Printf("Finished full setup in %s\n", startupTime.Truncate(time.Second).String())

	// Wait for Serve to return or for kill signal to be caught.
	err = func() error {
		select {
		case err := <-srv.ServeErr():
			return err
		case <-sigChan:
			fmt.Println("\rCaught stop signal, quitting...")
			return srv.Close()
		}
	}()
	if err != nil {
		log.Fatalln(err)
	}

	// Wait for server to complete shutdown.
	srv.WaitClose()

	return nil
}
