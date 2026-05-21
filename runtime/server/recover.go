package server

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// ErrHandlerPanic is the sentinel every recovered handler panic wraps. A caller
// can branch with errors.Is(err, ErrHandlerPanic); the rendered message names
// the offending tool or resource and the recovered value.
//
// A panicking tool or resource handler is a bug in the app author's code, not a
// protocol condition — but the "no panic across the MCP boundary" rule
// (AGENTS.md §5, §13) demands the server process survive it. The handler
// wrappers recover the panic and convert it into this error so a single bad
// handler degrades one tools/call or resources/read into a clean error result
// instead of crashing the server for every connected host.
var ErrHandlerPanic = fmt.Errorf("dockyard/runtime/server: handler panicked")

// panicError is the typed error a recovered handler panic becomes. It wraps
// ErrHandlerPanic and carries the recovered value's rendered form.
type panicError struct {
	// kind is "tool" or "resource" — the handler surface that panicked.
	kind string
	// name is the wire name (tool) or URI (resource) of the panicking handler.
	name string
	// value is the recovered panic value, rendered with %v.
	value string
}

func (e *panicError) Error() string {
	return fmt.Sprintf("dockyard/runtime/server: %s %q handler panicked: %s",
		e.kind, e.name, e.value)
}

// Unwrap reports ErrHandlerPanic so errors.Is(err, ErrHandlerPanic) holds.
func (e *panicError) Unwrap() error { return ErrHandlerPanic }

// guardHandler runs fn and converts a panic into a typed *panicError instead of
// unwinding past the MCP boundary. It is the single chokepoint every
// handler-invocation path routes through, so the "no panic across the MCP
// boundary" rule is a toolchain-enforced guarantee, not a docstring instruction
// (AGENTS.md §5, §13; D-053).
//
// kind is "tool" or "resource"; name is the wire name or URI; log receives a
// structured record of the recovered panic — including the stack — so the bug
// is diagnosable even though it never reaches the host. A nil log falls back to
// slog.Default().
//
// On a clean return guardHandler returns fn's own error verbatim; on a panic it
// returns a *panicError wrapping ErrHandlerPanic. The SDK turns either non-nil
// error into a CallToolResult/ReadResource error result, so a panicking handler
// becomes an error result and the server keeps serving.
func guardHandler(ctx context.Context, log *slog.Logger, kind, name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			pe := &panicError{kind: kind, name: name, value: fmt.Sprintf("%v", r)}
			if log == nil {
				log = slog.Default()
			}
			log.ErrorContext(ctx, "dockyard recovered a handler panic",
				slog.String("handler.kind", kind),
				slog.String("handler.name", name),
				slog.String("panic", pe.value),
				slog.String("stack", string(debug.Stack())),
			)
			err = pe
		}
	}()
	return fn()
}
