package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"text/template"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"go.uber.org/zap"
)

const (
	// verifyTemplate contains the text send by email when a
	// new user account is being created.
	verifyTemplate = `
		<!-- template.html -->
		<!DOCTYPE html>
		<html>
		<body>
			<h2>Please Verify Your Email Address</h2>
			<p>This is your one-time code to complete your account registration. This code is valid within the next 15 minutes.</p>
			<h1>{{.Code}}</h1>
		</body>
		</html>
	`

	// resetTemplate contains the text send by email when a
	// user wants to reset their password.
	resetTemplate = `
		<!-- template.html -->
		<!DOCTYPE html>
		<html>
		<body>
			<h2>Reset Your Password</h2>
			<p>Click on the following link to enter a new password. This link is valid within the next 60 minutes.</p>
			<p><a href="{{.Path}}?token={{.Token}}">{{.Path}}?token={{.Token}}</a></p>
		</body>
		</html>
	`
)

type (
	// verificationCode holds a email verification code.
	verificationCode struct {
		Code string
	}

	// resetLink holds the parts of a password reset link.
	resetLink struct {
		Path  string
		Token string
	}
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

// checkPasswordResets is a helper function that checks if the remote
// host has exceeded the password reset attempt count and sends
// a response if it has.
func (s *Server) checkPasswordResets(w http.ResponseWriter, req *http.Request) error {
	err := s.checkAndUpdatePasswordResets(getRemoteHost(req))
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many password reset requests",
			}, http.StatusTooManyRequests)
	}
	return err
}

// checkVerifications is a helper function that checks if the remote
// host has exceeded the verification attempt count and sends a response
// if it has.
func (s *Server) checkVerifications(w http.ResponseWriter, req *http.Request) error {
	err := s.checkAndUpdateVerifications(getRemoteHost(req))
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many verification requests",
			}, http.StatusTooManyRequests)
	}
	return err
}

// checkInvalidTokens is a helper function that checks if the remote
// host has exceeded the invalid token submission count and sends
// a response if it has.
func (s *Server) checkInvalidTokens(w http.ResponseWriter, req *http.Request) error {
	err := s.checkAndUpdateInvalidTokens(getRemoteHost(req))
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many invalid token submissions",
			}, http.StatusTooManyRequests)
	}
	return err
}

// checkAbuse is a helper function that checks if the remote host
// has exceeded the API call limit and sends a response if it has.
func (s *Server) checkAbuse(w http.ResponseWriter, req *http.Request) error {
	err := s.checkCalls(getRemoteHost(req))
	if err != nil {
		s.writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many calls",
			}, http.StatusTooManyRequests)
	}
	return err
}

// authHandlerGET handles the GET /auth requests.
func (s *Server) authHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

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
	if (reset && prefix != account.ResetPrefix) || (!reset && prefix != account.CookiePrefix) {
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
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Decode request body.
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

// authSignupHandlerPOST handles the POST /auth/signup requests.
func (s *Server) authSignupHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Decode request body.
	dec, err := s.prepareDecoder(w, req)
	if err != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
		Code  string `json:"code,omitempty"`
	}
	httpError, code := s.handleDecodeError(dec.Decode(&data))
	if code != http.StatusOK {
		s.writeError(w, httpError, code)
		return
	}

	// Check request fields for validity.
	email, httpError := checkEmail(data.Email)
	if httpError.Code != httpErrorNone {
		s.writeError(w, httpError, http.StatusBadRequest)
		return
	}

	var password string
	if data.Code == "" {
		password = req.Header.Get("X-Satellite-Password")
		if httpError := checkPassword(password); httpError.Code != httpErrorNone {
			s.writeError(w, httpError, http.StatusBadRequest)
			return
		}
	}

	// Check if the email address is already registered and/or verified.
	var found, verified bool
	var verifyError error
	acc, err := s.accounts.FindAccount(email)
	if err == nil {
		found = true
		verified = acc.Verified
		verifyError = acc.VerifyCode(data.Code)
	}

	if found && verified { // account fully registered
		s.writeError(w,
			Error{
				Code:    httpErrorEmailUsed,
				Message: "email address already used",
			}, http.StatusBadRequest)
		return
	}

	if data.Code == "" {
		if !found { // no account yet
			acc, err = s.accounts.NewAccount(email, password)
			if err != nil {
				s.log.Error("failed to create account", zap.Error(err))
				s.writeError(w,
					Error{
						Code:    httpErrorInternal,
						Message: "internal error",
					}, http.StatusInternalServerError)
				return
			}
		}

		// Send verification code by email.
		if ok := s.sendVerificationCodeByMail(w, req, acc); !ok {
			return
		}
	} else {
		if !found { // no account yet but a verificaion code is there
			s.writeError(w,
				Error{
					Code:    httpErrorNotFound,
					Message: "email address not found",
				}, http.StatusBadRequest)
			return
		}

		// Check if the code is correct.
		if verifyError != nil && errors.Is(verifyError, account.ErrWrongCode) {
			// Check and update stats.
			if err := s.checkFailedLogins(w, req); err != nil {
				return
			}

			s.writeError(w,
				Error{
					Code:    httpErrorTokenInvalid,
					Message: "invalid code",
				}, http.StatusUnauthorized)
			return
		} else if verifyError != nil && errors.Is(verifyError, account.ErrCodeExpired) {
			s.writeError(w,
				Error{
					Code:    httpErrorTokenExpired,
					Message: "code already expired",
				}, http.StatusUnauthorized)
			return
		}

		// All good, mark the account as verified.
		if err := s.accounts.SetVerified(acc); err != nil {
			s.log.Error("failed to verify account", zap.Error(err))
			s.writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		// Generate a cookie.
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
	}

	s.writeSuccess(w)
}

