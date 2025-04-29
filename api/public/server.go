package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"github.com/mike76-dev/sia-satellite/mail"
	"go.uber.org/zap"
)

const (
	// httpMaxBodySize enforces a maximum read of 1MiB from the request body.
	httpMaxBodySize = 1048576 // 1MiB.
)

// Server is the public API server.
type Server struct {
	accounts  *account.AccountManager
	authStats map[string]authenticationStats
	callStats map[string]int
	mail      mail.MailSender
	log       *zap.Logger

	router   http.Handler
	routerMu sync.RWMutex

	mu        sync.Mutex
	closeChan chan struct{}
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.routerMu.RLock()
	s.router.ServeHTTP(w, r)
	s.routerMu.RUnlock()
}

// buildHTTPRoutes sets up and returns an httprouter.Router connected to the server.
func (s *Server) buildHTTPRoutes() {
	router := httprouter.New()

	// /auth requests.
	router.GET("/auth", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		s.authHandlerGET(w, req, ps)
	})
	router.POST("/auth/login", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		s.authLoginHandlerPOST(w, req, ps)
	})
	router.POST("/auth/login/:provider", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		s.authLoginProviderHandlerPOST(w, req, ps)
	})
	router.POST("/auth/signup", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		s.authSignupHandlerPOST(w, req, ps)
	})

	s.routerMu.Lock()
	s.router = router
	s.routerMu.Unlock()
}

// NewServer returns an initialized public API server.
func NewServer(am *account.AccountManager, ms mail.MailSender, logger *zap.Logger) *Server {
	s := &Server{
		accounts:  am,
		mail:      ms,
		log:       logger,
		authStats: make(map[string]authenticationStats),
		callStats: make(map[string]int),
		closeChan: make(chan struct{}),
	}

	go s.pruneAuthStats()

	s.buildHTTPRoutes()
	return s
}

// Close shuts down the server.
func (s *Server) Close() {
	s.closeChan <- struct{}{}
}

// writeError writes an error to the API caller.
func (s *Server) writeError(w http.ResponseWriter, err Error, code int) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(code)
	encodingErr := json.NewEncoder(w).Encode(err)
	if _, isJsonErr := encodingErr.(*json.SyntaxError); isJsonErr {
		s.log.Error("failed to encode API error response", zap.Error(encodingErr))
	}
}

// writeJSON writes the object to the ResponseWriter. If the encoding fails, an
// error is written instead. The Content-Type of the response header is set
// accordingly.
func (s *Server) writeJSON(w http.ResponseWriter, obj any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	err := json.NewEncoder(w).Encode(obj)
	if _, isJsonErr := err.(*json.SyntaxError); isJsonErr {
		s.log.Error("failed to encode API response", zap.Error(err))
	}
}

// writeSuccess writes the HTTP header with status 204 No Content to the
// ResponseWriter. WriteSuccess should only be used to indicate that the
// requested action succeeded AND there is no data to return.
func (s *Server) writeSuccess(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// prepareDecoder is a helper function that returns an initialized
// json.Decoder.
func (s *Server) prepareDecoder(w http.ResponseWriter, r *http.Request) (*json.Decoder, error) {
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
func (s *Server) handleDecodeError(err error) (Error, int) {
	if err == nil {
		return Error{}, http.StatusOK
	}
	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError

	switch {
	// Catch any syntax errors in the JSON.
	case errors.As(err, &syntaxError):
		return Error{
			Code:    httpErrorBadRequest,
			Message: "wrong request body format",
		}, http.StatusBadRequest

	// Catch a potential io.ErrUnexpectedEOF error in the JSON.
	case errors.Is(err, io.ErrUnexpectedEOF):
		return Error{
			Code:    httpErrorBadRequest,
			Message: "wrong request body format",
		}, http.StatusBadRequest

	// Catch any type errors.
	case errors.As(err, &unmarshalTypeError):
		return Error{
			Code:    httpErrorBadRequest,
			Message: "request body contains an invalid value",
		}, http.StatusBadRequest

	// Catch the error caused by extra unexpected fields in the request
	// body.
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return Error{
			Code:    httpErrorBadRequest,
			Message: "request body contains an unknown field",
		}, http.StatusBadRequest

	// An io.EOF error is returned by Decode() if the request body is
	// empty.
	case errors.Is(err, io.EOF):
		return Error{
			Code:    httpErrorBadRequest,
			Message: "request body is empty",
		}, http.StatusBadRequest

	// Catch the error caused by the request body being too large.
	case err.Error() == "http: request body too large":
		return Error{
			Code:    httpErrorBadRequest,
			Message: "request body too large",
		}, http.StatusRequestEntityTooLarge

	// Otherwise send a 500 Internal Server Error response.
	default:
		s.log.Error("failed to decode JSON", zap.Error(err))
		return Error{
			Code:    httpErrorInternal,
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
		return strings.TrimPrefix(v, name+"=")
	}
	return ""
}
