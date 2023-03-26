package proto

import (
	"context"
	"fmt"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// Dependencies.
type (
	transactionBuilder interface {
		AddArbitraryData(arb []byte) uint64
		AddFileContract(types.FileContract) uint64
		AddFileContractRevision(types.FileContractRevision) uint64
		AddMinerFee(types.Currency) uint64
		AddParents([]types.Transaction)
		AddSiacoinInput(types.SiacoinInput) uint64
		AddSiacoinOutput(types.SiacoinOutput) uint64
		AddTransactionSignature(types.TransactionSignature) uint64
		Copy() modules.TransactionBuilder
		FundSiacoins(types.Currency) error
		Sign(bool) ([]types.Transaction, error)
		UnconfirmedParents() ([]types.Transaction, error)
		View() (types.Transaction, []types.Transaction)
		ViewAdded() (parents, coins, funds, signatures []int)
	}

	transactionPool interface {
		AcceptTransactionSet([]types.Transaction) error
		FeeEstimation() (min types.Currency, max types.Currency)
	}

	hostDB interface {
		IncrementSuccessfulInteractions(key types.SiaPublicKey) error
		IncrementFailedInteractions(key types.SiaPublicKey) error
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
func HostSettings(address string, hpk types.SiaPublicKey) (modules.HostExternalSettings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), settingsHostTimeout)
	defer cancel()

	var hes modules.HostExternalSettings
	err := WithTransportV2(ctx, address, hpk, func(t *rhpv2.Transport) (err error) {
		hes, err = RPCSettings(ctx, t)
		return
	})

	return hes, err
}
