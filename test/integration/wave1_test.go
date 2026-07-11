// This file is the Wave 1 wave-end end-to-end integration test (AGENTS.md
// §17 / §17.7 step 5). Wave 1 shipped three independent foundation phases —
// runtime/server, internal/protocolcodec and runtime/store — each depending
// only on phase 00. They do not yet consume one another: there is no deep
// runtime seam between them at this point in the build.
//
// This test is therefore the wave-boundary regression gate, not a fabricated
// integration. It imports and exercises all three packages' REAL public
// surfaces — a real runtime/server over the SDK in-memory transport, the real
// protocolcodec codecs, and BOTH real store drivers (inmem and sqlitestore) —
// in one test binary, proves the wave's surface is alive and composes cleanly
// side by side, covers a failure mode on each subsystem, and runs an N>=10
// concurrency stress under -race with a goroutine-leak assertion after
// teardown. See decision D-028.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/store"

	// Importing the two V1 store-driver packages runs their init blocks, which
	// register the drivers — the same wiring a Dockyard app uses. No mocks at
	// the seam boundary. The packages are also referenced for their exported
	// DriverName constants, so these are ordinary (not blank) imports.
	"github.com/hurtener/dockyard/runtime/store/inmem"
	"github.com/hurtener/dockyard/runtime/store/sqlitestore"
)

// ---- shared fixtures --------------------------------------------------------

// greetIn / greetOut is a trivial typed tool contract — the contract-first
// shape runtime/server.AddTool infers a JSON Schema from (RFC §6, P1).
type greetIn struct {
	Name string `json:"name"`
}

type greetOut struct {
	Greeting string `json:"greeting"`
}

func greetHandler(_ context.Context, in greetIn) (greetOut, error) {
	return greetOut{Greeting: "hello, " + in.Name}, nil
}

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// newWaveServer constructs a real runtime/server with one registered typed
// tool — the minimum to prove the server surface composes.
func newWaveServer(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.New(server.Info{
		Name:    "wave1-e2e",
		Title:   "Wave 1 E2E",
		Version: "0.1.0",
	}, &server.Options{Logger: quietLogger()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := server.AddTool(s, server.ToolDef{
		Name:        "greet",
		Description: "greet someone by name",
	}, greetHandler); err != nil {
		t.Fatalf("server.AddTool: %v", err)
	}
	return s
}

// connectWithTeardown serves s over the SDK in-memory transport and returns a
// connected client session plus an explicit teardown func. The teardown closes
// the session, cancels the serve goroutine and waits for it to exit — so a
// caller can prove the wave fully unwinds before a goroutine-leak assertion.
func connectWithTeardown(t *testing.T, s *server.Server) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	serverT, clientT := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())

	srvErr := make(chan error, 1)
	go func() { srvErr <- s.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wave1-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}

	var once sync.Once
	teardown := func() {
		once.Do(func() {
			_ = session.Close()
			cancel()
			select {
			case <-srvErr:
			case <-time.After(2 * time.Second):
				t.Error("server did not shut down")
			}
		})
	}
	return session, teardown
}

// connect is connectWithTeardown with teardown registered as a t.Cleanup hook —
// the convenient form for a straight-line test.
func connect(t *testing.T, s *server.Server) *mcpsdk.ClientSession {
	t.Helper()
	session, teardown := connectWithTeardown(t, s)
	t.Cleanup(teardown)
	return session
}

// callGreet invokes the greet tool over a session and returns the typed
// output. It returns an error rather than calling t.Fatalf so it is safe to
// call from a worker goroutine in the concurrency stress test.
func callGreet(session *mcpsdk.ClientSession, name string) (greetOut, error) {
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "greet",
		Arguments: greetIn{Name: name},
	})
	if err != nil {
		return greetOut{}, fmt.Errorf("CallTool: %w", err)
	}
	if res.IsError {
		return greetOut{}, fmt.Errorf("CallTool returned IsError: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return greetOut{}, fmt.Errorf("marshal structured content: %w", err)
	}
	var out greetOut
	if err := json.Unmarshal(raw, &out); err != nil {
		return greetOut{}, fmt.Errorf("unmarshal structured content: %w", err)
	}
	return out, nil
}

// storeDrivers names both real V1 store drivers and how to address each. A
// fresh DSN per call keeps sqlite tests isolated on the filesystem.
func storeDrivers(t *testing.T) []struct {
	name string
	dsn  func() string
} {
	t.Helper()
	return []struct {
		name string
		dsn  func() string
	}{
		{inmem.DriverName, func() string { return "" }},
		{sqlitestore.DriverName, func() string { return filepath.Join(t.TempDir(), "wave1.db") }},
	}
}

// ---- 1. the three surfaces, exercised together ------------------------------

