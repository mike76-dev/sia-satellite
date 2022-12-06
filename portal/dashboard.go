package portal

import (
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
)

type (
	// userBalance holds the current balance as well as
	// the data on the chosen payment scheme.
	userBalance struct {
		Email      string  `json: "email"`
		Subscribed bool    `json: "subscribed"`
		Balance    float64 `json: "balance"`
		Currency   string  `json: "currency"`
	}
)

// balanceHandlerPOST handles the POST /dashboard/balance requests.
func (api *portalAPI) balanceHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var dr struct {Token string `json: "token"`}
	err, code := api.handleDecodeError(w, dec.Decode(&dr))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Decode and verify the token.
	prefix, email, expires, tErr := api.portal.decodeToken(dr.Token)
	if tErr != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code: httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
		Error{
			Code: httpErrorTokenExpired,
			Message: "token already expired",
		}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	count, cErr := api.portal.countEmails(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if count == 0 {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Retrieve the balance information from the database.
	var ub *userBalance
	if ub, cErr = api.portal.getBalance(email); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	writeJSON(w, ub)
}
