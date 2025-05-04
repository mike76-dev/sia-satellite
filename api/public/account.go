package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"go.uber.org/zap"
)

// defaultTokenDuration is the default duration of an API token.
const defaultTokenDuration = 7 * 24 * 3600 // 7 days

// accountHandlerGET handles the GET /account requests.
func (s *Server) accountHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
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
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "email address not found",
			}, http.StatusUnauthorized)
		return
	}

	balance := Balance{
		Total:  acc.Balance.Total.Siacoins(),
		Locked: acc.Balance.Locked.Siacoins(),
	}

	if acc.Balance.Negative {
		balance.Total = -balance.Total
	}

	s.writeJSON(w, AccountResponse{
		Email:       acc.Email,
		CreatedAt:   acc.CreatedAt,
		Verified:    acc.Verified,
		PaymentPlan: acc.PaymentPlan,
		Balance:     balance,
		Currency: Currency{
			Code:   acc.Currency,
			SCRate: s.accounts.GetSiacoinRate(acc.Currency),
		},
		StripeID: acc.StripeID,
	})
}

// accountTokenHandlerGET handles the GET /account/token requests.
func (s *Server) accountTokenHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Extract the authentication token.
	token := getCookie(req, "X-Satellite-Token")
	if token == "" {
		s.writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "no token provided",
			}, http.StatusUnauthorized)
		return
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
	if prefix != account.CookiePrefix {
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
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "email address not found",
			}, http.StatusUnauthorized)
		return
	}

	// Decode the request params.
	duration := int64(defaultTokenDuration)
	d := req.FormValue("duration")
	if d != "" {
		duration, err = strconv.ParseInt(d, 10, 64)
		if err != nil || duration <= 0 || duration > math.MaxInt64/int64(time.Nanosecond) {
			s.writeError(w,
				Error{
					Code:    httpErrorBadRequest,
					Message: "invalid duration parameter",
				}, http.StatusBadRequest)
			return
		}
	}

	// Generate a token.
	apiToken, err := s.accounts.GenerateToken(account.APIPrefix, acc.Email, time.Now().Add(time.Duration(duration)*time.Nanosecond))
	if err != nil {
		s.log.Error("failed to generate API token", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to generate token",
			}, http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, AccountTokenResponse{Token: apiToken})
}
