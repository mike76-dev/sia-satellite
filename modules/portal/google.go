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
		// Retrieve the action type.
		action := req.FormValue("action")
		if action != "signup" && action != "login" {
			api.portal.log.Println("ERROR: wrong action type")
			writeError(w,
				Error{
					Code:    httpErrorBadRequest,
					Message: "wrong action type",
				}, http.StatusBadRequest)
			return
		}

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

			// Check against the action type.
			if action != "signup" {
				writeError(w,
					Error{
						Code:    httpErrorEmailInvalid,
						Message: "invalid email provided",
					}, http.StatusBadRequest)
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
		} else {
			if action == "signup" {
				writeError(w,
					Error{
						Code:    httpErrorEmailUsed,
						Message: "user already exists",
					}, http.StatusBadRequest)
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
}

// initGoogle loads the Google client ID.
func initGoogle() {
	googleClientID = os.Getenv("SATD_GOOGLE_CLIENT")
}
