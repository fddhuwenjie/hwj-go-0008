package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

// handleCreateDocument: POST /collections/{collectionID}/documents
func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	var req service.CreateDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("invalid JSON body: "+err.Error()))
		return
	}
	req.CollectionID = collectionID(r)
	req.Actor = firstNonEmpty(req.Actor, actorFromRequest(r))
	req.IdempotencyKey = idempotencyKey(r)

	doc, err := s.svc.CreateDocument(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// handleGetDocument: GET /collections/{collectionID}/documents/{documentID}
func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := s.svc.GetDocument(r.Context(), collectionID(r), documentID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleUpdateDocument: PUT /collections/{collectionID}/documents/{documentID}
func (s *Server) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("invalid JSON body: "+err.Error()))
		return
	}
	req.CollectionID = collectionID(r)
	req.DocumentID = documentID(r)
	req.Actor = firstNonEmpty(req.Actor, actorFromRequest(r))
	req.IdempotencyKey = idempotencyKey(r)

	doc, err := s.svc.UpdateDocument(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleBatchUpdate: POST /collections/{collectionID}/documents/batch
func (s *Server) handleBatchUpdate(w http.ResponseWriter, r *http.Request) {
	var req service.BatchUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, badRequest("invalid JSON body: "+err.Error()))
		return
	}
	req.CollectionID = collectionID(r)
	req.Actor = firstNonEmpty(req.Actor, actorFromRequest(r))
	req.IdempotencyKey = idempotencyKey(r)

	res, err := s.svc.BatchUpdateDocuments(r.Context(), req)
	if err != nil {
		// On batch conflict, the result body is still meaningful; write it
		// alongside the error status.
		if res != nil {
			status, erresp := mapError(err)
			erresp.Detail = mergeDetail(erresp.Detail, map[string]any{
				"applied":  res.Applied,
				"failures": res.Failures,
			})
			writeJSON(w, status, erresp)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleListDocuments: GET /collections/{collectionID}/documents?tags=a,b&limit=10
func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var filter service.ListFilter
	if tagParam := q.Get("tags"); tagParam != "" {
		filter.Tags = splitCSV(tagParam)
	}
	if lim := q.Get("limit"); lim != "" {
		n, err := strconv.Atoi(lim)
		if err != nil || n < 0 {
			writeError(w, badRequest("invalid query parameter: limit must be a non-negative integer"))
			return
		}
		filter.Limit = n
	}
	docs, err := s.svc.ListDocuments(r.Context(), collectionID(r), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs})
}

// handleDocumentHistory: GET /collections/{collectionID}/documents/{documentID}/history
func (s *Server) handleDocumentHistory(w http.ResponseWriter, r *http.Request) {
	revs, err := s.svc.GetDocumentHistory(r.Context(), collectionID(r), documentID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

// splitCSV splits a comma-separated string, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeDetail merges src into dst (dst values are overwritten). Returns dst.
func mergeDetail(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
