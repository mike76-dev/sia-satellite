package portal

import (
	"encoding/json"
	"errors"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/golang/gddo/httputil/header"
	"github.com/julienschmidt/httprouter"
)

const (
	// httpContentTypeError is returned when the header content type is not
	// "application/json".
	httpContentTypeError = "Content-Type header is not application/json"

	// httpMaxBodySize enforces a maximum read of 1MiB from the request body.
	httpMaxBodySize = 1048576 // 1MiB.
)

// portalAPI implements the http.Handler interface.
type portalAPI struct {
	router   http.Handler
	routerMu sync.RWMutex
}

// ServeHTTP implements the http.Handler interface.
func (api *portalAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.routerMu.RLock()
	api.router.ServeHTTP(w, r)
	api.routerMu.RUnlock()
}

// buildHttpRoutes sets up and returns an * httprouter.Router connected
// to the given api.
func (api *portalAPI) buildHTTPRoutes() {
	router := httprouter.New()

	router.POST("/authme", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		authMeHandlerPOST(w, req, ps)
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
	api := &portalAPI{}
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
// the event of an error. Only the Message field is required. More fields may
// be added to this struct in the future for better error reporting.
type Error struct {
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
	if r.Header.Get("Content-Type") != "" {
		value, _ := header.ParseValueAndParams(r.Header, "Content-Type")
		if value != "application/json" {
			return Error{httpContentTypeError}
		}
	}
	return Error{}
}

// prepareDecoder is a helper function that returns an initialized
// json.Decoder.
func prepareDecoder(w http.ResponseWriter, r *http.Request) (*json.Decoder, error) {
	// Check the response header first.
	if err := checkHeader(w, r); err.Message != "" {
		writeError(w, Error{httpContentTypeError}, http.StatusUnsupportedMediaType)
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

// handleDecodeError is a helper function that parses the json.Decoder
// errors and returns an error message and a response code.
func handleDecodeError(w http.ResponseWriter, err error) (Error, int) {
	if err == nil {
		return Error{}, http.StatusOK
	}
	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError

	switch {
		// Catch any syntax errors in the JSON.
		case errors.As(err, &syntaxError):
			return Error{"wrong request body format"}, http.StatusBadRequest

		// Catch a potential io.ErrUnexpectedEOF error in the JSON.
		case errors.Is(err, io.ErrUnexpectedEOF):
			return Error{"wrong request body format"}, http.StatusBadRequest

		// Catch any type errors.
		case errors.As(err, &unmarshalTypeError):
			return Error{"request body contains an invalid value"}, http.StatusBadRequest

		// Catch the error caused by extra unexpected fields in the request
		// body.
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return Error{"request body contains an unknown field"}, http.StatusBadRequest

		// An io.EOF error is returned by Decode() if the request body is
		// empty.
		case errors.Is(err, io.EOF):
			return Error{"request body is empty"}, http.StatusBadRequest

		// Catch the error caused by the request body being too large.
		case err.Error() == "http: request body too large":
			return Error{"request body too large"}, http.StatusRequestEntityTooLarge

		// Otherwise send a 500 Internal Server Error response.
		default:
			return Error{"internal error"}, http.StatusInternalServerError
	}
}
