package portal

import (
	"bytes"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/julienschmidt/httprouter"
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
	    <p>Click on the following link to complete your account registration. This link is valid within the next 24 hours.</p>
	    <p><a href="{{.Path}}?token={{.Token}}">{{.Path}}?token={{.Token}}</a></p>
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
	// authLink holds the parts of an authentication link.
	authLink struct {
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
	var smalls, caps, digits, specials bool
	for _, c := range pwd {
		switch {
		case unicode.IsNumber(c):
			digits = true
		case unicode.IsLower(c):
			smalls = true
		case unicode.IsUpper(c):
			caps = true
		case unicode.IsPunct(c) || unicode.IsSymbol(c):
			specials = true
		default:
		}
	}
	if !smalls || !caps || !digits || !specials {
		return Error{
			Code:    httpErrorPasswordNotCompliant,
			Message: "insecure password",
		}
	}
	return Error{}
}

// loginHandlerPOST handles the POST /auth/login requests.
func (api *portalAPI) loginHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
	}
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}
	email := strings.ToLower(data.Email)
	password := req.Header.Get("Satellite-Password")

	// Check if the user account exists.
	exists, cErr := api.portal.userExists(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	if !exists {
		// Wrong email address. Check and update stats.
		if err := api.portal.checkAndUpdateFailedLogins(getRemoteHost(req)); err != nil {
			writeError(w,
				Error{
					Code:    httpErrorTooManyRequests,
					Message: "too many failed login attempts",
				}, http.StatusTooManyRequests)
			return
		}
		writeError(w,
			Error{
				Code:    httpErrorWrongCredentials,
				Message: "invalid combination of email and password",
			}, http.StatusBadRequest)
		return
	}

	// Check if the account is verified and the password is correct.
	verified, passwordOK, cErr := api.portal.isVerified(email, password)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Wrong password or the email is not verified.
	if !passwordOK || !verified {
		// Check and update login stats.
		if err := api.portal.checkAndUpdateFailedLogins(getRemoteHost(req)); err != nil {
			writeError(w,
				Error{
					Code:    httpErrorTooManyRequests,
					Message: "too many failed login attempts",
				}, http.StatusTooManyRequests)
			return
		}

		writeError(w,
			Error{
				Code:    httpErrorWrongCredentials,
				Message: "invalid combination of email and password",
			}, http.StatusBadRequest)
		return
	}

	// Login successful, generate a cookie.
	t := time.Now().Add(7 * 24 * time.Hour)
	token, tErr := api.portal.generateToken(cookiePrefix, email, t)
	if tErr != nil {
		api.portal.log.Printf("ERROR: error generating token: %v\n", tErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	cookie := http.Cookie{
		Name:    "satellite",
		Value:   token,
		Expires: t,
		Path:    "/",
	}

	// Send the cookie.
	http.SetCookie(w, &cookie)
	writeSuccess(w)
}

// registerHandlerPOST handles the POST /auth/register requests.
func (api *portalAPI) registerHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
	}
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Check request fields for validity.
	email, err := checkEmail(data.Email)
	if err.Code != httpErrorNone {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	password := req.Header.Get("Satellite-Password")
	if err := checkPassword(password); err.Code != httpErrorNone {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	// Check if the email address is already registered.
	exists, cErr := api.portal.userExists(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	if exists {
		// Check if the account is verified already.
		verified, passwordOK, cErr := api.portal.isVerified(email, password)
		if cErr != nil {
			api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
			writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		// Account fully registered.
		if verified {
			writeError(w,
				Error{
					Code:    httpErrorEmailUsed,
					Message: "email address already used",
				}, http.StatusBadRequest)
			return
		}

		// Wrong password.
		if !passwordOK {
			// Check and update login stats.
			if cErr := api.portal.checkAndUpdateFailedLogins(getRemoteHost(req)); cErr != nil {
				writeError(w,
					Error{
						Code:    httpErrorTooManyRequests,
						Message: "too many failed login attempts",
					}, http.StatusTooManyRequests)
				return
			}

			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "invalid combination of email and password",
				}, http.StatusBadRequest)
			return
		}
	}

	// Create a new account.
	if cErr := api.portal.updateAccount(email, password, false); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Send verification link by email.
	if !api.sendVerificationLinkByMail(w, req, email) {
		return
	}

	writeSuccess(w)
}

// sendVerificationLinkByMail is a wrapper function for sending a
// verification link by email.
func (api *portalAPI) sendVerificationLinkByMail(w http.ResponseWriter, req *http.Request, email string) bool {
	// Check and update stats.
	if err := api.portal.checkAndUpdateVerifications(getRemoteHost(req)); err != nil {
		writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many verification requests",
			}, http.StatusTooManyRequests)
		return false
	}

	// Generate a verification link.
	token, err := api.portal.generateToken(verifyPrefix, email, time.Now().Add(24*time.Hour))
	if err != nil {
		api.portal.log.Printf("ERROR: error generating token: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return false
	}
	path := req.Header["Referer"]
	if len(path) == 0 {
		api.portal.log.Printf("ERROR: unable to fetch referer URL")
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to fetch referer URL",
			}, http.StatusInternalServerError)
		return false
	}
	link := authLink{
		Path:  path[0],
		Token: token,
	}

	// Generate email body.
	t := template.New("verify")
	t, err = t.Parse(verifyTemplate)
	if err != nil {
		api.portal.log.Printf("ERROR: unable to parse HTML template: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send verification link",
			}, http.StatusInternalServerError)
		return false
	}
	var b bytes.Buffer
	t.Execute(&b, link)

	// Send verification link by email.
	err = api.portal.ms.SendMail("Sia Satellite", email, "Action Required", &b)
	if err != nil {
		api.portal.log.Printf("ERROR: unable to send verification link: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send verification link",
			}, http.StatusInternalServerError)
		return false
	}

	return true
}

