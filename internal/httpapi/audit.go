package httpapi

import (
	"net/http"
	"strconv"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// handleAudit: GET /audit?collection_id=...&document_id=...&actor=...&type=...&min_sequence=...&limit=...
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var filter domain.AuditFilter
	filter.CollectionID = q.Get("collection_id")
	filter.DocumentID = q.Get("document_id")
	filter.Actor = q.Get("actor")
	filter.Type = domain.AuditEventType(q.Get("type"))
	if ms := q.Get("min_sequence"); ms != "" {
		n, err := strconv.ParseInt(ms, 10, 64)
		if err != nil || n < 0 {
			writeError(w, badRequest("min_sequence must be a non-negative integer"))
			return
		}
		filter.MinSequence = n
	}
	if lim := q.Get("limit"); lim != "" {
		n, err := strconv.Atoi(lim)
		if err != nil || n < 0 {
			writeError(w, badRequest("limit must be a non-negative integer"))
			return
		}
		filter.Limit = n
	}
	events, err := s.svc.ListAuditEvents(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}
