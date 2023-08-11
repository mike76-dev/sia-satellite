package contractor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

// managedUploadSector uploads a single sector to the host.
func (c *Contractor) managedUploadSector(ctx context.Context, rpk, hpk types.PublicKey, sector *[rhpv2.SectorSize]byte) (root types.Hash256, err error) {
	// Get the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.Hash256{}, ErrRenterNotFound
	}

	// Get the contract ID.
	var fcid types.FileContractID
	contracts := c.staticContracts.ByRenter(rpk)
	for _, contract := range contracts {
		if contract.HostPublicKey == hpk {
			fcid = contract.ID
			break
		}
	}
	if fcid == (types.FileContractID{}) {
		return types.Hash256{}, errors.New("couldn't find the contract")
	}

	// Fetch the host.
	host, exists, err := c.hdb.Host(hpk)
	if err != nil {
		return types.Hash256{}, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return types.Hash256{}, errHostNotFound
	}
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return types.Hash256{}, modules.AddContext(err, "failed to get host name")
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Increase Successful/Failed interactions accordingly.
	var hostFault bool
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(hpk)
			if hostFault {
				err = fmt.Errorf("%v: %v", errHostFault, err)
			}
		} else {
			c.hdb.IncrementSuccessfulInteractions(hpk)
		}
	}()

	// Derive the ephemeral keys.
	esk := modules.DeriveEphemeralKey(renter.PrivateKey, hpk)
	ak := modules.DeriveAccountKey(renter.AccountKey, hpk)

	// Derive the account ID.
	accountID := rhpv3.Account(ak.PublicKey())

	// Initiate the protocol.
	err = proto.WithTransportV3(ctx, siamuxAddr, hpk, func(t *rhpv3.Transport) (err error) {
		// Fetch the latest revision.
		rev, err := proto.RPCLatestRevision(ctx, t, fcid)
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get latest revision")
		}
		if rev.RevisionNumber == math.MaxUint64 {
			return fmt.Errorf("revision number has reached max, fcid %v", rev.ParentID)
		}

		// Get the cost estimation.
		cost, _, _, err := proto.UploadSectorCost(host.PriceTable, rev.WindowEnd)
		if err != nil {
			return modules.AddContext(err, "unable to estimate costs")
		}

		// If the cost is greater than MaxEphemeralAccountBalance, we pay by contract.
		var byContract bool
		if cost.Cmp(host.Settings.MaxEphemeralAccountBalance) > 0 {
			byContract = true
		}

		// Fetch the price table.
		pt, err := proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
			payment := rhpv3.PayByEphemeralAccount(accountID, pt.UpdatePriceTableCost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
			return &payment, nil
		})
		if errors.Is(err, errBalanceInsufficient) {
			pt, err = proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
				payment, err := payByContract(&rev, pt.UpdatePriceTableCost, accountID, esk)
				if err != nil {
					return nil, err
				}
				return &payment, nil
			})
		}
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get price table")
		}

		if byContract {
			payment, ok := rhpv3.PayByContract(&rev, cost, accountID, esk)
			if !ok {
				return errors.New("failed to create payment")
			}

			root, _, err = proto.RPCAppendSector(ctx, t, esk, pt, rev, &payment, sector)
			if err != nil {
				hostFault = true
				return err
			}

			// Update the contract.
			err = c.UpdateContract(rev, nil, cost, types.ZeroCurrency, types.ZeroCurrency)
			if err != nil {
				return modules.AddContext(err, "unable to update contract")
			}

			return nil
		}

		// Fetch the account balance.
		payment := rhpv3.PayByEphemeralAccount(accountID, pt.AccountBalanceCost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
		curr, err := proto.RPCAccountBalance(ctx, t, &payment, accountID, pt.UID)
		if errors.Is(err, errBalanceInsufficient) {
			payment, err := payByContract(&rev, pt.AccountBalanceCost, accountID, esk)
			if err != nil {
				return err
			}
			curr, err = proto.RPCAccountBalance(ctx, t, &payment, accountID, pt.UID)
		}
		if err != nil {
			hostFault = true
			return err
		}

		// Fund the account if the balance is insufficient.
		if curr.Cmp(cost) < 0 {
			amount := cost.Sub(curr)

			// Cap the amount by the amount of money left in the contract.
			renterFunds := rev.ValidRenterPayout()
			possibleFundCost := pt.FundAccountCost.Add(pt.UpdatePriceTableCost)
			if renterFunds.Cmp(possibleFundCost) <= 0 {
				return fmt.Errorf("insufficient funds to fund account: %v <= %v", renterFunds, possibleFundCost)
			} else if maxAmount := renterFunds.Sub(possibleFundCost); maxAmount.Cmp(amount) < 0 {
				amount = maxAmount
			}

			amount = amount.Add(pt.FundAccountCost)
			pmt, err := payByContract(&rev, amount, rhpv3.Account{}, esk) // No account needed for funding.
			if err != nil {
				return err
			}
			if err := proto.RPCFundAccount(ctx, t, &pmt, accountID, pt.UID); err != nil {
				hostFault = true
				return fmt.Errorf("failed to fund account with %v; %w", amount, err)
			}

			// Update the contract.
			err = c.UpdateContract(rev, nil, types.ZeroCurrency, types.ZeroCurrency, amount)
			if err != nil {
				return modules.AddContext(err, "unable to update contract")
			}
		}

		// Upload the data.
		payment = rhpv3.PayByEphemeralAccount(accountID, cost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
		root, _, err = proto.RPCAppendSector(ctx, t, esk, pt, rev, &payment, sector)
		if err != nil {
			hostFault = true
			return err
		}

		return nil
	})

	return
}