// TestWave1SurfacesCompose boots a real server with a typed tool, round-trips
// Apps and Tasks `_meta`/capability shapes through a real protocolcodec codec,
// and runs a transactional put/get/scan on BOTH real store drivers — all in one
// test. The three subsystems are independent foundations, so this proves they
// are each alive and coexist cleanly in one binary; it does not fabricate a
// seam between them (AGENTS.md §17; D-028).
func TestWave1SurfacesCompose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// --- runtime/server: boot + invoke a typed tool over the transport ---
	s := newWaveServer(t)
	if got := s.Tools(); len(got) != 1 || got[0] != "greet" {
		t.Fatalf("server.Tools() = %v, want [greet]", got)
	}
	session := connect(t, s)
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "greet" {
		t.Fatalf("ListTools = %+v, want one tool named greet", list.Tools)
	}
	got, err := callGreet(session, "dockyard")
	if err != nil {
		t.Fatalf("callGreet: %v", err)
	}
	if got.Greeting != "hello, dockyard" {
		t.Fatalf("greet = %q, want %q", got.Greeting, "hello, dockyard")
	}

	// --- internal/protocolcodec: round-trip an Apps _meta shape, a Tasks
	// _meta shape, and both capability blocks through a real codec ---
	codec := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)

	// Apps tool `_meta.ui` round-trip.
	appsIn := protocolcodec.AppsToolMeta{
		ResourceURI: "ui://wave1/greet",
		Visibility:  []string{protocolcodec.VisibilityModel, protocolcodec.VisibilityApp},
	}
	appsMeta, err := codec.EncodeAppsToolMeta(nil, appsIn)
	if err != nil {
		t.Fatalf("EncodeAppsToolMeta: %v", err)
	}
	appsOut, ok, err := codec.DecodeAppsToolMeta(appsMeta)
	if err != nil || !ok {
		t.Fatalf("DecodeAppsToolMeta: ok=%v err=%v", ok, err)
	}
	if appsOut.ResourceURI != appsIn.ResourceURI || len(appsOut.Visibility) != 2 {
		t.Fatalf("Apps tool meta round-trip: got %+v want %+v", appsOut, appsIn)
	}

	// Apps extension capability block round-trip.
	appsCapRaw, err := codec.EncodeAppsExtensionCapability(protocolcodec.AppsExtensionCapability{
		MIMETypes: []string{protocolcodec.MIMETypeApp},
	})
	if err != nil {
		t.Fatalf("EncodeAppsExtensionCapability: %v", err)
	}
	if appsCap, ok, err := codec.DecodeAppsExtensionCapability(appsCapRaw); err != nil || !ok ||
		len(appsCap.MIMETypes) != 1 || appsCap.MIMETypes[0] != protocolcodec.MIMETypeApp {
		t.Fatalf("Apps capability round-trip: cap=%+v ok=%v err=%v", appsCap, ok, err)
	}

	// Tasks `Task` object round-trip.
	now := time.Unix(1_700_000_000, 0).UTC()
	taskRaw, err := codec.EncodeTask(protocolcodec.Task{
		ID:            "task-wave1",
		Status:        protocolcodec.TaskWorking,
		StatusMessage: "running",
		CreatedAt:     now,
		LastUpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("EncodeTask: %v", err)
	}
	taskOut, err := codec.DecodeTask(taskRaw)
	if err != nil {
		t.Fatalf("DecodeTask: %v", err)
	}
	if taskOut.ID != "task-wave1" || taskOut.Status != protocolcodec.TaskWorking ||
		!taskOut.CreatedAt.Equal(now) {
		t.Fatalf("Task round-trip: got %+v", taskOut)
	}

	// Tasks server-capability block round-trip.
	tasksCapRaw, err := codec.EncodeTasksServerCapability(protocolcodec.TasksServerCapability{
		List: true, Cancel: true, ToolsCall: true,
	})
	if err != nil {
		t.Fatalf("EncodeTasksServerCapability: %v", err)
	}
	tasksCap, ok, err := codec.DecodeTasksServerCapability(tasksCapRaw)
	if err != nil || !ok {
		t.Fatalf("DecodeTasksServerCapability: ok=%v err=%v", ok, err)
	}
	if !tasksCap.List || !tasksCap.Cancel || !tasksCap.ToolsCall {
		t.Fatalf("Tasks capability round-trip: got %+v", tasksCap)
	}

	// --- runtime/store: open each real driver, migrate, transactional
	// put/get/scan ---
	for _, drv := range storeDrivers(t) {
		t.Run("store/"+drv.name, func(t *testing.T) {
			t.Parallel()
			st, err := store.Open(ctx, drv.name, drv.dsn())
			if err != nil {
				t.Fatalf("store.Open(%q): %v", drv.name, err)
			}
			t.Cleanup(func() {
				if err := st.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			})
			if err := st.Migrate(ctx, nil); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if err := st.Ping(ctx); err != nil {
				t.Fatalf("Ping: %v", err)
			}

			// Write three keys in one transaction.
			if err := st.Update(ctx, func(tx store.Tx) error {
				for _, k := range []string{"app:a", "app:b", "other:c"} {
					if err := tx.Put("wave1", k, []byte("v-"+k)); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatalf("Update: %v", err)
			}

			// Read back + prefix scan in a read-only transaction.
			if err := st.View(ctx, func(tx store.Tx) error {
				got, err := tx.Get("wave1", "app:a")
				if err != nil {
					return err
				}
				if string(got) != "v-app:a" {
					t.Fatalf("Get app:a = %q, want %q", got, "v-app:a")
				}
				kvs, err := tx.Scan("wave1", "app:")
				if err != nil {
					return err
				}
				if len(kvs) != 2 || kvs[0].Key != "app:a" || kvs[1].Key != "app:b" {
					t.Fatalf("Scan(app:) = %+v, want [app:a app:b] ordered", kvs)
				}
				return nil
			}); err != nil {
				t.Fatalf("View: %v", err)
			}
		})
	}
}

// ---- 2. failure modes — at least one per subsystem --------------------------

// TestWave1FailureModes proves each Wave 1 subsystem rejects a malformed input
// with a typed error rather than panicking across a boundary (AGENTS.md §13).
func TestWave1FailureModes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// runtime/server: an invalid tool schema (a non-object input type has no
	// object JSON Schema) is rejected as an error, never a panic.
	t.Run("server/invalid-tool-schema", func(t *testing.T) {
		t.Parallel()
		s, err := server.New(server.Info{Name: "bad-tool", Version: "0.0.1"},
			&server.Options{Logger: quietLogger()})
		if err != nil {
			t.Fatalf("server.New: %v", err)
		}
		err = server.AddTool(s, server.ToolDef{Name: "bad"},
			func(_ context.Context, _ string) (greetOut, error) { return greetOut{}, nil })
		if err == nil {
			t.Fatal("AddTool with a non-object input: want error, got nil")
		}
	})

	// protocolcodec: a malformed `_meta.ui` value (a string where an object is
	// required) decodes to a wrapped ErrMalformedMeta.
	t.Run("protocolcodec/malformed-meta", func(t *testing.T) {
		t.Parallel()
		codec := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)
		_, _, err := codec.DecodeAppsToolMeta(protocolcodec.Meta{"ui": "not-an-object"})
		if !errors.Is(err, protocolcodec.ErrMalformedMeta) {
			t.Fatalf("DecodeAppsToolMeta(malformed): got %v, want ErrMalformedMeta", err)
		}
	})

	// protocolcodec: a malformed Tasks `Task` (an unknown status) is rejected.
	t.Run("protocolcodec/invalid-task-status", func(t *testing.T) {
		t.Parallel()
		codec := protocolcodec.CodecFor(protocolcodec.VersionMCP20251125)
		_, err := codec.DecodeTask(json.RawMessage(
			`{"taskId":"x","status":"bogus","createdAt":"2026-01-01T00:00:00Z","lastUpdatedAt":"2026-01-01T00:00:00Z","ttl":null}`))
		if !errors.Is(err, protocolcodec.ErrMalformedMeta) {
			t.Fatalf("DecodeTask(unknown status): got %v, want ErrMalformedMeta", err)
		}
	})

	// store: an operation on a closed Store returns ErrClosed, on both drivers.
	for _, drv := range storeDrivers(t) {
		t.Run("store/"+drv.name+"/closed", func(t *testing.T) {
			t.Parallel()
			st, err := store.Open(ctx, drv.name, drv.dsn())
			if err != nil {
				t.Fatalf("store.Open(%q): %v", drv.name, err)
			}
			if err := st.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			err = st.View(ctx, func(store.Tx) error { return nil })
			if !errors.Is(err, store.ErrClosed) {
				t.Fatalf("View after Close on %q: got %v, want ErrClosed", drv.name, err)
			}
		})
	}

	// store: the factory rejects an unregistered driver name with the sentinel.
	t.Run("store/unknown-driver", func(t *testing.T) {
		t.Parallel()
		if _, err := store.Open(ctx, "postgres", ""); !errors.Is(err, store.ErrUnknownDriver) {
			t.Fatalf("Open unknown driver: got %v, want ErrUnknownDriver", err)
		}
	})
}

