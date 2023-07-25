package modules

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

	// GetCredits retrieves the promotion data.
	GetCredits() CreditData

	// SetCredits updates the promotion data.
	SetCredits(CreditData)
}
