package portal

import (
	"errors"
	"time"
)

const (
	// authStatsCheckFrequency defines how often the authentication
	// stats are pruned.
	authStatsCheckFrequency = 10 * time.Minute

	// authStatsPruneThreshold defines how old the authentication
	// stats may become before they are pruned.
	authStatsPruneThreshold = 24 * time.Hour

	// authStatsCountResetThreshold defines when the counter needs
	// to be reset to zero after the last activity.
	authStatsCountResetThreshold = time.Hour

	// maxVerifications is how many times a verification link may be
	// requested per hour from the same IP.
	maxVerifications = 3

	// maxFailedLogins is how many failed login attempts per hour
	// may be accepted from the same IP.
	maxFailedLogins = 3

	// maxPasswordResets is how many times a password reset link may
	// be requested per hour from the same IP.
	maxPasswordResets = 3
)

type (
	// authAttempts keeps track of specific authentication activities.
	authAttempts struct {
		LastAttempt  int64
		Count        int64
	}

	// authenticationStats is the summary of authentication attempts
	// from a single IP address.
	authenticationStats struct {
		RemoteHost       string
		FailedLogins     authAttempts
		Verifications    authAttempts
		PasswordResets   authAttempts
	}
)

// threadedPruneAuthStats checks if any of the stats have expired
// and removes them.
func (p *Portal) threadedPruneAuthStats() {
	for {
		select {
		case <-p.tg.StopChan():
			return
		case <-time.After(authStatsCheckFrequency):
		}

		func() {
			err := p.tg.Add()
			if err != nil {
				return
			}
			defer p.tg.Done()

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
				stats := p.authStats[entry.RemoteHost]
				if fl > authStatsCountResetThreshold.Seconds() {
					stats.FailedLogins.Count = 0
				}
				if vr > authStatsCountResetThreshold.Seconds() {
					stats.Verifications.Count = 0
				}
				if pr > authStatsCountResetThreshold.Seconds() {
					stats.PasswordResets.Count = 0
				}
				p.authStats[entry.RemoteHost] = stats
			}
		}()
	}
}

// checkAndUpdateVerifications checks if there are too many verification
// links requested from the same IP and updates the stats.
func (p *Portal) checkAndUpdateVerifications(host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats, ok := p.authStats[host]

	// No such IP in the map yet.
	if !ok {
		p.authStats[host] = authenticationStats{
			RemoteHost: host,
			FailedLogins: authAttempts{},
			Verifications: authAttempts{
				LastAttempt: time.Now().Unix(),
				Count: 1,
			},
			PasswordResets: authAttempts{},
		}
		return nil
	}

	// IP exists but no verification requests yet.
	if (stats.Verifications.Count == 0) {
		stats.Verifications.LastAttempt = time.Now().Unix()
		stats.Verifications.Count = 1
		p.authStats[host] = stats
		return nil
	}

	// Check for abuse.
	stats.Verifications.LastAttempt = time.Now().Unix()
	stats.Verifications.Count++
	span := time.Now().Unix() - stats.Verifications.LastAttempt
	if span == 0 {
		span = 1 // To avoid division by zero.
	}
	p.authStats[host] = stats

	if float64(stats.Verifications.Count) / float64(span) > maxVerifications {
		return errors.New("too many verification requests from " + host)
	}

	return nil
}

// checkAndUpdateFailedLogins checks if there are too many failed
// login attempts from the same IP and updates the stats.
func (p *Portal) checkAndUpdateFailedLogins(host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats, ok := p.authStats[host]

	// No such IP in the map yet.
	if !ok {
		p.authStats[host] = authenticationStats{
			RemoteHost: host,
			FailedLogins: authAttempts{
				LastAttempt: time.Now().Unix(),
				Count: 1,
			},
			Verifications: authAttempts{},
			PasswordResets: authAttempts{},
		}
		return nil
	}

	// IP exists but no failed logins yet.
	if (stats.FailedLogins.Count == 0) {
		stats.FailedLogins.LastAttempt = time.Now().Unix()
		stats.FailedLogins.Count = 1
		p.authStats[host] = stats
		return nil
	}

	// Check for abuse.
	stats.FailedLogins.LastAttempt = time.Now().Unix()
	stats.FailedLogins.Count++
	span := time.Now().Unix() - stats.FailedLogins.LastAttempt
	if span == 0 {
		span = 1 // To avoid division by zero.
	}
	p.authStats[host] = stats

	if float64(stats.FailedLogins.Count) / float64(span) > maxFailedLogins {
		return errors.New("too many failed logins from " + host)
	}

	return nil
}

// checkAndUpdatePasswordResets checks if there are too many password
// reset requests from the same IP and updates the stats.
func (p *Portal) checkAndUpdatePasswordResets(host string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats, ok := p.authStats[host]

	// No such IP in the map yet.
	if !ok {
		p.authStats[host] = authenticationStats{
			RemoteHost: host,
			FailedLogins: authAttempts{},
			Verifications: authAttempts{},
			PasswordResets: authAttempts{
				LastAttempt: time.Now().Unix(),
				Count: 1,
			},
		}
		return nil
	}

	// IP exists but no password resets yet.
	if (stats.PasswordResets.Count == 0) {
		stats.PasswordResets.LastAttempt = time.Now().Unix()
		stats.PasswordResets.Count = 1
		p.authStats[host] = stats
		return nil
	}

	// Check for abuse.
	stats.PasswordResets.LastAttempt = time.Now().Unix()
	stats.PasswordResets.Count++
	span := time.Now().Unix() - stats.PasswordResets.LastAttempt
	if span == 0 {
		span = 1 // To avoid division by zero.
	}
	p.authStats[host] = stats

	if float64(stats.PasswordResets.Count) / float64(span) > maxPasswordResets {
		return errors.New("too many password reset requests from " + host)
	}

	return nil
}
