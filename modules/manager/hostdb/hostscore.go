package hostdb

import (
	"math"
	"math/big"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb/hosttree"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
)

// contractPriceForScore returns the contract price of the host used for
// scoring. Since we don't know whether rhpv2 or rhpv3 are used, we return the
// bigger one for a pesimistic score.
func contractPriceForScore(he modules.HostDBEntry) types.Currency {
	cp := he.Settings.ContractPrice
	if cp.Cmp(he.PriceTable.ContractPrice) > 0 {
		cp = he.PriceTable.ContractPrice
	}
	return cp
}

// bytesToSectors converts bytes to the number of sectors.
func bytesToSectors(bytes uint64) uint64 {
	numSectors := bytes / rhpv2.SectorSize
	if bytes%rhpv2.SectorSize != 0 {
		numSectors++
	}
	return numSectors
}

// uploadCostForScore returns the cost of the data uploading used for the
// host scoring.
func uploadCostForScore(a modules.Allowance, he modules.HostDBEntry, bytes uint64) types.Currency {
	asc := he.PriceTable.BaseCost().Add(he.PriceTable.AppendSectorCost(a.Period))
	uploadSectorCostRHPv3, _ := asc.Total()
	numSectors := bytesToSectors(bytes)
	return uploadSectorCostRHPv3.Mul64(numSectors)
}

// downloadCostForScore returns the cost of the data downloading used for the
// host scoring.
func downloadCostForScore(he modules.HostDBEntry, bytes uint64) types.Currency {
	rsc := he.PriceTable.BaseCost().Add(he.PriceTable.ReadSectorCost(rhpv2.SectorSize))
	downloadSectorCostRHPv3, _ := rsc.Total()
	numSectors := bytesToSectors(bytes)
	return downloadSectorCostRHPv3.Mul64(numSectors)
}

// storageCostForScore returns the cost of storing the data with the host used
// for the host scoring.
func storageCostForScore(a modules.Allowance, he modules.HostDBEntry, bytes uint64) types.Currency {
	asc := he.PriceTable.BaseCost().Add(he.PriceTable.AppendSectorCost(a.Period))
	storeSectorCostRHPv3 := asc.Storage
	numSectors := bytesToSectors(bytes)
	return storeSectorCostRHPv3.Mul64(numSectors)
}

// hostPeriodCostForScore is a helper function that calculates the cost of
// storing data on the host with the given allowance settings.
func hostPeriodCostForScore(a modules.Allowance, he modules.HostDBEntry) types.Currency {
	// Compute how much data we upload, download and store.
	uploadPerHost := uint64(float64(a.ExpectedUpload*a.TotalShards/a.MinShards) / float64(a.Hosts))
	downloadPerHost := uint64(float64(a.ExpectedDownload*a.TotalShards/a.MinShards) / float64(a.Hosts))
	storagePerHost := uint64(float64(a.ExpectedStorage*a.TotalShards/a.MinShards) / float64(a.Hosts))

	// Compute the individual costs.
	hostCollateral := rhpv2.ContractFormationCollateral(a.Period, storagePerHost, he.Settings)
	hostContractPrice := contractPriceForScore(he)
	hostUploadCost := uploadCostForScore(a, he, uploadPerHost)
	hostDownloadCost := downloadCostForScore(he, downloadPerHost)
	hostStorageCost := storageCostForScore(a, he, storagePerHost)
	siafundFee := hostCollateral.
		Add(hostContractPrice).
		Add(hostUploadCost).
		Add(hostDownloadCost).
		Add(hostStorageCost).
		Mul64(39).
		Div64(1000)

	// Add it all up. We multiply the contract price here since we might refresh
	// a contract multiple times.
	return hostContractPrice.Mul64(3).
		Add(hostUploadCost).
		Add(hostDownloadCost).
		Add(hostStorageCost).
		Add(siafundFee)
}

