package hostdb

import (
	"bytes"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/mike76-dev/sia-satellite/external"
	"github.com/mike76-dev/sia-satellite/internal/utils"
	rhpv4 "go.sia.tech/core/rhp/v4"
	"go.sia.tech/core/types"
	"go.uber.org/zap"
)

// HostDBEntry represents a single host.
type HostDBEntry struct {
	PublicKey    types.PublicKey
	FirstSeen    time.Time
	KnownSince   uint64
	NetAddress   string
	IPNets       []string
	LastIPChange time.Time
	Score        external.HostScoreBreakdown
	Interactions map[string]external.HostInteraction
	Country      string
	Settings     rhpv4.HostSettings
}

// HostDB keeps a list of online hosts that is updated regularly.
type HostDB struct {
	hosts     map[types.PublicKey]*HostDBEntry
	db        *sql.DB
	log       *zap.Logger
	mu        sync.Mutex
	closeChan chan struct{}
}

// New returns an initialized HostDB.
func New(db *sql.DB, log *zap.Logger) (*HostDB, error) {
	hdb := &HostDB{
		db:        db,
		log:       log,
		hosts:     make(map[types.PublicKey]*HostDBEntry),
		closeChan: make(chan struct{}),
	}

	if err := hdb.load(); err != nil {
		return nil, utils.AddContext(err, "couldn't load hosts")
	}

	go hdb.update()

	return hdb, nil
}

// Close shuts down the HostDB.
func (hdb *HostDB) Close() {
	hdb.closeChan <- struct{}{}
}

// load loads the HostDB from the database.
func (hdb *HostDB) load() error {
	stmt, err := hdb.db.Prepare(`
		SELECT
			node,
			uptime,
			downtime,
			last_seen,
			active_hosts,
			price_score,
			storage_score,
			collateral_score,
			interactions_score,
			uptime_score,
			age_score,
			version_score,
			latency_score,
			benchmarks_score,
			contracts_score,
			total_score,
			successes,
			failures
		FROM hdb_interactions
		WHERE public_key = ?
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't prepare statement")
	}
	defer stmt.Close()

	rows, err := hdb.db.Query(`
		SELECT
			public_key,
			first_seen,
			known_since,
			net_address,
			ip_nets,
			last_ip_change,
			price_score,
			storage_score,
			collateral_score,
			interactions_score,
			uptime_score,
			age_score,
			version_score,
			latency_score,
			benchmarks_score,
			contracts_score,
			total_score,
			country,
			settings
		FROM hdb_hosts
		WHERE is_online = TRUE
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't query hosts")
	}

	for rows.Next() {
		pk := make([]byte, 32)
		var firstSeen, lastIPChange int64
		var knownSince uint64
		var netAddress, ipNets, country string
		var pricesScore, storageScore, collateralScore, interactionsScore, uptimeScore float64
		var ageScore, versionScore, latencyScore, benchmarksScore, contractsScore, totalScore float64
		var settings []byte
		if err := rows.Scan(
			&pk,
			&firstSeen,
			&knownSince,
			&netAddress,
			&ipNets,
			&lastIPChange,
			&pricesScore,
			&storageScore,
			&collateralScore,
			&interactionsScore,
			&uptimeScore,
			&ageScore,
			&versionScore,
			&latencyScore,
			&benchmarksScore,
			&contractsScore,
			&totalScore,
			&country,
			&settings,
		); err != nil {
			rows.Close()
			return utils.AddContext(err, "couldn't scan host data")
		}

		host := &HostDBEntry{
			PublicKey:    types.PublicKey(pk),
			FirstSeen:    time.Unix(firstSeen, 0),
			KnownSince:   knownSince,
			NetAddress:   netAddress,
			IPNets:       strings.Split(ipNets, ";"),
			LastIPChange: time.Unix(lastIPChange, 0),
			Score: external.HostScoreBreakdown{
				PricesScore:       pricesScore,
				StorageScore:      storageScore,
				CollateralScore:   collateralScore,
				InteractionsScore: interactionsScore,
				UptimeScore:       uptimeScore,
				AgeScore:          ageScore,
				VersionScore:      versionScore,
				LatencyScore:      latencyScore,
				BenchmarksScore:   benchmarksScore,
				ContractsScore:    contractsScore,
				TotalScore:        totalScore,
			},
			Interactions: make(map[string]external.HostInteraction),
			Country:      country,
		}
		if len(settings) > 0 {
			d := types.NewBufDecoder(settings)
			host.Settings.DecodeFrom(d)
			if err := d.Err(); err != nil {
				rows.Close()
				return utils.AddContext(err, "couldn't decode host settings")
			}
		}
		hdb.hosts[host.PublicKey] = host
	}
	rows.Close()

	for pk, host := range hdb.hosts {
		var node string
		var activeHosts int
		var uptime, downtime, lastSeen int64
		var pricesScore, storageScore, collateralScore, interactionsScore, uptimeScore float64
		var ageScore, versionScore, latencyScore, benchmarksScore, contractsScore, totalScore float64
		var successes, failures float64
		rows, err := stmt.Query(pk[:])
		if err != nil {
			return utils.AddContext(err, "couldn't query interactions")
		}

		for rows.Next() {
			if err := rows.Scan(
				&node,
				&uptime,
				&downtime,
				&lastSeen,
				&activeHosts,
				&pricesScore,
				&storageScore,
				&collateralScore,
				&interactionsScore,
				&uptimeScore,
				&ageScore,
				&versionScore,
				&latencyScore,
				&benchmarksScore,
				&contractsScore,
				&totalScore,
				&successes,
				&failures,
			); err != nil {
				return utils.AddContext(err, "couldn't scan interactions")
			}

			host.Interactions[node] = external.HostInteraction{
				Uptime:      time.Duration(uptime) * time.Second,
				Downtime:    time.Duration(downtime) * time.Second,
				LastSeen:    time.Unix(lastSeen, 0),
				ActiveHosts: activeHosts,
				Score: external.HostScoreBreakdown{
					PricesScore:       pricesScore,
					StorageScore:      storageScore,
					CollateralScore:   collateralScore,
					InteractionsScore: interactionsScore,
					UptimeScore:       uptimeScore,
					AgeScore:          ageScore,
					VersionScore:      versionScore,
					LatencyScore:      latencyScore,
					BenchmarksScore:   benchmarksScore,
					ContractsScore:    contractsScore,
					TotalScore:        totalScore,
				},
				Successes: successes,
				Failures:  failures,
			}
		}
		rows.Close()
	}

	return nil
}

