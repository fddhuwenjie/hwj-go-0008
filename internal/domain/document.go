package domain

import "time"

// Document is the current state of a versioned document. Content and Tags are
// the only mutable-shaped fields (a string is immutable in Go, but Tags is a
// slice); Clone therefore copies the tags slice so callers cannot mutate the
// service's internal storage.
type Document struct {
	ID           string    `json:"id"`
	CollectionID string    `json:"collection_id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	Tags         []string  `json:"tags"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Clone returns a deep copy of the document, including a copy of the tags slice.
// Callers receive clones so that mutations to a returned document never leak
// back into the service's internal state.
func (d Document) Clone() Document {
	out := Document{
		ID:           d.ID,
		CollectionID: d.CollectionID,
		Title:        d.Title,
		Content:      d.Content,
		Version:      d.Version,
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
	}
	if d.Tags != nil {
		tags := make([]string, len(d.Tags))
		copy(tags, d.Tags)
		out.Tags = tags
	}
	return out
}

// Revision is an immutable historical snapshot of a document at a given
// version. The service appends a revision on creation and on every update, so
// the revision log is the complete version history of a document.
type Revision struct {
	Version    int       `json:"version"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Tags       []string  `json:"tags"`
	Actor      string    `json:"actor,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	ModifiedAt time.Time `json:"modified_at"`
}

// Clone returns a deep copy of the revision.
func (r Revision) Clone() Revision {
	out := Revision{
		Version:    r.Version,
		Title:      r.Title,
		Content:    r.Content,
		Actor:      r.Actor,
		Reason:     r.Reason,
		ModifiedAt: r.ModifiedAt,
	}
	if r.Tags != nil {
		tags := make([]string, len(r.Tags))
		copy(tags, r.Tags)
		out.Tags = tags
	}
	return out
}

// CloneRevisions returns a deep copy of a revision slice.
func CloneRevisions(in []Revision) []Revision {
	if in == nil {
		return nil
	}
	out := make([]Revision, len(in))
	for i := range in {
		out[i] = in[i].Clone()
	}
	return out
}
