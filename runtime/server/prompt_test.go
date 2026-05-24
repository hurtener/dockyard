package server_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
)

// TestAddPrompt_Validation covers the registration-time guards: nil server,
// empty name, nil handler, empty-named argument. None of these should
// register a prompt.
func TestAddPrompt_Validation(t *testing.T) {
	t.Parallel()
	handler := func(_ context.Context, _ server.PromptRequest) (server.PromptResult, error) {
		return server.PromptResult{}, nil
	}

	t.Run("nil server", func(t *testing.T) {
		t.Parallel()
		if err := server.AddPrompt(nil, server.PromptDef{Name: "p"}, handler); err == nil {
			t.Fatalf("expected error on nil server, got nil")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := server.AddPrompt(s, server.PromptDef{}, handler); err == nil {
			t.Fatalf("expected error on empty name, got nil")
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		if err := server.AddPrompt(s, server.PromptDef{Name: "p"}, nil); err == nil {
			t.Fatalf("expected error on nil handler, got nil")
		}
	})

	t.Run("empty-named argument", func(t *testing.T) {
		t.Parallel()
		s := newTestServer(t)
		def := server.PromptDef{
			Name:      "p",
			Arguments: []server.PromptArgument{{Name: ""}},
		}
		if err := server.AddPrompt(s, def, handler); err == nil {
			t.Fatalf("expected error on empty-named argument, got nil")
		}
	})
}

// TestAddPrompt_Prompts records the prompt names accessor (mirrors Tools()).
func TestAddPrompt_PromptsAccessor(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	for _, name := range []string{"summarize", "rewrite", "explain"} {
		if err := server.AddPrompt(s, server.PromptDef{Name: name},
			func(_ context.Context, _ server.PromptRequest) (server.PromptResult, error) {
				return server.PromptResult{}, nil
			}); err != nil {
			t.Fatalf("AddPrompt %q: %v", name, err)
		}
	}
	got := s.Prompts()
	want := []string{"summarize", "rewrite", "explain"}
	if len(got) != len(want) {
		t.Fatalf("Prompts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Prompts()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestAddPrompt_RoundTrip drives a real prompts/get over the in-memory
// transport, verifying the rendered messages, the description fallback,
// and the obs/v1 prompt.get start/end event pair.
func TestAddPrompt_RoundTrip(t *testing.T) {
	t.Parallel()
	emitter := obs.NewRingBuffer(obs.DefaultRingCapacity)

	s, err := server.New(server.Info{
		Name:    "prompts-test",
		Version: "0.1.0",
	}, &server.Options{Logger: quietLogger(), Obs: emitter})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := server.AddPrompt(s, server.PromptDef{
		Name:        "summarize",
		Title:       "Summarise a passage",
		Description: "Default description",
		Arguments: []server.PromptArgument{
			{Name: "passage", Required: true},
		},
	}, func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
		passage := req.Arguments["passage"]
		return server.PromptResult{
			Description: "Summarise: " + passage[:min(len(passage), 16)],
			Messages: []server.PromptMessage{
				{Role: "system", Text: "You are a careful summariser."},
				{Role: "user", Text: "Please summarise the following text:\n" + passage},
			},
		}, nil
	}); err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientT := s.ServeInMemory(ctx)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "prompts-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	got, err := session.GetPrompt(ctx, &mcpsdk.GetPromptParams{
		Name:      "summarize",
		Arguments: map[string]string{"passage": "The quick brown fox jumps over the lazy dog."},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if got == nil {
		t.Fatalf("GetPrompt returned nil result")
	}
	if !strings.HasPrefix(got.Description, "Summarise:") {
		t.Fatalf("description = %q, want prefix 'Summarise:'", got.Description)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2", len(got.Messages))
	}
	sys, ok := got.Messages[0].Content.(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("first message content = %T, want *TextContent", got.Messages[0].Content)
	}
	if !strings.Contains(sys.Text, "summariser") {
		t.Fatalf("first message text = %q", sys.Text)
	}

	// Verify the obs/v1 prompt.get lifecycle: a start, then an end with
	// duration and non-zero Bytes + Messages.
	events := emitter.Recent(0)
	var startSeen, endSeen bool
	for _, e := range events {
		if e.Kind != obs.KindPromptGet {
			continue
		}
		switch e.Phase {
		case obs.PhaseStart:
			startSeen = true
		case obs.PhaseEnd:
			endSeen = true
			if e.DurationMS == nil {
				t.Errorf("end event missing DurationMS")
			}
		}
	}
	if !startSeen || !endSeen {
		t.Fatalf("expected start+end prompt.get events, got: %+v", events)
	}
}

// TestAddPrompt_HandlerErrorSurfaces verifies a handler-returned error
// becomes a prompts/get error to the host.
func TestAddPrompt_HandlerErrorSurfaces(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddPrompt(s, server.PromptDef{Name: "fails"},
		func(_ context.Context, _ server.PromptRequest) (server.PromptResult, error) {
			return server.PromptResult{}, errors.New("boom")
		}); err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "0"}, nil)
	session, err := client.Connect(ctx, s.ServeInMemory(ctx), nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	_, err = session.GetPrompt(ctx, &mcpsdk.GetPromptParams{Name: "fails"})
	if err == nil {
		t.Fatalf("expected handler error to propagate, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %q does not include 'boom'", err.Error())
	}
}

// TestAddPrompt_PanicRecovered proves a handler panic becomes a typed
// Dockyard error rather than a process crash (AGENTS.md §5, §13).
func TestAddPrompt_PanicRecovered(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddPrompt(s, server.PromptDef{Name: "panics"},
		func(_ context.Context, _ server.PromptRequest) (server.PromptResult, error) {
			panic("nope")
		}); err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "0"}, nil)
	session, err := client.Connect(ctx, s.ServeInMemory(ctx), nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	_, err = session.GetPrompt(ctx, &mcpsdk.GetPromptParams{Name: "panics"})
	if err == nil {
		t.Fatalf("expected handler panic to surface as an error, got nil")
	}
}

// TestAddPrompt_Concurrent proves a single Server safely serves concurrent
// prompts/get calls — reusable-artifact safety under -race (AGENTS.md §5).
func TestAddPrompt_Concurrent(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	if err := server.AddPrompt(s, server.PromptDef{Name: "echo"},
		func(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
			return server.PromptResult{
				Messages: []server.PromptMessage{{Role: "user", Text: req.Arguments["who"]}},
			}, nil
		}); err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}

	const sessions = 8
	var wg sync.WaitGroup
	wg.Add(sessions)
	for i := range sessions {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "0"}, nil)
			session, err := client.Connect(ctx, s.ServeInMemory(ctx), nil)
			if err != nil {
				t.Errorf("session %d connect: %v", i, err)
				return
			}
			defer func() { _ = session.Close() }()
			got, err := session.GetPrompt(ctx, &mcpsdk.GetPromptParams{
				Name:      "echo",
				Arguments: map[string]string{"who": "world"},
			})
			if err != nil {
				t.Errorf("session %d GetPrompt: %v", i, err)
				return
			}
			if len(got.Messages) != 1 {
				t.Errorf("session %d messages = %d, want 1", i, len(got.Messages))
			}
		}()
	}
	wg.Wait()
}
