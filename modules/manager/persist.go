package manager

import (
	"path/filepath"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.sia.tech/core/types"
	"go.uber.org/zap"
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
		m.log.Error("failed to apply database update", zap.Error(err))
		m.dbTx.Rollback()
	}
	// Begin a new tx.
	m.dbTx, err = m.db.Begin()
	if err != nil {
		m.log.Error("failed to initialize a db transaction", zap.Error(err))
	}
}

// initPersist initializes the database.
func (m *Manager) initPersist(dir string) error {
	// Create the logger.
	logger, closeFn, err := persist.NewFileLogger(filepath.Join(dir, logFile))
	if err != nil {
		return modules.AddContext(err, "unable to initialize the Manager's logger")
	}
	m.log = logger
	m.tg.AfterStop(func() {
		closeFn()
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
		err := m.dbTx.Commit()
		m.mu.Unlock()
		if err != nil {
			m.log.Error("unable to close transaction properly during shutdown", zap.Error(err))
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

	// Spawn a thread to fetch the exchange rates.
	go m.threadedFetchExchangeRates()

	// Spawn a thread to prune incomplete multipart uploads.
	go m.threadedPruneMultipartUploads()

	// Spawn a thread to check if the satellite is out od sync.
	go m.threadedCheckOutOfSync()

	// Subscribe to the consensus set using the most recent consensus change.
	go func() {
		err := m.sync(m.cm.Tip())
		if err != nil {
			m.log.Error("failed to subscribe", zap.Error(err))
			return
		}
	}()

	return nil
}

func (m *Manager) sync(index types.ChainIndex) error {
	for index != m.cm.Tip() {
		select {
		case <-m.tg.StopChan():
			return nil
		default:
		}
		crus, caus, err := m.cm.UpdatesSince(index, 1000)
		if err != nil {
			m.log.Error("failed to subscribe to chain manager", zap.Error(err))
			return err
		} else if err := m.UpdateChainState(crus, caus); err != nil {
			m.log.Error("failed to update chain state", zap.Error(err))
			return err
		}
		if len(caus) > 0 {
			index = caus[len(caus)-1].State.Index
		}
	}
	return nil
}
