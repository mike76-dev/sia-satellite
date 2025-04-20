package hostdb

import (
	"math"
	"math/big"

	"github.com/mike76-dev/sia-satellite/external"
	rhpv4 "go.sia.tech/core/rhp/v4"
	"go.sia.tech/core/types"
	renterd "go.sia.tech/renterd/api"
)

// RenterSettings combines the relevant renter-specific settings.
type RenterSettings struct {
	MaxContractPrice types.Currency `json:"maxContractPriuce"`
	MaxDownloadPrice types.Currency `json:"maxDownloadPrice"`
	MaxUploadPrice   types.Currency `json:"maxUploadPrice"`
	MaxStoragePrice  types.Currency `json:"maxStoragePrice"`
	MinShards        int            `json:"minShards"`
	TotalShards      int            `json:"totalShards"`
}

// ScoreBasis defines the basis for calculating the score.
type ScoreBasis int

const (
	ScoreBasisGlobal ScoreBasis = iota
	ScoreBasisEU
	ScoreBasisUS
	ScoreBasisAP
)

// calculateScore calculates the score of a host using the
// renter-specific metrics.
func calculateScore(host HostDBEntry, config renterd.ContractsConfig, settings RenterSettings, basis ScoreBasis) external.HostScoreBreakdown {
	expectedRedundancy := 1.0
	if settings.MinShards > 0 {
		expectedRedundancy = float64(settings.TotalShards) / float64(settings.MinShards)
	}
	// idealDataPerHost is the amount of data that we would have to put on each
	// host assuming that our storage requirements were spread evenly across
	// every single host.
	idealDataPerHost := float64(config.Storage) * expectedRedundancy / float64(config.Amount)

	// allocationPerHost is the amount of data that we would like to be able to
	// put on each host, because data is not always spread evenly across the
	// hosts during upload. Slower hosts may get very little data, more
	// expensive hosts may get very little data, and other factors can skew the
	// distribution. allocationPerHost takes into account the skew and tries to
	// ensure that there's enough allocation per host to accommodate for a skew.
	// NOTE: assume that data is not spread evenly and the host with the most
	// data will store twice the expectation
	allocationPerHost := idealDataPerHost * 2

	appendCost := host.Settings.Prices.RPCAppendSectorsCost(1, config.Period).RenterCost()
	uploadCost := host.Settings.Prices.RPCWriteSectorCost(rhpv4.SectorSize).RenterCost()
	uploadSectorCost := appendCost.Add(uploadCost)
	maxCollateral := host.Settings.MaxCollateral
	collateral := host.Settings.Prices.Collateral
	egressPrice := host.Settings.Prices.RPCReadSectorCost(rhpv4.SectorSize).RenterCost().Div64(rhpv4.SectorSize)
	ingressPrice := host.Settings.Prices.RPCWriteSectorCost(rhpv4.SectorSize).RenterCost().Div64(rhpv4.SectorSize)
	storagePrice := host.Settings.Prices.RPCAppendSectorsCost(1, config.Period).RenterCost().Div64(rhpv4.SectorSize)
	remainingStorage := host.Settings.RemainingStorage * rhpv4.SectorSize

	var is, us, ls, bs float64
	var ok bool
	var interactions external.HostInteraction
	switch basis {
	case ScoreBasisEU:
		interactions, ok = host.Interactions["europe"]
	case ScoreBasisUS:
		interactions, ok = host.Interactions["east-us"]
	case ScoreBasisAP:
		interactions, ok = host.Interactions["asia"]
	}

	if ok {
		is = interactions.Score.InteractionsScore
		us = interactions.Score.UptimeScore
		ls = interactions.Score.LatencyScore
		bs = interactions.Score.BenchmarksScore
	} else if basis == ScoreBasisGlobal {
		is = host.Score.InteractionsScore
		us = host.Score.UptimeScore
		ls = host.Score.LatencyScore
		bs = host.Score.BenchmarksScore
	}

	hsb := external.HostScoreBreakdown{
		AgeScore:          host.Score.AgeScore,
		CollateralScore:   collateralScore(uploadSectorCost, maxCollateral, collateral, uint64(allocationPerHost), config.Period),
		InteractionsScore: is,
		PricesScore:       priceAdjustmentScore(egressPrice, ingressPrice, storagePrice, settings),
		StorageScore:      storageRemainingScore(remainingStorage, allocationPerHost),
		UptimeScore:       us,
		VersionScore:      host.Score.VersionScore,
		ContractsScore:    host.Score.ContractsScore,
		LatencyScore:      ls,
		BenchmarksScore:   bs,
	}

	hsb.TotalScore = hsb.Score()
	return hsb
}

