package server

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// captureHandler is a minimal slog.Handler that records every emitted record so
// a test can assert on level and message. It is safe for concurrent use.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// domainWarns returns the WARN records whose message mentions the dedicated
// origin (the D-176 stdio guard), with the named App attribute attached.
func (h *captureHandler) domainWarns() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		if r.Level != slog.LevelWarn || !strings.Contains(r.Message, "dedicated origin") {
			continue
		}
		var app string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "app" {
				app = a.Value.String()
			}
			return true
		})
		out = append(out, app)
	}
	return out
}

func newServerWithLogger(t *testing.T, h slog.Handler) *Server {
	t.Helper()
	s, err := New(Info{Name: "warn-test", Version: "0.0.1"}, &Options{Logger: slog.New(h)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestWarnDomainOnStdio_WarnsForDomainApp proves the D-176 stdio guard: a
// stdio-only server with an App carrying a non-empty Domain logs an slog.Warn
// naming the App; an App with no Domain does not warn.
func TestWarnDomainOnStdio_WarnsForDomainApp(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	s := newServerWithLogger(t, h)

	if err := s.RegisterAppLink("widgets", AppLink{
		URI:    "ui://warn-test/widgets",
		Domain: "a904794854a047f6.claudemcpcontent.com",
	}); err != nil {
		t.Fatalf("RegisterAppLink(widgets): %v", err)
	}
	if err := s.RegisterAppLink("plain", AppLink{URI: "ui://warn-test/plain"}); err != nil {
		t.Fatalf("RegisterAppLink(plain): %v", err)
	}

	s.warnDomainOnStdio(context.Background())

	warned := h.domainWarns()
	if len(warned) != 1 {
		t.Fatalf("domain warnings = %v, want exactly one (for the Domain-bearing App)", warned)
	}
	if warned[0] != "widgets" {
		t.Errorf("warned App = %q, want widgets", warned[0])
	}
}

// TestWarnDomainOnStdio_SilentWithoutDomain proves a stdio server whose Apps
// declare no Domain stays silent.
func TestWarnDomainOnStdio_SilentWithoutDomain(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	s := newServerWithLogger(t, h)
	if err := s.RegisterAppLink("plain", AppLink{URI: "ui://warn-test/plain"}); err != nil {
		t.Fatalf("RegisterAppLink: %v", err)
	}

	s.warnDomainOnStdio(context.Background())

	if warned := h.domainWarns(); len(warned) != 0 {
		t.Errorf("domain warnings = %v, want none", warned)
	}
}

// TestEmitLegacyToolUIMeta_Getter proves the D-177 opt-in is plumbed from
// Options onto the Server: New mirrors Options.EmitLegacyToolUIMeta onto the
// getter the runtime/tool builder reads. Default off.
func TestEmitLegacyToolUIMeta_Getter(t *testing.T) {
	t.Parallel()
	off := newServerWithLogger(t, &captureHandler{})
	if off.EmitLegacyToolUIMeta() {
		t.Error("default EmitLegacyToolUIMeta() = true, want false")
	}
	on, err := New(Info{Name: "legacy-on", Version: "0.0.1"},
		&Options{Logger: slog.New(&captureHandler{}), EmitLegacyToolUIMeta: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !on.EmitLegacyToolUIMeta() {
		t.Error("EmitLegacyToolUIMeta() = false after opting in, want true")
	}
	// A nil receiver is safe (defensive — the getter guards it).
	var nilSrv *Server
	if nilSrv.EmitLegacyToolUIMeta() {
		t.Error("nil *Server EmitLegacyToolUIMeta() = true, want false")
	}
}

// TestHTTPHandler_DoesNotWarnDomain proves the guard is stdio-only: building and
// using the HTTP transport for a server with a Domain-bearing App emits no
// dedicated-origin warning (a dedicated origin IS honoured on a remote
// connector — D-176).
func TestHTTPHandler_DoesNotWarnDomain(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	s := newServerWithLogger(t, h)
	if err := s.RegisterAppLink("widgets", AppLink{
		URI:    "ui://warn-test/widgets",
		Domain: "a904794854a047f6.claudemcpcontent.com",
	}); err != nil {
		t.Fatalf("RegisterAppLink: %v", err)
	}

	if _, err := s.HTTPHandler(&HTTPOptions{Security: DefaultHTTPSecurity()}); err != nil {
		t.Fatalf("HTTPHandler: %v", err)
	}

	if warned := h.domainWarns(); len(warned) != 0 {
		t.Errorf("HTTP transport emitted domain warnings %v, want none", warned)
	}
}
