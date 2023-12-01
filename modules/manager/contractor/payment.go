package contractor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

var (
	// errBalanceInsufficient occurs when a withdrawal failed because the
	// account balance was insufficient.
	errBalanceInsufficient = errors.New("ephemeral account balance was insufficient")

	// errInsufficientFunds is returned by various RPCs when the satellite is
	// unable to provide sufficient payment to the host.
	errInsufficientFunds = errors.New("insufficient funds")

	// errMaxRevisionReached occurs when trying to revise a contract that has
	// already reached the highest possible revision number. Usually happens
	// when trying to use a renewed contract.
	errMaxRevisionReached = errors.New("contract has reached the maximum number of revisions")

	// errPriceTableExpired is returned by the host when the price table that
	// corresponds to the id it was given is already expired and thus no longer
	// valid.
	errPriceTableExpired = errors.New("price table requested is expired")

	// errPriceTableNotFound is returned by the host when it can not find a
	// price table that corresponds with the id we sent it.
	errPriceTableNotFound = errors.New("price table not found")

	// errSectorNotFound is returned by the host when it can not find the
	// requested sector.
	errSectorNotFound = errors.New("sector not found")
)

func payByContract(rev *types.FileContractRevision, amount types.Currency, refundAcct rhpv3.Account, sk types.PrivateKey) (rhpv3.PayByContractRequest, error) {
	if rev.RevisionNumber == math.MaxUint64 {
		return rhpv3.PayByContractRequest{}, errMaxRevisionReached
	}
	payment, ok := rhpv3.PayByContract(rev, amount, refundAcct, sk)
	if !ok {
		return rhpv3.PayByContractRequest{}, errInsufficientFunds
	}
	return payment, nil
}

// managedFundAccount tries to add funds to an ephemeral account.
func (c *Contractor) managedFundAccount(rpk, hpk types.PublicKey, balance types.Currency) (pt rhpv3.HostPriceTable, err error) {
	// Get the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return rhpv3.HostPriceTable{}, ErrRenterNotFound
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
		return rhpv3.HostPriceTable{}, errors.New("couldn't find the contract")
	}

	// Fetch the host.
	host, exists, err := c.hdb.Host(hpk)
	if err != nil {
		return rhpv3.HostPriceTable{}, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return rhpv3.HostPriceTable{}, errHostNotFound
	}
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return rhpv3.HostPriceTable{}, modules.AddContext(err, "failed to get host name")
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), fundAccountTimeout)
	defer cancelFunc()
	go func() {
		select {
		case <-c.tg.StopChan():
			cancelFunc()
		case <-ctx.Done():
		}
	}()

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

	// Derive ephemeral key.
	esk := modules.DeriveEphemeralKey(renter.PrivateKey, hpk)

	// Derive the account ID.
	accountID := rhpv3.Account(modules.DeriveAccountKey(renter.AccountKey, hpk).PublicKey())

	// Initiate the protocol.
	err = proto.WithTransportV3(ctx, siamuxAddr, hpk, func(t *rhpv3.Transport) (err error) {
		// Fetch the latest revision.
		rev, err := proto.RPCLatestRevision(ctx, t, fcid)
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get latest revision")
		}

		// Fetch the price table.
		pt, err = proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
			payment, err := payByContract(&rev, pt.UpdatePriceTableCost, accountID, esk)
			if err != nil {
				return nil, err
			}
			return &payment, nil
		})
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get price table")
		}

		// Retrieve the account balance.
		payment, err := payByContract(&rev, pt.AccountBalanceCost, accountID, esk)
		if err != nil {
			return err
		}
		curr, err := proto.RPCAccountBalance(ctx, t, &payment, accountID, pt.UID)
		if err != nil {
			return err // Host interactions have already been recorded.
		}

		if curr.Cmp(balance) >= 0 {
			return nil
		}
		amount := balance.Sub(curr)

		// Cap the amount by the amount of money left in the contract.
		renterFunds := rev.ValidRenterPayout()
		possibleFundCost := pt.FundAccountCost.Add(pt.UpdatePriceTableCost)
		if renterFunds.Cmp(possibleFundCost) <= 0 {
			return fmt.Errorf("insufficient funds to fund account: %v <= %v", renterFunds, possibleFundCost)
		} else if maxAmount := renterFunds.Sub(possibleFundCost); maxAmount.Cmp(amount) < 0 {
			amount = maxAmount
		}

		amount = amount.Add(pt.FundAccountCost)
		payment, err = payByContract(&rev, amount, rhpv3.Account{}, esk) // No account needed for funding.
		if err != nil {
			return err
		}
		if err := proto.RPCFundAccount(ctx, t, &payment, accountID, pt.UID); err != nil {
			hostFault = true
			return fmt.Errorf("failed to fund account with %v; %w", amount, err)
		}

		// Update the contract.
		err = c.UpdateContract(rev, nil, types.ZeroCurrency, types.ZeroCurrency, amount)

		return err
	})

	return
}
