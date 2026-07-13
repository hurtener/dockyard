package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// createdTaskMarkerKey is an in-process handoff from the typed tool adapter to
// receiving middleware. The middleware always removes it by replacing the SDK
// CallToolResult; it is never an extension wire key.
const createdTaskMarkerKey = "dockyard.internal/created-task"

const admissionCleanupTimeout = time.Second

type sdkTaskResult struct {
	mcpsdk.ResultBase
	raw json.RawMessage
}

func (r *sdkTaskResult) MarshalJSON() ([]byte, error) { return r.raw, nil }

func createdTaskResultMiddleware() mcpsdk.Middleware {
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			ctx, admission := tasks.WithDeferredAdmission(ctx)
			cleanup := func(run func(context.Context) error) error {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), admissionCleanupTimeout)
				defer cancel()
				return run(cleanupCtx)
			}
			abort := func(err error) (mcpsdk.Result, error) {
				abortErr := cleanup(admission.Abort)
				if abortErr == nil {
					return nil, err
				}
				return nil, errors.Join(err, abortErr)
			}
			result, err := next(ctx, method, req)
			if err != nil {
				return abort(err)
			}
			if result == nil {
				return abort(errors.New("dockyard/runtime/server: tools/call returned a nil result"))
			}
			callResult, ok := result.(*mcpsdk.CallToolResult)
			if !ok || callResult.Meta == nil {
				if abortErr := cleanup(admission.Abort); abortErr != nil {
					return nil, abortErr
				}
				return result, nil
			}
			created, ok := callResult.Meta[createdTaskMarkerKey].(*tasks.CreatedTask)
			if !ok || created == nil {
				if abortErr := cleanup(admission.Abort); abortErr != nil {
					return nil, abortErr
				}
				return result, nil
			}
			serverReq, ok := req.(interface {
				ProtocolVersion() string
				ClientCapabilities() *mcpsdk.ClientCapabilities
			})
			if !ok {
				return abort(fmt.Errorf("dockyard/runtime/server: tools/call has unexpected request type %T", req))
			}
			canonical, registered := admission.Canonical(created.ID)
			if !registered {
				return abort(fmt.Errorf("dockyard/runtime/server: CreatedTask %q was not created during this tools/call", created.ID))
			}
			modern := serverReq.ProtocolVersion() >= string(protocolcodec.VersionMCP20260728)
			if modern && !clientSupportsTasks(serverReq.ClientCapabilities()) {
				if !canonical.Required {
					delete(callResult.Meta, createdTaskMarkerKey)
					if len(callResult.Meta) == 0 {
						callResult.Meta = nil
					}
					if abortErr := cleanup(admission.Abort); abortErr != nil {
						return nil, abortErr
					}
					return callResult, nil
				}
				return abort(missingTasksCapabilityError())
			}
			var admitted bool
			admitErr := cleanup(func(cleanupCtx context.Context) error {
				var err error
				canonical, admitted, err = admission.AdmitCanonical(cleanupCtx, created.ID)
				return err
			})
			if admitErr != nil {
				return nil, admitErr
			}
			if !admitted {
				return abort(fmt.Errorf("dockyard/runtime/server: CreatedTask %q was not created during this tools/call", created.ID))
			}
			raw, encodeErr := encodeCreatedTaskResult(modern, &canonical)
			if encodeErr != nil {
				return nil, encodeErr
			}
			return &sdkTaskResult{raw: raw}, nil
		}
	}
}

func encodeCreatedTaskResult(modern bool, created *tasks.CreatedTask) (json.RawMessage, error) {
	codec := protocolcodec.CodecFor(protocolcodec.DefaultVersion)
	if modern {
		codec = protocolcodec.CodecFor(protocolcodec.VersionMCP20260728)
	}
	return codec.EncodeCreateTaskResult(protocolcodec.CreateTaskResult{Task: protocolcodec.Task{
		ID: created.ID, Status: protocolcodec.TaskStatus(created.Status), StatusMessage: created.StatusMessage,
		CreatedAt: created.CreatedAt, LastUpdatedAt: created.LastUpdatedAt,
		TTL: created.TTL, PollInterval: created.PollInterval,
	}})
}

func missingTasksCapabilityError() error {
	data, err := json.Marshal(map[string]any{
		"requiredCapabilities": map[string]any{
			"extensions": map[string]any{protocolcodec.ModernTasksExtension: map[string]any{}},
		},
	})
	if err != nil {
		return err
	}
	return &jsonrpc.Error{Code: mcpsdk.CodeMissingRequiredClientCapabilities,
		Message: "io.modelcontextprotocol/tasks client capability required but not declared", Data: data}
}

func clientSupportsTasks(c *mcpsdk.ClientCapabilities) bool {
	if c == nil {
		return false
	}
	_, ok := c.Extensions[protocolcodec.ModernTasksExtension]
	return ok
}
