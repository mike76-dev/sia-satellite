package modules

import "time"

// OnHoldThreshold is how much time shall pass until we put the
// account in pre-payment mode.
const OnHoldThreshold = 24 * time.Hour

// CreditData contains the information about any running promotion.
type CreditData struct {
	Amount    float64 `json:"amount"`
	Remaining uint64  `json:"remaining"`
}

// Portal implements the portal server.
type Portal interface {
	Alerter

	// Close safely shuts down the portal.
	Close() error

	// GetAnnouncement returns the current portal announcement.
	GetAnnouncement() (string, error)

	// GetCredits retrieves the promotion data.
	GetCredits() CreditData

	// SetAnnouncement sets a new portal announcement.
	SetAnnouncement(string) error

	// SetCredits updates the promotion data.
	SetCredits(CreditData)
}
