package service

import (
	"context"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
)

// appendAudit records an audit event. The caller must hold s.mu (write). It
// assigns the next sequence number, making the stream totally ordered.
func (s *Service) appendAudit(e domain.AuditEvent) {
	s.auditSeq++
	e.Sequence = s.auditSeq
	if e.Timestamp.IsZero() {
		e.Timestamp = s.clock.Now()
	}
	s.audit = append(s.audit, e)
}

// ListAuditEvents returns audit events matching the filter, ordered by sequence
// ascending. A non-positive Limit returns all matches.
func (s *Service) ListAuditEvents(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditEvent, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.AuditEvent, 0)
	events := s.audit
	if filter.Limit > 0 && len(events) > filter.Limit {
		events = events[len(events)-filter.Limit:]
	}
	for _, e := range events {
		if filter.CollectionID != "" && e.CollectionID != filter.CollectionID {
			continue
		}
		if filter.DocumentID != "" && e.DocumentID != filter.DocumentID {
			continue
		}
		if filter.Actor != "" && e.Actor != filter.Actor {
			continue
		}
		if filter.Type != "" && e.Type != filter.Type {
			continue
		}
		if filter.MinSequence > 0 && e.Sequence < filter.MinSequence {
			continue
		}
		out = append(out, e.Clone())
	}

	return out, nil
}