// sendPasswordResetLinkByMail is a wrapper function for sending a
// password reset link by email.
func (api *portalAPI) sendPasswordResetLinkByMail(w http.ResponseWriter, req *http.Request, email string) bool {
	// Generate a password reset link.
	token, err := api.portal.generateToken(resetPrefix, email, time.Now().Add(time.Hour))
	if err != nil {
		api.portal.log.Printf("ERROR: error generating token: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return false
	}
	path := req.Header["Referer"]
	if len(path) == 0 {
		api.portal.log.Printf("ERROR: unable to fetch referer URL")
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to fetch referer URL",
			}, http.StatusInternalServerError)
		return false
	}
	link := authLink{
		Path:  path[0],
		Token: token,
	}

	// Generate email body.
	t := template.New("reset")
	t, err = t.Parse(resetTemplate)
	if err != nil {
		api.portal.log.Printf("ERROR: unable to parse HTML template: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send password reset link",
			}, http.StatusInternalServerError)
		return false
	}
	var b bytes.Buffer
	t.Execute(&b, link)

	// Send password reset link by email.
	err = api.portal.ms.SendMail("Sia Satellite", email, "Reset Your Password", &b)
	if err != nil {
		api.portal.log.Printf("ERROR: unable to send password reset link: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "unable to send password reset link",
			}, http.StatusInternalServerError)
		return false
	}

	return true
}

// registerResendHandlerPOST handles the POST /auth/register/resend requests.
func (api *portalAPI) registerResendHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
	}
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Send verification link by email.
	if !api.sendVerificationLinkByMail(w, req, data.Email) {
		return
	}

	writeSuccess(w)
}

// authHandlerGET handles the GET /auth requests.
func (api *portalAPI) authHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	token := req.Header.Get("Satellite-Token")

	// Decode the token.
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil {
		writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "unable to decode token",
			}, http.StatusBadRequest)
		return
	}

	switch prefix {
	case verifyPrefix:
		// A verify token received. Check the validity.
		if expires.Before(time.Now()) {
			writeError(w,
				Error{
					Code:    httpErrorTokenExpired,
					Message: "link already expired",
				}, http.StatusBadRequest)
			return
		}

		// Update the user account.
		err := api.portal.updateAccount(email, "", true)
		if err != nil {
			writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "unable to verify account",
				}, http.StatusInternalServerError)
			return
		}

		// Check if any promo action is running.
		err = api.portal.creditAccount(email)
		if err != nil {
			api.portal.log.Printf("ERROR: unable to credit user account: %v\n", err)
		}

	case resetPrefix:
		// A password reset token received. Check the validity.
		if expires.Before(time.Now()) {
			writeError(w,
				Error{
					Code:    httpErrorTokenExpired,
					Message: "link already expired",
				}, http.StatusBadRequest)
			return
		}

		// Check if the email address is already registered.
		exists, cErr := api.portal.userExists(email)
		if cErr != nil {
			api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
			writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		if !exists {
			// No such email address found. This can only happen if
			// the account was deleted.
			writeError(w,
				Error{
					Code:    httpErrorNotFound,
					Message: "email address not found",
				}, http.StatusBadRequest)
			return
		}

		// Generate a change cookie. This one has a different name, so
		// that a password reset is not confused for a password change.
		// Set the expiration the same as of the password reset token.
		ct, err := api.portal.generateToken(changePrefix, email, expires)
		if err != nil {
			api.portal.log.Printf("ERROR: error generating token: %v\n", err)
			writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}
		cookie := http.Cookie{
			Name:    "satellite-change",
			Value:   ct,
			Expires: expires,
			Path:    "/",
		}
		http.SetCookie(w, &cookie)

	default:
		writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "prefix not supported",
			}, http.StatusBadRequest)
		return
	}

	writeSuccess(w)
}

