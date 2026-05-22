package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hurtener/dockyard/runtime/obs"
)

func newLogTestServer(t *testing.T, emitter obs.Emitter) *Server {
	t.Helper()
	s, err := New(Info{Name: "logbridge-test", Version: "0.0.1"}, &Options{Obs: emitter})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestLogBridge_EmitsObsLogEvent(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(64)
	s := newLogTestServer(t, ring)
	b := s.LogBridge()

	// A nil session: the MCP notifications/message side is skipped, but the
	// obs/v1 log event is still emitted so an out-of-request record is
	// observable.
	err := b.Log(context.Background(), LogRecord{
		Level:   LogWarning,
		Logger:  "tool.search",
		Message: "slow query",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	events := ring.Recent(0)
	if len(events) != 1 {
		t.Fatalf("got %d obs events, want 1", len(events))
	}
	ev := events[0]
	if ev.Kind != obs.KindLog {
		t.Fatalf("event kind = %q, want %q", ev.Kind, obs.KindLog)
	}
	if ev.Phase != obs.PhaseEmit {
		t.Fatalf("event phase = %q, want %q", ev.Phase, obs.PhaseEmit)
	}
	var p obs.LogPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("decode LogPayload: %v", err)
	}
	if p.Level != "warning" || p.Logger != "tool.search" || p.Message != "slow query" {
		t.Fatalf("LogPayload = %+v, want level=warning logger=tool.search message=\"slow query\"", p)
	}
}

func TestLogBridge_NormalisesUnknownLevel(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(8)
	s := newLogTestServer(t, ring)
	b := s.LogBridge()

	if err := b.Log(context.Background(), LogRecord{
		Level:   LogLevel("bogus"),
		Message: "x",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	var p obs.LogPayload
	if err := json.Unmarshal(ring.Recent(0)[0].Payload, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Level != string(LogInfo) {
		t.Fatalf("unknown level normalised to %q, want %q", p.Level, LogInfo)
	}
}

func TestLogBridge_NoEmitterStillSafe(t *testing.T) {
	t.Parallel()
	// A server with no obs emitter still returns a usable bridge; the obs side
	// discards, the MCP side is unaffected.
	s, err := New(Info{Name: "no-obs", Version: "0.0.1"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b := s.LogBridge()
	if b == nil {
		t.Fatal("LogBridge() returned nil")
	}
	if err := b.Log(context.Background(), LogRecord{Level: LogInfo, Message: "ok"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
}

func TestLogBridge_LogToNilSessionEmitsObsOnly(t *testing.T) {
	t.Parallel()
	ring := obs.NewRingBuffer(8)
	s := newLogTestServer(t, ring)
	b := s.LogBridge()

	// LogTo with an explicit nil session: the MCP delivery is skipped, the
	// obs/v1 log event is still emitted.
	if err := b.LogTo(context.Background(), nil, LogRecord{
		Level:   LogError,
		Message: "out-of-request record",
	}); err != nil {
		t.Fatalf("LogTo: %v", err)
	}
	events := ring.Recent(0)
	if len(events) != 1 || events[0].Kind != obs.KindLog {
		t.Fatalf("LogTo did not emit exactly one obs/v1 log event, got %d", len(events))
	}
}

func TestLogLevel_Valid(t *testing.T) {
	t.Parallel()
	valid := []LogLevel{
		LogDebug, LogInfo, LogNotice, LogWarning,
		LogError, LogCritical, LogAlert, LogEmergency,
	}
	for _, l := range valid {
		if !l.valid() {
			t.Errorf("LogLevel %q reported invalid", l)
		}
	}
	for _, l := range []LogLevel{"", "trace", "fatal"} {
		if l.valid() {
			t.Errorf("LogLevel %q reported valid", l)
		}
	}
}
