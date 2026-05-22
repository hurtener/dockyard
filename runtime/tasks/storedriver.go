package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/store"
)

// This file is the durable TaskStore driver (RFC §8.5, §13). It is a typed
// facade over the Store seam — not a separate Store driver — exactly the
// sub-store pattern the Store package documents (D-025): the TaskStore layers
// typed task structure over the generic namespaced KV primitive, owning its own
// forward-only migration. It therefore inherits every Store driver: the durable
// driver runs over the modernc.org/sqlite Store for persistent HTTP/Portico
// apps and over the in-memory Store for tests, with no new CGo dependency.
//
// The TaskStore conformance suite (runtime/tasks/taskstoretest) proves the
// durable facade against every backing — the CLAUDE.md §9 "proven by the shared
// conformance suite" rule, applied at the sub-store layer. See D-070.

// taskStoreNamespace is the Store KV namespace the durable TaskStore keeps its
// task rows in. One key per task: the task ID maps to the JSON-encoded row.
const taskStoreNamespace = "dockyard_tasks"

// taskStoreMigrationID is the durable TaskStore's single forward-only
// migration. The TaskStore rows are schemaless JSON KV values, so the migration
// only seeds a schema-version marker — but registering it makes the TaskStore a
// first-class migration owner (every future TaskStore schema step appends here,
// never edits this one; CLAUDE.md §9).
const taskStoreMigrationID = "0001_tasks_init"

// taskStoreSchemaVersion is the on-disk row-format version. A row decoded with
// a newer version than the binary understands is a hard error rather than a
// silent misread.
const taskStoreSchemaVersion = 1

// Migrations returns the durable TaskStore's forward-only migrations as a
// caller-owned [store.MigrationSet] (D-073). An application composes this set
// with any other sub-store's set ([store.MigrationSet.Extend]) and passes the
// result to Store.Migrate, then constructs the TaskStore with [NewStore].
//
// Migrations returns a fresh set on every call — there is no process-global
// registry — so it is safe to call concurrently from independent test
// fixtures, each migrating its own store with no shared state and no external
// locking. This replaces the former RegisterMigrations, which mutated a
// process-global registry and forced callers to serialize their fixtures.
func Migrations() *store.MigrationSet {
	return store.NewMigrationSet().MustAdd(store.Migration{
		ID: taskStoreMigrationID,
		Up: func(_ context.Context, tx store.Tx) error {
			marker, err := json.Marshal(map[string]int{"schemaVersion": taskStoreSchemaVersion})
			if err != nil {
				return err
			}
			return tx.Put(taskStoreNamespace, schemaMarkerKey, marker)
		},
	})
}

// taskRow is the durable on-disk shape of a TaskRecord. It is deliberately a
// distinct struct from TaskRecord (and from protocolcodec.Task): the durable
// format is owned by this driver and versioned independently of the runtime
// type, and no raw experimental protocol struct is persisted (P3). Timestamps
// are RFC3339Nano strings for stable, human-readable rows.
type taskRow struct {
	SchemaVersion int                      `json:"schemaVersion"`
	ID            string                   `json:"id"`
	Status        protocolcodec.TaskStatus `json:"status"`
	StatusMessage string                   `json:"statusMessage,omitempty"`
	CreatedAt     string                   `json:"createdAt"`
	UpdatedAt     string                   `json:"updatedAt"`
	RequestedTTL  *int64                   `json:"requestedTtl,omitempty"`
	TTL           *int64                   `json:"ttl,omitempty"`
	ExpiresAt     string                   `json:"expiresAt,omitempty"`
	PollInterval  *int64                   `json:"pollInterval,omitempty"`
	Method        string                   `json:"method,omitempty"`
	ToolName      string                   `json:"toolName,omitempty"`
	AuthContext   string                   `json:"authContext,omitempty"`
	ResultPayload json.RawMessage          `json:"resultPayload,omitempty"`
	ResultErr     string                   `json:"resultErr,omitempty"`
}

