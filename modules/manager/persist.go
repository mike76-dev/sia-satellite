package manager

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/sync"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
)

const (
	// logFile is the name of the log file.
	logFile = "manager.log"

	// saveFrequency defines how often the manager should be saved to disk.
	saveFrequency = time.Minute * 2
)

// threadedRegularSync will make sure that sync gets called on the database
// every once in a while.
func (m *Manager) threadedRegularSync() {
	if err := m.tg.Add(); err != nil {
		return
	}
	defer m.tg.Done()
	for {
		select {
		case <-m.tg.StopChan():
			return
		case <-time.After(saveFrequency):
			m.mu.Lock()
			m.syncDB()
			m.mu.Unlock()
		}
	}
}

// syncDB commits the current global transaction and immediately begins a new
// one.
func (m *Manager) syncDB() {
	// Check if we are not syncing already.
	if m.syncing {
		return
	}
	m.syncing = true
	defer func() {
		m.syncing = false
	}()

	// Commit the existing tx.
	err := m.dbTx.Commit()
	if err != nil {
		m.log.Severe("ERROR: failed to apply database update:", err)
		m.dbTx.Rollback()
	}
	// Begin a new tx.
	m.dbTx, err = m.db.Begin()
	if err != nil {
		m.log.Severe("ERROR: failed to initialize a db transaction:", err)
	}
}

// initPersist initializes the database.
func (m *Manager) initPersist(dir string) error {
	// Create the logger.
	var err error
	m.log, err = persist.NewFileLogger(filepath.Join(dir, logFile))
	if err != nil {
		return modules.AddContext(err, "unable to initialize the Manager's logger")
	}
	m.tg.AfterStop(func() {
		err := m.log.Close()
		if err != nil {
			fmt.Println("Unable to close the Manager's logger:", err)
		}
	})

	// Load email preferences.
	err = m.getEmailPreferences()
	if err != nil {
		return modules.AddContext(err, "unable to load email preferences")
	}

	// Load prices.
	err = m.loadPrices()
	if err != nil {
		return modules.AddContext(err, "unable to load prices")
	}

	// Load maintenance flag.
	err = m.loadMaintenance()
	if err != nil {
		return modules.AddContext(err, "couldn't load maintenance flag")
	}

	// Calculate the size of the temporary files.
	total, err := m.GetBufferSize()
	if err != nil {
		return modules.AddContext(err, "couldn't calculate buffer size")
	}
	m.bufferSize = total

	// Create the global tx that will be used for most persist actions.
	m.dbTx, err = m.db.Begin()
	if err != nil {
		return modules.AddContext(err, "unable to begin Manager's dbTx")
	}
	m.tg.AfterStop(func() {
		m.mu.Lock()
		for m.syncing {
		}
		err := m.dbTx.Commit()
		m.mu.Unlock()
		if err != nil {
			m.log.Println("ERROR: unable to close transaction properly during shutdown:", err)
		}
	})

	// Load the persisted data.
	m.currentMonth, m.prevMonth, err = dbGetBlockTimestamps(m.dbTx)
	if err != nil {
		return modules.AddContext(err, "couldn't load block timestamps")
	}
	m.hostAverages, err = dbGetAverages(m.dbTx)
	if err != nil {
		return modules.AddContext(err, "couldn't load network averages")
	}

	// Spin up the thread that occasionally synchronizes the database.
	go m.threadedRegularSync()

	// Spawn a thread to calculate the host network averages.
	go m.threadedCalculateAverages()

	// Spawn the threads to fetch the exchange rates.
	go m.threadedFetchExchangeRates()

	// Subscribe to the consensus set using the most recent consensus change.
	go func() {
		err := m.cs.ConsensusSetSubscribe(m, modules.ConsensusChangeRecent, m.tg.StopChan())
		if modules.ContainsError(err, sync.ErrStopped) {
			return
		}
		if err != nil {
			m.log.Critical(err)
			return
		}
	}()
	m.tg.OnStop(func() {
		m.cs.Unsubscribe(m)
	})

	return nil
}
