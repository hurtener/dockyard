package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// This file holds the task security model (RFC §8.5, §15; brief 02 §4.5):
// auth-context binding of tasks/get|result|cancel and the auth-scoped
// tasks/list. Crypto-strong task IDs live in id.go (CryptoID, 128-bit
// crypto/rand) — kept from Phase 13, confirmed sufficient here.
//
// Auth-context binding closes the brief 02 §4.5 sharp edge: with an auth
// context, a task created under one context must not be reachable from another.
// The rejection is deliberately indistinguishable from "task not found" — the
// receiver must not leak the existence of another context's task.

// DispatchAs routes one tasks/* JSON-RPC request on behalf of the requestor
// identified by authContext, enforcing auth-context binding (RFC §8.5):
//
//   - tasks/get, tasks/result, tasks/cancel reject a task that exists under a
//     different auth context with a typed rejection that does not reveal the
//     task's existence (it is reported exactly as a missing task).
//   - tasks/list is scoped to authContext — the requestor sees only its own
//     tasks — and is served only when the engine can identify requestors.
//
// authContext is the opaque requestor-identity token; empty means an
// unauthenticated requestor. DispatchAs is the auth-aware entry point the
// transport mount uses; the bare [Engine.Dispatch] is equivalent to DispatchAs
// with an empty context and is kept for the inspector and unauthenticated
// stdio.
func (e *Engine) DispatchAs(
	ctx context.Context, authContext, method string, params json.RawMessage,
) (json.RawMessage, error) {
	switch method {
	case MethodGet, MethodResult, MethodCancel:
		// Bind the target task to the caller's context before serving the
		// method. A task under another context is reported as not found.
		if err := e.bindTaskAccess(ctx, authContext, params); err != nil {
			return nil, err
		}
		return e.Dispatch(ctx, method, params)
	case MethodList:
		if !e.listOn {
			return nil, fmt.Errorf("%w: %q (tasks/list not advertised)", ErrUnknownMethod, method)
		}
		return e.handleListScoped(ctx, authContext, params)
	default:
		return e.Dispatch(ctx, method, params)
	}
}

// bindTaskAccess loads the task named by params and rejects access when its
// AuthContext does not match the caller's. The rejection is ErrCrossContext,
// which carries the same "task not found" message and the same JSON-RPC code as
// ErrTaskNotFound — a host cannot tell a cross-context task from a missing one
// (brief 02 §4.5 "Avoid": do not leak another context's task existence).
func (e *Engine) bindTaskAccess(ctx context.Context, authContext string, params json.RawMessage) error {
	p, err := e.codec.DecodeTaskIDParams(params)
	if err != nil {
		return err
	}
	rec, err := e.store.Get(ctx, p.ID)
	if err != nil {
		return err
	}
	if rec.AuthContext != authContext {
		e.log.WarnContext(ctx, "cross-context task access rejected",
			slog.String("taskId", p.ID))
		return fmt.Errorf("%w: %q", ErrCrossContext, p.ID)
	}
	return nil
}

// handleListScoped serves tasks/list scoped to authContext — the only listing a
// receiver that identifies requestors serves (RFC §8.5). It pages over the
// caller's own tasks through the auth-scoped store seam.
func (e *Engine) handleListScoped(
	ctx context.Context, authContext string, params json.RawMessage,
) (json.RawMessage, error) {
	p, err := e.codec.DecodeListTasksParams(params)
	if err != nil {
		return nil, err
	}
	recs, next, err := e.store.ListByAuthContext(ctx, authContext, p.Cursor, 0)
	if err != nil {
		return nil, err
	}
	return e.encodeList(recs, next)
}
