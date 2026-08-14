package httpapi

import (
	"net/http"

	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

// handleRegisterCollection: POST /collections
func (s *Server) handleRegisterCollection(w http.ResponseWriter, r *http.Request) {
	var req service.RegisterCollectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("invalid JSON body: "+err.Error()))
		return
	}
	req.Actor = firstNonEmpty(req.Actor, actorFromRequest(r))
	req.IdempotencyKey = idempotencyKey(r)

	col, err := s.svc.RegisterCollection(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, col)
}

// handleListCollections: GET /collections
func (s *Server) handleListCollections(w http.ResponseWriter, r *http.Request) {
	cols, err := s.svc.ListCollections(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": cols})
}

// handleGetCollection: GET /collections/{collectionID}
func (s *Server) handleGetCollection(w http.ResponseWriter, r *http.Request) {
	col, err := s.svc.GetCollection(r.Context(), collectionID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, col)
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
