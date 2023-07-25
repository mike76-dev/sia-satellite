package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

type (
	// TpoolFeeGET contains the current estimated fee.
	TpoolFeeGET struct {
		Minimum types.Currency `json:"minimum"`
		Maximum types.Currency `json:"maximum"`
	}

	// TpoolRawGET contains the requested transaction encoded to the raw
	// format, along with the id of that transaction.
	TpoolRawGET struct {
		ID          types.TransactionID `json:"id"`
		Parents     []byte              `json:"parents"`
		Transaction []byte              `json:"transaction"`
	}

	// TpoolConfirmedGET contains information about whether or not
	// the transaction has been seen on the blockhain.
	TpoolConfirmedGET struct {
		Confirmed bool `json:"confirmed"`
	}

	// TpoolTxnsGET contains the information about the tpool's transactions.
	TpoolTxnsGET struct {
		Transactions []types.Transaction `json:"transactions"`
	}
)

// RegisterRoutesTransactionPool is a helper function to register all
// transaction pool routes.
func RegisterRoutesTransactionPool(router *httprouter.Router, tpool modules.TransactionPool) {
	router.GET("/tpool/fee", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		tpoolFeeHandlerGET(tpool, w, req, ps)
	})
	router.GET("/tpool/raw/:id", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		tpoolRawHandlerGET(tpool, w, req, ps)
	})
	router.POST("/tpool/raw", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		tpoolRawHandlerPOST(tpool, w, req, ps)
	})
	router.GET("/tpool/confirmed/:id", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		tpoolConfirmedGET(tpool, w, req, ps)
	})
	router.GET("/tpool/transactions", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		tpoolTransactionsHandler(tpool, w, req, ps)
	})
}

// decodeTransactionID will decode a transaction id from a string.
func decodeTransactionID(txidStr string) (txid types.TransactionID, err error) {
	err = txid.UnmarshalText([]byte(txidStr))
	return
}

// tpoolFeeHandlerGET returns the current estimated fee. Transactions with
// fees lower than the estimated fee may take longer to confirm.
func tpoolFeeHandlerGET(tpool modules.TransactionPool, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	min, max := tpool.FeeEstimation()
	WriteJSON(w, TpoolFeeGET{
		Minimum: min,
		Maximum: max,
	})
}

// tpoolRawHandlerGET will provide the raw byte representation of a
// transaction that matches the input id.
func tpoolRawHandlerGET(tpool modules.TransactionPool, w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	txid, err := decodeTransactionID(ps.ByName("id"))
	if err != nil {
		WriteError(w, Error{"error decoding transaction id: " + err.Error()}, http.StatusBadRequest)
		return
	}
	txn, parents, exists := tpool.Transaction(txid)
	if !exists {
		WriteError(w, Error{"transaction not found in transaction pool"}, http.StatusBadRequest)
		return
	}

	var p, t bytes.Buffer
	e := types.NewEncoder(&p)
	e.WritePrefix(len(parents))
	for _, parent := range parents {
		parent.EncodeTo(e)
	}
	e.Flush()
	e = types.NewEncoder(&t)
	txn.EncodeTo(e)
	e.Flush()
	WriteJSON(w, TpoolRawGET{
		ID:          txid,
		Parents:     p.Bytes(),
		Transaction: t.Bytes(),
	})
}

// tpoolRawHandlerPOST takes a raw encoded transaction set and posts
// it to the transaction pool, relaying it to the transaction pool's peers
// regardless of if the set is accepted.
func tpoolRawHandlerPOST(tpool modules.TransactionPool, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var parents []types.Transaction
	var txn types.Transaction
	
	// JSON, base64, and raw binary are accepted.
	if err := json.Unmarshal([]byte(req.FormValue("parents")), &parents); err != nil {
		rawParents, err := base64.StdEncoding.DecodeString(req.FormValue("parents"))
		if err != nil {
			rawParents = []byte(req.FormValue("parents"))
		}
		pBuf := bytes.NewBuffer(rawParents)
		d := types.NewDecoder(io.LimitedReader{R: pBuf, N: int64(len(rawParents))})
		l := d.ReadPrefix()
		if err := d.Err(); err != nil {
			WriteError(w, Error{"error decoding parents: " + err.Error()}, http.StatusBadRequest)
			return
		}
		parents = make([]types.Transaction, l)
		for i := 0; i < l; i++ {
			parents[i].DecodeFrom(d)
			if err := d.Err(); err != nil {
				WriteError(w, Error{"error decoding parents: " + err.Error()}, http.StatusBadRequest)
				return
			}
		}
	}
	if err := json.Unmarshal([]byte(req.FormValue("transaction")), &txn); err != nil {
		rawTransaction, err := base64.StdEncoding.DecodeString(req.FormValue("transaction"))
		if err != nil {
			rawTransaction = []byte(req.FormValue("transaction"))
		}
		tBuf := bytes.NewBuffer(rawTransaction)
		d := types.NewDecoder(io.LimitedReader{R: tBuf, N: int64(len(rawTransaction))})
		txn.DecodeFrom(d)
		if err := d.Err(); err != nil {
			WriteError(w, Error{"error decoding transaction: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}

	// Broadcast the transaction set, so that they are passed to any peers that
	// may have rejected them earlier.
	txnSet := append(parents, txn)
	tpool.Broadcast(txnSet)
	err := tpool.AcceptTransactionSet(txnSet)
	if err != nil && !strings.Contains(err.Error(), modules.ErrDuplicateTransactionSet.Error()) {
		WriteError(w, Error{"error accepting transaction set: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// tpoolConfirmedGET returns whether the specified transaction has
// been seen on the blockchain.
func tpoolConfirmedGET(tpool modules.TransactionPool, w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	txid, err := decodeTransactionID(ps.ByName("id"))
	if err != nil {
		WriteError(w, Error{"error decoding transaction id: " + err.Error()}, http.StatusBadRequest)
		return
	}
	confirmed, err := tpool.TransactionConfirmed(txid)
	if err != nil {
		WriteError(w, Error{"error fetching transaction status: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, TpoolConfirmedGET{
		Confirmed: confirmed,
	})
}

// tpoolTransactionsHandler returns the current transactions of the transaction
// pool
func tpoolTransactionsHandler(tpool modules.TransactionPool, w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	txns := tpool.Transactions()
	WriteJSON(w, TpoolTxnsGET{
		Transactions: txns,
	})
}