// resetHandlerPOST handles the POST /auth/reset requests.
func (api *portalAPI) resetHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check and update password reset stats.
	if cErr := api.portal.checkAndUpdatePasswordResets(getRemoteHost(req)); cErr != nil {
		writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many password reset requests",
			}, http.StatusTooManyRequests)
		return
	}

	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
	}
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Check if such account exists.
	exists, cErr := api.portal.userExists(data.Email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	if !exists {
		// Do not return an error. Otherwise we would give a potential
		// attacker a hint.
		writeSuccess(w)
		return
	}

	// Send password reset link by email.
	if !api.sendPasswordResetLinkByMail(w, req, data.Email) {
		return
	}

	writeSuccess(w)
}

// resetResendHandlerPOST handles the POST /auth/reset/resend requests.
func (api *portalAPI) resetResendHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check and update stats.
	if err := api.portal.checkAndUpdatePasswordResets(getRemoteHost(req)); err != nil {
		writeError(w,
			Error{
				Code:    httpErrorTooManyRequests,
				Message: "too many password reset requests",
			}, http.StatusTooManyRequests)
		return
	}

	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Email string `json:"email"`
	}
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Send password reset link by email.
	if !api.sendPasswordResetLinkByMail(w, req, data.Email) {
		return
	}

	writeSuccess(w)
}

// changeHandlerPOST handles the POST /auth/change requests.
func (api *portalAPI) changeHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check password for validity.
	password := req.Header.Get("Satellite-Password")
	if err := checkPassword(password); err.Code != httpErrorNone {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	// Decode and verify the token.
	reset := false
	token := getCookie(req, "satellite")
	if token == "" {
		// Password reset was requested.
		token = getCookie(req, "satellite-change")
		reset = true
	}
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil || (reset && prefix != changePrefix) || (!reset && prefix != cookiePrefix) {
		writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
			Error{
				Code:    httpErrorTokenExpired,
				Message: "token already expired",
			}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	exists, cErr := api.portal.userExists(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if !exists {
		writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Update the account with the new password. Set it to
	// verified in any case, because the email address has
	// been verified.
	if cErr := api.portal.updateAccount(email, password, true); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	writeSuccess(w)
}

// deleteHandlerGET handles the GET /auth/delete requests.
func (api *portalAPI) deleteHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, err := api.verifyCookie(w, token)
	if err != nil {
		return
	}

	// Check if the account is allowed to be deleted.
	ub, err := api.portal.manager.GetBalance(email)
	if err != nil {
		api.portal.log.Printf("ERROR: couldn't get account balance: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	if ub.Balance+ub.Locked < 0 {
		writeError(w,
			Error{
				Code:    httpErrorBadRequest,
				Message: "balance negative",
			}, http.StatusBadRequest)
		return
	}

	// Remove the account from the database.
	if err = api.portal.deleteAccount(email); err != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Remove renter from the memory.
	api.portal.manager.DeleteRenter(email)

	writeSuccess(w)
}

// verifyCookie is a helper function that verifies the satellite token.
func (api *portalAPI) verifyCookie(w http.ResponseWriter, token string) (email string, err error) {
	// Decode the token.
	prefix, email, expires, err := api.portal.decodeToken(token)
	if err != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	// Check if the token has expired.
	if expires.Before(time.Now()) {
		writeError(w,
			Error{
				Code:    httpErrorTokenExpired,
				Message: "token already expired",
			}, http.StatusBadRequest)
		err = errors.New("token already expired")
		return
	}

	// Check if the user account exists.
	exists, err := api.portal.userExists(email)
	if err != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", err)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if !exists {
		writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		err = errors.New("no such account")
		return
	}

	return
}

// creditAccount checks if any promo action is running and credits
// the user account if so.
func (p *Portal) creditAccount(email string) error {
	// Check first.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.credits.Remaining <= 0 {
		return nil
	}

	// Credit the account.
	err := p.addPayment(email, p.credits.Amount, "USD")
	if err != nil {
		return err
	}

	// Decrease the remaining credits and save.
	p.credits.Remaining--
	return p.saveCredits()
}
