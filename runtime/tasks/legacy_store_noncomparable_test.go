package tasks_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/tasks"
)

type nonComparableLegacyStore []tasks.TaskStore

func (s nonComparableLegacyStore) Create(ctx context.Context, rec tasks.TaskRecord) error {
	return s[0].Create(ctx, rec)
}
func (s nonComparableLegacyStore) Get(ctx context.Context, id string) (tasks.TaskRecord, error) {
	return s[0].Get(ctx, id)
}
func (s nonComparableLegacyStore) Transition(ctx context.Context, id string, to protocolcodec.TaskStatus, msg string) (tasks.TaskRecord, error) {
	return s[0].Transition(ctx, id, to, msg)
}
func (s nonComparableLegacyStore) SetResult(ctx context.Context, id string, result tasks.TaskResult) error {
	return s[0].SetResult(ctx, id, result)
}
func (s nonComparableLegacyStore) AddInputRequest(ctx context.Context, id string, req tasks.InputRequest) error {
	return s[0].AddInputRequest(ctx, id, req)
}
func (s nonComparableLegacyStore) ApplyInputResponses(ctx context.Context, id string, responses map[string]tasks.TaskInputResponse) (map[string]tasks.TaskInputResponse, tasks.TaskRecord, error) {
	return s[0].ApplyInputResponses(ctx, id, responses)
}
func (s nonComparableLegacyStore) List(ctx context.Context, cursor string, limit int) ([]tasks.TaskRecord, string, error) {
	return s[0].List(ctx, cursor, limit)
}
func (s nonComparableLegacyStore) ListByAuthContext(ctx context.Context, authContext, cursor string, limit int) ([]tasks.TaskRecord, string, error) {
	return s[0].ListByAuthContext(ctx, authContext, cursor, limit)
}
func (s nonComparableLegacyStore) Delete(ctx context.Context, id string) error {
	return s[0].Delete(ctx, id)
}
func (s nonComparableLegacyStore) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	return s[0].PurgeExpired(ctx, now)
}

func TestLegacyStoreFallbackRejectsNonComparableStore(t *testing.T) {
	_, err := tasks.NewEngine(nonComparableLegacyStore{tasks.NewInMemoryStore()}, nil)
	if err == nil || !strings.Contains(err.Error(), "coordination identity") || !strings.Contains(err.Error(), "CoordinationIdentityProvider") {
		t.Fatalf("NewEngine error = %v, want actionable coordination-identity rejection", err)
	}
}

type dynamicallyNonComparableIdentity struct {
	value any
}

type dynamicallyNonComparableIdentityStore struct {
	tasks.TaskStore
	identity any
}

func (s *dynamicallyNonComparableIdentityStore) TaskStoreCoordinationIdentity() any {
	return s.identity
}

func TestCoordinationIdentityProviderRejectsDynamicallyNonComparableIdentity(t *testing.T) {
	for name, identity := range map[string]any{
		"nil":                      nil,
		"typed nil":                (*int)(nil),
		"interface contains slice": dynamicallyNonComparableIdentity{value: []byte{1}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("NewEngine panicked for invalid coordination identity: %v", recovered)
				}
			}()

			store := &dynamicallyNonComparableIdentityStore{TaskStore: tasks.NewInMemoryStore(), identity: identity}
			_, err := tasks.NewEngine(store, nil)
			if err == nil || !strings.Contains(err.Error(), "CoordinationIdentityProvider returned a nil or non-comparable identity") {
				t.Fatalf("NewEngine error = %v, want actionable identity rejection", err)
			}
		})
	}
}

type nonComparableAtomicStore struct {
	tasks.TaskStore
	tasks.AtomicCreateTaskStore
	tasks.AtomicFinalizeTaskStore
	identity *int
	padding  []byte
}

func (s nonComparableAtomicStore) TaskStoreCoordinationIdentity() any { return s.identity }

func TestAtomicNonComparableStoreRoutesPeerCancellation(t *testing.T) {
	base := tasks.NewInMemoryStore()
	identity := new(int)
	store := nonComparableAtomicStore{
		TaskStore:               base,
		AtomicCreateTaskStore:   base.(tasks.AtomicCreateTaskStore),
		AtomicFinalizeTaskStore: base.(tasks.AtomicFinalizeTaskStore),
		identity:                identity,
		padding:                 []byte{1},
	}
	owner, err := tasks.NewEngine(store, &tasks.Options{GenerateID: func() (string, error) { return "noncomparable-atomic", nil }})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := tasks.NewEngine(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	stopped := make(chan struct{})
	created, err := owner.CreateToolTask(context.Background(), tasks.CreateToolCallParams{
		ToolName: "work", AuthContext: "alice",
		Run: func(ctx context.Context) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := peer.DispatchModern(context.Background(), "alice", tasks.MethodCancel, tasks.ModernRequest{TaskID: created.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("peer cancellation did not stop the owning worker")
	}
}
