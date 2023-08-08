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
	// errInsufficientFunds is returned by various RPCs when the satellite is
	// unable to provide sufficient payment to the host.
	errInsufficientFunds = errors.New("insufficient funds")

	// errMaxRevisionReached occurs when trying to revise a contract that has
	// already reached the highest possible revision number. Usually happens
	// when trying to use a renewed contract.
	errMaxRevisionReached = errors.New("contract has reached the maximum number of revisions")
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

// managedAccountBalance returns the balance of an ephemeral account.
func (c *Contractor) managedAccountBalance(rpk, hpk types.PublicKey) (balance types.Currency, err error) {
	// Get the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.ZeroCurrency, ErrRenterNotFound
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
		return types.ZeroCurrency, errors.New("couldn't find the contract")
	}

	// Fetch the host.
	host, exists, err := c.hdb.Host(hpk)
	if err != nil {
		return types.ZeroCurrency, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return types.ZeroCurrency, errHostNotFound
	}
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return types.ZeroCurrency, modules.AddContext(err, "failed to get host name")
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), syncAccountTimeout)
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
		pt, err := proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
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
		balance, err = proto.RPCAccountBalance(ctx, t, &payment, accountID, pt.UID)
		if err != nil {
			hostFault = true
		}

		return err
	})

	return
}
