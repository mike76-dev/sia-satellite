package api

import (
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"go.uber.org/zap"
)

// accountSettingsGougingHandlerPOST handles the POST /account/settings/gouging requests.
func (s *Server) accountSettingsGougingHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Extract the authentication token.
	token := req.FormValue("token")
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
		// Check and update stats.
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
	if prefix != account.APIPrefix {
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

	// Decode request body.
	dec, err := s.prepareDecoder(w, req)
	if err != nil {
		return
	}

	var gs account.GougingSettings
	httpError, code := s.handleDecodeError(dec.Decode(&gs))
	if code != http.StatusOK {
		s.writeError(w, httpError, code)
		return
	}

	// Update the settings.
	if err := s.accounts.UpdateGougingSettings(acc, gs); err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "failed to update gouging settings",
			}, http.StatusInternalServerError)
		return
	}

	s.writeSuccess(w)
}

// accountSettingsUploadHandlerPOST handles the POST /account/settings/upload requests.
func (s *Server) accountSettingsUploadHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Extract the authentication token.
	token := req.FormValue("token")
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
		// Check and update stats.
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
	if prefix != account.APIPrefix {
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

	// Decode request body.
	dec, err := s.prepareDecoder(w, req)
	if err != nil {
		return
	}

	var us account.UploadSettings
	httpError, code := s.handleDecodeError(dec.Decode(&us))
	if code != http.StatusOK {
		s.writeError(w, httpError, code)
		return
	}

	// Update the settings.
	if err := s.accounts.UpdateUploadSettings(acc, us); err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "failed to update upload settings",
			}, http.StatusInternalServerError)
		return
	}

	s.writeSuccess(w)
}
