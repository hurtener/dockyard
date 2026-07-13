package tasks

import (
	"context"
	"errors"
	"sync"
	"time"
)

type deferredAdmissionKey struct{}

type deferredTaskStart struct {
	canonical CreatedTask
	start     func(context.Context) error
	abort     func(context.Context) error
}

// DeferredAdmission delays task handler goroutines created during one server
// tool call until the server has validated the final response.
type DeferredAdmission struct {
	mu      sync.Mutex
	pending map[string]deferredTaskStart
	done    bool
}

// WithDeferredAdmission returns a context on which CreateToolTask defers work
// and a controller used by the server edge to start exactly the accepted task.
func WithDeferredAdmission(ctx context.Context) (context.Context, *DeferredAdmission) {
	a := &DeferredAdmission{pending: make(map[string]deferredTaskStart)}
	return context.WithValue(ctx, deferredAdmissionKey{}, a), a
}

func deferredAdmissionFromContext(ctx context.Context) *DeferredAdmission {
	a, _ := ctx.Value(deferredAdmissionKey{}).(*DeferredAdmission)
	return a
}

func withoutDeferredAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, deferredAdmissionKey{}, (*DeferredAdmission)(nil))
}

func (a *DeferredAdmission) add(id string, canonical CreatedTask, start func(context.Context) error, abort func(context.Context) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return errors.Join(errors.New("task admission already resolved"), abort(context.Background()))
	}
	a.pending[id] = deferredTaskStart{canonical: cloneCreatedTask(canonical), start: start, abort: abort}
	return nil
}

// Canonical returns the immutable task projection captured for id without
// admitting it. It returns false when id was not created under this admission.
func (a *DeferredAdmission) Canonical(id string) (CreatedTask, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pending, ok := a.pending[id]
	if !ok || a.done {
		return CreatedTask{}, false
	}
	return cloneCreatedTask(pending.canonical), true
}

// Admit starts id and aborts every other task created during the call. It
// returns false when id was not created under this admission.
func (a *DeferredAdmission) Admit(ctx context.Context, id string) (bool, error) {
	_, ok, err := a.AdmitCanonical(ctx, id)
	return ok, err
}

// AdmitCanonical starts id, aborts every other task created during the call,
// and returns the immutable task projection captured by the engine at creation.
// Sibling cleanup shares ctx. If admitting the selected task fails, its rollback
// receives one fresh detached context bounded by ctx's original deadline budget.
func (a *DeferredAdmission) AdmitCanonical(ctx context.Context, id string) (CreatedTask, bool, error) {
	rollbackTimeout := time.Duration(0)
	if deadline, ok := ctx.Deadline(); ok {
		rollbackTimeout = time.Until(deadline)
	}
	return a.admitCanonical(ctx, id, func(abort func(context.Context) error) error {
		if rollbackTimeout <= 0 {
			return abort(ctx)
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		return abort(rollbackCtx)
	})
}

func (a *DeferredAdmission) admitCanonical(
	ctx context.Context, id string, rollback func(func(context.Context) error) error,
) (CreatedTask, bool, error) {
	a.mu.Lock()
	if a.done {
		a.mu.Unlock()
		return CreatedTask{}, false, nil
	}
	a.done = true
	chosen, ok := a.pending[id]
	delete(a.pending, id)
	rejected := a.pending
	a.pending = nil
	a.mu.Unlock()
	err := abortDeferredTasks(ctx, rejected)
	if err != nil || !ok {
		if ok {
			err = errors.Join(err, rollback(chosen.abort))
		}
		return CreatedTask{}, ok, err
	}
	if startErr := chosen.start(ctx); startErr != nil {
		return CreatedTask{}, true, errors.Join(startErr, rollback(chosen.abort))
	}
	return cloneCreatedTask(chosen.canonical), true, nil
}

// Abort removes every deferred task without starting its handler.
func (a *DeferredAdmission) Abort(ctx context.Context) error {
	a.mu.Lock()
	if a.done {
		a.mu.Unlock()
		return nil
	}
	a.done = true
	pending := a.pending
	a.pending = nil
	a.mu.Unlock()
	return abortDeferredTasks(ctx, pending)
}

func abortDeferredTasks(ctx context.Context, pending map[string]deferredTaskStart) error {
	if len(pending) == 0 {
		return nil
	}
	errs := make(chan error, len(pending))
	var wg sync.WaitGroup
	for _, task := range pending {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- task.abort(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	var err error
	for abortErr := range errs {
		err = errors.Join(err, abortErr)
	}
	return err
}

func cloneCreatedTask(task CreatedTask) CreatedTask {
	task.TTL = cloneInt64(task.TTL)
	task.PollInterval = cloneInt64(task.PollInterval)
	return task
}
