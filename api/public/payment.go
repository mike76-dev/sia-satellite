package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"go.uber.org/zap"
)

// paymentsHandlerGET handles the GET /payments requests.
func (s *Server) paymentsHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Extract the authentication token.
	var apiToken bool
	token := req.FormValue("token")
	if token != "" { // an API token
		apiToken = true
	} else {
		token = getCookie(req, "X-Satellite-Token")
		if token == "" {
			s.writeError(w,
				Error{
					Code:    httpErrorTokenInvalid,
					Message: "no token provided",
				}, http.StatusUnauthorized)
			return
		}
	}

	// Decode the token.
	prefix, email, expires, err := s.accounts.DecodeToken(token)
	if err != nil {
		// Check and update login stats.
		if err := s.checkInvalidTokens(w, req); err != nil {
			return
		}
		s.log.Error("failed to decode token", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "unable to decode token",
			}, http.StatusUnauthorized)
		return
	}

	// Check the token type.
	if (apiToken && prefix != account.APIPrefix) || (!apiToken && prefix != account.CookiePrefix) {
		s.writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "wrong token type",
			}, http.StatusUnauthorized)
		return
	}

	// Check the token validity.
	if expires.Before(time.Now()) {
		s.writeError(w,
			Error{
				Code:    httpErrorTokenExpired,
				Message: "token already expired",
			}, http.StatusUnauthorized)
		return
	}

	// Retrieve the user account.
	acc, err := s.accounts.FindAccount(email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		s.writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "email address not found",
			}, http.StatusBadRequest)
		return
	}

	// Decode the request params.
	var offset, limit int64
	off := req.FormValue("offset")
	if off != "" {
		offset, err = strconv.ParseInt(off, 10, 64)
		if err != nil || offset < 0 {
			s.writeError(w,
				Error{
					Code:    httpErrorBadRequest,
					Message: "invalid offset parameter",
				}, http.StatusBadRequest)
			return
		}
	}

	lim := req.FormValue("limit")
	if lim != "" {
		limit, err = strconv.ParseInt(lim, 10, 64)
		if err != nil {
			s.writeError(w,
				Error{
					Code:    httpErrorBadRequest,
					Message: "invalid limit parameter",
				}, http.StatusBadRequest)
			return
		}
	} else {
		limit = -1
	}

	// Retrieve the payment history.
	payments, err := s.accounts.GetPayments(acc, int(offset), int(limit))
	if err != nil {
		s.log.Error("failed to retrieve payment history", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to retrieve payment history",
			}, http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, payments)
}