func rowFromRecord(r TaskRecord) taskRow {
	row := taskRow{
		SchemaVersion: taskStoreSchemaVersion,
		ID:            r.ID,
		Status:        r.Status,
		StatusMessage: r.StatusMessage,
		CreatedAt:     r.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:     r.UpdatedAt.UTC().Format(time.RFC3339Nano),
		RequestedTTL:  r.RequestedTTL,
		TTL:           r.TTL,
		PollInterval:  r.PollInterval,
		Method:        r.Method,
		ToolName:      r.ToolName,
		AuthContext:   r.AuthContext,
		ResultPayload: r.Result.Payload,
		ResultErr:     r.Result.Err,
	}
	if !r.ExpiresAt.IsZero() {
		row.ExpiresAt = r.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return row
}

func recordFromRow(row taskRow) (TaskRecord, error) {
	if row.SchemaVersion > taskStoreSchemaVersion {
		return TaskRecord{}, fmt.Errorf(
			"dockyard/runtime/tasks: task row %q has schema version %d, this binary understands %d",
			row.ID, row.SchemaVersion, taskStoreSchemaVersion)
	}
	created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return TaskRecord{}, fmt.Errorf("dockyard/runtime/tasks: task row %q createdAt: %w", row.ID, err)
	}
	updated, err := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	if err != nil {
		return TaskRecord{}, fmt.Errorf("dockyard/runtime/tasks: task row %q updatedAt: %w", row.ID, err)
	}
	rec := TaskRecord{
		ID:            row.ID,
		Status:        row.Status,
		StatusMessage: row.StatusMessage,
		CreatedAt:     created.UTC(),
		UpdatedAt:     updated.UTC(),
		RequestedTTL:  row.RequestedTTL,
		TTL:           row.TTL,
		PollInterval:  row.PollInterval,
		Method:        row.Method,
		ToolName:      row.ToolName,
		AuthContext:   row.AuthContext,
		Result:        TaskResult{Payload: row.ResultPayload, Err: row.ResultErr},
	}
	if row.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339Nano, row.ExpiresAt)
		if err != nil {
			return TaskRecord{}, fmt.Errorf("dockyard/runtime/tasks: task row %q expiresAt: %w", row.ID, err)
		}
		rec.ExpiresAt = expires.UTC()
	}
	return rec, nil
}

// durableStore is the Store-seam-backed TaskStore driver. It is safe for
// concurrent use: every method runs inside a Store transaction, and the Store
// seam itself guarantees concurrency-safety (RFC §13). The "__schema__" key is
// reserved and never returned as a task.
type durableStore struct {
	st store.Store
}

// schemaMarkerKey is the reserved key holding the schema-version marker; it is
// never a task ID (a task ID is "task_" + 32 hex chars — see CryptoID).
const schemaMarkerKey = "__schema__"

// NewStore returns the durable TaskStore driver layered over s — the Store-seam
// TaskStore Phase 14 supplies behind the Phase 13 seam (RFC §8.5). The caller
// is responsible for having run [Migrations] through s.Migrate before
// constructing tasks against the store; NewStore itself does no migration so
// migration timing stays under application control (matching store.Open).
//
// s must be non-nil. The durable store carries TTL/expiry fields and an
// auth-context-scoped listing; the lifecycle controls (clamping, the purge
// sweep) live in the Engine and lifecycle.go, not the driver.
func NewStore(s store.Store) (TaskStore, error) {
	if s == nil {
		return nil, errors.New("dockyard/runtime/tasks: NewStore requires a non-nil store.Store")
	}
	return &durableStore{st: s}, nil
}

