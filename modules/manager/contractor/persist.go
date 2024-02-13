package contractor

import (
	"time"

	"go.uber.org/zap"
)

// saveFrequency determines how often the Contractor will be saved.
const saveFrequency = 2 * time.Minute

// load loads the Contractor persistence data from disk.
func (c *Contractor) load() error {
	err := c.initDB()
	if err != nil {
		c.log.Error("couldn't initialize database", zap.Error(err))
		return err
	}

	err = c.loadState()
	if err != nil {
		c.log.Error("couldn't load sync state", zap.Error(err))
		return err
	}

	err = c.loadDoubleSpent()
	if err != nil {
		c.log.Error("couldn't load double-spent contracts", zap.Error(err))
		return err
	}

	err = c.loadRenewHistory()
	if err != nil {
		c.log.Error("couldn't load renewal history", zap.Error(err))
		return err
	}

	err = c.loadRenters()
	if err != nil {
		c.log.Error("couldn't load renters", zap.Error(err))
		return err
	}

	c.staticWatchdog.load()
	if err != nil {
		return err
	}
	c.staticWatchdog.blockHeight = c.tip.Height

	return nil
}

// save saves the Contractor persistence data to disk.
func (c *Contractor) save() error {
	err := c.updateState()
	if err != nil {
		c.log.Error("couldn't save sync state", zap.Error(err))
		return err
	}

	err = c.updateDoubleSpent()
	if err != nil {
		c.log.Error("couldn't save double-spent contracts", zap.Error(err))
		return err
	}

	c.staticWatchdog.save()
	if err != nil {
		return err
	}

	return nil
}

// threadedSaveLoop periodically saves the Contractor persistence.
func (c *Contractor) threadedSaveLoop() {
	err := c.tg.Add()
	if err != nil {
		return
	}
	defer c.tg.Done()

	for {
		select {
		case <-c.tg.StopChan():
			return
		case <-time.After(saveFrequency):
			c.mu.Lock()
			err = c.save()
			c.mu.Unlock()
			if err != nil {
				c.log.Error("difficulties saving the Contractor", zap.Error(err))
			}
		}
	}
}