// priceAdjustmentScore computes a score between 0 and 1 for a host giving its
// price settings and the hostdb's allowance.
//   - 0.5 is returned if the host's costs exactly match the settings.
//   - If the host is cheaper than expected, a linear bonus is applied. The best
//     score of 1 is reached when the ratio between host cost and expectations is
//     10x.
//   - If the host is more expensive than expected, an exponential malus is applied.
//     A 2x ratio will already cause the score to drop to 0.16 and a 3x ratio causes
//     it to drop to 0.05.
func (hdb *HostDB) priceAdjustmentScore(allowance modules.Allowance, entry modules.HostDBEntry) float64 {
	// Divide by zero mitigation.
	if allowance.Hosts == 0 {
		allowance.Hosts = 1
	}
	if allowance.MinShards == 0 {
		allowance.MinShards = 1
	}
	if allowance.TotalShards == 0 {
		allowance.TotalShards = 1
	}

	hostPeriodBudget := allowance.Funds.Div64(allowance.Hosts)
	hostPeriodCost := hostPeriodCostForScore(allowance, entry)

	ratio := new(big.Rat).SetFrac(hostPeriodCost.Big(), hostPeriodBudget.Big())
	fRatio, _ := ratio.Float64()
	switch ratio.Cmp(new(big.Rat).SetUint64(1)) {
	case 0:
		return 0.5 // Ratio is exactly 1 -> score is 0.5.
	case 1:
		// Cost is greater than budget -> score is in range (0; 0.5).
		//
		return 1.5 / math.Pow(3, fRatio)
	case -1:
		// Cost < budget -> score is (0.5; 1].
		s := 0.44 + 0.06*(1/fRatio)
		if s > 1.0 {
			s = 1.0
		}
		return s
	}
	panic("unreachable")
}

