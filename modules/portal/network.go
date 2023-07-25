package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/julienschmidt/httprouter"
)

const (
	// httpContentTypeError is returned when the header content type is not
	// "application/json".
	httpContentTypeError = "Content-Type header is not application/json"

	// httpMaxBodySize enforces a maximum read of 1MiB from the request body.
	httpMaxBodySize = 1048576 // 1MiB.
)

// Error codes provided in an HTTP response.
const (
	httpErrorNone                 = 0
	httpErrorInternal             = 1
	httpErrorBadRequest           = 2

	httpErrorEmailInvalid         = 10
	httpErrorEmailUsed            = 11
	httpErrorEmailTooLong         = 12

	httpErrorPasswordTooShort     = 20
	httpErrorPasswordTooLong      = 21
	httpErrorPasswordNotCompliant = 22

	httpErrorWrongCredentials     = 30
	httpErrorTooManyRequests      = 31

	httpErrorTokenInvalid         = 40
	httpErrorTokenExpired         = 41

	httpErrorNotFound             = 50
)

// portalAPI implements the http.Handler interface.
type portalAPI struct {
	portal   *Portal
	router   http.Handler
	routerMu sync.RWMutex
}

// ServeHTTP implements the http.Handler interface.
func (api *portalAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.routerMu.RLock()
	api.router.ServeHTTP(w, r)
	api.routerMu.RUnlock()
}

// buildHttpRoutes sets up and returns an httprouter.Router connected
// to the given api.
func (api *portalAPI) buildHTTPRoutes() {
	router := httprouter.New()

	// /auth requests.
	router.GET("/auth", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.authHandlerGET(w, req, ps)
	})
	router.POST("/auth/login", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.loginHandlerPOST(w, req, ps)
	})
	router.POST("/auth/register", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.registerHandlerPOST(w, req, ps)
	})
	router.POST("/auth/register/resend", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.registerResendHandlerPOST(w, req, ps)
	})
	router.POST("/auth/reset", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.resetHandlerPOST(w, req, ps)
	})
	router.POST("/auth/reset/resend", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.resetResendHandlerPOST(w, req, ps)
	})
	router.POST("/auth/change", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.changeHandlerPOST(w, req, ps)
	})
	router.GET("/auth/delete", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.deleteHandlerGET(w, req, ps)
	})

	// /dashboard requests.
	router.GET("/dashboard/balance", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.balanceHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/payments", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.paymentsHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/averages", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.averagesHandlerGET(w, req, ps)
	})
	router.POST("/dashboard/hosts", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.hostsHandlerPOST(w, req, ps)
	})
	router.GET("/dashboard/seed", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.seedHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/key", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.keyHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/contracts", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.contractsHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/blockheight", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.blockHeightHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/spendings", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.spendingsHandlerGET(w, req, ps)
	})
	router.GET("/dashboard/settings", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.settingsHandlerGET(w, req, ps)
	})

	// /stripe requests.
	router.POST("/stripe/create-payment-intent", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.paymentHandlerPOST(w, req, ps)
	})
	router.POST("/stripe/webhook", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		api.webhookHandlerPOST(w, req, ps)
	})

	api.routerMu.Lock()
	api.router = router
	api.routerMu.Unlock()
	return
}

// initNetworking starts the portal server listening at the
// specified address.
func (p *Portal) initNetworking(address string) error {
	// Create the listener.
	l, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	p.listener = l

	// Initialize the portal API.
	api := &portalAPI{
		portal: p,
	}
	api.buildHTTPRoutes()

	// Start the portal API server.
	srv := &http.Server{Handler: api}
	go srv.Serve(l)
	p.log.Println("INFO: listening on", l.Addr())

	// Spin up a goroutine to stop the server on shutdown.
	go func() {
		<-p.closeChan
		srv.Shutdown(context.Background())
	}()

	return nil
}

// writeError writes an error to the API caller.
func writeError(w http.ResponseWriter, err Error, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	encodingErr := json.NewEncoder(w).Encode(err)
	if _, isJsonErr := encodingErr.(*json.SyntaxError); isJsonErr {
		log.Println("ERROR: failed to encode API error response:", encodingErr)
	}
}

