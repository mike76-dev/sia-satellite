package wallet

import (
	"errors"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// estimatedTransactionSize is the estimated size of a transaction used to send
// siacoins.
const estimatedTransactionSize = 750

// sortedOutputs is a struct containing a slice of siacoin outputs and their
// corresponding ids. sortedOutputs can be sorted using the sort package.
type sortedOutputs struct {
	ids     []types.SiacoinOutputID
	outputs []types.SiacoinOutput
}

// DustThreshold returns the quantity per byte below which a Currency is
// considered to be Dust.
func (w *Wallet) DustThreshold() (types.Currency, error) {
	if err := w.tg.Add(); err != nil {
		return types.Currency{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	minFee, _ := w.tpool.FeeEstimation()
	return minFee.Mul64(3), nil
}

// ConfirmedBalance returns the balance of the wallet according to all of the
// confirmed transactions.
func (w *Wallet) ConfirmedBalance() (siacoinBalance types.Currency, siafundBalance uint64, siafundClaimBalance types.Currency, err error) {
	if err := w.tg.Add(); err != nil {
		return types.ZeroCurrency, 0, types.ZeroCurrency, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	// dustThreshold has to be obtained separate from the lock.
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return types.ZeroCurrency, 0, types.ZeroCurrency, modules.ErrWalletShutdown
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure durability of reported balance.
	if err = w.syncDB(); err != nil {
		return
	}

	dbForEachSiacoinOutput(w.dbTx, func(_ types.SiacoinOutputID, sco types.SiacoinOutput) {
		if sco.Value.Cmp(dustThreshold) > 0 {
			siacoinBalance = siacoinBalance.Add(sco.Value)
		}
	})

	siafundPool, err := dbGetSiafundPool(w.dbTx)
	if err != nil {
		return
	}
	dbForEachSiafundOutput(w.dbTx, func(_ types.SiafundOutputID, sfo types.SiafundOutput, claimStart types.Currency) {
		siafundBalance = siafundBalance + sfo.Value
		if claimStart.Cmp(siafundPool) > 0 {
			// Skip claims larger than the Siafund pool. This should only
			// occur if the Siafund pool has not been initialized yet.
			return
		}
		siafundClaimBalance = siafundClaimBalance.Add(siafundPool.Sub(claimStart).Mul64(sfo.Value).Div64(modules.SiafundCount))
	})
	return
}

// UnconfirmedBalance returns the number of outgoing and incoming Siacoins in
// the unconfirmed transaction set. Refund outputs are included in this
// reporting.
func (w *Wallet) UnconfirmedBalance() (outgoingSiacoins types.Currency, incomingSiacoins types.Currency, err error) {
	if err := w.tg.Add(); err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	// dustThreshold has to be obtained separate from the lock.
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	curr := w.unconfirmedProcessedTransactions.head
	for curr != nil {
		pt := curr.txn
		for _, input := range pt.Inputs {
			if input.FundType == specifierSiacoinInput && input.WalletAddress {
				outgoingSiacoins = outgoingSiacoins.Add(input.Value)
			}
		}
		for _, output := range pt.Outputs {
			if output.FundType == types.SpecifierSiacoinOutput && output.WalletAddress && output.Value.Cmp(dustThreshold) > 0 {
				incomingSiacoins = incomingSiacoins.Add(output.Value)
			}
		}
		curr = curr.next
	}
	return
}

// SendSiacoins creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned. Fees
// are added to the amount sent.
func (w *Wallet) SendSiacoins(amount types.Currency, dest types.Address) ([]types.Transaction, error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()

	_, fee := w.tpool.FeeEstimation()
	fee = fee.Mul64(estimatedTransactionSize)
	return w.managedSendSiacoins(amount, fee, dest)
}

// SendSiacoinsFeeIncluded creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned. Fees
// are subtracted from the amount sent.
func (w *Wallet) SendSiacoinsFeeIncluded(amount types.Currency, dest types.Address) ([]types.Transaction, error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()

	_, fee := w.tpool.FeeEstimation()
	fee = fee.Mul64(estimatedTransactionSize)
	// Don't allow sending an amount equal to the fee, as zero spending is not
	// allowed and would error out later.
	if amount.Cmp(fee) <= 0 {
		w.log.Println("ERROR: attempt to send coins has failed - not enough to cover fee")
		return nil, modules.AddContext(modules.ErrLowBalance, "not enough coins to cover fee")
	}
	return w.managedSendSiacoins(amount.Sub(fee), fee, dest)
}

// managedSendSiacoins creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned.
func (w *Wallet) managedSendSiacoins(amount, fee types.Currency, dest types.Address) (txns []types.Transaction, err error) {
	// Check if consensus is synced.
	if !w.cs.Synced() {
		return nil, errors.New("cannot send Siacoin until fully synced")
	}

	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		w.log.Println("ERROR: attempt to send coins has failed - wallet is locked")
		return nil, modules.ErrLockedWallet
	}

	output := types.SiacoinOutput{
		Value:   amount,
		Address: dest,
	}

	txn := types.Transaction{
		SiacoinOutputs: []types.SiacoinOutput{output},
		MinerFees:      []types.Currency{fee},
	}

	parentTxn, err := w.FundTransaction(&txn, amount.Add(fee))
	if err != nil {
		w.log.Println("ERROR: attempt to send coins has failed - failed to fund transaction:", err)
		w.ReleaseInputs(txn)
		return nil, modules.AddContext(err, "unable to fund transaction")
	}

	cf := modules.FullCoveredFields()
	err = w.SignTransaction(&txn, nil, cf)
	if err != nil {
		w.log.Println("ERROR: attempt to send coins has failed - failed to sign transaction:", err)
		w.ReleaseInputs(txn)
		return nil, modules.AddContext(err, "unable to sign transaction")
	}

	txnSet := append([]types.Transaction{parentTxn}, txn)
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		w.log.Println("ERROR: attempt to send coins has failed - transaction pool rejected transaction:", err)
		w.ReleaseInputs(txn)
		return nil, modules.AddContext(err, "unable to get transaction accepted")
	}

	w.log.Printf("INFO: submitted a Siacoin transfer transaction set for value %v with fees %v, IDs:\n", amount, fee)
	for _, txn := range txnSet {
		w.log.Println("\t", txn.ID())
	}

	return txnSet, nil
}

// SendSiacoinsMulti creates a transaction that includes the specified
// outputs. The transaction is submitted to the transaction pool and is also
// returned.
func (w *Wallet) SendSiacoinsMulti(outputs []types.SiacoinOutput) (txns []types.Transaction, err error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()
	w.log.Println("INFO: beginning call to SendSiacoinsMulti")

	// Check if consensus is synced.
	if !w.cs.Synced() {
		return nil, errors.New("cannot send Siacoin until fully synced")
	}

	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		w.log.Println("ERROR: attempt to send coins has failed - wallet is locked")
		return nil, modules.ErrLockedWallet
	}

	// Calculate estimated transaction fee.
	_, tpoolFee := w.tpool.FeeEstimation()
	// We don't want send-to-many transactions to fail.
	tpoolFee = tpoolFee.Mul64(2)
	// Estimated transaction size in bytes.
	tpoolFee = tpoolFee.Mul64(1000 + 60 * uint64(len(outputs)))

	txn := types.Transaction{
		SiacoinOutputs: outputs,
		MinerFees:      []types.Currency{tpoolFee},
	}

	// Calculate total cost to wallet.
	//
	// NOTE: we only want to call FundTransaction once; that way, it will
	// (ideally) fund the entire transaction with a single input, instead of
	// many smaller ones.
	totalCost := tpoolFee
	for _, sco := range outputs {
		totalCost = totalCost.Add(sco.Value)
	}
	parentTxn, err := w.FundTransaction(&txn, totalCost)
	if err != nil {
		w.ReleaseInputs(txn)
		return nil, modules.AddContext(err, "unable to fund transaction")
	}

	cf := modules.FullCoveredFields()
	err = w.SignTransaction(&txn, nil, cf)
	if err != nil {
		w.log.Println("ERROR: attempt to send coins has failed - failed to sign transaction:", err)
		w.ReleaseInputs(txn)
		return nil, modules.AddContext(err, "unable to sign transaction")
	}

	txnSet := append([]types.Transaction{parentTxn}, txn)
	w.log.Println("INFO: attempting to broadcast a multi-send over the network")
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		w.log.Println("ERROR: attempt to send coins has failed - transaction pool rejected transaction:", err)
		w.ReleaseInputs(txn)
		return nil, modules.AddContext(err, "unable to get transaction accepted")
	}

	// Log the success.
	var outputList string
	for _, output := range outputs {
		outputList = outputList + "\n\tAddress: " + output.Address.String() + "\n\tValue: " + output.Value.String() + "\n"
	}
	w.log.Printf("INFO: successfully broadcast transaction with id %v, fee %v, and the following outputs: %v", txnSet[len(txnSet) - 1].ID(), tpoolFee, outputList)

	return txnSet, nil
}

// Len returns the number of elements in the sortedOutputs struct.
func (so sortedOutputs) Len() int {
	return len(so.ids)
}

// Less returns whether element 'i' is less than element 'j'. The currency
// value of each output is used for comparison.
func (so sortedOutputs) Less(i, j int) bool {
	return so.outputs[i].Value.Cmp(so.outputs[j].Value) < 0
}

// Swap swaps two elements in the sortedOutputs set.
func (so sortedOutputs) Swap(i, j int) {
	so.ids[i], so.ids[j] = so.ids[j], so.ids[i]
	so.outputs[i], so.outputs[j] = so.outputs[j], so.outputs[i]
}
