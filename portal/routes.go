package portal

import (
	"bytes"
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
)

// authLink holds the parts of an authentication link.
type authLink struct {
	Path  string
	Token string
}

type (
	// authRequest holds the body of an /authme POST request.
	authRequest struct {
		Email    string `json: "email"`
		Password string `json: "password"`
	}
)

// checkEmail is a helper function that validates an email address.
// If the email address is valid, it is returned in lowercase.
func checkEmail(address string) (string, Error) {
	_, err := mail.ParseAddress(address)
	if err != nil {
		return "", Error{
			Code: httpErrorEmailInvalid,
			Message: "the email address is invalid",
		}
	}
	if len(address) > 48 {
		return "", Error{
			Code: httpErrorEmailTooLong,
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
			Code: httpErrorPasswordTooShort,
			Message: "the password is too short",
		}
	}
	if len(pwd) > 255 {
		return Error{
			Code: httpErrorPasswordTooLong,
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
			Code: httpErrorPasswordNotCompliant,
			Message: "insecure password",
		}
	}
	return Error{}
}

// authHandlerPOST handles the POST /auth requests.
func (api *portalAPI) authHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var auth authRequest
	err, code := api.handleDecodeError(w, dec.Decode(&auth))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// TODO implement auth code.

	writeSuccess(w)
}

// registerHandlerPOST handles the POST /register requests.
func (api *portalAPI) registerHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var reg authRequest
	err, code := api.handleDecodeError(w, dec.Decode(&reg))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Check request fields for validity.	
	email, err := checkEmail(reg.Email)
	if err.Code != httpErrorNone {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	password := reg.Password
	if err := checkPassword(password); err.Code != httpErrorNone {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	// Check if the email address is already registered.
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
	if count > 0 {
		writeError(w,
			Error{
				Code: httpErrorEmailUsed,
				Message: "email already registered",
			}, http.StatusNotAcceptable)
		return
	}

	// Send verification link by email.
	if !api.sendVerificationLinkByMail(w, req, email) {
		return
	}

	// Create a new account.
	if cErr := api.portal.createAccount(email, password); cErr != nil {
		api.portal.log.Printf("ERROR: error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	writeSuccess(w)
}

// sendVerificationLinkByMail is a wrapper function for sending a
// verification link by email.
func (api *portalAPI) sendVerificationLinkByMail(w http.ResponseWriter, req *http.Request, email string) bool {
	// Check and update stats. This is done after the email check but
	// we may decide to do it at an earlier step in the future.
	if err := api.portal.checkAndUpdateVerifications(req.RemoteAddr); err != nil {
		writeError(w,
			Error{
				Code: httpErrorTooManyRequests,
				Message: "too many verification requests",
			}, http.StatusTooManyRequests)
		return false
	}

	// Generate a verification link.
	token := api.portal.generateToken(verifyPrefix, email, time.Now().Add(24 * time.Hour))
	path := req.Header["Referer"]
	if len(path) == 0 {
		api.portal.log.Printf("ERROR: unable to fetch referer URL")
		writeError(w,
			Error{
				Code: httpErrorInternal,
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
	t, err := t.Parse(verifyTemplate)
	if err != nil {
		api.portal.log.Printf("ERROR: unable to parse HTML template: %v\n", err)
		writeError(w,
			Error{
				Code: httpErrorInternal,
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
				Code: httpErrorInternal,
				Message: "unable to send verification link",
			}, http.StatusInternalServerError)
		return false
	}

	return true
}

// registerResendHandlerPOST handles the POST /register/resend requests.
func (api *portalAPI) registerResendHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode request body.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Email string `json: "email"`
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