// writeJSON writes the object to the ResponseWriter. If the encoding fails, an
// error is written instead. The Content-Type of the response header is set
// accordingly.
func writeJSON(w http.ResponseWriter, obj interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	err := json.NewEncoder(w).Encode(obj)
	if _, isJsonErr := err.(*json.SyntaxError); isJsonErr {
		log.Println("ERROR: failed to encode API error response:", err)
	}
}

// writeSuccess writes the HTTP header with status 204 No Content to the
// ResponseWriter. WriteSuccess should only be used to indicate that the
// requested action succeeded AND there is no data to return.
func writeSuccess(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error is a type that is encoded as JSON and returned in an API response in
// the event of an error.
type Error struct {
	// Code identifies the error and enables an easier client-side error handling.
	Code    int    `json:"code"`
	// Message describes the error in English. Typically it is set to
	// `err.Error()`. This field is required.
	Message string `json:"message"`
}

// Error implements the error interface for the Error type. It returns only the
// Message field.
func (err Error) Error() string {
	return err.Message
}

// checkHeader checks the HTTP request header for the right content type.
func checkHeader(w http.ResponseWriter, r *http.Request) Error {
	value := r.Header.Get("Content-Type")
	if value != "" && !strings.Contains(value, "application/json") {
		return Error{
			Code: httpErrorBadRequest,
			Message: httpContentTypeError,
		}
	}
	return Error{}
}

// prepareDecoder is a helper function that returns an initialized
// json.Decoder.
func prepareDecoder(w http.ResponseWriter, r *http.Request) (*json.Decoder, error) {
	// Check the response header first.
	if err := checkHeader(w, r); err.Code != httpErrorNone {
		writeError(w, err, http.StatusUnsupportedMediaType)
		return nil, errors.New(err.Message)
	}

	// Limit the request body size.
	r.Body = http.MaxBytesReader(w, r.Body, httpMaxBodySize)

	// Initialize the decoder and instruct it to not accept any undeclared
	// fields in the body JSON.
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	// Return the decoder.
	return dec, nil
}

// handleDecodeError parses the json.Decoder errors and returns an
// error message and a response code.
func (api *portalAPI) handleDecodeError(w http.ResponseWriter, err error) (Error, int) {
	if err == nil {
		return Error{}, http.StatusOK
	}
	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError

	switch {
		// Catch any syntax errors in the JSON.
		case errors.As(err, &syntaxError):
			return Error{
				Code: httpErrorBadRequest,
				Message: "wrong request body format",
			}, http.StatusBadRequest

		// Catch a potential io.ErrUnexpectedEOF error in the JSON.
		case errors.Is(err, io.ErrUnexpectedEOF):
			return Error{
				Code: httpErrorBadRequest,
				Message: "wrong request body format",
			}, http.StatusBadRequest

		// Catch any type errors.
		case errors.As(err, &unmarshalTypeError):
			return Error{
				Code: httpErrorBadRequest,
				Message: "request body contains an invalid value",
			}, http.StatusBadRequest

		// Catch the error caused by extra unexpected fields in the request
		// body.
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return Error{
				Code: httpErrorBadRequest,
				Message: "request body contains an unknown field",
			}, http.StatusBadRequest

		// An io.EOF error is returned by Decode() if the request body is
		// empty.
		case errors.Is(err, io.EOF):
			return Error{
				Code: httpErrorBadRequest,
				Message: "request body is empty",
			}, http.StatusBadRequest

		// Catch the error caused by the request body being too large.
		case err.Error() == "http: request body too large":
			return Error{
				Code: httpErrorBadRequest,
				Message: "request body too large",
			}, http.StatusRequestEntityTooLarge

		// Otherwise send a 500 Internal Server Error response.
		default:
			api.portal.log.Printf("ERROR: failed to decode JSON: %v\n", err)
			return Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError
	}
}

// getRemoteHost returns the address of the remote host.
func getRemoteHost(r *http.Request) (host string) {
	host, _, _ = net.SplitHostPort(r.RemoteAddr)
	if host == "127.0.0.1" || host == "localhost" {
		xff := r.Header.Values("X-Forwarded-For")
		if len(xff) > 0 {
			host = xff[0]
		}
	}
	return
}

// getCookie is a helper function that retrieves the cookie value.
func getCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err == nil {
		v := cookie.Value
		return strings.TrimPrefix(v, name + "=")
	}
	return ""
}
