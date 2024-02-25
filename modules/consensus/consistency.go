package consensus

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"

	"lukechampine.com/frand"
)

// manageErr handles an error detected by the consistency checks.
func manageErr(tx *sql.Tx, err error) {
	markInconsistency(tx)
	fmt.Println(err)
}

// checkSiacoinCount checks that the number of siacoins countable within the
// consensus set equal the expected number of siacoins for the block height.
func checkSiacoinCount(tx *sql.Tx) {
	// Add all of the delayed Siacoin outputs.
	dscoSiacoins, err := countDelayedSiacoins(tx)
	if err != nil {
		manageErr(tx, err)
	}

	// Add all of the Siacoin outputs.
	scoSiacoins, err := countSiacoins(tx)
	if err != nil {
		manageErr(tx, err)
	}

	// Add all of the payouts from file contracts.
	fcSiacoins, err := countFileContractPayouts(tx)
	if err != nil {
		manageErr(tx, err)
	}

	// Add all of the siafund claims.
	claimSiacoins, err := countSiafundClaims(tx)
	if err != nil {
		manageErr(tx, err)
	}

	expectedSiacoins := modules.CalculateNumSiacoins(blockHeight(tx))
	totalSiacoins := dscoSiacoins.Add(scoSiacoins).Add(fcSiacoins).Add(claimSiacoins)
	if !totalSiacoins.Equals(expectedSiacoins) {
		diagnostics := fmt.Sprintf("Number of Siacoins\nDsco: %v\nSco: %v\nFc: %v\nClaim: %v\n", dscoSiacoins, scoSiacoins, fcSiacoins, claimSiacoins)
		if totalSiacoins.Cmp(expectedSiacoins) < 0 {
			diagnostics += fmt.Sprintf("total: %v\nexpected: %v\n expected is bigger: %v", totalSiacoins, expectedSiacoins, expectedSiacoins.Sub(totalSiacoins))
		} else {
			diagnostics += fmt.Sprintf("total: %v\nexpected: %v\n expected is smaller: %v", totalSiacoins, expectedSiacoins, totalSiacoins.Sub(expectedSiacoins))
		}
		manageErr(tx, errors.New(diagnostics))
	}
}

// checkSiafundCount checks that the number of siafunds countable within the
// consensus set equal the expected number of siafunds for the block height.
func checkSiafundCount(tx *sql.Tx) {
	total, err := countSiafunds(tx)
	if err != nil {
		manageErr(tx, err)
	}
	if total != modules.SiafundCount {
		manageErr(tx, errors.New("wrong number of siafunds in the consensus set"))
	}
}

// checkDSCOs scans the sets of delayed siacoin outputs and checks for
// consistency.
func checkDSCOs(tx *sql.Tx) {
	// Create a map to track which delayed siacoin output maps exist,
	// a map to track which ids have appeared in the dsco set, and another
	// map to count the values.
	dscoTracker := make(map[uint64]struct{})
	idMap := make(map[types.SiacoinOutputID]struct{})
	total := make(map[uint64]types.Currency)

	// Iterate through all the delayed siacoin outputs, and check that they
	// are for the correct heights.
	rows, err := tx.Query("SELECT height, scoid, bytes FROM cs_dsco")
	if err != nil {
		manageErr(tx, err)
		return
	}

	for rows.Next() {
		var height uint64
		var scoid types.SiacoinOutputID
		id := make([]byte, 32)
		var scoBytes []byte
		if err := rows.Scan(&height, &id, &scoBytes); err != nil {
			rows.Close()
			manageErr(tx, err)
			return
		}

		dscoTracker[height] = struct{}{}

		// Check that the output id has not appeared in another dsco.
		copy(scoid[:], id[:])
		_, exists := idMap[scoid]
		if exists {
			rows.Close()
			manageErr(tx, errors.New("repeat delayed siacoin output"))
			return
		}
		idMap[scoid] = struct{}{}

		// Sum the funds.
		var sco types.SiacoinOutput
		d := types.NewBufDecoder(scoBytes)
		sco.DecodeFrom(d)
		if err := d.Err(); err != nil {
			rows.Close()
			manageErr(tx, err)
			return
		}
		if t, exists := total[height]; exists {
			total[height] = t.Add(sco.Value)
		} else {
			total[height] = sco.Value
		}
	}
	rows.Close()

	// Check that the minimum value has been achieved - the coinbase from
	// an earlier block is guaranteed to be in the bucket.
	for height, value := range total {
		minimumValue := modules.CalculateCoinbase(height - modules.MaturityDelay)
		if value.Cmp(minimumValue) < 0 {
			manageErr(tx, errors.New("total number of coins in the delayed output bucket is incorrect"))
			return
		}
	}

	// Check that all of the correct heights are represented.
	currentHeight := blockHeight(tx)
	expectedBuckets := 0
	for i := currentHeight + 1; i <= currentHeight+modules.MaturityDelay; i++ {
		if i < modules.MaturityDelay {
			continue
		}
		_, exists := dscoTracker[i]
		if !exists {
			manageErr(tx, errors.New("missing a dsco bucket"))
			return
		}
		expectedBuckets++
	}
	if len(dscoTracker) != expectedBuckets {
		manageErr(tx, errors.New("too many dsco buckets"))
	}
}

// checkConsistency runs a series of checks to make sure that the consensus set
// is consistent with some rules that should always be true.
func (cs *ConsensusSet) checkConsistency(tx *sql.Tx) {
	if cs.checkingConsistency {
		return
	}

	cs.checkingConsistency = true
	checkDSCOs(tx)
	checkSiacoinCount(tx)
	checkSiafundCount(tx)
	cs.checkingConsistency = false
}

// maybeCheckConsistency runs a consistency check with a small probability.
// Useful for detecting database corruption in production without needing to go
// through the extremely slow process of running a consistency check every
// block.
func (cs *ConsensusSet) maybeCheckConsistency(tx *sql.Tx) {
	if frand.Intn(1000) == 0 {
		cs.checkConsistency(tx)
	}
}

// TODO: Check that every file contract has an expiration too, and that the
// number of file contracts + the number of expirations is equal.
