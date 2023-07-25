package gateway

import (
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
)

const (
	// logFile is the name of the log file.
	logFile = "gateway.log"
)

type (
	// persist contains all of the persistent gateway data.
	persistence struct {
		RouterURL string

		// Blocklisted IPs.
		Blocklist []string
	}
)

// load loads the Gateway's persistent data from disk.
func (g *Gateway) load() error {
	// Load nodes.
	nodeRows, err := g.db.Query("SELECT address, outbound FROM gw_nodes")
	if err != nil {
		return err
	}

	for nodeRows.Next() {
		var address modules.NetAddress
		var outbound bool
		if err := nodeRows.Scan(&address, &outbound); err != nil {
			g.log.Println("ERROR: unable to retrieve node:", err)
			continue
		}
		n := &node{
			NetAddress:      address,
			WasOutboundPeer: outbound,
		}
		g.nodes[address] = n
	}
	nodeRows.Close()

	// Load Gateway persistence.
	g.db.QueryRow("SELECT router_url FROM gw_url").Scan(&g.persist.RouterURL)
	rows, err := g.db.Query("SELECT ip FROM gw_blocklist")
	if err != nil {
		return err
	}

	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			g.log.Println("ERROR: unable to retrieve blocklist IP:", err)
			continue
		}
		g.persist.Blocklist = append(g.persist.Blocklist, ip)
	}
	rows.Close()

	// Create map from blocklist.
	for _, ip := range g.persist.Blocklist {
		g.blocklist[ip] = struct{}{}
	}

	return nil
}

// save stores the Gateway's persistent data in the database.
func (g *Gateway) save() error {	
	// Save Gateway persistence.
	_, err := g.db.Exec("UPDATE gw_url SET router_url = ?", g.persist.RouterURL)
	if err != nil {
		return err
	}

	tx, err := g.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM gw_blocklist")
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, ip := range g.persist.Blocklist {
		_, err = tx.Exec("INSERT INTO gw_blocklist (ip) VALUES (?)", ip)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		g.log.Println("ERROR: unable to save Gateway persistence:", err)
		return err
	}

	// Save nodes.
	tx, err = g.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM gw_nodes")
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, n := range g.nodes {
		_, err = tx.Exec(`
			INSERT INTO gw_nodes (address, outbound)
			VALUES (?, ?)
		`, n.NetAddress, n.WasOutboundPeer)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		g.log.Println("ERROR: unable to save Gateway nodes:", err)
		return err
	}

	return nil
}

// threadedSaveLoop periodically saves the gateway nodes.
func (g *Gateway) threadedSaveLoop() {
	for {
		select {
		case <-g.threads.StopChan():
			return
		case <-time.After(saveFrequency):
		}

		func() {
			err := g.threads.Add()
			if err != nil {
				return
			}
			defer g.threads.Done()

			g.mu.Lock()
			err = g.save()
			if err != nil {
				g.log.Println("ERROR: unable to save gateway:", err)
			}
			g.mu.Unlock()
		}()
	}
}