// doUpdate retrieves a new list of hosts and saves it.
func (hdb *HostDB) doUpdate() error {
	hosts, err := external.GetHosts()
	if err != nil {
		return err
	}

	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	tx, err := hdb.db.Begin()
	if err != nil {
		return utils.AddContext(err, "couldn't start transaction")
	}

	// Reset the database.
	_, err = tx.Exec("UPDATE hdb_hosts SET is_online = FALSE")
	if err != nil {
		tx.Rollback()
		return utils.AddContext(err, "couldn't reset database")
	}

	hostStmt, err := tx.Prepare(`
		REPLACE INTO hdb_hosts (
			is_online,
			public_key,
			first_seen,
			known_since,
			net_address,
			ip_nets,
			last_ip_change,
			price_score,
			storage_score,
			collateral_score,
			interactions_score,
			uptime_score,
			age_score,
			version_score,
			latency_score,
			benchmarks_score,
			contracts_score,
			total_score,
			country,
			settings
		) VALUES (TRUE, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return utils.AddContext(err, "couldn't prepare hosts statement")
	}
	defer hostStmt.Close()

	intStmt, err := tx.Prepare(`
		REPLACE INTO hdb_interactions (
			public_key,
			node,
			uptime,
			downtime,
			last_seen,
			active_hosts,
			price_score,
			storage_score,
			collateral_score,
			interactions_score,
			uptime_score,
			age_score,
			version_score,
			latency_score,
			benchmarks_score,
			contracts_score,
			total_score,
			successes,
			failures
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return utils.AddContext(err, "couldn't prepare interactions statement")
	}
	defer intStmt.Close()

	hdb.hosts = make(map[types.PublicKey]*HostDBEntry)
	for _, host := range hosts {
		hdb.hosts[host.PublicKey] = &HostDBEntry{
			PublicKey:    host.PublicKey,
			FirstSeen:    host.FirstSeen,
			KnownSince:   host.KnownSince,
			NetAddress:   host.NetAddress,
			IPNets:       host.IPNets,
			LastIPChange: host.LastIPChange,
			Score:        host.Score,
			Interactions: host.Interactions,
			Country:      host.Country,
			Settings:     host.Settings,
		}

		for node, interaction := range host.Interactions {
			_, err := intStmt.Exec(
				host.PublicKey[:],
				node,
				int64(interaction.Uptime.Seconds()),
				int64(interaction.Downtime.Seconds()),
				interaction.LastSeen.Unix(),
				interaction.ActiveHosts,
				interaction.Score.PricesScore,
				interaction.Score.StorageScore,
				interaction.Score.CollateralScore,
				interaction.Score.InteractionsScore,
				interaction.Score.UptimeScore,
				interaction.Score.AgeScore,
				interaction.Score.VersionScore,
				interaction.Score.LatencyScore,
				interaction.Score.BenchmarksScore,
				interaction.Score.ContractsScore,
				interaction.Score.TotalScore,
				interaction.Successes,
				interaction.Failures,
			)
			if err != nil {
				tx.Rollback()
				return utils.AddContext(err, "couldn't save interactions")
			}
		}

		var settings bytes.Buffer
		e := types.NewEncoder(&settings)
		if (host.Settings != rhpv4.HostSettings{}) {
			host.Settings.EncodeTo(e)
			e.Flush()
		}

		_, err = hostStmt.Exec(
			host.PublicKey[:],
			host.FirstSeen.Unix(),
			host.KnownSince,
			host.NetAddress,
			strings.Join(host.IPNets, ";"),
			host.LastIPChange.Unix(),
			host.Score.PricesScore,
			host.Score.StorageScore,
			host.Score.CollateralScore,
			host.Score.InteractionsScore,
			host.Score.UptimeScore,
			host.Score.AgeScore,
			host.Score.VersionScore,
			host.Score.LatencyScore,
			host.Score.BenchmarksScore,
			host.Score.ContractsScore,
			host.Score.TotalScore,
			host.Country,
			settings.Bytes(),
		)
		if err != nil {
			tx.Rollback()
			return utils.AddContext(err, "couldn't save host")
		}
	}

	if err := tx.Commit(); err != nil {
		return utils.AddContext(err, "couldn't commit transaction")
	}

	return nil
}

// update calls doUpdate at regular intervals.
func (hdb *HostDB) update() {
	for {
		if err := hdb.doUpdate(); err != nil {
			hdb.log.Error("failed to update hosts", zap.Error(err))
		}

		select {
		case <-hdb.closeChan:
			return
		case <-time.After(2 * time.Hour):
		}
	}
}
