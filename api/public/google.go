package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"github.com/mike76-dev/sia-satellite/external"
	"go.uber.org/zap"
)

// Google glient ID.
var googleClientID string

// googleAuth is the object provided by Google during OAuth.
type googleAuth struct {
	ClientID   string `json:"clientId"`
	Credential string `json:"credential"`
}

// authLoginProviderHandlerPOST handles the POST /auth/login/:provider requests.
func (s *Server) authLoginProviderHandlerPOST(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	// Retrieve provider name.
	provider := ps.ByName("provider")

	if provider == "google" {
		// Retrieve the action type.
		action := req.FormValue("action")
		if action != "signup" && action != "login" {
			s.log.Error("wrong action type")
			s.writeError(w,
				Error{
					Code:    HttpErrorBadRequest,
					Message: "wrong action type",
				}, http.StatusBadRequest)
			return
		}

		// Decode the request body.
		dec, err := s.prepareDecoder(w, req)
		if err != nil {
			return
		}

		var data googleAuth
		httpError, code := s.handleDecodeError(dec.Decode(&data))
		if code != http.StatusOK {
			s.writeError(w, httpError, code)
			return
		}

		// Verify client ID.
		if data.ClientID != googleClientID {
			s.log.Error("wrong client ID", zap.String("ClientID", data.ClientID))
			s.writeError(w,
				Error{
					Code:    HttpErrorWrongCredentials,
					Message: "wrong client ID",
				}, http.StatusUnauthorized)
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
			s.log.Error("couldn't parse claims", zap.Error(err))
			s.writeError(w,
				Error{
					Code:    HttpErrorInternal,
					Message: "couldn't parse claims",
				}, http.StatusInternalServerError)
			return
		}

		// Verify the issuer.
		if issuer, ok := claims["iss"]; !ok || (issuer != "accounts.google.com" && issuer != "https://accounts.google.com") {
			s.log.Error("invalid issuer", zap.String("issuer", issuer.(string)))
			s.writeError(w,
				Error{
					Code:    HttpErrorWrongCredentials,
					Message: "invalid issuer",
				}, http.StatusUnauthorized)
			return
		}

		// Verify the audience.
		if audience, ok := claims["aud"]; !ok || audience != googleClientID {
			s.log.Error("invalid audience", zap.String("audience", audience.(string)))
			s.writeError(w,
				Error{
					Code:    HttpErrorWrongCredentials,
					Message: "invalid issuer",
				}, http.StatusUnauthorized)
			return
		}

		// Verify the expiration time.
		var expires interface{}
		var ok bool
		if expires, ok = claims["exp"]; !ok {
			s.log.Error("invalid expiration time", zap.Float64("expires", expires.(float64)))
			s.writeError(w,
				Error{
					Code:    HttpErrorWrongCredentials,
					Message: "invalid expiration time",
				}, http.StatusUnauthorized)
			return
		}
		if expires.(float64) < float64(time.Now().Unix()) {
			s.log.Error("token has expired", zap.Float64("expires", expires.(float64)))
			s.writeError(w,
				Error{
					Code:    HttpErrorWrongCredentials,
					Message: "token has expired",
				}, http.StatusUnauthorized)
			return
		}

		// Check if the email is verified.
		if verified := claims["email_verified"]; verified != "true" && verified != true {
			s.log.Error("email not verified")
			s.writeError(w,
				Error{
					Code:    HttpErrorWrongCredentials,
					Message: "email not verified",
				}, http.StatusUnauthorized)
			return
		}

		// Retrieve the email address.
		email := strings.ToLower(claims["email"].(string))

		// Create an account if it doesn't exist yet.
		_, err = s.accounts.FindAccount(email)
		if err != nil {
			// Check and update stats.
			if err := s.checkFailedLogins(w, req); err != nil {
				return
			}

			// Check against the action type.
			if action != "signup" {
				s.writeError(w,
					Error{
						Code:    HttpErrorEmailInvalid,
						Message: "invalid email provided",
					}, http.StatusBadRequest)
				return
			}

			// Create a new account.
			_, err := s.accounts.NewAccount(email, "")
			if err != nil {
				s.log.Error("error querying database", zap.Error(err))
				s.writeError(w,
					Error{
						Code:    HttpErrorInternal,
						Message: "internal error",
					}, http.StatusInternalServerError)
				return
			}
		} else {
			if action == "signup" {
				s.writeError(w,
					Error{
						Code:    HttpErrorEmailUsed,
						Message: "user already exists",
					}, http.StatusBadRequest)
				return
			}
		}

		// Login successful, generate a cookie.
		t := time.Now().Add(7 * 24 * time.Hour)
		token, err := s.accounts.GenerateToken(account.CookiePrefix, email, t)
		if err != nil {
			s.log.Error("error generating token", zap.Error(err))
			s.writeError(w,
				Error{
					Code:    HttpErrorInternal,
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
		return
	}

	s.writeError(w,
		Error{
			Code:    HttpErrorBadRequest,
			Message: "provider not supported",
		}, http.StatusBadRequest)
}

// initGoogle loads the Google client ID.
func initGoogle() {
	googleClientID = os.Getenv("SATD_GOOGLE_CLIENT")
}
