package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

// ConsensusGET contains general information about the consensus set, with tags
// to support idiomatic json encodings.
type ConsensusGET struct {
	Synced       bool           `json:"synced"`
	Height       uint64         `json:"height"`
	CurrentBlock types.BlockID  `json:"currentblock"`
	Target       modules.Target `json:"target"`
	Difficulty   types.Currency `json:"difficulty"`
}

// ConsensusHeadersGET contains information from a blocks header.
type ConsensusHeadersGET struct {
	BlockID types.BlockID `json:"blockid"`
}

// ConsensusBlocksGet contains all fields of a types.Block and additional
// fields for ID and Height.
type ConsensusBlocksGet struct {
	ID           types.BlockID           `json:"id"`
	Height       uint64                  `json:"height"`
	ParentID     types.BlockID           `json:"parentid"`
	Nonce        uint64                  `json:"nonce"`
	Difficulty   types.Currency          `json:"difficulty"`
	Timestamp    time.Time               `json:"timestamp"`
	MinerPayouts []types.SiacoinOutput   `json:"minerpayouts"`
	Transactions []ConsensusBlocksGetTxn `json:"transactions"`
}

// ConsensusBlocksGetTxn contains all fields of a types.Transaction and an
// additional ID field.
type ConsensusBlocksGetTxn struct {
	ID                    types.TransactionID               `json:"id"`
	SiacoinInputs         []types.SiacoinInput              `json:"siacoininputs"`
	SiacoinOutputs        []ConsensusBlocksGetSiacoinOutput `json:"siacoinoutputs"`
	FileContracts         []ConsensusBlocksGetFileContract  `json:"filecontracts"`
	FileContractRevisions []types.FileContractRevision      `json:"filecontractrevisions"`
	StorageProofs         []types.StorageProof              `json:"storageproofs"`
	SiafundInputs         []types.SiafundInput              `json:"siafundinputs"`
	SiafundOutputs        []ConsensusBlocksGetSiafundOutput `json:"siafundoutputs"`
	MinerFees             []types.Currency                  `json:"minerfees"`
	ArbitraryData         [][]byte                          `json:"arbitrarydata"`
	TransactionSignatures []types.TransactionSignature      `json:"transactionsignatures"`
}

// ConsensusBlocksGetFileContract contains all fields of a types.FileContract
// and an additional ID field.
type ConsensusBlocksGetFileContract struct {
	ID                 types.FileContractID              `json:"id"`
	Filesize           uint64                            `json:"filesize"`
	FileMerkleRoot     types.Hash256                     `json:"filemerkleroot"`
	WindowStart        uint64                            `json:"windowstart"`
	WindowEnd          uint64                            `json:"windowend"`
	Payout             types.Currency                    `json:"payout"`
	ValidProofOutputs  []ConsensusBlocksGetSiacoinOutput `json:"validproofoutputs"`
	MissedProofOutputs []ConsensusBlocksGetSiacoinOutput `json:"missedproofoutputs"`
	UnlockHash         types.Hash256                     `json:"unlockhash"`
	RevisionNumber     uint64                            `json:"revisionnumber"`
}

// ConsensusBlocksGetSiacoinOutput contains all fields of a types.SiacoinOutput
// and an additional ID field.
type ConsensusBlocksGetSiacoinOutput struct {
	ID         types.SiacoinOutputID `json:"id"`
	Value      types.Currency        `json:"value"`
	Address    types.Address         `json:"unlockhash"`
}

// ConsensusBlocksGetSiafundOutput contains all fields of a types.SiafundOutput
// and an additional ID field.
type ConsensusBlocksGetSiafundOutput struct {
	ID         types.SiafundOutputID `json:"id"`
	Value      uint64                `json:"value"`
	Address    types.Address         `json:"unlockhash"`
}

// RegisterRoutesConsensus is a helper function to register all consensus routes.
func RegisterRoutesConsensus(router *httprouter.Router, cs modules.ConsensusSet) {
	router.GET("/consensus", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		consensusHandler(cs, w, req, ps)
	})
	router.GET("/consensus/blocks", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		consensusBlocksHandler(cs, w, req, ps)
	})
	router.POST("/consensus/validate/transactionset", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		consensusValidateTransactionsetHandler(cs, w, req, ps)
	})
}

