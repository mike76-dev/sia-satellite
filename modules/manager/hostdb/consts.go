package hostdb

import (
	"time"
)

const (
	// historicInteractionDecay defines the decay of the HistoricSuccessfulInteractions
	// and HistoricFailedInteractions after every block for a host entry.
	historicInteractionDecay = 0.9995

	// historicInteractionDecayLimit defines the number of historic
	// interactions required before decay is applied.
	historicInteractionDecayLimit = 500

	// hostRequestTimeout indicates how long a host has to respond to a dial.
	hostRequestTimeout = 2 * time.Minute

	// hostScanDeadline indicates how long a host has to complete an entire
	// scan.
	hostScanDeadline = 4 * time.Minute

	// maxHostDowntime specifies the maximum amount of time that a host is
	// allowed to be offline while still being in the hostdb.
	maxHostDowntime       = maxHostDownTimeInDays * 24 * time.Hour
	maxHostDownTimeInDays = 20

	// maxSettingsLen indicates how long in bytes the host settings field is
	// allowed to be before being ignored as a DoS attempt.
	maxSettingsLen = 10e3

	// minScans specifies the number of scans that a host should have before the
	// scans start getting compressed.
	minScans = 12

	// minScansForSpeedup is the number of successful scan that needs to be
	// completed before the dial up timeout for scans is reduced. This ensures
	// that we have a sufficient sample size of scans for estimating the worst
	// case timeout.
	minScansForSpeedup = 25

	// recentInteractionWeightLimit caps the number of recent interactions as a
	// percentage of the historic interactions, to be certain that a large
	// amount of activity in a short period of time does not overwhelm the
	// score for a host.
	//
	// Non-stop heavy interactions for half a day can result in gaining more
	// than half the total weight at this limit.
	recentInteractionWeightLimit = 0.01

	// scanSpeedupMedianMultiplier is the number with which the median of the
	// initial scans is multiplied to speedup the initial scan after
	// minScansForSpeedup successful scans.
	scanSpeedupMedianMultiplier = 5

	// hostCheckupQuantity specifies the number of hosts that get scanned every
	// time there is a regular scanning operation.
	hostCheckupQuantity = 2500

	// scanningThreads is the number of threads that will be probing hosts for
	// their settings and checking for reliability.
	maxScanningThreads = 80

	// maxScanSleep is the maximum amount of time that the hostdb will sleep
	// between performing scans of the hosts.
	maxScanSleep = 8 * time.Hour

	// scanCheckInterval is the interval used when waiting for the scanList to
	// empty itself and for waiting on the consensus set to be synced.
	scanCheckInterval = time.Second

	// minScanSleep is the minimum amount of time that the hostdb will sleep
	// between performing scans of the hosts.
	minScanSleep = time.Hour + 20*time.Minute
)