// priceAdjustmentScore computes a score between 0 and 1 for a host giving its
// price settings and the renter's configuration.
//   - 0.5 is returned if the host's costs exactly match the settings.
//   - If the host is cheaper than expected, a linear bonus is applied. The best
//     score of 1 is reached when the ratio between host cost and expectations is
//     10x.
//   - If the host is more expensive than expected, an exponential malus is applied.
//     A 2x ratio will already cause the score to drop to 0.16 and a 3x ratio causes
//     it to drop to 0.05.
func priceAdjustmentScore(dppb, uppb, sppb types.Currency, rs RenterSettings) float64 {
	priceScore := func(actual, max types.Currency) float64 {
		threshold := max.Div64(2)
		if threshold.IsZero() {
			return 1.0 // no gouging settings defined
		}

		ratio := new(big.Rat).SetFrac(actual.Big(), threshold.Big())
		fRatio, _ := ratio.Float64()
		switch ratio.Cmp(new(big.Rat).SetUint64(1)) {
		case 0:
			return 0.5 // ratio is exactly 1 -> score is 0.5
		case 1:
			// actual is greater than threshold -> score is in range (0; 0.5)
			return 1.5 / math.Pow(3, fRatio)
		case -1:
			// actual < threshold -> score is (0.5; 1]
			s := 0.5 * (1 / fRatio)
			if s > 1.0 {
				s = 1.0
			}
			return s
		}
		panic("unreachable")
	}

	// Compute scores for download, upload and storage and combine them.
	downloadScore := priceScore(dppb, rs.MaxDownloadPrice)
	uploadScore := priceScore(uppb, rs.MaxUploadPrice)
	storageScore := priceScore(sppb, rs.MaxStoragePrice)
	return (downloadScore + uploadScore + storageScore) / 3.0
}

func storageRemainingScore(remainingStorage uint64, allocationPerHost float64) float64 {
	// hostExpectedStorage is the amount of storage that we expect to be able to
	// store on this host overall.
	hostExpectedStorage := float64(remainingStorage) * 0.25
	// The score for the host is the square of the amount of storage we
	// expected divided by the amount of storage we want. If we expect to be
	// able to store more data on the host than we need to allocate, the host
	// gets full score for storage.
	if hostExpectedStorage >= allocationPerHost {
		return 1
	}
	// Otherwise, the score of the host is the fraction of the data we expect
	// raised to the storage penalty exponentiation.
	storageRatio := hostExpectedStorage / allocationPerHost
	return math.Pow(storageRatio, 2.0)
}