// consensusBlocksGetFromBlock is a helper method that uses a types.Block, types.BlockHeight and
// types.Currency to create a ConsensusBlocksGet object.
func consensusBlocksGetFromBlock(b types.Block, h uint64, d types.Currency) ConsensusBlocksGet {
	txns := make([]ConsensusBlocksGetTxn, 0, len(b.Transactions))
	for _, t := range b.Transactions {
		// Get the transaction's SiacoinOutputs.
		scos := make([]ConsensusBlocksGetSiacoinOutput, 0, len(t.SiacoinOutputs))
		for i, sco := range t.SiacoinOutputs {
			scos = append(scos, ConsensusBlocksGetSiacoinOutput{
				ID:      t.SiacoinOutputID(i),
				Value:   sco.Value,
				Address: sco.Address,
			})
		}
		// Get the transaction's SiafundOutputs.
		sfos := make([]ConsensusBlocksGetSiafundOutput, 0, len(t.SiafundOutputs))
		for i, sfo := range t.SiafundOutputs {
			sfos = append(sfos, ConsensusBlocksGetSiafundOutput{
				ID:      t.SiafundOutputID(i),
				Value:   sfo.Value,
				Address: sfo.Address,
			})
		}
		// Get the transaction's FileContracts.
		fcos := make([]ConsensusBlocksGetFileContract, 0, len(t.FileContracts))
		for i, fc := range t.FileContracts {
			// Get the FileContract's valid proof outputs.
			fcid := t.FileContractID(i)
			vpos := make([]ConsensusBlocksGetSiacoinOutput, 0, len(fc.ValidProofOutputs))
			for j, vpo := range fc.ValidProofOutputs {
				vpos = append(vpos, ConsensusBlocksGetSiacoinOutput{
					ID:      fcid.ValidOutputID(j),
					Value:   vpo.Value,
					Address: vpo.Address,
				})
			}
			// Get the FileContract's missed proof outputs.
			mpos := make([]ConsensusBlocksGetSiacoinOutput, 0, len(fc.MissedProofOutputs))
			for j, mpo := range fc.MissedProofOutputs {
				mpos = append(mpos, ConsensusBlocksGetSiacoinOutput{
					ID:      fcid.MissedOutputID(j),
					Value:   mpo.Value,
					Address: mpo.Address,
				})
			}
			fcos = append(fcos, ConsensusBlocksGetFileContract{
				ID:                 fcid,
				Filesize:           fc.Filesize,
				FileMerkleRoot:     fc.FileMerkleRoot,
				WindowStart:        fc.WindowStart,
				WindowEnd:          fc.WindowEnd,
				Payout:             fc.Payout,
				ValidProofOutputs:  vpos,
				MissedProofOutputs: mpos,
				UnlockHash:         fc.UnlockHash,
				RevisionNumber:     fc.RevisionNumber,
			})
		}
		txns = append(txns, ConsensusBlocksGetTxn{
			ID:                    t.ID(),
			SiacoinInputs:         t.SiacoinInputs,
			SiacoinOutputs:        scos,
			FileContracts:         fcos,
			FileContractRevisions: t.FileContractRevisions,
			StorageProofs:         t.StorageProofs,
			SiafundInputs:         t.SiafundInputs,
			SiafundOutputs:        sfos,
			MinerFees:             t.MinerFees,
			ArbitraryData:         t.ArbitraryData,
			TransactionSignatures: t.Signatures,
		})
	}
	return ConsensusBlocksGet{
		ID:           b.ID(),
		Height:       h,
		ParentID:     b.ParentID,
		Nonce:        b.Nonce,
		Difficulty:   d,
		Timestamp:    b.Timestamp,
		MinerPayouts: b.MinerPayouts,
		Transactions: txns,
	}
}

// consensusHandler handles the API calls to /consensus.
func consensusHandler(cs modules.ConsensusSet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	height := cs.Height()
	b, found := cs.BlockAtHeight(height)
	if !found {
		err := "Failed to fetch block for current height"
		WriteError(w, Error{err}, http.StatusInternalServerError)
		return
	}
	cbid := b.ID()
	currentTarget, _ := cs.ChildTarget(cbid)
	WriteJSON(w, ConsensusGET{
		Synced:       cs.Synced(),
		Height:       height,
		CurrentBlock: cbid,
		Target:       currentTarget,
		Difficulty:   currentTarget.Difficulty(),
	})
}

// consensusBlocksHandler handles the API calls to /consensus/blocks
// endpoint.
func consensusBlocksHandler(cs modules.ConsensusSet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get query params and check them.
	id, height := req.FormValue("id"), req.FormValue("height")
	if id != "" && height != "" {
		WriteError(w, Error{"can't specify both id and height"}, http.StatusBadRequest)
		return
	}
	if id == "" && height == "" {
		WriteError(w, Error{"either id or height has to be provided"}, http.StatusBadRequest)
		return
	}

	var b types.Block
	var h uint64
	var exists bool

	// Handle request by id.
	if id != "" {
		var bid types.BlockID
		if err := bid.UnmarshalText([]byte(id)); err != nil {
			WriteError(w, Error{"failed to unmarshal blockid"}, http.StatusBadRequest)
			return
		}
		b, h, exists = cs.BlockByID(bid)
	}
	// Handle request by height.
	if height != "" {
		if _, err := fmt.Sscan(height, &h); err != nil {
			WriteError(w, Error{"failed to parse block height"}, http.StatusBadRequest)
			return
		}
		b, exists = cs.BlockAtHeight(h)
	}
	// Check if block was found.
	if !exists {
		WriteError(w, Error{"block doesn't exist"}, http.StatusBadRequest)
		return
	}

	target, _ := cs.ChildTarget(b.ID())
	d := target.Difficulty()

	// Write response.
	WriteJSON(w, consensusBlocksGetFromBlock(b, h, d))
}

// consensusValidateTransactionsetHandler handles the API calls to
// /consensus/validate/transactionset.
func consensusValidateTransactionsetHandler(cs modules.ConsensusSet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var txnset []types.Transaction
	err := json.NewDecoder(req.Body).Decode(&txnset)
	if err != nil {
		WriteError(w, Error{"could not decode transaction set: " + err.Error()}, http.StatusBadRequest)
		return
	}
	_, err = cs.TryTransactionSet(txnset)
	if err != nil {
		WriteError(w, Error{"transaction set validation failed: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}