// storageRemainingScore computes a score for a host based on its remaining
// storage and the amount of data already stored with that host.
func (hdb *HostDB) storageRemainingScore(allowance modules.Allowance, entry modules.HostDBEntry) float64 {
	// Determine how much data the renter is storing on this host.
	var storedData float64
	if cis, exists := hdb.knownContracts[entry.PublicKey.String()]; exists {
		for _, ci := range cis {
			storedData += float64(ci.StoredData)
		}
	}

	// Divide by zero mitigation.
	if allowance.Hosts == 0 {
		allowance.Hosts = 1
	}
	if allowance.MinShards == 0 {
		allowance.MinShards = 1
	}
	if allowance.TotalShards == 0 {
		allowance.TotalShards = 1
	}

	// idealDataPerHost is the amount of data that we would have to put on each
	// host assuming that our storage requirements were spread evenly across
	// every single host.
	idealDataPerHost := float64(allowance.ExpectedStorage*allowance.TotalShards/allowance.MinShards) / float64(allowance.Hosts)

	// allocationPerHost is the amount of data that we would like to be able to
	// put on each host, because data is not always spread evenly across the
	// hosts during upload. Slower hosts may get very little data, more
	// expensive hosts may get very little data, and other factors can skew the
	// distribution. allocationPerHost takes into account the skew and tries to
	// ensure that there's enough allocation per host to accommodate for a skew.
	// NOTE: assume that data is not spread evenly and the host with the most
	// data will store twice the expectation.
	allocationPerHost := idealDataPerHost * 2

	// hostExpectedStorage is the amount of storage that we expect to be able to
	// store on this host overall, which should include the stored data that is
	// already on the host.
	hostExpectedStorage := (float64(entry.Settings.RemainingStorage) * 0.25) + storedData

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

// ageScore computes a score for a host based on its age.
func (hdb *HostDB) ageScore(entry modules.HostDBEntry) float64 {
	// Sanity check.
	if entry.FirstSeen == 0 {
		return 0
	}

	weights := []struct {
		age    uint64
		factor float64
	}{
		{128, 1.5},
		{64, 2},
		{32, 2},
		{16, 2},
		{8, 3},
		{4, 3},
		{2, 3},
		{1, 3},
	}

	height := hdb.blockHeight
	age := (height - entry.FirstSeen) / modules.BlocksPerDay
	weight := 1.0
	for _, w := range weights {
		if age >= w.age {
			break
		}
		weight /= w.factor
	}

	return weight
}

// collateralScore computes a score for a host based on its collateral settings.
func (hdb *HostDB) collateralScore(allowance modules.Allowance, entry modules.HostDBEntry) float64 {
	// Divide by zero mitigation.
	if allowance.Hosts == 0 {
		allowance.Hosts = 1
	}
	if allowance.MinShards == 0 {
		allowance.MinShards = 1
	}
	if allowance.TotalShards == 0 {
		allowance.TotalShards = 1
	}

	// Ignore hosts which have set their max collateral to 0.
	if entry.Settings.MaxCollateral.IsZero() || entry.Settings.Collateral.IsZero() {
		return 0
	}

	// Convenience variables.
	duration := allowance.Period
	storage := allowance.ExpectedStorage * allowance.TotalShards / allowance.MinShards

	// Calculate the expected collateral.
	expectedCollateral := entry.Settings.Collateral.Mul64(storage).Mul64(duration)
	expectedCollateralMax := entry.Settings.MaxCollateral.Div64(2) // 2x buffer - renter may end up storing extra data.
	if expectedCollateral.Cmp(expectedCollateralMax) > 0 {
		expectedCollateral = expectedCollateralMax
	}

	// Avoid division by zero.
	if expectedCollateral.IsZero() {
		expectedCollateral = types.NewCurrency64(1)
	}

	// Determine a cutoff at 20% of the budgeted per-host funds.
	// Meaning that an 'ok' host puts in 1/5 of what the renter puts into a
	// contract. Beyond that the score increases linearly and below that
	// decreases exponentially.
	cutoff := hostPeriodCostForScore(allowance, entry).Div64(5)

	// calculate the weight. We use the same approach here as in
	// priceAdjustScore but with a different cutoff.
	ratio := new(big.Rat).SetFrac(cutoff.Big(), expectedCollateral.Big())
	fRatio, _ := ratio.Float64()
	switch ratio.Cmp(new(big.Rat).SetUint64(1)) {
	case 0:
		return 0.5 // Ratio is exactly 1 -> score is 0.5.
	case 1:
		// Collateral is below cutoff -> score is in range (0; 0.5).
		//
		return 1.5 / math.Pow(3, fRatio)
	case -1:
		// Collateral is beyond cutoff -> score is (0.5; 1].
		s := 0.5 * (1 / fRatio)
		if s > 1.0 {
			s = 1.0
		}
		return s
	}
	panic("unreachable")
}

// interactionScore computes a score for a host based on its interaction history.
func (hdb *HostDB) interactionScore(entry modules.HostDBEntry) float64 {
	success, fail := 30.0, 1.0
	success += entry.HistoricSuccessfulInteractions
	fail += entry.HistoricFailedInteractions
	return math.Pow(success/(success+fail), 10)
}

// uptimeScore computes a score for a host based on its historic uptime.
func (hdb *HostDB) uptimeScore(entry modules.HostDBEntry) float64 {
	uptime := entry.HistoricUptime
	downtime := entry.HistoricDowntime
	var lastScanSuccess, secondLastScanSuccess bool

	// Special cases.
	totalScans := len(entry.ScanHistory)
	switch totalScans {
	case 0:
		return 0.25 // No scans yet.
	case 1:
		lastScanSuccess = entry.ScanHistory[totalScans-1].Success
		if lastScanSuccess {
			return 0.75 // 1 successful scan.
		} else {
			return 0.25 // 1 failed scan.
		}
	case 2:
		lastScanSuccess = entry.ScanHistory[totalScans-1].Success
		secondLastScanSuccess = entry.ScanHistory[totalScans-2].Success
		if lastScanSuccess && secondLastScanSuccess {
			return 0.85
		} else if lastScanSuccess || secondLastScanSuccess {
			return 0.5
		} else {
			return 0.05
		}
	}

	// Account for the interval between the most recent interaction and the
	// current time.
	lastScanSuccess = entry.ScanHistory[totalScans-1].Success
	finalInterval := time.Since(entry.ScanHistory[totalScans-1].Timestamp)
	if lastScanSuccess {
		uptime += finalInterval
	} else {
		downtime += finalInterval
	}
	ratio := float64(uptime) / float64(uptime+downtime)

	// Unconditionally forgive up to 2% downtime.
	if ratio >= 0.98 {
		ratio = 1
	}

	// Forgive downtime inversely proportional to the number of interactions;
	// e.g. if we have only interacted 4 times, and half of the interactions
	// failed, assume a ratio of 88% rather than 50%.
	ratio = math.Max(ratio, 1-(0.03*float64(totalScans)))

	// Calculate the penalty for poor uptime. Penalties increase extremely
	// quickly as uptime falls away from 95%.
	return math.Pow(ratio, 200*math.Min(1-ratio, 0.30))
}

// versionScore computes a score for a host based on its version.
func (hdb *HostDB) versionScore(entry modules.HostDBEntry) float64 {
	versions := []struct {
		version string
		penalty float64
	}{
		{"1.6.0", 0.99},
		{"1.5.9", 0.00},
	}
	weight := 1.0
	for _, v := range versions {
		if build.VersionCmp(entry.Settings.Version, v.version) < 0 {
			weight *= v.penalty
		}
	}
	return weight
}

// managedCalculateHostScoreFn creates a hosttree.ScoreFunc given an
// Allowance.
//
// NOTE: the hosttree.ScoreFunc that is returned accesses fields of the hostdb.
// The hostdb lock must be held while utilizing the ScoreFunc.
func (hdb *HostDB) managedCalculateHostScoreFn(allowance modules.Allowance) hosttree.ScoreFunc {
	return func(entry modules.HostDBEntry) hosttree.ScoreBreakdown {
		return hosttree.HostAdjustments{
			AgeAdjustment:              hdb.ageScore(entry),
			CollateralAdjustment:       hdb.collateralScore(allowance, entry),
			InteractionAdjustment:      hdb.interactionScore(entry),
			PriceAdjustment:            hdb.priceAdjustmentScore(allowance, entry),
			StorageRemainingAdjustment: hdb.storageRemainingScore(allowance, entry),
			UptimeAdjustment:           hdb.uptimeScore(entry),
			VersionAdjustment:          hdb.versionScore(entry),
		}
	}
}

// EstimateHostScore takes the host's settings and returns the estimated
// score of that host in the hostdb.
func (hdb *HostDB) EstimateHostScore(allowance modules.Allowance, entry modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	if err := hdb.tg.Add(); err != nil {
		return modules.HostScoreBreakdown{}, err
	}
	defer hdb.tg.Done()
	return hdb.managedEstimatedScoreBreakdown(allowance, entry)
}

// ScoreBreakdown provides a detailed set of elements of the host's overall score.
func (hdb *HostDB) ScoreBreakdown(entry modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	if err := hdb.tg.Add(); err != nil {
		return modules.HostScoreBreakdown{}, err
	}
	defer hdb.tg.Done()
	return hdb.managedScoreBreakdown(entry)
}

// managedEstimatedScoreBreakdown computes the score breakdown of a host.
func (hdb *HostDB) managedEstimatedScoreBreakdown(allowance modules.Allowance, entry modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	scoreFunc := hdb.managedCalculateHostScoreFn(allowance)

	return scoreFunc(entry).HostScoreBreakdown(), nil
}

// managedScoreBreakdown computes the score breakdown of a host.
func (hdb *HostDB) managedScoreBreakdown(entry modules.HostDBEntry) (modules.HostScoreBreakdown, error) {
	return hdb.scoreFunc(entry).HostScoreBreakdown(), nil
}