// collateralScore computes the score a host receives for its collateral
// settings relative to its prices. The params have the following meaning
// 'uploadSectorCost' - the cost of uploading and storing a sector worth of data
// 'maxCollateral' - the maximum collateral the host is willing to put up
// 'collateralCost' - the amount of collateral the host is willing to put up per byte
// 'allocationPerHost' - amount of data we expect to store on the host
// 'period' - the period for which the collateral is put up.
func collateralScore(uploadSectorCost, maxCollateral, collateralCost types.Currency, allocationPerHost, period uint64) float64 {
	// Ignore hosts which have set their max collateral to 0.
	if maxCollateral.IsZero() || collateralCost.IsZero() {
		return 0
	}

	// Convenience variables.
	ratioNum := uint64(3)
	ratioDenom := uint64(2)

	// Compute the cost of storing.
	numSectors := bytesToSectors(allocationPerHost)
	storageCost := uploadSectorCost.Mul64(numSectors)

	// Calculate the expected collateral for the host allocation.
	expectedCollateral := collateralCost.Mul64(allocationPerHost).Mul64(period)
	if expectedCollateral.Cmp(maxCollateral) > 0 {
		expectedCollateral = maxCollateral
	}

	// Avoid division by zero.
	if expectedCollateral.IsZero() {
		expectedCollateral = types.NewCurrency64(1)
	}

	// Determine a cutoff at 150% of the storage cost. Meaning that a host
	// should be willing to put in at least 1.5x the amount of money the renter
	// expects to spend on storage on that host.
	cutoff := storageCost.Mul64(ratioNum).Div64(ratioDenom)

	// The score is a linear function between 0 and 1 where the upper limit is
	// 4 times the cutoff. Beyond that, we don't care if a host puts in more
	// collateral.
	cutoffMultiplier := uint64(4)

	if expectedCollateral.Cmp(cutoff) < 0 {
		return math.SmallestNonzeroFloat64 // expectedCollateral <= cutoff -> score is basically 0
	} else if expectedCollateral.Cmp(cutoff.Mul64(cutoffMultiplier)) >= 0 {
		return 1 // expectedCollateral is 10x cutoff -> score is 1
	} else {
		// Perform linear interpolation for all other values.
		slope := new(big.Rat).SetFrac(new(big.Int).SetInt64(1), cutoff.Mul64(cutoffMultiplier).Big())
		intercept := new(big.Rat).Mul(slope, new(big.Rat).SetInt(cutoff.Big())).Neg(slope)
		score := new(big.Rat).SetInt(expectedCollateral.Big())
		score = score.Mul(score, slope)
		score = score.Add(score, intercept)
		fScore, _ := score.Float64()
		if fScore > 1 {
			return 1.0
		}
		return fScore
	}
}

func bytesToSectors(bytes uint64) uint64 {
	numSectors := bytes / rhpv4.SectorSize
	if bytes%rhpv4.SectorSize != 0 {
		numSectors++
	}
	return numSectors
}

// calculateBenchmarkData calculates the average benchmark results of a host.
func calculateBenchmarkData(host HostDBEntry, basis ScoreBasis) (int64, float64, float64) {
	var interactions external.HostInteraction
	var ok bool
	switch basis {
	case ScoreBasisEU:
		interactions, ok = host.Interactions["europe"]
	case ScoreBasisUS:
		interactions, ok = host.Interactions["east-us"]
	case ScoreBasisAP:
		interactions, ok = host.Interactions["asia"]
	}

	if !ok { // the host was probably not benchmarked by this node yet
		return -1, 0, 0
	}

	var count int
	var latency int64
	var ul, dl float64
	for _, scan := range interactions.ScanHistory {
		if scan.Success {
			count++
			latency += scan.Latency.Nanoseconds()
		}
	}
	if count > 0 {
		latency /= int64(count)
	} else {
		latency = -1
	}

	count = 0
	for _, benchmark := range interactions.BenchmarkHistory {
		if benchmark.Success {
			count++
			ul += benchmark.UploadSpeed
			dl += benchmark.DownloadSpeed
		}
	}
	if count > 0 {
		ul /= float64(count)
		dl /= float64(count)
	}

	return latency, ul, dl
}

// calculateGlobalBenchmarkData calculates the average benchmark results of a host globally.
func calculateGlobalBenchmarkData(host HostDBEntry) (int64, float64, float64) {
	l1, ul1, dl1 := calculateBenchmarkData(host, ScoreBasisEU)
	l2, ul2, dl2 := calculateBenchmarkData(host, ScoreBasisUS)
	l3, ul3, dl3 := calculateBenchmarkData(host, ScoreBasisAP)

	var latency int64
	var ul, dl float64
	var count int
	if l1 > 0 {
		latency += l1
		ul += ul1
		dl += dl1
		count++
	}

	if l2 > 0 {
		latency += l2
		ul += ul2
		dl += dl2
		count++
	}

	if l3 > 0 {
		latency += l3
		ul += ul3
		dl += dl3
		count++
	}

	if count == 0 {
		return -1, 0, 0
	} else {
		return latency / int64(count), ul / float64(count), dl / float64(count)
	}
}
