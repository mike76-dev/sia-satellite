package contractor

import (
	"go.sia.tech/core/types"
)

// EncodeTo implements types.EncoderTo.
func (fcs *fileContractStatus) EncodeTo(e *types.Encoder) {
	e.WriteUint64(fcs.formationSweepHeight)
	e.WriteBool(fcs.contractFound)
	e.WriteUint64(fcs.revisionFound)
	e.WriteUint64(fcs.storageProofFound)
	e.WritePrefix(len(fcs.formationTxnSet))
	for _, txn := range fcs.formationTxnSet {
		txn.EncodeTo(e)
	}
	var outputs []types.SiacoinOutputID
	for scoid := range fcs.parentOutputs {
		outputs = append(outputs, scoid)
	}
	e.WritePrefix(len(outputs))
	for _, scoid := range outputs {
		scoid.EncodeTo(e)
	}
	fcs.sweepTxn.EncodeTo(e)
	e.WritePrefix(len(fcs.sweepParents))
	for _, parent := range fcs.sweepParents {
		parent.EncodeTo(e)
	}
	e.WriteUint64(fcs.windowStart)
	e.WriteUint64(fcs.windowEnd)
}

// DecodeFrom implements types.DecoderFrom.
func (fcs *fileContractStatus) DecodeFrom(d *types.Decoder) {
	fcs.formationSweepHeight = d.ReadUint64()
	fcs.contractFound = d.ReadBool()
	fcs.revisionFound = d.ReadUint64()
	fcs.storageProofFound = d.ReadUint64()
	fcs.formationTxnSet = make([]types.Transaction, d.ReadPrefix())
	for i := range fcs.formationTxnSet {
		fcs.formationTxnSet[i].DecodeFrom(d)
	}
	fcs.parentOutputs = make(map[types.SiacoinOutputID]struct{})
	n := d.ReadPrefix()
	for i := 0; i < n; i++ {
		var scoid types.SiacoinOutputID
		scoid.DecodeFrom(d)
		fcs.parentOutputs[scoid] = struct{}{}
	}
	fcs.sweepTxn.DecodeFrom(d)
	fcs.sweepParents = make([]types.Transaction, d.ReadPrefix())
	for i := range fcs.sweepParents {
		fcs.sweepParents[i].DecodeFrom(d)
	}
	fcs.windowStart = d.ReadUint64()
	fcs.windowEnd = d.ReadUint64()
}
