package portal

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/external"
)

// Google glient ID.
var googleClientID string

// googleAuth is the object provided by Google during OAuth.
type googleAuth struct {
	ClientID   string `json:"clientId"`
	Credential string `json:"credential"`
}

// authHandlerPOST handles the POST /auth/login/:provider requests.
func (api *portalAPI) authHandlerPOST(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	// Retrieve provider name.
	provider := ps.ByName("provider")

	if provider == "google" {
		// Decode the request body.
		dec, err := prepareDecoder(w, req)
		if err != nil {
			return
		}

		var data googleAuth
		gErr, code := api.handleDecodeError(w, dec.Decode(&data))
		if code != http.StatusOK {
			writeError(w, gErr, code)
			return
		}

		// Verify client ID.
		if data.ClientID != googleClientID {
			api.portal.log.Println("ERROR: wrong client ID")
			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "wrong client ID",
				}, http.StatusBadRequest)
			return
		}

		// Verify and parse credential.
		claims := jwt.MapClaims{}
		_, err = jwt.ParseWithClaims(data.Credential, claims, func(token *jwt.Token) (interface{}, error) {
			pem, err := external.GetGooglePublicKey(fmt.Sprintf("%s", token.Header["kid"]))
			if err != nil {
				return nil, err
			}
			key, err := jwt.ParseRSAPublicKeyFromPEM([]byte(pem))
			if err != nil {
				return nil, err
			}
			return key, nil
		})
		if err != nil {
			api.portal.log.Println("ERROR: couldn't parse claims:", err)
			writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "couldn't parse claims",
				}, http.StatusInternalServerError)
			return
		}

		// Verify the issuer.
		if issuer, ok := claims["iss"]; !ok || (issuer != "accounts.google.com" && issuer != "https://accounts.google.com") {
			api.portal.log.Println("ERROR: invalid issuer")
			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "invalid issuer",
				}, http.StatusBadRequest)
			return
		}

		// Verify the audience.
		if audience, ok := claims["aud"]; !ok || audience != googleClientID {
			api.portal.log.Println("ERROR: invalid audience")
			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "invalid issuer",
				}, http.StatusBadRequest)
			return
		}

		// Verify the expiration time.
		var expires interface{}
		var ok bool
		if expires, ok = claims["exp"]; !ok {
			api.portal.log.Println("ERROR: invalid expiration time")
			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "invalid expiration time",
				}, http.StatusBadRequest)
			return
		}
		if expires.(float64) < float64(time.Now().Unix()) {
			api.portal.log.Println("ERROR: token has expired")
			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "token has expired",
				}, http.StatusBadRequest)
			return
		}

		// Check if the email is verified.
		if verified := claims["email_verified"]; verified != "true" && verified != true {
			api.portal.log.Println("ERROR: email not verified")
			writeError(w,
				Error{
					Code:    httpErrorWrongCredentials,
					Message: "email not verified",
				}, http.StatusBadRequest)
			return
		}

		// Retrieve the email address.
		email := strings.ToLower(claims["email"].(string))

		// Create an account if it doesn't exist yet.
		exists, err := api.portal.userExists(email)
		if err != nil {
			api.portal.log.Println("ERROR: couldn't verify user account:", err)
			writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "couldn't verify account",
				}, http.StatusInternalServerError)
			return
		}
		if !exists {
			// Check and update stats.
			if err := api.portal.checkAndUpdateFailedLogins(getRemoteHost(req)); err != nil {
				writeError(w,
					Error{
						Code:    httpErrorTooManyRequests,
						Message: "too many failed login attempts",
					}, http.StatusTooManyRequests)
				return
			}

			// Create a new account.
			if err := api.portal.updateAccount(email, "", true); err != nil {
				api.portal.log.Printf("ERROR: error querying database: %v\n", err)
				writeError(w,
					Error{
						Code:    httpErrorInternal,
						Message: "internal error",
					}, http.StatusInternalServerError)
				return
			}
		}

		// Login successful, generate a cookie.
		t := time.Now().Add(7 * 24 * time.Hour)
		token, err := api.portal.generateToken(cookiePrefix, email, t)
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
			Name:    "satellite",
			Value:   token,
			Expires: t,
			Path:    "/",
		}

		// Send the cookie.
		http.SetCookie(w, &cookie)
		writeSuccess(w)
		return
	}

	writeError(w,
		Error{
			Code:    httpErrorBadRequest,
			Message: "provider not supported",
		}, http.StatusBadRequest)

	/*switch prefix {
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

	writeSuccess(w)*/
}

// initGoogle loads the Google client ID.
func initGoogle() {
	googleClientID = os.Getenv("SATD_GOOGLE_CLIENT")
}
