package apps

import (
	"errors"
	"sync"
	"testing"
	"testing/fstest"
)

func TestBundle_Validate_Populated(t *testing.T) {
	if err := EmbeddedBundle().Validate(); err != nil {
		t.Errorf("EmbeddedBundle().Validate() = %v, want nil", err)
	}
}

func TestBundle_Validate_EmptyTree(t *testing.T) {
	// An embed.FS whose dist/ root resolved but holds no files — the runtime
	// analogue of a `vite build` that has not produced a bundle (RFC §14).
	empty := fstest.MapFS{}
	if err := NewBundle(empty, "dist").Validate(); !errors.Is(err, ErrEmptyBundle) {
		t.Errorf("Validate on an empty FS = %v, want ErrEmptyBundle", err)
	}
}

func TestBundle_Validate_MissingRoot(t *testing.T) {
	// A bundle pointed at a root that does not exist in the FS.
	fsys := fstest.MapFS{"other/file.html": {Data: []byte("x")}}
	if err := NewBundle(fsys, "dist").Validate(); !errors.Is(err, ErrEmptyBundle) {
		t.Errorf("Validate on a missing root = %v, want ErrEmptyBundle", err)
	}
}

func TestBundle_Validate_NilFS(t *testing.T) {
	if err := (Bundle{}).Validate(); !errors.Is(err, ErrEmptyBundle) {
		t.Errorf("zero Bundle Validate = %v, want ErrEmptyBundle", err)
	}
}

func TestBundle_HTML_ReadsBuiltEntry(t *testing.T) {
	html, err := EmbeddedBundle().HTML("web/src/apps/customer-health.svelte")
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}
	if len(html) == 0 {
		t.Fatal("HTML returned an empty body")
	}
	if got := string(html); got[:9] != "<!doctype" {
		t.Errorf("HTML body does not look like a built bundle: %.20q", got)
	}
}

func TestBundle_HTML_MissingEntry(t *testing.T) {
	_, err := EmbeddedBundle().HTML("web/src/apps/no-such.svelte")
	if !errors.Is(err, ErrBundleEntryNotFound) {
		t.Errorf("HTML for a missing entry = %v, want ErrBundleEntryNotFound", err)
	}
}

func TestBundle_HTML_NonSvelteEntry(t *testing.T) {
	_, err := EmbeddedBundle().HTML("web/src/apps/customer-health.txt")
	if !errors.Is(err, ErrBundleEntryNotFound) {
		t.Errorf("HTML for a non-.svelte entry = %v, want ErrBundleEntryNotFound", err)
	}
}

func TestBundle_HTML_EmptyArtifact(t *testing.T) {
	fsys := fstest.MapFS{"dist/blank.html": {Data: []byte{}}}
	_, err := NewBundle(fsys, "dist").HTML("web/src/apps/blank.svelte")
	if !errors.Is(err, ErrBundleEntryNotFound) {
		t.Errorf("HTML for an empty artifact = %v, want ErrBundleEntryNotFound", err)
	}
}

func TestEntryStem(t *testing.T) {
	cases := map[string]string{
		"web/src/apps/customer-health.svelte": "customer-health",
		"order-status.svelte":                 "order-status",
		"customer-health.txt":                 "",
		"":                                    "",
		"web/src/apps/":                       "",
	}
	for in, want := range cases {
		if got := entryStem(in); got != want {
			t.Errorf("entryStem(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBundle_ConcurrentUse proves a Bundle is safe for concurrent use — it is a
// reusable artifact backing the ui:// resource handler (AGENTS.md §5, §14).
func TestBundle_ConcurrentUse(t *testing.T) {
	b := EmbeddedBundle()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := b.Validate(); err != nil {
				t.Errorf("concurrent Validate: %v", err)
			}
			if _, err := b.HTML("web/src/apps/customer-health.svelte"); err != nil {
				t.Errorf("concurrent HTML: %v", err)
			}
		}()
	}
	wg.Wait()
}
