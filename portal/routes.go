package portal

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode"

	"github.com/julienschmidt/httprouter"
)

type (
	// authMeRequest holds the body of an /authme POST request.
	authMeRequest struct {
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
func checkPassword(pwd string) error {
	if len(pwd) < 8 {
		return errors.New("the password is too short")
	}
	if len(pwd) > 255 {
		return errors.New("the password is too long")
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
		return errors.New("insecure password")
	}
	return nil	
}

// authMeHandlerPOST handles the POST /authme requests.
func authMeHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var auth authMeRequest
	err, code := handleDecodeError(w, dec.Decode(&auth))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}
	
	/*email := auth.Email
	if _, ok := checkEmail(email); !ok {
		writeError(w, Error{"invalid email address"}, http.StatusBadRequest)
		return
	}
	password := auth.Password
	if err := checkPassword(password); err != nil {
		writeError(w, Error{"invalid password: " + err.Error()}, http.StatusBadRequest)
		return
	}*/

	// TODO implement auth code.

	writeSuccess(w)
}