// sendVerificationCodeByMail is a wrapper function for sending a
// verification code by email.
func (s *Server) sendVerificationCodeByMail(w http.ResponseWriter, req *http.Request, acc *account.Account) bool {
	// Check and update stats.
	if err := s.checkVerifications(w, req); err != nil {
		return false
	}

	// Generate a verification code.
	code := verificationCode{Code: acc.GenerateCode(time.Now().Add(15 * time.Minute))}

	// Generate email body.
	t := template.New("verify")
	t, err := t.Parse(verifyTemplate)
	if err != nil {
		s.log.Error("unable to parse HTML template", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send verification code",
			}, http.StatusInternalServerError)
		return false
	}
	var b bytes.Buffer
	t.Execute(&b, code)

	// Send verification code by email.
	err = s.mail.SendMail("Sia Satellite", acc.Email, "Action Required", &b)
	if err != nil {
		s.log.Error("unable to send verification code", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send verification code",
			}, http.StatusInternalServerError)
		return false
	}

	return true
}

// sendPasswordResetLinkByMail is a wrapper function for sending a
// password reset link by email.
func (s *Server) sendPasswordResetLinkByMail(w http.ResponseWriter, req *http.Request, email string) bool {
	// Generate a password reset link.
	token, err := s.accounts.GenerateToken(account.ResetPrefix, email, time.Now().Add(time.Hour))
	if err != nil {
		s.log.Error("error generating token", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return false
	}
	path := req.Header["Referer"]
	if len(path) == 0 {
		s.log.Error("unable to fetch referer URL")
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to fetch referer URL",
			}, http.StatusInternalServerError)
		return false
	}
	link := resetLink{
		Path:  path[0],
		Token: token,
	}

	// Generate email body.
	t := template.New("reset")
	t, err = t.Parse(resetTemplate)
	if err != nil {
		s.log.Error("unable to parse HTML template", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send password reset link",
			}, http.StatusInternalServerError)
		return false
	}
	var b bytes.Buffer
	t.Execute(&b, link)

	// Send password reset link by email.
	err = s.mail.SendMail("Sia Satellite", email, "Reset Your Password", &b)
	if err != nil {
		s.log.Error("unable to send password reset link", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send password reset link",
			}, http.StatusInternalServerError)
		return false
	}

	return true
}

// authSignupResendHandlerPOST handles the POST /auth/signup/resend requests.
func (s *Server) authSignupResendHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Decode request body.
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

	// Retrieve the user account.
	acc, err := s.accounts.FindAccount(data.Email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		s.writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "email address not found",
			}, http.StatusBadRequest)
		return
	}

	if acc.Verified { // already verified, no need to send a code
		s.writeError(w,
			Error{
				Code:    httpErrorEmailUsed,
				Message: "email address already used",
			}, http.StatusBadRequest)
		return
	}

	// Send verification code by email.
	if ok := s.sendVerificationCodeByMail(w, req, acc); !ok {
		return
	}

	s.writeSuccess(w)
}

// authResetHandlerPOST handles the POST /auth/reset requests.
func (s *Server) authResetHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse. This may be redundant, but shouldn't hurt.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Check and update stats.
	if err := s.checkPasswordResets(w, req); err != nil {
		return
	}

	// Decode request body.
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

	// Retrieve the user account.
	_, err = s.accounts.FindAccount(data.Email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		// Do not return an error. Otherwise we would give a potential
		// attacker a hint.
		s.writeSuccess(w)
		return
	}

	// Send password reset link by email.
	if ok := s.sendPasswordResetLinkByMail(w, req, data.Email); !ok {
		return
	}

	s.writeSuccess(w)
}

// authResetResendHandlerPOST handles the POST /auth/reset/resend requests.
func (s *Server) authResetResendHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse. This may be redundant, but shouldn't hurt.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Check and update stats.
	if err := s.checkPasswordResets(w, req); err != nil {
		return
	}

	// Decode request body.
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

	// Retrieve the user account.
	_, err = s.accounts.FindAccount(data.Email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		// Do not return an error. Otherwise we would give a potential
		// attacker a hint.
		s.writeSuccess(w)
		return
	}

	// Send password reset link by email. Note that we don't check now if
	// the account is verified. We will need to do that at a later point.
	if ok := s.sendPasswordResetLinkByMail(w, req, data.Email); !ok {
		return
	}

	s.writeSuccess(w)
}

// authChangeHandlerGET handles the GET /auth/change requests.
func (s *Server) authChangeHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Extract the authentication token.
	var apiToken, reset bool
	token := req.FormValue("token")
	if token != "" { // an API token
		apiToken = true
	} else {
		token = getCookie(req, "X-Satellite-Token")
		if token == "" {
			token = getCookie(req, "X-Satellite-Change")
			reset = true
			if token == "" {
				s.writeError(w,
					Error{
						Code:    httpErrorTokenInvalid,
						Message: "no token provided",
					}, http.StatusUnauthorized)
				return
			}
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
	if (apiToken && prefix != account.APIPrefix) || (!apiToken && ((reset && prefix != account.ChangePrefix) || (!reset && prefix != account.CookiePrefix))) {
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

	// Check new password for validity.
	password := req.Header.Get("X-Satellite-Password")
	if httpError := checkPassword(password); httpError.Code != httpErrorNone {
		s.writeError(w, httpError, http.StatusBadRequest)
		return
	}

	// Check if the email address is registered.
	_, err = s.accounts.FindAccount(email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		s.writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "email address not found",
			}, http.StatusBadRequest)
		return
	}

	// Change the account password.
	if err := s.accounts.ChangePassword(email, password); err != nil {
		s.log.Error("failed to change password", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "couldn't change password",
			}, http.StatusInternalServerError)
		return
	}

	s.writeSuccess(w)
}
