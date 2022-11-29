package portal

import (
	"errors"
	"net"
	"time"
)

const (
	// authStatsCheckFrequency defines how often the authentication
	// stats are pruned.
	authStatsCheckFrequency = time.Minute * 10

	// authStatsPruneThreshold defines how old the authentication
	// stats may become before they are pruned.
	authStatsPruneThreshold = time.Hour * 24

	// authStatsCountResetThreshold defines when the counter needs
	// to be reset to zero after the last activity.
	authStatsCountResetThreshold = time.Hour

	// maxVerifications is how many times a verification link may be
	// requested per hour from the same IP.
	maxVerifications = 3
)

type (
	// authAttempts keeps track of specific authentication activities.
	authAttempts struct {
		LastAttempt  int64 `json: "last"`
		Count        int64 `json: "count"`
	}

	// authenticationStats is the summary of authentication attempts
	// from a single IP address.
	authenticationStats struct {
		RemoteHost       string        `json: "host"`
		FailedLogins     *authAttempts `json: "loginfail"`
		Verifications    *authAttempts `json: "verification"`
		PasswordResets   *authAttempts `json: "reset"`
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

			now := time.Now().Unix()
			for ip, entry := range p.authStats {
				// Check if the entry needs to be pruned.
				fl := float64(now - entry.FailedLogins.LastAttempt)
				vr := float64(now - entry.Verifications.LastAttempt)
				pr := float64(now - entry.PasswordResets.LastAttempt)
				min := fl
				if vr < min {
					min = vr
				}
				if pr < min {
					min = pr
				}
				if min > authStatsPruneThreshold.Seconds() {
					delete(p.authStats, ip)
					continue
				}

				// Check if the counters need to be reset.
				if fl > authStatsCountResetThreshold.Seconds() {
					p.authStats[entry.RemoteHost].FailedLogins.Count = 0
				}
				if vr > authStatsCountResetThreshold.Seconds() {
					p.authStats[entry.RemoteHost].Verifications.Count = 0
				}
				if pr > authStatsCountResetThreshold.Seconds() {
					p.authStats[entry.RemoteHost].PasswordResets.Count = 0
				}
			}
		}()
	}
}

// checkAndUpdateVerifications checks if there are too many verification
// links are requested from the same IP and updates the stats.
func (p *Portal) checkAndUpdateVerifications(addr string) error {
	host, _, _ := net.SplitHostPort(addr)
	p.mu.Lock()
	defer p.mu.Unlock()

	as, ok := p.authStats[host]

	// No such IP in the map yet.
	if !ok {
		p.authStats[host] = authenticationStats{
			RemoteHost: host,
			Verifications: &authAttempts{
				LastAttempt: time.Now().Unix(),
				Count: 1,
			},
		}
		return nil
	}

	// IP exists but no signup attempts yet.
	if (as.Verifications.Count == 0) {
		p.authStats[host].Verifications.LastAttempt = time.Now().Unix()
		p.authStats[host].Verifications.Count = 1
		return nil
	}

	// Check for abuse.
	p.authStats[host].Verifications.LastAttempt = time.Now().Unix()
	p.authStats[host].Verifications.Count++
	span := time.Now().Unix() - as.Verifications.LastAttempt
	if span == 0 {
		span = 1 // To avoid division by zero.
	}

	if float64(as.Verifications.Count) / float64(span) > maxVerifications {
		return errors.New("too many verification requests from " + host)
	}

	return nil
}
