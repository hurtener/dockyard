// Package handlers implements the backend-tools-only example's tool
// handlers over a small, in-process bookmarks catalog.
//
// The Catalog type is the integration seam — replace its body with a real
// store (sqlite, Postgres, an HTTP API client) and the typed contract is
// unchanged. The example deliberately keeps the storage in-memory so the
// server is offline-friendly and the demo is reproducible.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/hurtener/dockyard/examples/backend-tools-only/internal/contracts"
)

// Catalog is the bookmarks store backing this example's handlers. It is
// safe for concurrent use (every handler call goes through one mutex).
//
// Replace the body of Catalog with a real backing store (a Dockyard
// runtime/store.Store driver, a Postgres client, an HTTP API) and the
// handler surface is unchanged — the typed contracts are the integration
// surface.
type Catalog struct {
	mu        sync.Mutex
	bookmarks map[string]contracts.Bookmark
	nextID    func() string
}

// NewCatalog seeds a Catalog with a small starter set so an inspector run
// shows something meaningful on first invocation. The starter set is
// hand-chosen to demonstrate the tag filter and the search substring.
func NewCatalog() *Catalog {
	c := &Catalog{
		bookmarks: map[string]contracts.Bookmark{},
		nextID:    randID,
	}
	seed := []contracts.Bookmark{
		{
			Title: "Go: Effective Go",
			URL:   "https://go.dev/doc/effective_go",
			Notes: "Canonical Go style guide.",
			Tags:  []string{"go", "docs"},
		},
		{
			Title: "MCP — Model Context Protocol",
			URL:   "https://modelcontextprotocol.io",
			Notes: "The protocol Dockyard implements server-side.",
			Tags:  []string{"mcp", "docs"},
		},
		{
			Title: "Dockyard — Go-native MCP framework",
			URL:   "https://github.com/hurtener/dockyard",
			Notes: "This example ships in examples/backend-tools-only.",
			Tags:  []string{"dockyard", "mcp"},
		},
	}
	for _, b := range seed {
		b.ID = c.nextID()
		c.bookmarks[b.ID] = b
	}
	return c
}

// ListBookmarks is the list_bookmarks handler. An empty Tag returns every
// entry, sorted by Title; a non-empty Tag filters case-insensitively.
func (c *Catalog) ListBookmarks(_ context.Context, in contracts.ListBookmarksInput) (contracts.ListBookmarksOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wantTag := strings.ToLower(strings.TrimSpace(in.Tag))
	var out []contracts.Bookmark
	for _, b := range c.bookmarks {
		if wantTag != "" && !hasTag(b.Tags, wantTag) {
			continue
		}
		out = append(out, b)
	}
	sortBookmarks(out)
	return contracts.ListBookmarksOutput{Bookmarks: out, Total: len(out)}, nil
}

// AddBookmark is the add_bookmark handler. Title + URL are required.
func (c *Catalog) AddBookmark(_ context.Context, in contracts.AddBookmarkInput) (contracts.AddBookmarkOutput, error) {
	title := strings.TrimSpace(in.Title)
	url := strings.TrimSpace(in.URL)
	if title == "" {
		return contracts.AddBookmarkOutput{}, errors.New("add_bookmark: title is required")
	}
	if url == "" {
		return contracts.AddBookmarkOutput{}, errors.New("add_bookmark: url is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := contracts.Bookmark{
		ID:    c.nextID(),
		Title: title,
		URL:   url,
		Notes: strings.TrimSpace(in.Notes),
		Tags:  normaliseTags(in.Tags),
	}
	c.bookmarks[b.ID] = b
	return contracts.AddBookmarkOutput{Bookmark: b}, nil
}

// SearchBookmarks is the search_bookmarks handler. The match is a
// case-insensitive substring across Title, URL, and Notes.
func (c *Catalog) SearchBookmarks(_ context.Context, in contracts.SearchBookmarksInput) (contracts.SearchBookmarksOutput, error) {
	query := strings.ToLower(strings.TrimSpace(in.Query))
	if query == "" {
		return contracts.SearchBookmarksOutput{Bookmarks: []contracts.Bookmark{}, Total: 0}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []contracts.Bookmark
	for _, b := range c.bookmarks {
		if matches(b, query) {
			out = append(out, b)
		}
	}
	sortBookmarks(out)
	return contracts.SearchBookmarksOutput{Bookmarks: out, Total: len(out)}, nil
}

// --- helpers ----------------------------------------------------------------

func matches(b contracts.Bookmark, q string) bool {
	if strings.Contains(strings.ToLower(b.Title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(b.URL), q) {
		return true
	}
	if strings.Contains(strings.ToLower(b.Notes), q) {
		return true
	}
	return false
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

func normaliseTags(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, t := range in {
		tt := strings.TrimSpace(t)
		if tt == "" {
			continue
		}
		key := strings.ToLower(tt)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tt)
	}
	return out
}

func sortBookmarks(b []contracts.Bookmark) {
	sort.Slice(b, func(i, j int) bool {
		return strings.ToLower(b[i].Title) < strings.ToLower(b[j].Title)
	})
}

// randID returns a short, crypto-random hex token used as a Bookmark ID.
// The catalog never reuses an ID; a 64-bit token is more than enough for
// the demo and keeps the on-the-wire shape compact.
func randID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Pathological — rand.Read on POSIX never fails. If it does, the
		// demo is unrecoverable; surface a constant so the error is
		// visible rather than crashing the server.
		return "id-rand-failed"
	}
	return hex.EncodeToString(raw[:])
}
