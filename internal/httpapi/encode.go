package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// errorResponse is the standard error body.
type errorResponse struct {
	Error  string         `json:"error"`
	Code   string         `json:"code,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// writeJSON encodes v as JSON with the given status. It writes the
// Content-Type header and never writes a partial body on encode failure
// (the header is written first, but encoding to the buffer is done fully before
// the response body is committed).
func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "response encoding failed"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
	_, _ = w.Write([]byte("\n"))
}

// decodeJSON reads a JSON body into v and rejects unknown fields. An empty body
// is allowed only when the caller handles it (it yields a zero value).
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		// An EOF means an empty body; treat as invalid input.
		return err
	}
	// Reject trailing data after the first JSON object.
	if dec.More() {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

// mapError translates a domain error into an HTTP status and a code. It returns
// (status, errorResponse). Unknown errors yield 500.
func mapError(err error) (int, errorResponse) {
	resp := errorResponse{Error: err.Error()}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		resp.Code = "unavailable"
		resp.Error = "request canceled or timed out"
		return http.StatusServiceUnavailable, resp
	case errors.Is(err, domain.ErrInvalidInput):
		resp.Code = "invalid_input"
		if ve := (*domain.ValidationError)(nil); errors.As(err, &ve) && ve.Field != "" {
			resp.Detail = map[string]any{"field": ve.Field}
		}
		return http.StatusBadRequest, resp
	case errors.Is(err, domain.ErrCollectionNotFound), errors.Is(err, domain.ErrDocumentNotFound),
		errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrLockNotHeld):
		resp.Code = "not_found"
		return http.StatusNotFound, resp
	case errors.Is(err, domain.ErrVersionConflict):
		resp.Code = "version_conflict"
		if vce := (*domain.VersionConflictError)(nil); errors.As(err, &vce) {
			resp.Detail = map[string]any{
				"document_id": vce.DocumentID,
				"expected":    vce.Expected,
				"actual":      vce.Actual,
			}
		}
		return http.StatusConflict, resp
	case errors.Is(err, domain.ErrLockHeldByOther):
		resp.Code = "lock_conflict"
		if lce := (*domain.LockConflictError)(nil); errors.As(err, &lce) {
			resp.Detail = map[string]any{
				"document_id":    lce.DocumentID,
				"current_holder": lce.CurrentHolder,
				"expires_at":     lce.ExpiresAt,
			}
		}
		return http.StatusConflict, resp
	case errors.Is(err, domain.ErrLockExpired):
		resp.Code = "lock_expired"
		return http.StatusGone, resp
	case errors.Is(err, domain.ErrIdempotencyConflict):
		resp.Code = "idempotency_conflict"
		return http.StatusConflict, resp
	case errors.Is(err, domain.ErrBatchConflict):
		resp.Code = "batch_conflict"
		if bce := (*domain.BatchConflictError)(nil); errors.As(err, &bce) {
			fs := make([]map[string]any, 0, len(bce.Failures))
			for _, f := range bce.Failures {
				fs = append(fs, map[string]any{
					"index":       f.Index,
					"document_id": f.DocumentID,
					"reason":      f.Reason,
				})
			}
			resp.Detail = map[string]any{"failures": fs}
		}
		return http.StatusConflict, resp
	default:
		// Includes context cancellation -> 503.
		resp.Code = "internal_error"
		return http.StatusInternalServerError, resp
	}
}

// writeError writes the mapped error response.
func writeError(w http.ResponseWriter, err error) {
	status, resp := mapError(err)
	if status == http.StatusInternalServerError {
		// Keep the body generic for 500s but preserve code.
		resp.Error = "internal error"
	}
	writeJSON(w, status, resp)
}

// badRequest wraps a message as a domain ValidationError so it maps to 400.
func badRequest(msg string) error {
	return domain.NewValidationError("request", msg)
}
