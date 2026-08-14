package domain

import "time"

// Collection is a registered grouping of documents. Collections are immutable
// after registration (only metadata is exposed).
type Collection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Clone returns a deep copy of the collection. Because Collection has no slice
// or map fields, this is effectively a value copy, but the method is provided
// for symmetry with other entities and to future-proof against added fields.
func (c Collection) Clone() Collection {
	return Collection{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
	}
}