// ---- 3. concurrency stress under -race + goroutine-leak gate ----------------

// TestWave1ConcurrencyStress drives all three Wave 1 surfaces concurrently from
// N>=10 goroutines and asserts no race (the -race detector does the asserting)
// and no goroutine leak after teardown. Each subsystem owns a reusable artifact
// that must be safe under concurrent use (AGENTS.md §5): one Server serving
// many sessions, one stateless Codec, and one Store per driver.
func TestWave1ConcurrencyStress(t *testing.T) {
	ctx := context.Background()

	// Settle pre-existing goroutines, then snapshot the baseline.
	baseline := stableGoroutineCount()

	// One shared Server and one shared Codec across all workers.
	srv := newWaveServer(t)
	codec := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)

	// One shared Store per driver — opened and migrated up front.
	type drvStore struct {
		name string
		st   store.Store
	}
	var stores []drvStore
	for _, drv := range storeDrivers(t) {
		st, err := store.Open(ctx, drv.name, drv.dsn())
		if err != nil {
			t.Fatalf("store.Open(%q): %v", drv.name, err)
		}
		if err := st.Migrate(ctx, nil); err != nil {
			t.Fatalf("Migrate(%q): %v", drv.name, err)
		}
		stores = append(stores, drvStore{drv.name, st})
	}

	const workers = 16 // N >= 10
	const iterations = 25

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func(w int) {
			defer wg.Done()

			// Each worker gets its own client session against the shared
			// server — proves the Server is safe across concurrent sessions.
			// The worker tears its own session down before returning so the
			// post-wait goroutine-leak assertion sees a fully unwound wave.
			session, teardown := connectWithTeardown(t, srv)
			defer teardown()

			for i := range iterations {
				// runtime/server: invoke the typed tool.
				got, err := callGreet(session, "w")
				if err != nil {
					t.Errorf("worker %d: callGreet: %v", w, err)
					return
				}
				if got.Greeting != "hello, w" {
					t.Errorf("worker %d: greet = %q", w, got.Greeting)
					return
				}

				// protocolcodec: round-trip a Tasks `_meta` related-task key
				// and an Apps tool meta through the shared stateless codec.
				base := protocolcodec.Meta{"w": w}
				rel, err := codec.EncodeRelatedTaskMeta(base, "task-w")
				if err != nil {
					t.Errorf("worker %d: EncodeRelatedTaskMeta: %v", w, err)
					return
				}
				if id, ok, err := codec.DecodeRelatedTaskMeta(rel); err != nil || !ok || id != "task-w" {
					t.Errorf("worker %d: DecodeRelatedTaskMeta: id=%q ok=%v err=%v", w, id, ok, err)
					return
				}
				if base["w"] != w {
					t.Errorf("worker %d: codec mutated the caller's base map", w)
					return
				}

				// runtime/store: a transactional put/get against every shared
				// driver, in a worker-private key so writes do not collide.
				for _, ds := range stores {
					if err := ds.st.Update(ctx, func(tx store.Tx) error {
						return tx.Put("stress", key(w, i), []byte("v"))
					}); err != nil {
						t.Errorf("worker %d: Update(%s): %v", w, ds.name, err)
						return
					}
					if err := ds.st.View(ctx, func(tx store.Tx) error {
						got, err := tx.Get("stress", key(w, i))
						if err != nil {
							return err
						}
						if string(got) != "v" {
							return errors.New("unexpected value " + string(got))
						}
						return nil
					}); err != nil {
						t.Errorf("worker %d: View(%s): %v", w, ds.name, err)
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()

	// Tear the shared stores down before the leak check. Every worker has
	// already torn down its own server session via its deferred teardown, so
	// after wg.Wait the only Wave 1 resources left open are these stores.
	for _, ds := range stores {
		if err := ds.st.Close(); err != nil {
			t.Errorf("Close(%s): %v", ds.name, err)
		}
	}

	assertNoGoroutineLeak(t, baseline)
}

// key builds a worker-private, collision-free store key.
func key(worker, iter int) string {
	return "w" + itoa(worker) + ":i" + itoa(iter)
}

// itoa is a tiny dependency-free int formatter for test keys.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// stableGoroutineCount returns runtime.NumGoroutine once it has held steady
// across consecutive samples — a best-effort way to read a quiescent baseline
// without a third-party leak detector.
func stableGoroutineCount() int {
	prev := runtime.NumGoroutine()
	for range 50 {
		time.Sleep(10 * time.Millisecond)
		cur := runtime.NumGoroutine()
		if cur == prev {
			return cur
		}
		prev = cur
	}
	return prev
}

// assertNoGoroutineLeak fails the test if the goroutine count has not returned
// to (at most) its baseline after teardown. It polls because session and
// transport goroutines unwind asynchronously after Close/cancel; a small slack
// absorbs test-runtime background goroutines.
func assertNoGoroutineLeak(t *testing.T, baseline int) {
	assertNoGoroutineLeakWithSlack(t, baseline, 2)
}

func assertNoGoroutineLeakWithSlack(t *testing.T, baseline, slack int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := runtime.NumGoroutine()
		if got <= baseline+slack {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("goroutine leak: %d goroutines after teardown, baseline %d (slack %d)",
				got, baseline, slack)
			return
		}
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
}
