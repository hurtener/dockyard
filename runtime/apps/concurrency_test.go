package apps_test

import (
	"context"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/apps"
)

// TestConcurrentResourceRead is the reusable-artifact concurrency test
// (AGENTS.md §5, §14): one App registered once is read concurrently by many
// goroutines over one session. Every read must return identical content and
// identical _meta.ui with no data race — the resource-read handler and its
// captured _meta are shared, so the handler must never mutate them.
func TestConcurrentResourceRead(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://concurrent/main"
	const html = "<html><body>concurrent app</body></html>"
	if err := apps.Register(s, apps.App{
		URI:         uri,
		Name:        "concurrent",
		HTML:        []byte(html),
		CSP:         apps.CSP{Connect: []string{"https://api.example.com"}},
		Permissions: apps.Permissions{ClipboardWrite: true},
		Domain:      "concurrent-origin",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	ctx := context.Background()

	const workers = 16
	const readsPerWorker = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers*readsPerWorker)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerWorker; j++ {
				read, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
				if err != nil {
					errs <- err
					return
				}
				if len(read.Contents) != 1 || read.Contents[0].Text != html {
					errs <- errReadMismatch
					return
				}
				if read.Contents[0].MIMEType != apps.MIMETypeApp {
					errs <- errReadMismatch
					return
				}
				ui, ok := read.Contents[0].Meta["ui"].(map[string]any)
				if !ok || ui["domain"] != "concurrent-origin" {
					errs <- errReadMismatch
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent resource read: %v", err)
	}
}

// errReadMismatch flags a concurrent read that returned unexpected content.
var errReadMismatch = mismatchError("concurrent read returned unexpected content or _meta")

type mismatchError string

func (e mismatchError) Error() string { return string(e) }

// TestConcurrentToolMetaFor proves ToolMetaFor is safe to call concurrently —
// it is a pure function that allocates a fresh map per call.
func TestConcurrentToolMetaFor(t *testing.T) {
	t.Parallel()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			meta, err := apps.ToolMetaFor(apps.ToolLink{
				ResourceURI: "ui://x/main",
				Visibility:  []string{apps.VisibilityApp},
			})
			if err != nil {
				errs <- err
				return
			}
			if _, ok := meta["ui"].(map[string]any); !ok {
				errs <- errReadMismatch
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent ToolMetaFor: %v", err)
	}
}
