package api

import (
	"errors"
	"time"
)

const (
	// authStatsCheckFrequency defines how often the authentication
	// stats are pruned.
	authStatsCheckFrequency = 10 * time.Minute

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
	// authenticationStats is the summary of authentication attempts
	// from a single IP address.
	authenticationStats struct {
		failedLogins   int
		verifications  int
		passwordResets int
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

		// Check if the auth stats need to be pruned.
		if time.Since(s.authTimer) > authStatsCountResetThreshold {
			s.authStats = make(map[string]authenticationStats)
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
		s.authStats[host] = authenticationStats{verifications: 1}
		return nil
	}

	// Increment the counter.
	stats.verifications++
	s.authStats[host] = stats

	// Check for abuse.
	if stats.verifications > maxVerifications {
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
		s.authStats[host] = authenticationStats{failedLogins: 1}
		return nil
	}

	// Increment the counter.
	stats.failedLogins++
	s.authStats[host] = stats

	// Check for abuse.
	if stats.failedLogins > maxFailedLogins {
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
		s.authStats[host] = authenticationStats{passwordResets: 1}
		return nil
	}

	// Increment the counter.
	stats.passwordResets++
	s.authStats[host] = stats

	// Check for abuse.
	if stats.passwordResets > maxPasswordResets {
		return errors.New("too many password reset requests from " + host)
	}

	return nil
}
