package contractor

import (
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// managedFindMinAllowedHostScores uses a set of random hosts from the hostdb to
// calculate minimum acceptable score for a host to be marked GFR and GFU.
func (c *Contractor) managedFindMinAllowedHostScores(rpk types.PublicKey) (types.Currency, types.Currency, error) {
	// Pull a new set of hosts from the hostdb that could be used as a new set
	// to match the allowance. The lowest scoring host of these new hosts will
	// be used as a baseline for determining whether our existing contracts are
	// worthwhile.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.ZeroCurrency, types.ZeroCurrency, ErrRenterNotFound
	}
	hostCount := int(renter.Allowance.Hosts)
	hosts, err := c.hdb.RandomHostsWithAllowance(hostCount+randomHostsBufferForScore, nil, nil, renter.Allowance)
	if err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, err
	}

	if len(hosts) == 0 {
		return types.ZeroCurrency, types.ZeroCurrency, errors.New("no hosts returned in RandomHostsWithAllowance")
	}

	// Find the minimum score that a host is allowed to have to be considered
	// good for upload.
	var minScoreGFR, minScoreGFU types.Currency
	sb, err := c.hdb.ScoreBreakdown(hosts[0])
	if err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, err
	}

	lowestScore := sb.Score
	for i := 1; i < len(hosts); i++ {
		score, err := c.hdb.ScoreBreakdown(hosts[i])
		if err != nil {
			return types.ZeroCurrency, types.ZeroCurrency, err
		}
		if score.Score.Cmp(lowestScore) < 0 {
			lowestScore = score.Score
		}
	}
	// Set the minimum acceptable score to a factor of the lowest score.
	minScoreGFR = lowestScore.Div(scoreLeewayGoodForRenew)
	minScoreGFU = lowestScore.Div(scoreLeewayGoodForUpload)

	return minScoreGFR, minScoreGFU, nil
}

// managedMarkContractUtility checks an active contract in the contractor and
// figures out whether the contract is useful for uploading, and whether the
// contract should be renewed.
func (c *Contractor) managedMarkContractUtility(contract modules.RenterContract, minScoreGFR, minScoreGFU types.Currency) error {
	// Acquire contract.
	sc, ok := c.staticContracts.Acquire(contract.ID)
	if !ok {
		return errors.New("managedMarkContractUtility: unable to acquire contract")
	}
	defer c.staticContracts.Return(sc)

	// Get latest metadata.
	u := sc.Metadata().Utility

	// If the utility is locked, do nothing.
	if u.Locked {
		return nil
	}

	// Get host from hostdb and check that it's not filtered.
	host, u, needsUpdate := c.managedHostInHostDBCheck(contract)
	if needsUpdate {
		if err := c.managedUpdateContractUtility(sc, u); err != nil {
			c.log.Println("ERROR: unable to acquire and update contract utility:", err)
			return modules.AddContext(err, "unable to update utility after hostdb check")
		}
		return nil
	}

	// Do critical contract checks and update the utility if any checks fail.
	u, needsUpdate = c.managedCriticalUtilityChecks(sc, host)
	if needsUpdate {
		err := c.managedUpdateContractUtility(sc, u)
		if err != nil {
			c.log.Println("ERROR: unable to acquire and update contract utility:", err)
			return modules.AddContext(err, "unable to update utility after criticalUtilityChecks")
		}
		return nil
	}

	sb, err := c.hdb.ScoreBreakdown(host)
	if err != nil {
		c.log.Println("ERROR: unable to get ScoreBreakdown for", host.PublicKey.String(), "got err:", err)
		return nil // It may just be this host that has an issue.
	}

	// Check the host scorebreakdown against the minimum accepted scores.
	u, utilityUpdateStatus := c.managedCheckHostScore(contract, sb, minScoreGFR, minScoreGFU)
	if utilityUpdateStatus == necessaryUtilityUpdate {
		err = c.managedUpdateContractUtility(sc, u)
		if err != nil {
			c.log.Println("ERROR: unable to acquire and update contract utility:", err)
			return modules.AddContext(err, "unable to update utility after checkHostScore")
		}
		return nil
	}

	// All checks passed, marking contract as GFU and GFR.
	if !u.GoodForUpload || !u.GoodForRenew {
		c.log.Println("INFO: marking contract as being both GoodForUpload and GoodForRenew:", u.GoodForUpload, u.GoodForRenew, contract.ID)
	}
	u.GoodForUpload = true
	u.GoodForRenew = true

	// Apply changes.
	err = c.managedUpdateContractUtility(sc, u)
	if err != nil {
		c.log.Println("ERROR: unable to acquire and update contract utility:", err)
		return modules.AddContext(err, "unable to update utility after all checks passed.")
	}

	return nil
}

// managedMarkContractsUtility checks every active contract in the contractor and
// figures out whether the contract is useful for uploading, and whether the
// contract should be renewed.
func (c *Contractor) managedMarkContractsUtility(rpk types.PublicKey) error {
	minScoreGFR, minScoreGFU, err := c.managedFindMinAllowedHostScores(rpk)
	if err != nil {
		return err
	}

	// Update utility fields for each contract.
	for _, contract := range c.staticContracts.ByRenter(rpk) {
		err := c.managedMarkContractUtility(contract, minScoreGFR, minScoreGFU)
		if err != nil {
			return err
		}
	}

	return nil
}
