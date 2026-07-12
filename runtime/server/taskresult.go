package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/tasks"
)

// createdTaskMarkerKey is an in-process handoff from the typed tool adapter to
// receiving middleware. The middleware always removes it by replacing the SDK
// CallToolResult; it is never an extension wire key.
const createdTaskMarkerKey = "dockyard.internal/created-task"

type sdkTaskResult struct {
	mcpsdk.ResultBase
	raw json.RawMessage
}

func (r *sdkTaskResult) MarshalJSON() ([]byte, error) { return r.raw, nil }

func createdTaskResultMiddleware() mcpsdk.Middleware {
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil || method != "tools/call" || result == nil {
				return result, err
			}
			callResult, ok := result.(*mcpsdk.CallToolResult)
			if !ok || callResult.Meta == nil {
				return result, nil
			}
			created, ok := callResult.Meta[createdTaskMarkerKey].(*tasks.CreatedTask)
			if !ok || created == nil {
				return result, nil
			}
			serverReq, ok := req.(interface {
				ProtocolVersion() string
				ClientCapabilities() *mcpsdk.ClientCapabilities
			})
			if !ok {
				return nil, fmt.Errorf("dockyard/runtime/server: tools/call has unexpected request type %T", req)
			}
			modern := serverReq.ProtocolVersion() >= string(protocolcodec.VersionMCP20260728)
			if modern && !clientSupportsTasks(serverReq.ClientCapabilities()) {
				if !created.Required {
					delete(callResult.Meta, createdTaskMarkerKey)
					if len(callResult.Meta) == 0 {
						callResult.Meta = nil
					}
					return callResult, nil
				}
				data, marshalErr := json.Marshal(map[string]any{
					"requiredCapabilities": map[string]any{
						"extensions": map[string]any{protocolcodec.ModernTasksExtension: map[string]any{}},
					},
				})
				if marshalErr != nil {
					return nil, marshalErr
				}
				return nil, &jsonrpc.Error{Code: mcpsdk.CodeMissingRequiredClientCapabilities,
					Message: "io.modelcontextprotocol/tasks client capability required but not declared", Data: data}
			}
			codec := protocolcodec.CodecFor(protocolcodec.DefaultVersion)
			if modern {
				codec = protocolcodec.CodecFor(protocolcodec.VersionMCP20260728)
			}
			raw, encodeErr := codec.EncodeCreateTaskResult(protocolcodec.CreateTaskResult{Task: protocolcodec.Task{
				ID: created.ID, Status: protocolcodec.TaskStatus(created.Status), StatusMessage: created.StatusMessage,
				CreatedAt: created.CreatedAt, LastUpdatedAt: created.LastUpdatedAt,
				TTL: created.TTL, PollInterval: created.PollInterval,
			}})
			if encodeErr != nil {
				return nil, encodeErr
			}
			return &sdkTaskResult{raw: raw}, nil
		}
	}
}

func clientSupportsTasks(c *mcpsdk.ClientCapabilities) bool {
	if c == nil {
		return false
	}
	_, ok := c.Extensions[protocolcodec.ModernTasksExtension]
	return ok
}
