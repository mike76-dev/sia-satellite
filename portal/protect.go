package portal

import (
	"time"
)

const (
	// authStatsCheckFrequency defines how often the authentication
	// stats are pruned.
	authStatsCheckFrequency = time.Minute * 30

	// authStatsPruneThreshold defines how old the authentication
	// stats may become before they are pruned.
	authStatsPruneThreshold = time.Hour * 24
)

type (
	// authAttempts keeps track of specific authentication activities.
	authAttempts struct {
		FirstAttempt time.Time `json: "first"`
		LastAttempt  time.Time `json: "last"`
		Count        uint64    `json: "count"`
	}

	// authenticationStats is the summary of authentication attempts
	// from a single IP address.
	authenticationStats struct {
		RemoteHost       string       `json: "host"`
		SuccessfulLogins authAttempts `json: "loginsuccess"`
		FailedLogins     authAttempts `json: "loginfail"`
		SignupRequests   authAttempts `json: "signup"`
		PasswordResets   authAttempts `json: "reset"`
	}
)

// threadedPruneAuthStats checks if any of the stats have expired
// and removes them.
func (p *Portal) threadedPruneAuthStats() {
	for {
		select {
		case <-p.threads.StopChan():
			return
		case <-time.After(authStatsCheckFrequency):
		}

		func() {
			err := p.threads.Add()
			if err != nil {
				return
			}
			defer p.threads.Done()

			p.mu.Lock()
			defer p.mu.Unlock()

			for ip, entry := range p.authStats {
				sl := time.Since(entry.SuccessfulLogins.LastAttempt)
				fl := time.Since(entry.FailedLogins.LastAttempt)
				sr := time.Since(entry.SignupRequests.LastAttempt)
				pr := time.Since(entry.PasswordResets.LastAttempt)
				min := sl
				if fl.Seconds() < min.Seconds() {
					min = fl
				}
				if sr.Seconds() < min.Seconds() {
					min = sr
				}
				if pr.Seconds() < min.Seconds() {
					min = pr
				}
				if min.Seconds() > authStatsPruneThreshold.Seconds() {
					delete(p.authStats, ip)
				}
			}
		}()
	}
}
