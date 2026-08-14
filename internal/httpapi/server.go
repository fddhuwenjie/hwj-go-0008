// Package httpapi exposes the document versioning and edit-lock service over a
// JSON HTTP API.
//
// Routes (all request and response bodies are application/json):
//
//	POST   /collections                                          register a collection
//	GET    /collections                                          list collections
//	GET    /collections/{collectionID}                          get a collection
//	POST   /collections/{collectionID}/documents                create a document
//	GET    /collections/{collectionID}/documents                list documents (tag filter)
//	POST   /collections/{collectionID}/documents/batch          atomic batch update
//	GET    /collections/{collectionID}/documents/{documentID}   get a document
//	PUT    /collections/{collectionID}/documents/{documentID}   update a document (expected version)
//	GET    /collections/{collectionID}/documents/{documentID}/history   version history
//	POST   /collections/{collectionID}/documents/{documentID}/lock      acquire lock
//	PUT    /collections/{collectionID}/documents/{documentID}/lock      renew lock
//	DELETE /collections/{collectionID}/documents/{documentID}/lock      release lock
//	POST   /locks/reap                                           reap expired locks
//	GET    /audit                                                query audit stream
//	GET    /healthz                                              liveness
//
// Write operations accept an optional "Idempotency-Key" header; a repeated
// request with the same key and an identical body replays the original result.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

// Server wires the HTTP routes to a service.
type Server struct {
	svc *service.Service
	mux *http.ServeMux
}

// New builds a Server backed by svc and registers all routes.
func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// routes registers handlers on the mux using Go 1.22 method+pattern routing.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)

	s.mux.HandleFunc("POST /collections", s.handleRegisterCollection)
	s.mux.HandleFunc("GET /collections", s.handleListCollections)
	s.mux.HandleFunc("GET /collections/{collectionID}", s.handleGetCollection)

	s.mux.HandleFunc("POST /collections/{collectionID}/documents", s.handleCreateDocument)
	s.mux.HandleFunc("GET /collections/{collectionID}/documents", s.handleListDocuments)
	s.mux.HandleFunc("POST /collections/{collectionID}/documents/batch", s.handleBatchUpdate)
	s.mux.HandleFunc("GET /collections/{collectionID}/documents/{documentID}", s.handleGetDocument)
	s.mux.HandleFunc("PUT /collections/{collectionID}/documents/{documentID}", s.handleUpdateDocument)
	s.mux.HandleFunc("GET /collections/{collectionID}/documents/{documentID}/history", s.handleDocumentHistory)

	s.mux.HandleFunc("POST /collections/{collectionID}/documents/{documentID}/lock", s.handleAcquireLock)
	s.mux.HandleFunc("PUT /collections/{collectionID}/documents/{documentID}/lock", s.handleRenewLock)
	s.mux.HandleFunc("DELETE /collections/{collectionID}/documents/{documentID}/lock", s.handleReleaseLock)

	s.mux.HandleFunc("POST /locks/reap", s.handleReapLocks)
	s.mux.HandleFunc("GET /audit", s.handleAudit)
}

// Run starts an HTTP server on addr with graceful shutdown on ctx cancellation.
// It blocks until the server stops.
func (s *Server) Run(ctx context.Context, addr string) error {
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

// healthResponse is the body for /healthz.
type healthResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// idempotencyKey reads the optional Idempotency-Key header.
func idempotencyKey(r *http.Request) string {
	return r.Header.Get("Idempotency-Key")
}

// collectionID returns the path value for {collectionID}.
func collectionID(r *http.Request) string {
	return r.PathValue("collectionID")
}

// documentID returns the path value for {documentID}.
func documentID(r *http.Request) string {
	return r.PathValue("documentID")
}

// actorFromRequest returns the request actor: the "X-Actor" header if present,
// otherwise the empty string. This keeps the actor out of request bodies for
// operations where it is purely attribution.
func actorFromRequest(r *http.Request) string {
	return r.Header.Get("X-Actor")
}

// ensure the service import is referenced for docs even if unused at compile.
var _ = service.New
