package api

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"go.uber.org/zap"
)

// checkEmail is a helper function that validates an email address.
// If the email address is valid, it is returned in lowercase.
func checkEmail(address string) (string, Error) {
	_, err := mail.ParseAddress(address)
	if err != nil {
		return "", Error{
			Code:    httpErrorEmailInvalid,
			Message: "the email address is invalid",
		}
	}
	if len(address) > 64 {
		return "", Error{
			Code:    httpErrorEmailTooLong,
			Message: "the email address is too long",
		}
	}
	return strings.ToLower(address), Error{}
}

// checkPassword is a helper function that checks if the password
// complies with the rules.
func checkPassword(pwd string) Error {
	if len(pwd) < 8 {
		return Error{
			Code:    httpErrorPasswordTooShort,
			Message: "the password is too short",
		}
	}
	if len(pwd) > 255 {
		return Error{
			Code:    httpErrorPasswordTooLong,
			Message: "the password is too long",
		}
	}
	return Error{}
}

// checkFailedLogins is a helper function that checks if the remote
// host has exceeded the failed login attempt count and sends a response
// if it has.
func (s *Server) checkFailedLogins(w http.ResponseWriter, req *http.Request) error {
	err := s.checkAndUpdateFailedLogins(getRemoteHost(req))
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many failed login attempts",
			}, http.StatusTooManyRequests)
	}
	return err
}

// checkFailedResets is a helper function that checks if the remote
// host has exceeded the password reset attempt count and sends
// a response if it has.
func (s *Server) checkPasswordResets(w http.ResponseWriter, req *http.Request) error {
	err := s.checkAndUpdatePasswordResets(getRemoteHost(req))
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many failed login attempts",
			}, http.StatusTooManyRequests)
	}
	return err
}

// authHandlerGET handles the GET /auth requests.
func (s *Server) authHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Extract the authentication token.
	var reset bool
	token := req.Header.Get("X-Satellite-Token")
	if token != "" { // a password reset was requested
		reset = true
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
		if err := s.checkFailedLogins(w, req); err != nil {
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
	if (reset && prefix != account.ResetPrefix) || prefix != account.CookiePrefix {
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

	// Generate a change cookie. This one has a different name, so
	// a password reset is not confused for a password change.
	// Set the expiration the same as of the password reset token.
	if reset {
		changeToken, err := s.accounts.GenerateToken(account.ChangePrefix, email, expires)
		if err != nil {
			s.log.Error("error generating token", zap.Error(err))
			s.writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		cookie := http.Cookie{
			Name:    "X-Satellite-Change",
			Value:   changeToken,
			Expires: expires,
			Path:    "/",
		}
		http.SetCookie(w, &cookie)
	}

	s.writeSuccess(w)
}

// authLoginHandlerPOST handles the POST /auth/login requests.
func (s *Server) authLoginHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	dec, err := s.prepareDecoder(w, req)
	if err != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
	}
	httpError, code := s.handleDecodeError(dec.Decode(&data))
	if code != http.StatusOK {
		s.writeError(w, httpError, code)
		return
	}
	email := strings.ToLower(data.Email)
	password := req.Header.Get("X-Satellite-Password")

	// Check if the user account exists.
	acc, err := s.accounts.FindAccount(email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		// Wrong email address. Check and update stats.
		if err := s.checkFailedLogins(w, req); err != nil {
			return
		}
		s.writeError(w,
			Error{
				Code:    httpErrorWrongCredentials,
				Message: "invalid combination of email and password",
			}, http.StatusBadRequest)
		return
	}

	// Check if the password is correct.
	if err := s.accounts.VerifyPassword(email, password); err != nil && errors.Is(err, account.ErrWrongPassword) {
		// Check and update login stats.
		if err := s.checkFailedLogins(w, req); err != nil {
			return
		}
		s.writeError(w,
			Error{
				Code:    httpErrorWrongCredentials,
				Message: "invalid combination of email and password",
			}, http.StatusBadRequest)
		return
	} else if err != nil {
		s.log.Error("failed to verify password", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Check if the account is verified.
	if !acc.Verified {
		// Check and update login stats.
		if err := s.checkFailedLogins(w, req); err != nil {
			return
		}
		s.writeError(w,
			Error{
				Code:    httpErrorUnverified,
				Message: "account not verified",
			}, http.StatusUnauthorized)
		return
	}

	// Login successful, generate a cookie.
	t := time.Now().Add(7 * 24 * time.Hour)
	token, err := s.accounts.GenerateToken(account.CookiePrefix, email, t)
	if err != nil {
		s.log.Error("error generating token", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	cookie := http.Cookie{
		Name:    "X-Satellite-Token",
		Value:   token,
		Expires: t,
		Path:    "/",
	}

	// Send the cookie.
	http.SetCookie(w, &cookie)
	s.writeSuccess(w)
}
