package externalwire

// Meta exercises encoding/json field-name semantics across a package boundary.
type Meta struct {
	Dash     string `json:"-,"`
	Fallback string `json:"bad©name,omitempty"`
	Ignored  string `json:"-"`
}
