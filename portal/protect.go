package portal

import (
	"errors"
	"net"
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

var (
	// maxSignupRequests is how many new accounts may be created in a
	// given time from the same IP. It is unlikely that a user would
	// need to register more than one account.
	maxSignupRequests = 3 / time.Hour.Seconds()
)

type (
	// authAttempts keeps track of specific authentication activities.
	authAttempts struct {
		FirstAttempt int64 `json: "first"`
		LastAttempt  int64 `json: "last"`
		Count        int64 `json: "count"`
	}

	// authenticationStats is the summary of authentication attempts
	// from a single IP address.
	authenticationStats struct {
		RemoteHost       string       `json: "host"`
		SuccessfulLogins *authAttempts `json: "loginsuccess"`
		FailedLogins     *authAttempts `json: "loginfail"`
		SignupRequests   *authAttempts `json: "signup"`
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
				sl := now - entry.SuccessfulLogins.LastAttempt
				fl := now - entry.FailedLogins.LastAttempt
				sr := now - entry.SignupRequests.LastAttempt
				pr := now - entry.PasswordResets.LastAttempt
				min := sl
				if fl < min {
					min = fl
				}
				if sr < min {
					min = sr
				}
				if pr < min {
					min = pr
				}
				if float64(min) > authStatsPruneThreshold.Seconds() {
					delete(p.authStats, ip)
				}
			}
		}()
	}
}

// checkAndUpdateSignupRequests checks if there are too many signup
// requests coming from the same IP and updates the stats.
func (p *Portal) checkAndUpdateSignupRequests(addr string) error {
	host, _, _ := net.SplitHostPort(addr)
	p.mu.Lock()
	defer p.mu.Unlock()

	as, ok := p.authStats[host]

	// No such IP in the map yet.
	if !ok {
		p.authStats[host] = authenticationStats{
			RemoteHost: host,
			SignupRequests: &authAttempts{
				FirstAttempt: time.Now().Unix(),
				LastAttempt: time.Now().Unix(),
				Count: 1,
			},
		}
		return nil
	}

	// IP exists but no signup attempts yet.
	if (as.SignupRequests.Count == 0) {
		p.authStats[host].SignupRequests.FirstAttempt = time.Now().Unix()
		p.authStats[host].SignupRequests.LastAttempt = time.Now().Unix()
		p.authStats[host].SignupRequests.Count = 1
		return nil
	}

	// Check for abuse.
	p.authStats[host].SignupRequests.LastAttempt = time.Now().Unix()
	p.authStats[host].SignupRequests.Count++
	span := time.Now().Unix() - as.SignupRequests.FirstAttempt
	if span == 0 {
		span = 1 // To avoid division by zero.
	}

	if float64(as.SignupRequests.Count) / float64(span) > maxSignupRequests {
		return errors.New("too many signup requests from " + host)
	}

	return nil
}
