package wallet

import (
	"github.com/mike76-dev/sia-satellite/modules"
)

// processedTransactionNode is a single node in the list that points
// to the next node.
type processedTransactionNode struct {
	txn  modules.ProcessedTransaction
	next *processedTransactionNode
}

// processedTransactionList is a singly unsorted linked list of
// modules.ProcessedTransaction elements. It is more performant than
// the previously used array.
type processedTransactionList struct {
	head *processedTransactionNode
	tail *processedTransactionNode
}

// add adds a new modules.ProcessedTransaction to the list.
func (ptl processedTransactionList) add(pt modules.ProcessedTransaction) {
	node := &processedTransactionNode{txn: pt}
	if ptl.head == nil {
		ptl.head = node
	} else {
		ptl.tail.next = node
	}
	ptl.tail = node
}
