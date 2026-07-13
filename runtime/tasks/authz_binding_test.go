package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestVerifiedContextOverridesTaskCreationAndBindsSupplyInput(t *testing.T) {
	e, err := NewEngine(NewInMemoryStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequestAuthContext(context.Background(), "verified-alice")
	created, err := e.createForToolCall(ctx, CreateToolCallParams{ToolName: "x", AuthContext: "spoofed-bob", Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.AuthContext != "verified-alice" {
		t.Fatalf("auth context = %q", rec.AuthContext)
	}
	params := json.RawMessage(`{"taskId":"` + created.ID + `","data":{}}`)
	if _, err := e.DispatchAs(context.Background(), "verified-bob", MethodSupplyInput, params); !errors.Is(err, ErrCrossContext) {
		t.Fatalf("cross-principal supplyInput = %v", err)
	}
}

func TestUnauthenticatedCreationRetainsExplicitAuthContext(t *testing.T) {
	e, _ := NewEngine(NewInMemoryStore(), nil)
	created, err := e.createForToolCall(context.Background(), CreateToolCallParams{ToolName: "x", AuthContext: "custom", Run: func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }})
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := e.store.Get(context.Background(), created.ID)
	if rec.AuthContext != "custom" {
		t.Fatalf("auth context = %q", rec.AuthContext)
	}
}
