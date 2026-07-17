package server

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// ErrResourceNotFound lets dynamic resource handlers report a missing concrete
// URI without depending on SDK error types.
var ErrResourceNotFound = errors.New("dockyard/runtime/server: resource not found")

type encodedResult struct {
	mcpsdk.ResultBase
	raw json.RawMessage
}

func (r *encodedResult) MarshalJSON() ([]byte, error) { return r.raw, nil }

func responseSemanticsMiddleware(info Info, opts *Options) mcpsdk.Middleware {
	listPolicy := resourceListCachePolicy(opts)
	serverInfo := protocolcodec.ServerInfo{Name: info.Name, Title: info.Title, Version: info.Version}
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			version := requestProtocolVersion(req)
			presence := &structuredPresenceState{}
			ctx = context.WithValue(ctx, structuredPresenceKey{}, presence)
			readCache := &resourceCacheState{}
			ctx = context.WithValue(ctx, resourceCacheKey{}, readCache)
			result, err := next(ctx, method, req)
			if err != nil {
				if method == "resources/read" && isResourceNotFound(err, resourceURI(req)) {
					return nil, resourceNotFoundError(req, version)
				}
				return result, err
			}
			resourceResult := method == "resources/list" || method == "resources/templates/list" || method == "resources/read"
			if version != protocolcodec.VersionMCP20260728 && (method != "tools/call" || !presence.set) && !resourceResult {
				return result, nil
			}
			raw, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return nil, marshalErr
			}
			if method == "tools/call" && presence.set {
				raw, marshalErr = protocolcodec.EncodeStructuredPresence(raw, presence.present, presence.explicitNull)
				if marshalErr != nil {
					return nil, marshalErr
				}
			}
			if resourceResult {
				cache := listPolicy
				if method == "resources/read" {
					cache = readCache.policy
				}
				raw, marshalErr = protocolcodec.EncodeCacheMetadata(version, raw, protocolcodec.CacheMetadata{
					TTLMs: int64(cache.TTL / time.Millisecond), Scope: normalizedCacheScope(cache.Scope, method == "resources/read"),
				})
				if marshalErr != nil {
					return nil, marshalErr
				}
			}
			raw, marshalErr = protocolcodec.EncodeResultType(version, raw)
			if marshalErr != nil {
				return nil, marshalErr
			}
			// SEP-2575: the SDK annotates serverInfo into each modern result's
			// _meta after the middleware returns, but encodedResult ships
			// pre-baked bytes that shadow that annotation — so Dockyard injects
			// serverInfo here, consistent with how it owns resultType.
			raw, marshalErr = protocolcodec.EncodeServerInfo(version, raw, serverInfo)
			if marshalErr != nil {
				return nil, marshalErr
			}
			return &encodedResult{raw: raw}, nil
		}
	}
}

func requestProtocolVersion(req mcpsdk.Request) protocolcodec.ProtocolVersion {
	if r, ok := req.(interface{ ProtocolVersion() string }); ok && r.ProtocolVersion() == string(protocolcodec.VersionMCP20260728) {
		return protocolcodec.VersionMCP20260728
	}
	return protocolcodec.DefaultVersion
}

func resourceListCachePolicy(opts *Options) CachePolicy {
	if opts == nil {
		return CachePolicy{Scope: CacheScopePublic}
	}
	p := opts.ResourceListCache
	if p.Scope == "" {
		p.Scope = CacheScopePublic
	}
	return p
}

func normalizedCacheScope(scope CacheScope, resourceRead bool) string {
	if scope == "" {
		if resourceRead {
			return string(CacheScopePrivate)
		}
		return string(CacheScopePublic)
	}
	return string(scope)
}

func isResourceNotFound(err error, uri string) bool {
	if errors.Is(err, ErrResourceNotFound) {
		return true
	}
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams || rpcErr.Message != "Resource not found" {
		return false
	}
	var data struct {
		URI string `json:"uri"`
	}
	return json.Unmarshal(rpcErr.Data, &data) == nil && data.URI == uri
}

func resourceNotFoundError(req mcpsdk.Request, version protocolcodec.ProtocolVersion) error {
	uri := ""
	if r, ok := req.(*mcpsdk.ReadResourceRequest); ok && r.Params != nil {
		uri = r.Params.URI
	}
	data, _ := json.Marshal(map[string]string{"uri": uri})
	return &jsonrpc.Error{Code: protocolcodec.ResourceNotFoundCode(version), Message: "Resource not found", Data: data}
}

func resourceURI(req mcpsdk.Request) string {
	if r, ok := req.(*mcpsdk.ReadResourceRequest); ok && r.Params != nil {
		return r.Params.URI
	}
	return ""
}

type structuredPresenceKey struct{}

type structuredPresenceState struct {
	set          bool
	present      bool
	explicitNull bool
}

func setStructuredPresence(ctx context.Context, present, explicitNull bool) {
	if state, ok := ctx.Value(structuredPresenceKey{}).(*structuredPresenceState); ok {
		state.set = true
		state.present = present
		state.explicitNull = explicitNull
	}
}

func structuredPresence(value any, forced bool) (present, explicitNull bool) {
	nilValue := value == nil
	if !nilValue {
		v := reflect.ValueOf(value)
		switch v.Kind() {
		case reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			nilValue = v.IsNil()
		}
	}
	if nilValue {
		return forced, forced
	}
	return true, false
}

type resourceCacheKey struct{}

type resourceCacheState struct{ policy CachePolicy }

func setResourceCache(ctx context.Context, policy CachePolicy) {
	if state, ok := ctx.Value(resourceCacheKey{}).(*resourceCacheState); ok {
		state.policy = policy
	}
}
