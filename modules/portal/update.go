package portal

import (
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.uber.org/zap"
)

// walletCheckInterval determines how often the wallet is checked
// for new transactions.
const walletCheckInterval = time.Minute

// onHoldCheckInterval determines how often the accounts, which are
// on hold, are checked.
const onHoldCheckInterval = 30 * time.Minute

// announcementCheckInterval determines how often the check is done
// if the portal announcement has expired.
const announcementCheckInterval = 10 * time.Minute

// threadedWatchForNewTxns performs the wallet checks with set
// intervals.
func (p *Portal) threadedWatchForNewTxns() {
	if err := p.tg.Add(); err != nil {
		return
	}
	defer p.tg.Done()

	for {
		select {
		case <-p.tg.StopChan():
			return
		case <-time.After(walletCheckInterval):
		}
		p.managedCheckWallet()
	}
}

// managedCheckWallet checks the wallet for any new transactions
// and puts them on the watch list.
func (p *Portal) managedCheckWallet() {
	addrs, err := p.getSiacoinAddresses()
	if err != nil {
		p.log.Error("couldn't get account addresses", zap.Error(err))
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	txns := p.cm.PoolTransactions()
	for _, txn := range txns {
		txid := txn.ID()
		if _, exists := p.transactions[txid]; exists {
			continue
		}
		for _, sco := range txn.SiacoinOutputs {
			if email, exists := addrs[sco.Address]; exists {
				if err := p.addSiacoinPayment(email, sco.Value, txid); err != nil {
					p.log.Error("couldn't add SC payment", zap.Error(err))
					continue
				}
				p.transactions[txid] = sco.Address
			}
		}
	}
}

// UpdateChainState applies or reverts the updates from the ChainManager.
func (p *Portal) UpdateChainState(reverted []chain.RevertUpdate, applied []chain.ApplyUpdate, addrs map[types.Address]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, cru := range reverted {
		for _, txn := range cru.Block.Transactions {
			if _, exists := p.transactions[txn.ID()]; exists {
				delete(p.transactions, txn.ID())
				if err := p.revertSiacoinPayment(txn.ID()); err != nil {
					p.log.Error("couldn't revert SC payment", zap.Error(err))
				}
			}
		}
	}

	for _, cau := range applied {
		for _, txn := range cau.Block.Transactions {
			txid := txn.ID()
			for _, sco := range txn.SiacoinOutputs {
				if email, exists := addrs[sco.Address]; exists {
					if _, exists := p.transactions[txid]; !exists {
						if err := p.addSiacoinPayment(email, sco.Value, txid); err != nil {
							p.log.Error("couldn't add SC payment", zap.Error(err))
							continue
						}
						p.transactions[txid] = sco.Address
					}
				}
			}
		}
	}

	for txid := range p.transactions {
		if err := p.confirmSiacoinPayment(txid); err != nil {
			p.log.Error("couldn't confirm SC payment", zap.Error(err))
		}
	}

	return nil
}

// threadedCheckOnHoldAccounts performs the account checks with set
// intervals.
func (p *Portal) threadedCheckOnHoldAccounts() {
	if err := p.tg.Add(); err != nil {
		return
	}
	defer p.tg.Done()

	for {
		select {
		case <-p.tg.StopChan():
			return
		case <-time.After(onHoldCheckInterval):
		}
		p.managedCheckOnHoldAccounts()
	}
}

// threadedCheckAnnouncement performs the announcement checks with set
// intervals.
func (p *Portal) threadedCheckAnnouncement() {
	if err := p.tg.Add(); err != nil {
		return
	}
	defer p.tg.Done()

	for {
		select {
		case <-p.tg.StopChan():
			return
		case <-time.After(announcementCheckInterval):
		}
		p.managedCheckAnnouncement()
	}
}
