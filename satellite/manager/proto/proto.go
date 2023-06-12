package proto

import (
	"context"
	"fmt"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
	"go.sia.tech/siad/modules"
	siad "go.sia.tech/siad/types"
)

// Dependencies.
type (
	transactionBuilder interface {
		AddArbitraryData(arb []byte) uint64
		AddFileContract(siad.FileContract) uint64
		AddFileContractRevision(siad.FileContractRevision) uint64
		AddMinerFee(siad.Currency) uint64
		AddParents([]siad.Transaction)
		AddSiacoinInput(siad.SiacoinInput) uint64
		AddSiacoinOutput(siad.SiacoinOutput) uint64
		AddTransactionSignature(siad.TransactionSignature) uint64
		Copy() modules.TransactionBuilder
		FundSiacoins(siad.Currency) error
		Sign(bool) ([]siad.Transaction, error)
		UnconfirmedParents() ([]siad.Transaction, error)
		View() (siad.Transaction, []siad.Transaction)
		ViewAdded() (parents, coins, funds, signatures []int)
	}

	transactionPool interface {
		AcceptTransactionSet([]siad.Transaction) error
		FeeEstimation() (min siad.Currency, siad types.Currency)
	}

	hostDB interface {
		IncrementSuccessfulInteractions(key types.PublicKey) error
		IncrementFailedInteractions(key types.PublicKey) error
	}
)

// A revisionNumberMismatchError occurs if the host reports a different revision
// number than expected.
type revisionNumberMismatchError struct {
	ours, theirs uint64
}

func (e *revisionNumberMismatchError) Error() string {
	return fmt.Sprintf("our revision number (%v) does not match the host's (%v); the host may be acting maliciously", e.ours, e.theirs)
}

// IsRevisionMismatch returns true if err was caused by the host reporting a
// different revision number than expected.
func IsRevisionMismatch(err error) bool {
	_, ok := err.(*revisionNumberMismatchError)
	return ok
}

// HostSettings uses the Settings RPC to retrieve the host's settings.
func HostSettings(address string, hpk types.PublicKey) (settings rhpv2.HostSettings, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), settingsHostTimeout)
	defer cancel()

	err = WithTransportV2(ctx, address, hpk, func(t *rhpv2.Transport) (err error) {
		settings, err = RPCSettings(ctx, t)
		return
	})

	return
}
