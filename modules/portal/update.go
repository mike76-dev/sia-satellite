package portal

import (
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.sia.tech/core/types"
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
		p.log.Println("ERROR: couldn't get account addresses:", err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, email := range addrs {
		pts, err := p.w.AddressUnconfirmedTransactions(addr)
		if err != nil {
			p.log.Println("ERROR: couldn't get unconfirmed transactions:", err)
			continue
		}

		for _, pt := range pts {
			if _, exists := p.transactions[pt.TransactionID]; exists {
				continue
			}
			for _, output := range pt.Outputs {
				if output.FundType == types.SpecifierSiacoinOutput && output.WalletAddress {
					err := p.addSiacoinPayment(email, output.Value, pt.TransactionID)
					if err != nil {
						p.log.Println("ERROR: couldn't add SC payment:", err)
						continue
					}
					p.transactions[pt.TransactionID] = addr
				}
			}
		}
	}
}

// ProcessConsensusChange gets called to inform Portal about the
// changes in the consensus set.
func (p *Portal) ProcessConsensusChange(cc modules.ConsensusChange) {
	for _, block := range cc.RevertedBlocks {
		for _, txn := range block.Transactions {
			_, exists := p.transactions[txn.ID()]
			if exists {
				p.mu.Lock()
				delete(p.transactions, txn.ID())
				p.mu.Unlock()
				err := p.revertSiacoinPayment(txn.ID())
				if err != nil {
					p.log.Println("ERROR: couldn't revert SC payment:", err)
				}
			}
		}
	}

	for range cc.AppliedBlocks {
		p.mu.Lock()
		txns := p.transactions
		p.mu.Unlock()
		for txid := range txns {
			err := p.confirmSiacoinPayment(txid)
			if err != nil {
				p.log.Println("ERROR: couldn't confirm SC payment:", err)
			}
		}
	}
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
