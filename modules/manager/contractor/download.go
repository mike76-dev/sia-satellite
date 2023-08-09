package contractor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

const (
	// defaultWithdrawalExpiryBlocks is the number of blocks we add to the
	// current blockheight when we define an expiry block height for withdrawal
	// messages.
	defaultWithdrawalExpiryBlocks = 6
)

// DownloadSector downloads a single sector from the host.
func (c *Contractor) DownloadSector(rpk, hpk types.PublicKey, w io.Writer, root types.Hash256, offset, length uint32) (err error) {
	// Get the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return ErrRenterNotFound
	}

	// Fetch the host.
	host, exists, err := c.hdb.Host(hpk)
	if err != nil {
		return modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return errHostNotFound
	}
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return modules.AddContext(err, "failed to get host name")
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Create a context and set up its cancelling.
	ctx, cancelFunc := context.WithTimeout(context.Background(), downloadSectorTimeout)
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

	// Derive the account key.
	ak := modules.DeriveAccountKey(renter.AccountKey, hpk)

	// Derive the account ID.
	accountID := rhpv3.Account(ak.PublicKey())

	// Initiate the protocol.
	err = proto.WithTransportV3(ctx, siamuxAddr, hpk, func(t *rhpv3.Transport) (err error) {
		// Get the cost estimation.
		cost, err := readSectorCost(host.PriceTable, uint64(length))
		if err != nil {
			return modules.AddContext(err, "unable to estimate costs")
		}

		// Fund the account if the balance is insufficient.
		// This way we also get a valid price table.
		pt, err := c.managedFundAccount(rpk, hpk, cost)
		if err != nil {
			return modules.AddContext(err, "unable to fund account")
		}

		payment := rhpv3.PayByEphemeralAccount(accountID, cost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
		_, _, err = proto.RPCReadSector(ctx, t, w, pt, &payment, offset, length, root)
		if err != nil {
			hostFault = true
			return err
		}

		return nil
	})

	return err
}

// readSectorCost returns an overestimate for the cost of reading a sector from a host.
func readSectorCost(pt rhpv3.HostPriceTable, length uint64) (types.Currency, error) {
	rc := pt.BaseCost()
	rc = rc.Add(pt.ReadSectorCost(length))
	cost, _ := rc.Total()

	// Overestimate the cost by 5%.
	cost, overflow := cost.Mul64WithOverflow(21)
	if overflow {
		return types.ZeroCurrency, errors.New("overflow occurred while adding leeway to read sector cost")
	}
	return cost.Div64(20), nil
}
