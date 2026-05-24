// Package contracts holds the tool input/output contracts for the
// backend-tools-only example (Phase 28).
//
// Contract-first (Dockyard P1, RFC §6): the Go structs are the source of
// truth; `dockyard generate` produces the JSON Schema + TypeScript
// alongside them. Never hand-edit a generated artifact.
package contracts

// Bookmark is one entry in the in-memory catalog.
type Bookmark struct {
	// ID is the catalog's stable identifier (assigned on add).
	ID string `json:"id"`
	// Title is the human-facing label.
	Title string `json:"title"`
	// URL is the bookmarked address.
	URL string `json:"url"`
	// Notes is an optional free-text annotation.
	Notes string `json:"notes,omitempty"`
	// Tags is an optional list of free-form tags (case-insensitive).
	Tags []string `json:"tags,omitempty"`
}

// --- list_bookmarks ---------------------------------------------------------

// ListBookmarksInput filters the list. An empty Tag returns every entry.
type ListBookmarksInput struct {
	// Tag, when non-empty, filters to bookmarks that carry it
	// (case-insensitive match).
	Tag string `json:"tag,omitempty"`
}

// ListBookmarksOutput is the list result.
type ListBookmarksOutput struct {
	// Bookmarks is the matched set, sorted by Title.
	Bookmarks []Bookmark `json:"bookmarks"`
	// Total is the number of matched entries.
	Total int `json:"total"`
}

// --- add_bookmark -----------------------------------------------------------

// AddBookmarkInput is the contract for a single add. The handler assigns
// the ID; a caller-supplied ID is ignored.
type AddBookmarkInput struct {
	// Title is required; an empty Title is rejected.
	Title string `json:"title"`
	// URL is required; an empty URL is rejected.
	URL string `json:"url"`
	// Notes is optional.
	Notes string `json:"notes,omitempty"`
	// Tags is optional.
	Tags []string `json:"tags,omitempty"`
}

// AddBookmarkOutput carries the stored record (with the assigned ID).
type AddBookmarkOutput struct {
	// Bookmark is the stored record.
	Bookmark Bookmark `json:"bookmark"`
}

// --- search_bookmarks -------------------------------------------------------

// SearchBookmarksInput is the substring-search query. An empty Query
// matches nothing — by design, so an agent calling search with no query
// gets an empty result rather than the whole catalog.
type SearchBookmarksInput struct {
	// Query is the case-insensitive substring searched across Title, URL,
	// and Notes.
	Query string `json:"query"`
}

// SearchBookmarksOutput carries the matched entries.
type SearchBookmarksOutput struct {
	// Bookmarks is the matched set, sorted by Title.
	Bookmarks []Bookmark `json:"bookmarks"`
	// Total is the number of matched entries.
	Total int `json:"total"`
}
