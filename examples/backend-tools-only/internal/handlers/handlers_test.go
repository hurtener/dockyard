package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/examples/backend-tools-only/internal/contracts"
	"github.com/hurtener/dockyard/examples/backend-tools-only/internal/handlers"
)

// TestListBookmarks_All confirms a no-filter list returns the seed set,
// sorted by Title.
func TestListBookmarks_All(t *testing.T) {
	t.Parallel()
	c := handlers.NewCatalog()
	got, err := c.ListBookmarks(context.Background(), contracts.ListBookmarksInput{})
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if got.Total != 3 {
		t.Fatalf("Total = %d, want 3 seed entries", got.Total)
	}
	for i := 1; i < len(got.Bookmarks); i++ {
		if strings.ToLower(got.Bookmarks[i-1].Title) > strings.ToLower(got.Bookmarks[i].Title) {
			t.Fatalf("bookmarks not sorted by Title: %+v", got.Bookmarks)
		}
	}
}

// TestListBookmarks_TagFilter confirms case-insensitive tag filtering.
func TestListBookmarks_TagFilter(t *testing.T) {
	t.Parallel()
	c := handlers.NewCatalog()
	got, err := c.ListBookmarks(context.Background(), contracts.ListBookmarksInput{Tag: "DOCS"})
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if got.Total != 2 {
		t.Fatalf("Total = %d, want 2 entries tagged docs", got.Total)
	}
}

// TestAddBookmark_HappyPath confirms add stores the record and assigns an ID.
func TestAddBookmark_HappyPath(t *testing.T) {
	t.Parallel()
	c := handlers.NewCatalog()
	got, err := c.AddBookmark(context.Background(), contracts.AddBookmarkInput{
		Title: "New entry",
		URL:   "https://example.com",
		Tags:  []string{"example", "Example", " "}, // de-dupe + trim
	})
	if err != nil {
		t.Fatalf("AddBookmark: %v", err)
	}
	if got.Bookmark.ID == "" {
		t.Fatalf("AddBookmark returned empty ID")
	}
	if len(got.Bookmark.Tags) != 1 {
		t.Fatalf("Tags = %v, want one de-duped tag", got.Bookmark.Tags)
	}
}

// TestAddBookmark_Validation confirms Title + URL are required.
func TestAddBookmark_Validation(t *testing.T) {
	t.Parallel()
	c := handlers.NewCatalog()
	if _, err := c.AddBookmark(context.Background(), contracts.AddBookmarkInput{URL: "https://x"}); err == nil {
		t.Fatalf("expected error for missing title")
	}
	if _, err := c.AddBookmark(context.Background(), contracts.AddBookmarkInput{Title: "x"}); err == nil {
		t.Fatalf("expected error for missing URL")
	}
}

// TestSearchBookmarks_Match confirms the substring search matches across
// Title, URL, and Notes.
func TestSearchBookmarks_Match(t *testing.T) {
	t.Parallel()
	c := handlers.NewCatalog()
	got, err := c.SearchBookmarks(context.Background(), contracts.SearchBookmarksInput{Query: "dockyard"})
	if err != nil {
		t.Fatalf("SearchBookmarks: %v", err)
	}
	if got.Total < 1 {
		t.Fatalf("Total = %d, want >= 1 match for 'dockyard'", got.Total)
	}
}

// TestSearchBookmarks_EmptyQuery confirms an empty query returns nothing
// (an agent that calls search with no query gets nothing, not the whole
// catalog).
func TestSearchBookmarks_EmptyQuery(t *testing.T) {
	t.Parallel()
	c := handlers.NewCatalog()
	got, err := c.SearchBookmarks(context.Background(), contracts.SearchBookmarksInput{})
	if err != nil {
		t.Fatalf("SearchBookmarks: %v", err)
	}
	if got.Total != 0 {
		t.Fatalf("Total = %d, want 0 for an empty query", got.Total)
	}
}
