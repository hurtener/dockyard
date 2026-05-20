package protocolcodec

import (
	"sync"
	"testing"
	"time"
)

// TestCodecConcurrentReuse exercises a single Codec from many goroutines at
// once. A Codec is a reusable artifact (AGENTS.md §5, §14), so it must be safe
// under concurrent use; run under `-race` this proves it. The codec is
// stateless, so the real assertion is "no race, no shared-state corruption".
func TestCodecConcurrentReuse(t *testing.T) {
	t.Parallel()
	c := CodecFor(VersionApps20260126)

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				// Apps tool meta — each goroutine owns its base map.
				base := Meta{"g": g}
				toolMeta, err := c.EncodeAppsToolMeta(base, AppsToolMeta{
					ResourceURI: "ui://x/y",
					Visibility:  []string{VisibilityApp},
				})
				if err != nil {
					t.Errorf("g%d: encode tool meta: %v", g, err)
					return
				}
				if tm, ok, err := c.DecodeAppsToolMeta(toolMeta); err != nil || !ok ||
					tm.ResourceURI != "ui://x/y" {
					t.Errorf("g%d: decode tool meta: ok=%v err=%v", g, ok, err)
					return
				}
				if base["g"] != g {
					t.Errorf("g%d: base map mutated under concurrency", g)
					return
				}

				// Tasks Task round-trip.
				raw, err := c.EncodeTask(Task{
					ID: "t", Status: TaskWorking,
					CreatedAt: time.Unix(int64(i), 0).UTC(), LastUpdatedAt: time.Unix(int64(i), 0).UTC(),
				})
				if err != nil {
					t.Errorf("g%d: encode task: %v", g, err)
					return
				}
				if _, err := c.DecodeTask(raw); err != nil {
					t.Errorf("g%d: decode task: %v", g, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestCodecForConcurrentLookup hammers the registry lookup; the registry is
// read-only after init, so concurrent reads must be race-free.
func TestCodecForConcurrentLookup(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	wg.Add(32)
	for i := 0; i < 32; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if CodecFor(VersionMCP20251125) == nil {
					t.Error("nil codec")
					return
				}
				_ = KnownVersions()
			}
		}()
	}
	wg.Wait()
}