func (d *durableStore) Create(ctx context.Context, rec TaskRecord) error {
	if rec.ID == "" {
		return fmt.Errorf("%w: task record has empty ID", ErrInvalidParams)
	}
	if rec.ID == schemaMarkerKey {
		return fmt.Errorf("%w: task ID %q is reserved", ErrInvalidParams, rec.ID)
	}
	if rec.Status != protocolcodec.TaskWorking {
		return transitionError("", rec.Status)
	}
	row, err := json.Marshal(rowFromRecord(rec))
	if err != nil {
		return fmt.Errorf("dockyard/runtime/tasks: encode task row: %w", err)
	}
	return d.st.Update(ctx, func(tx store.Tx) error {
		if _, err := tx.Get(taskStoreNamespace, rec.ID); err == nil {
			return fmt.Errorf("dockyard/runtime/tasks: task %q already exists", rec.ID)
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return tx.Put(taskStoreNamespace, rec.ID, row)
	})
}

func (d *durableStore) Get(ctx context.Context, id string) (TaskRecord, error) {
	var rec TaskRecord
	err := d.st.View(ctx, func(tx store.Tx) error {
		got, err := readRow(tx, id)
		if err != nil {
			return err
		}
		rec = got
		return nil
	})
	return rec, err
}

// readRow loads and decodes one task row, mapping a missing key to the typed
// ErrTaskNotFound the engine expects.
func readRow(tx store.Tx, id string) (TaskRecord, error) {
	raw, err := tx.Get(taskStoreNamespace, id)
	if errors.Is(err, store.ErrNotFound) {
		return TaskRecord{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
	}
	if err != nil {
		return TaskRecord{}, err
	}
	var row taskRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return TaskRecord{}, fmt.Errorf("dockyard/runtime/tasks: decode task row %q: %w", id, err)
	}
	return recordFromRow(row)
}

func (d *durableStore) Transition(
	ctx context.Context, id string, to protocolcodec.TaskStatus, msg string,
) (TaskRecord, error) {
	var out TaskRecord
	err := d.st.Update(ctx, func(tx store.Tx) error {
		rec, err := readRow(tx, id)
		if err != nil {
			return err
		}
		// A redundant write of the status the task already holds is a no-op
		// success — the cooperative-cancellation rule (brief 02 §4.7): a late
		// terminal transition onto an already-terminal task must not error. A
		// redundant non-terminal write (working→working) refreshes the status
		// message — the TaskHandle progress-reporting path (RFC §8.4).
		if rec.Status == to {
			if !to.IsTerminal() && msg != "" && msg != rec.StatusMessage {
				rec.StatusMessage = msg
				rec.UpdatedAt = time.Now().UTC()
				row, err := json.Marshal(rowFromRecord(rec))
				if err != nil {
					return fmt.Errorf("dockyard/runtime/tasks: encode task row: %w", err)
				}
				if err := tx.Put(taskStoreNamespace, id, row); err != nil {
					return err
				}
			}
			out = rec
			return nil
		}
		if !rec.Status.CanTransitionTo(to) {
			return transitionError(rec.Status, to)
		}
		rec.Status = to
		rec.StatusMessage = msg
		rec.UpdatedAt = time.Now().UTC()
		row, err := json.Marshal(rowFromRecord(rec))
		if err != nil {
			return fmt.Errorf("dockyard/runtime/tasks: encode task row: %w", err)
		}
		if err := tx.Put(taskStoreNamespace, id, row); err != nil {
			return err
		}
		out = rec
		return nil
	})
	return out, err
}

func (d *durableStore) SetResult(ctx context.Context, id string, result TaskResult) error {
	return d.st.Update(ctx, func(tx store.Tx) error {
		rec, err := readRow(tx, id)
		if err != nil {
			return err
		}
		rec.Result = result
		row, err := json.Marshal(rowFromRecord(rec))
		if err != nil {
			return fmt.Errorf("dockyard/runtime/tasks: encode task row: %w", err)
		}
		return tx.Put(taskStoreNamespace, id, row)
	})
}

// scanRecords returns every task record in the store, in stable order
// (lexicographic by task ID — Store.Scan orders by key), excluding the reserved
// schema marker. It is the shared body of List, ListByAuthContext and
// PurgeExpired; a TaskStore page is small enough that an in-memory sort is
// cheaper than a typed index, and the Store seam exposes only a KV primitive
// (D-025).
func scanRecords(tx store.Tx) ([]TaskRecord, error) {
	kvs, err := tx.Scan(taskStoreNamespace, "")
	if err != nil {
		return nil, err
	}
	out := make([]TaskRecord, 0, len(kvs))
	for _, kv := range kvs {
		if kv.Key == schemaMarkerKey {
			continue
		}
		var row taskRow
		if err := json.Unmarshal(kv.Value, &row); err != nil {
			return nil, fmt.Errorf("dockyard/runtime/tasks: decode task row %q: %w", kv.Key, err)
		}
		rec, err := recordFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	// Order by creation time so a page is stable and chronological; ties break
	// on ID so the order is total and the cursor offset is deterministic.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (d *durableStore) List(ctx context.Context, cursor string, limit int) ([]TaskRecord, string, error) {
	return d.listFiltered(ctx, cursor, limit, func(TaskRecord) bool { return true })
}

func (d *durableStore) ListByAuthContext(
	ctx context.Context, authContext, cursor string, limit int,
) ([]TaskRecord, string, error) {
	return d.listFiltered(ctx, cursor, limit, func(r TaskRecord) bool {
		return r.AuthContext == authContext
	})
}

// listFiltered pages over the records matching keep, in stable order. The
// cursor is a 1-past-the-end offset into the filtered sequence — opaque to the
// caller, decoded only here (the same encoding the in-memory driver uses).
func (d *durableStore) listFiltered(
	ctx context.Context, cursor string, limit int, keep func(TaskRecord) bool,
) ([]TaskRecord, string, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	var all []TaskRecord
	if err := d.st.View(ctx, func(tx store.Tx) error {
		recs, err := scanRecords(tx)
		if err != nil {
			return err
		}
		all = recs
		return nil
	}); err != nil {
		return nil, "", err
	}
	filtered := all[:0:0]
	for _, r := range all {
		if keep(r) {
			filtered = append(filtered, r)
		}
	}
	start := 0
	if cursor != "" {
		i, err := decodeCursor(cursor)
		if err != nil || i < 0 || i > len(filtered) {
			return nil, "", fmt.Errorf("%w: bad cursor", ErrInvalidParams)
		}
		start = i
	}
	end := start + limit
	next := ""
	if end < len(filtered) {
		next = encodeCursor(end)
	} else {
		end = len(filtered)
	}
	page := make([]TaskRecord, 0, end-start)
	page = append(page, filtered[start:end]...)
	return page, next, nil
}

func (d *durableStore) Delete(ctx context.Context, id string) error {
	return d.st.Update(ctx, func(tx store.Tx) error {
		return tx.Delete(taskStoreNamespace, id)
	})
}

func (d *durableStore) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	purged := 0
	err := d.st.Update(ctx, func(tx store.Tx) error {
		recs, err := scanRecords(tx)
		if err != nil {
			return err
		}
		purged = 0
		for _, rec := range recs {
			if rec.IsExpired(now) {
				if err := tx.Delete(taskStoreNamespace, rec.ID); err != nil {
					return err
				}
				purged++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return purged, nil
}
