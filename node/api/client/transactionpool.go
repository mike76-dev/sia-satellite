package client

import (
	"bytes"
	"encoding/base64"
	"net/url"

	"github.com/mike76-dev/sia-satellite/node/api"

	"go.sia.tech/core/types"
)

// TransactionPoolFeeGet uses the /tpool/fee endpoint to get a fee estimation.
func (c *Client) TransactionPoolFeeGet() (tfg api.TpoolFeeGET, err error) {
	err = c.get("/tpool/fee", &tfg)
	return
}

// TransactionPoolRawPost uses the /tpool/raw endpoint to send a raw
// transaction to the transaction pool.
func (c *Client) TransactionPoolRawPost(txn types.Transaction, parents []types.Transaction) (err error) {
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
	values := url.Values{}
	values.Set("transaction", base64.StdEncoding.EncodeToString(t.Bytes()))
	values.Set("parents", base64.StdEncoding.EncodeToString(p.Bytes()))
	err = c.post("/tpool/raw", values.Encode(), nil)
	return
}

// TransactionPoolTransactionsGet uses the /tpool/transactions endpoint to get the
// transactions of the tpool.
func (c *Client) TransactionPoolTransactionsGet() (tptg api.TpoolTxnsGET, err error) {
	err = c.get("/tpool/transactions", &tptg)
	return
}
