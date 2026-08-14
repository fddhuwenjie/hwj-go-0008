package httpapi

import (
	"net/http"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

// lockBody is the JSON body for acquire and renew requests. ttl_seconds is
// optional and defaults to the service default when <= 0 or absent.
type lockBody struct {
	Holder         string `json:"holder"`
	TTLSeconds     int    `json:"ttl_seconds,omitempty"`
	Actor          string `json:"actor,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// ttl parses and normalizes a ttl_seconds value into a Duration.
func (b lockBody) ttl() time.Duration {
	if b.TTLSeconds <= 0 {
		return 0 // service applies the default
	}
	return time.Duration(b.TTLSeconds) * time.Second
}

// handleAcquireLock: POST /collections/{collectionID}/documents/{documentID}/lock
func (s *Server) handleAcquireLock(w http.ResponseWriter, r *http.Request) {
	var body lockBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, badRequest("invalid JSON body: "+err.Error()))
		return
	}
	req := service.AcquireLockRequest{
		CollectionID:   collectionID(r),
		DocumentID:     documentID(r),
		Holder:         body.Holder,
		TTL:            body.ttl(),
		Actor:          firstNonEmpty(body.Actor, actorFromRequest(r)),
		IdempotencyKey: firstNonEmpty(body.IdempotencyKey, idempotencyKey(r)),
	}
	lock, err := s.svc.AcquireLock(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lock)
}

// handleRenewLock: PUT /collections/{collectionID}/documents/{documentID}/lock
func (s *Server) handleRenewLock(w http.ResponseWriter, r *http.Request) {
	var body lockBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, badRequest("invalid JSON body: "+err.Error()))
		return
	}
	req := service.RenewLockRequest{
		CollectionID:   collectionID(r),
		DocumentID:     documentID(r),
		Holder:         body.Holder,
		TTL:            body.ttl(),
		Actor:          firstNonEmpty(body.Actor, actorFromRequest(r)),
		IdempotencyKey: firstNonEmpty(body.IdempotencyKey, idempotencyKey(r)),
	}
	lock, err := s.svc.RenewLock(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lock)
}

// handleReleaseLock: DELETE /collections/{collectionID}/documents/{documentID}/lock
func (s *Server) handleReleaseLock(w http.ResponseWriter, r *http.Request) {
	// DELETE bodies are allowed but often empty; accept holder from query or body.
	var body lockBody
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, badRequest("invalid JSON body: "+err.Error()))
			return
		}
	}
	if body.Holder == "" {
		body.Holder = r.URL.Query().Get("holder")
	}
	req := service.ReleaseLockRequest{
		CollectionID:   collectionID(r),
		DocumentID:     documentID(r),
		Holder:         body.Holder,
		Actor:          firstNonEmpty(body.Actor, actorFromRequest(r)),
		IdempotencyKey: firstNonEmpty(body.IdempotencyKey, idempotencyKey(r)),
	}
	if err := s.svc.ReleaseLock(r.Context(), req); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reapResponse is the body for POST /locks/reap.
type reapResponse struct {
	Reaped []string `json:"reaped"`
}

// handleReapLocks: POST /locks/reap
func (s *Server) handleReapLocks(w http.ResponseWriter, r *http.Request) {
	reaped, err := s.svc.ReapExpiredLocks(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if reaped == nil {
		reaped = []string{}
	}
	writeJSON(w, http.StatusOK, reapResponse{Reaped: reaped})
}
