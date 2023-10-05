package portal

import (
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
)

const (
	// saveFrequency defines how often the portal should be saved to disk.
	saveFrequency = time.Minute * 2
)

// load loads the Portal's persistent data from disk.
func (p *Portal) load() error {
	err := p.loadStats()
	if err != nil {
		return err
	}

	err = p.loadCredits()
	if err != nil {
		return err
	}

	return p.loadTransactions()
}

// save saves the Portal's persistent data to disk.
func (p *Portal) save() error {
	return modules.ComposeErrors(p.saveStats(), p.saveCredits())
}

// threadedSaveLoop periodically saves the Portal's persistent data.
func (p *Portal) threadedSaveLoop() {
	for {
		select {
		case <-p.tg.StopChan():
			return
		case <-time.After(saveFrequency):
		}

		func() {
			err := p.tg.Add()
			if err != nil {
				return
			}
			defer p.tg.Done()

			p.mu.Lock()
			defer p.mu.Unlock()
			err = p.save()
			if err != nil {
				p.log.Println("ERROR: unable to save portal persistence:", err)
			}
		}()
	}
}
