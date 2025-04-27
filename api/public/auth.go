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
