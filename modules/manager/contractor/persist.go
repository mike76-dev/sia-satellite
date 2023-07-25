package contractor

import (
	"time"
)

// saveFrequency determines how often the Contractor will be saved.
const saveFrequency = 2 * time.Minute

// load loads the Contractor persistence data from disk.
func (c *Contractor) load() error {
	err := c.initDB()
	if err != nil {
		c.log.Println("ERROR: couldn't initialize database:", err)
		return err
	}

	err = c.loadState()
	if err != nil {
		c.log.Println("ERROR: couldn't load sync state:", err)
		return err
	}

	err = c.loadDoubleSpent()
	if err != nil {
		c.log.Println("ERROR: couldn't load double-spent contracts:", err)
		return err
	}

	err = c.loadRenewHistory()
	if err != nil {
		c.log.Println("ERROR: couldn't load renewal history:", err)
		return err
	}

	err = c.loadRenters()
	if err != nil {
		c.log.Println("ERROR: couldn't load renters:", err)
		return err
	}

	c.staticWatchdog.load()
	if err != nil {
		return err
	}
	c.staticWatchdog.blockHeight = c.blockHeight

	return nil
}

// save saves the Contractor persistence data to disk.
func (c *Contractor) save() error {
	err := c.updateState()
	if err != nil {
		c.log.Println("ERROR: couldn't save sync state:", err)
		return err
	}

	err = c.updateDoubleSpent()
	if err != nil {
		c.log.Println("ERROR: couldn't save double-spent contracts:", err)
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
				c.log.Println("ERROR: difficulties saving the Contractor:", err)
			}
		}
	}
}
