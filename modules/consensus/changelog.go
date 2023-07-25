package consensus

import (
	"database/sql"

	"github.com/mike76-dev/sia-satellite/modules"
	
	"go.sia.tech/core/types"
)

type (
	// changeEntry records a single atomic change to the consensus set.
	changeEntry struct {
		RevertedBlocks []types.BlockID
		AppliedBlocks  []types.BlockID
	}

	// changeNode contains a change entry and a pointer to the next change
	// entry, and is the object that gets stored in the database.
	changeNode struct {
		Entry changeEntry
		Next  modules.ConsensusChangeID
	}
)

// EncodeTo implements types.EncoderTo.
func (ce *changeEntry) EncodeTo(e *types.Encoder) {
	e.WritePrefix(len(ce.RevertedBlocks))
	for _, rb := range ce.RevertedBlocks {
		rb.EncodeTo(e)
	}
	e.WritePrefix(len(ce.AppliedBlocks))
	for _, ab := range ce.AppliedBlocks {
		ab.EncodeTo(e)
	}
}

// DecodeFrom implements types.DecoderFrom.
func (ce *changeEntry) DecodeFrom(d *types.Decoder) {
	ce.RevertedBlocks = make([]types.BlockID, d.ReadUint64())
	for i := 0; i < len(ce.RevertedBlocks); i++ {
		ce.RevertedBlocks[i].DecodeFrom(d)
	}
	ce.AppliedBlocks = make([]types.BlockID, d.ReadUint64())
	for i := 0; i < len(ce.AppliedBlocks); i++ {
		ce.AppliedBlocks[i].DecodeFrom(d)
	}
}

// EncodeTo implements types.EncoderTo.
func (cn *changeNode) EncodeTo(e *types.Encoder) {
	cn.Entry.EncodeTo(e)
	e.Write(cn.Next[:])
}

// DecodeFrom implements types.DecoderFrom.
func (cn *changeNode) DecodeFrom(d *types.Decoder) {
	cn.Entry.DecodeFrom(d)
	d.Read(cn.Next[:])
}

// appendChangeLog adds a new change entry to the change log.
func appendChangeLog(tx *sql.Tx, ce changeEntry) error {
	// Insert the change entry.
	ceid := ce.ID()
	cn := changeNode{Entry: ce, Next: modules.ConsensusChangeID{}}
	err := saveConsensusChange(tx, ceid, cn)
	if err != nil {
		return err
	}

	// Update the tail node to point to the new change entry as the next entry.
	tailID := changeLogTailID(tx)
	if tailID != (modules.ConsensusChangeID{}) {
		// Get the old tail node.
		tailCN, err := loadConsensusChange(tx, tailID)
		if err != nil {
			return err
		}

		// Point the 'next' of the old tail node to the new tail node and
		// insert.
		tailCN.Next = ceid
		err = saveConsensusChange(tx, tailID, tailCN)
		if err != nil {
			return err
		}
	}

	// Update the tail id.
	return setChangeLogTailID(tx, ceid)
}

// getEntry returns the change entry with a given id, using a bool to indicate
// existence.
func getEntry(tx *sql.Tx, id modules.ConsensusChangeID) (ce changeEntry, exists bool) {
	cn, err := loadConsensusChange(tx, id)
	if err != nil {
		return changeEntry{}, false
	}
	return cn.Entry, true
}

// ID returns the id of a change entry.
func (ce *changeEntry) ID() modules.ConsensusChangeID {
	h := types.NewHasher()
	ce.EncodeTo(h.E)
	return modules.ConsensusChangeID(h.Sum())
}

// NextEntry returns the entry after the current entry.
func (ce *changeEntry) NextEntry(tx *sql.Tx) (ne changeEntry, exists bool) {
	ceid := ce.ID()
	cn, err := loadConsensusChange(tx, ceid)
	if err != nil {
		return changeEntry{}, false
	}
	return getEntry(tx, cn.Next)
}

// createChangeLog assumes that no change log exists and creates a new one.
func (cs *ConsensusSet) createChangeLog(tx *sql.Tx) error {
	// Add the genesis block as the first entry of the change log.
	ge := cs.genesisEntry()
	geid := ge.ID()
	cn := changeNode{
		Entry: ge,
		Next:  modules.ConsensusChangeID{},
	}

	err := saveConsensusChange(tx, geid, cn)
	if err != nil {
		return err
	}

	// Update the tail id.
	return setChangeLogTailID(tx, geid)
}

// genesisEntry returns the id of the genesis block log entry.
func (cs *ConsensusSet) genesisEntry() changeEntry {
	return changeEntry{
		AppliedBlocks: []types.BlockID{cs.blockRoot.Block.ID()},
	}
}
