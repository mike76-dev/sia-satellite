package portal

import (
	"bytes"
	"net/http"
	"net/mail"
	"strings"
	"unicode"

	"github.com/julienschmidt/httprouter"
)

type (
	// authRequest holds the body of an /authme POST request.
	authRequest struct {
		Email    string `json: "email"`
		Password string `json: "password"`
	}
)

// checkEmail is a helper function that validates an email address.
// If the email address is valid, it is returned in lowercase.
func checkEmail(address string) (string, bool) {
	_, err := mail.ParseAddress(address)
	if err != nil {
		return "", false
	}
	return strings.ToLower(address), true
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
	email := reg.Email
	if _, ok := checkEmail(email); !ok {
		writeError(w, Error{
			Code: httpErrorEmailInvalid,
			Message: "invalid email address",
		}, http.StatusBadRequest)
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

	// Check and update stats. This is done after the email check but
	// we may decide to do it at an earlier step in the future.
	if cErr := api.portal.checkAndUpdateSignupRequests(req.RemoteAddr); cErr != nil {
		writeError(w,
			Error{
				Code: httpErrorTooManyRequests,
				Message: "too many signup requests",
			}, http.StatusTooManyRequests)
		return
	}

	// Send verification link by email.
	var b bytes.Buffer
	b.Write([]byte("<h1>This is a test</h1>")) // TODO generate a verification link
	sErr := api.portal.ms.SendMail("Sia Satellite", email, "Action Required", &b)
	if sErr != nil {
		api.portal.log.Printf("ERROR: unable to send verification link: %v\n", sErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "unable to send verification link",
			}, http.StatusInternalServerError)
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
