package account

import (
	"errors"
	"fmt"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.uber.org/zap"
)

// checkForPayments checks the wallet for any new transactions
// and puts them on the watch list.
func (am *AccountManager) checkForPayments() {
	am.mu.Lock()
	defer am.mu.Unlock()

	txns := am.chain.V2PoolTransactions()
	for _, txn := range txns {
		txid := txn.ID()
		if _, exists := am.transactions[txid]; exists {
			continue
		}
		processed := make(map[string]bool)
		am.transactions[txid] = make(map[types.Address]string)
		for _, sco := range txn.SiacoinOutputs {
			if email, exists := am.addresses[sco.Address]; exists {
				if !processed[email] {
					acc := am.accounts[email]
					if err := am.newSiacoinPayment(acc, txn); err != nil {
						am.log.Error("couldn't add SC payment", zap.Error(err))
						continue
					}
					processed[email] = true
				}
				am.transactions[txid][sco.Address] = email
			}
		}
	}
}

// checkTransactions runs periodical checks for payments.
func (am *AccountManager) checkTransactions() {
	am.checkForPayments()
	for {
		select {
		case <-am.closeChan:
			return
		case <-time.After(time.Minute):
			am.checkForPayments()
		}
	}
}

// updateChainState applies or reverts the updates from the ChainManager.
func (am *AccountManager) updateChainState(reverted []chain.RevertUpdate, applied []chain.ApplyUpdate) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	for _, cru := range reverted {
		for _, txn := range cru.Block.V2Transactions() {
			txid := txn.ID()
			if accs, exists := am.transactions[txid]; exists {
				delete(am.transactions, txid)
				for _, email := range accs {
					acc := am.accounts[email]
					if err := am.revertSiacoinPayment(acc, txid); err != nil {
						am.log.Error("couldn't revert SC payment", zap.Error(err))
					}
				}
			}
		}
	}

	for _, cau := range applied {
		for _, txn := range cau.Block.V2Transactions() {
			txid := txn.ID()
			_, watched := am.transactions[txid]
			processed := make(map[string]bool)
			for _, sco := range txn.SiacoinOutputs {
				if email, exists := am.addresses[sco.Address]; exists {
					acc := am.accounts[email]
					if !watched {
						am.transactions[txid] = make(map[types.Address]string)
						if !processed[email] {
							if err := am.newSiacoinPayment(acc, txn); err != nil {
								am.log.Error("couldn't add SC payment", zap.Error(err))
								continue
							}
							processed[email] = true
						}
						am.transactions[txid][sco.Address] = email
					}
				}
			}
		}
	}

	for range reverted {
		for txid, accs := range am.transactions {
			for _, email := range accs {
				acc := am.accounts[email]
				if err := am.unconfirmSiacoinPayment(acc, txid); err != nil && !errors.Is(err, ErrUserNotFound) {
					am.log.Error("couldn't unconfirm SC payment", zap.Error(err))
				}
			}
		}
	}

	for range applied {
		for txid, accs := range am.transactions {
			for _, email := range accs {
				acc := am.accounts[email]
				if err := am.confirmSiacoinPayment(acc, txid); err != nil && !errors.Is(err, ErrUserNotFound) {
					am.log.Error("couldn't confirm SC payment", zap.Error(err))
				}
			}
		}
	}

	return nil
}

// sync updates the state of the account manager by reverting and then applying blocks.
func (am *AccountManager) sync(index types.ChainIndex) error {
	for index != am.chain.Tip() {
		select {
		case <-am.closeChan:
			return nil
		default:
		}

		reverted, applied, err := am.chain.UpdatesSince(index, 1)
		if err != nil {
			return fmt.Errorf("failed to get updates since %v: %w", index, err)
		} else if len(reverted) == 0 && len(applied) == 0 {
			return nil
		}

		if err := am.updateChainState(reverted, applied); err != nil {
			return utils.AddContext(err, "failed to update state")
		}

		if len(applied) > 0 {
			index = applied[len(applied)-1].State.Index
		} else {
			index = reverted[len(reverted)-1].State.Index
		}

		if err := am.saveTip(index); err != nil {
			return utils.AddContext(err, "failed to update index")
		}
	}

	return nil
}
