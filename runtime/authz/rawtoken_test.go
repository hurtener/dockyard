package authz

import (
	"context"
	"testing"
)

func TestRawTokenRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := WithRawToken(context.Background(), "header.payload.signature")
	got, ok := RawTokenFromContext(ctx)
	if !ok || got != "header.payload.signature" {
		t.Fatalf("RawTokenFromContext = %q, %v; want the token, true", got, ok)
	}
}

func TestRawTokenAbsentByDefault(t *testing.T) {
	t.Parallel()
	// The zero configuration never sets the token; the false result is the
	// normal case and must not be mistaken for a usable token.
	if got, ok := RawTokenFromContext(context.Background()); ok || got != "" {
		t.Fatalf("empty context = %q, %v; want \"\", false", got, ok)
	}
}

func TestRawTokenEmptyStringReadsAsAbsent(t *testing.T) {
	t.Parallel()
	// A stored empty string reads as absent, so a handler can never treat "" as
	// a forwardable token.
	ctx := WithRawToken(context.Background(), "")
	if got, ok := RawTokenFromContext(ctx); ok || got != "" {
		t.Fatalf("empty-token context = %q, %v; want \"\", false", got, ok)
	}
}
