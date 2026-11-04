// Package httpapi serves Shiyi's HTTP API.
//
// The package owns the wire contract of POST /messages: the bearer token
// check, the request body limit, the total request deadline and the mapping
// from an internal failure to one of the frozen error codes. It also serves
// GET /healthz and mounts the single page web interface at GET /.
//
// Nothing here interprets the submitted content: the original text is handed
// to the judge unchanged and is never logged.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"shiyi/internal/answer"
	"shiyi/internal/api"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/store"
)

// DefaultMaxBodyBytes is the request body limit used when the configuration
// does not set one.
const DefaultMaxBodyBytes int64 = 1 << 20

// DefaultRequestTimeout caps a single request when the configuration does not
// set one.
const DefaultRequestTimeout = 60 * time.Second

// Deps are the collaborators a Server needs.
//
// The struct is part of the frozen contract with the CLI: the field set does
// not change. The model clients themselves are built by the caller and are
// already wrapped by the judge and the answerer, so the API key never reaches
// this package.
type Deps struct {
	// Config is the loaded configuration. Only the fields that describe the
	// wire behaviour are used: AuthTokenEnv, MaxBodyBytes and
	// Budget.RequestTimeout.
	Config config.Config

	// Judge decides whether an input is a record, a query or neither.
	Judge classify.Judge

	// Store appends records to the sharded JSONL files.
	Store store.Store

	// Answerer turns a query into a cited answer.
	Answerer answer.Answerer

	// Logger receives one line per request. A nil logger means the default
	// logger of the standard library is used.
	Logger *slog.Logger
}

// Server serves the Shiyi HTTP API.
type Server struct {
	cfg      config.Config
	judge    classify.Judge
	store    store.Store
	answerer answer.Answerer
	logger   *slog.Logger

	maxBodyBytes   int64
	requestTimeout time.Duration

	// mu guards web, the handler mounted at GET /. It is read on every
	// request for the root path.
	mu  sync.RWMutex
	web http.Handler

	handler http.Handler
}

// New builds a server and checks that the process can actually serve.
//
// The checks are the ones that would otherwise show up as a confusing
// failure on the first request: ripgrep has to be executable, the data
// directory has to be writable and the model endpoints have to be
// configured. The API key is deliberately not checked here; reading it is
// the job of the model client and of the configuration loader.
func New(d Deps) (*Server, error) {
	if d.Judge == nil {
		return nil, errors.New("httpapi: judge is required")
	}
	if d.Store == nil {
		return nil, errors.New("httpapi: store is required")
	}
	if d.Answerer == nil {
		return nil, errors.New("httpapi: answerer is required")
	}

	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if err := validateConfig(d.Config); err != nil {
		return nil, err
	}

	maxBodyBytes := d.Config.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultMaxBodyBytes
	}
	requestTimeout := d.Config.Budget.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = DefaultRequestTimeout
	}

	s := &Server{
		cfg:            d.Config,
		judge:          d.Judge,
		store:          d.Store,
		answerer:       d.Answerer,
		logger:         logger,
		maxBodyBytes:   maxBodyBytes,
		requestTimeout: requestTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /messages", s.handleMessages)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	// Everything else belongs to the web interface when one is mounted. The
	// web handler owns its whole path space (the page and its assets), so it
	// is installed as the catch-all rather than at the root path alone.
	mux.HandleFunc("/", s.handleWebOrNotFound)

	s.handler = s.withRequestLog(mux)
	return s, nil
}

// Handler returns the HTTP handler of the server.
//
// It serves POST /messages and GET /healthz; every other path answers with
// 404 and the error shape of the API until a web interface is mounted.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// HandlerWithWeb mounts the web interface and returns the handler of the
// server.
//
// The web interface lives in its own package, so it is installed here instead
// of being a dependency of the server. Mounting it is optional: the server
// answers with 404 until a web handler is mounted.
func (s *Server) HandlerWithWeb(web http.Handler) http.Handler {
	s.mu.Lock()
	s.web = web
	s.mu.Unlock()
	return s.handler
}

// handleHealthz reports that the process is alive.
//
// It does not check the model: the availability of the service follows the
// availability of the model, but a liveness probe should not fail just
// because the provider is slow. It is never authenticated either.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleWebOrNotFound serves the single page web interface, and answers with
// 404 when no interface is mounted.
//
// The web handler owns its whole path space: it serves the page at / and its
// assets (the stylesheet and the script) alongside it. A method the interface
// does not accept is rejected by the interface itself.
func (s *Server) handleWebOrNotFound(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	web := s.web
	s.mu.RUnlock()

	if web == nil {
		s.writeNotFound(w)
		return
	}
	web.ServeHTTP(w, r)
}

// writeNotFound answers with 404 and the error shape of the API.
//
// The status code and the error code disagree here on purpose: the error
// code table of the API describes failures of a served endpoint, and there
// is no code for "this endpoint does not exist". invalid_request says that
// the client asked for something the server cannot serve, which is what a
// wrong path or a wrong method amounts to.
func (s *Server) writeNotFound(w http.ResponseWriter) {
	s.writeJSON(w, http.StatusNotFound, apiErrorResponse(api.CodeInvalidRequest, "没有这个接口"))
}
