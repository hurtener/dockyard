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

func TestWithoutRawTokenStripsExposedToken(t *testing.T) {
	t.Parallel()
	ctx := WithRawToken(context.Background(), "header.payload.signature")
	stripped := WithoutRawToken(ctx)
	if got, ok := RawTokenFromContext(stripped); ok || got != "" {
		t.Fatalf("stripped context = %q, %v; want \"\", false", got, ok)
	}
	// The original context is unchanged — stripping is a child-context operation.
	if got, ok := RawTokenFromContext(ctx); !ok || got != "header.payload.signature" {
		t.Fatalf("source context mutated by strip: %q, %v", got, ok)
	}
}

func TestWithoutRawTokenNoOpWhenAbsent(t *testing.T) {
	t.Parallel()
	base := context.Background()
	if WithoutRawToken(base) != base {
		t.Fatal("WithoutRawToken allocated a child context when no token was present")
	}
}
