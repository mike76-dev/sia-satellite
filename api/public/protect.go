package api

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

	// maxVerifications is how many times a verification code may be
	// requested per hour from the same IP.
	maxVerifications = 3

	// maxFailedLogins is how many failed login attempts per hour
	// may be accepted from the same IP.
	maxFailedLogins = 3

	// maxPasswordResets is how many times a password reset link may
	// be requested per hour from the same IP.
	maxPasswordResets = 3

	// maxAPICalls is how many single API calls may be accepted from
	// the same IP within authStatsCheckFrequency.
	maxAPICalls = 600
)

type (
	// authAttempts keeps track of specific authentication activities.
	authAttempts struct {
		LastAttempt int64
		Count       int64
	}

	// authenticationStats is the summary of authentication attempts
	// from a single IP address.
	authenticationStats struct {
		RemoteHost     string
		FailedLogins   authAttempts
		Verifications  authAttempts
		PasswordResets authAttempts
	}
)

// pruneAuthStats checks if any of the stats have expired and removes them.
func (s *Server) pruneAuthStats() {
	for {
		select {
		case <-s.closeChan:
			return
		case <-time.After(authStatsCheckFrequency):
		}

		s.mu.Lock()

		// Reset the call stats.
		s.callStats = make(map[string]int)

		now := time.Now().Unix()
		for ip, entry := range s.authStats {
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
				delete(s.authStats, ip)
				continue
			}

			// Check if the counters need to be reset.
			stats := s.authStats[entry.RemoteHost]
			if fl > authStatsCountResetThreshold.Seconds() {
				stats.FailedLogins.Count = 0
			}
			if vr > authStatsCountResetThreshold.Seconds() {
				stats.Verifications.Count = 0
			}
			if pr > authStatsCountResetThreshold.Seconds() {
				stats.PasswordResets.Count = 0
			}
			s.authStats[entry.RemoteHost] = stats
		}

		s.mu.Unlock()
	}
}

// checkCalls returns an error if there are too many API calls from
// the same IP.
func (s *Server) checkCalls(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	num := s.callStats[host]
	s.callStats[host] = num + 1
	if num >= maxAPICalls {
		return errors.New("too many API calls from " + host)
	}

	return nil
}

// checkAndUpdateVerifications checks if there are too many verification
// codes requested from the same IP and updates the stats.
func (s *Server) checkAndUpdateVerifications(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats, ok := s.authStats[host]

	// No such IP in the map yet.
	if !ok {
		s.authStats[host] = authenticationStats{
			RemoteHost:   host,
			FailedLogins: authAttempts{},
			Verifications: authAttempts{
				LastAttempt: time.Now().Unix(),
				Count:       1,
			},
			PasswordResets: authAttempts{},
		}
		return nil
	}

	// IP exists but no verification requests yet.
	if stats.Verifications.Count == 0 {
		stats.Verifications.LastAttempt = time.Now().Unix()
		stats.Verifications.Count = 1
		s.authStats[host] = stats
		return nil
	}

	// Check for abuse.
	span := time.Now().Unix() - stats.Verifications.LastAttempt
	if span == 0 {
		span = 1 // avoid division by zero
	}
	stats.Verifications.LastAttempt = time.Now().Unix()
	stats.Verifications.Count++
	s.authStats[host] = stats

	if float64(stats.Verifications.Count)/float64(span) > maxVerifications {
		return errors.New("too many verification requests from " + host)
	}

	return nil
}

// checkAndUpdateFailedLogins checks if there are too many failed
// login attempts from the same IP and updates the stats.
func (s *Server) checkAndUpdateFailedLogins(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats, ok := s.authStats[host]

	// No such IP in the map yet.
	if !ok {
		s.authStats[host] = authenticationStats{
			RemoteHost: host,
			FailedLogins: authAttempts{
				LastAttempt: time.Now().Unix(),
				Count:       1,
			},
			Verifications:  authAttempts{},
			PasswordResets: authAttempts{},
		}
		return nil
	}

	// IP exists but no failed logins yet.
	if stats.FailedLogins.Count == 0 {
		stats.FailedLogins.LastAttempt = time.Now().Unix()
		stats.FailedLogins.Count = 1
		s.authStats[host] = stats
		return nil
	}

	// Check for abuse.
	span := time.Now().Unix() - stats.FailedLogins.LastAttempt
	if span == 0 {
		span = 1 // avoid division by zero
	}
	stats.FailedLogins.LastAttempt = time.Now().Unix()
	stats.FailedLogins.Count++
	s.authStats[host] = stats

	if float64(stats.FailedLogins.Count)/float64(span) > maxFailedLogins {
		return errors.New("too many failed logins from " + host)
	}

	return nil
}

// checkAndUpdatePasswordResets checks if there are too many password
// reset requests from the same IP and updates the stats.
func (s *Server) checkAndUpdatePasswordResets(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats, ok := s.authStats[host]

	// No such IP in the map yet.
	if !ok {
		s.authStats[host] = authenticationStats{
			RemoteHost:    host,
			FailedLogins:  authAttempts{},
			Verifications: authAttempts{},
			PasswordResets: authAttempts{
				LastAttempt: time.Now().Unix(),
				Count:       1,
			},
		}
		return nil
	}

	// IP exists but no password resets yet.
	if stats.PasswordResets.Count == 0 {
		stats.PasswordResets.LastAttempt = time.Now().Unix()
		stats.PasswordResets.Count = 1
		s.authStats[host] = stats
		return nil
	}

	// Check for abuse.
	span := time.Now().Unix() - stats.PasswordResets.LastAttempt
	if span == 0 {
		span = 1 // avoid division by zero
	}
	stats.PasswordResets.LastAttempt = time.Now().Unix()
	stats.PasswordResets.Count++
	s.authStats[host] = stats

	if float64(stats.PasswordResets.Count)/float64(span) > maxPasswordResets {
		return errors.New("too many password reset requests from " + host)
	}

	return nil
}
