package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/server"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

func TestCachePolicyValidation(t *testing.T) {
	t.Parallel()
	for _, policy := range []server.CachePolicy{
		{TTL: -time.Millisecond, Scope: server.CacheScopePublic},
		{TTL: time.Nanosecond, Scope: server.CacheScopePublic},
		{Scope: "shared"},
	} {
		if _, err := server.New(server.Info{Name: "cache", Version: "1"}, &server.Options{ResourceListCache: policy}); err == nil {
			t.Fatalf("New accepted invalid policy %#v", policy)
		}
	}
	if strconv.IntSize == 32 {
		policy := server.CachePolicy{TTL: time.Duration(1<<31) * time.Millisecond, Scope: server.CacheScopePublic}
		if _, err := server.New(server.Info{Name: "cache", Version: "1"}, &server.Options{ResourceListCache: policy}); err == nil {
			t.Fatal("New accepted a TTL that overflows int on 32-bit platforms")
		}
	}
}

func TestStructuredOutputSupportsPrimitiveAndExplicitNull(t *testing.T) {
	t.Parallel()
	type input struct {
		Null  bool `json:"null"`
		Array bool `json:"array"`
	}
	s, err := server.New(server.Info{Name: "structured", Version: "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "value"}, nil, nil,
		func(_ context.Context, in input) (server.ToolOutput[any], error) {
			if in.Null {
				return server.ToolOutput[any]{Structured: nil, StructuredPresent: true}, nil
			}
			if in.Array {
				return server.ToolOutput[any]{Structured: []any{"a", 2}}, nil
			}
			return server.ToolOutput[any]{Structured: 42}, nil
		}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	for _, tc := range []struct {
		args     string
		want     string
		fallback string
	}{{`{"null":false,"array":false}`, `42`, `42`}, {`{"null":true,"array":false}`, `null`, `null`}, {`{"null":false,"array":true}`, `["a",2]`, `["a",2]`}} {
		raw := modernRPC(t, ts, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"value","arguments":`+tc.args+`,"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
		var envelope struct {
			Result map[string]json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if got, ok := envelope.Result["structuredContent"]; !ok || string(got) != tc.want {
			t.Fatalf("structuredContent = %s, present %v, want %s: %s", got, ok, tc.want, raw)
		}
		assertTextFallback(t, envelope.Result["content"], tc.fallback)
	}
}

func TestStructuredPresentTypedNilKinds(t *testing.T) {
	t.Parallel()
	type input struct{}
	type output struct {
		Value string `json:"value"`
	}
	s, err := server.New(server.Info{Name: "typed-nil", Version: "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "pointer-null"}, nil, nil, func(context.Context, input) (server.ToolOutput[*output], error) {
		return server.ToolOutput[*output]{StructuredPresent: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "map-null"}, nil, nil, func(context.Context, input) (server.ToolOutput[map[string]string], error) {
		return server.ToolOutput[map[string]string]{StructuredPresent: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "slice-null"}, nil, nil, func(context.Context, input) (server.ToolOutput[[]string], error) {
		return server.ToolOutput[[]string]{StructuredPresent: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.AddToolWithSchemas(s, server.ToolDef{Name: "pointer-absent"}, nil, nil, func(context.Context, input) (server.ToolOutput[*output], error) {
		return server.ToolOutput[*output]{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	for _, name := range []string{"pointer-null", "map-null", "slice-null"} {
		result := callToolResult(t, ts, name)
		if got, ok := result["structuredContent"]; !ok || string(got) != "null" {
			t.Fatalf("%s structuredContent = %s, present %v", name, got, ok)
		}
		assertTextFallback(t, result["content"], "null")
	}
	absent := callToolResult(t, ts, "pointer-absent")
	if _, ok := absent["structuredContent"]; ok {
		t.Fatalf("absent typed nil emitted structuredContent: %s", absent["structuredContent"])
	}
	assertNoTextFallback(t, absent["content"])
}

func TestModernResourceSemanticsRealHTTP(t *testing.T) {
	t.Parallel()
	s, err := server.New(server.Info{Name: "resources", Version: "1"}, &server.Options{
		ResourceListCache: server.CachePolicy{TTL: 2 * time.Second, Scope: server.CacheScopePrivate},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddResource(server.ResourceDef{URI: "test://present", Name: "present"}, func(context.Context, string) (server.ResourceContent, error) {
		return server.ResourceContent{Text: "ok", Cache: server.CachePolicy{TTL: time.Second, Scope: server.CacheScopePublic}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddResource(server.ResourceDef{URI: "test://private-default", Name: "private-default"}, func(context.Context, string) (server.ResourceContent, error) {
		return server.ResourceContent{Text: "private"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddResourceTemplate(server.ResourceTemplateDef{URITemplate: "test://dynamic/{id}", Name: "dynamic"}, func(context.Context, string) (server.ResourceContent, error) {
		return server.ResourceContent{}, errors.Join(server.ErrResourceNotFound, errors.New("catalog miss"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddResourceTemplate(server.ResourceTemplateDef{URITemplate: "test://invalid/{id}", Name: "invalid"}, func(context.Context, string) (server.ResourceContent, error) {
		return server.ResourceContent{}, &jsonrpc.Error{Code: -32602, Message: "ordinary invalid params"}
	}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Stateless20260728, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	list := modernRPC(t, ts, `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	assertResultCache(t, list, 2000, "private")
	templates := modernRPC(t, ts, `{"jsonrpc":"2.0","id":1,"method":"resources/templates/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	assertResultCache(t, templates, 2000, "private")
	read := modernRPC(t, ts, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"test://present","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	assertResultCache(t, read, 1000, "public")
	privateRead := modernRPC(t, ts, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"test://private-default","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	assertResultCache(t, privateRead, 0, "private")
	for _, uri := range []string{"test://unregistered", "test://dynamic/missing"} {
		raw := modernRPC(t, ts, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"`+uri+`","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
		assertErrorCode(t, raw, -32602)
	}
	invalid := modernRPC(t, ts, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"test://invalid/missing","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	assertError(t, invalid, -32602, "ordinary invalid params")
}

func TestLegacyResourceResponseOmitsCacheAndUsesLegacyMissingCode(t *testing.T) {
	t.Parallel()
	s, err := server.New(server.Info{Name: "legacy", Version: "1"}, &server.Options{ResourceListCache: server.CachePolicy{TTL: time.Second, Scope: server.CacheScopePublic}})
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&server.HTTPOptions{ProtocolMode: server.Legacy, Security: server.DefaultHTTPSecurity()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	init := legacyRPC(t, ts, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	session := init.header.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("missing session id")
	}
	list := legacyRPC(t, ts, session, `{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}`).body
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(list, &envelope); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope["result"], []byte(`"ttlMs"`)) || bytes.Contains(envelope["result"], []byte(`"cacheScope"`)) {
		t.Fatalf("legacy cache metadata leaked: %s", list)
	}
	templates := legacyRPC(t, ts, session, `{"jsonrpc":"2.0","id":4,"method":"resources/templates/list","params":{}}`).body
	if err := json.Unmarshal(templates, &envelope); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope["result"], []byte(`"ttlMs"`)) || bytes.Contains(envelope["result"], []byte(`"cacheScope"`)) {
		t.Fatalf("legacy template cache metadata leaked: %s", templates)
	}
	missing := legacyRPC(t, ts, session, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"test://missing"}}`).body
	assertErrorCode(t, missing, -32002)
}

type rpcResponse struct {
	header http.Header
	body   []byte
}

func modernRPC(t *testing.T, ts *httptest.Server, body string) []byte {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	var frame struct {
		Method string `json:"method"`
		Params struct {
			URI  string `json:"uri"`
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &frame); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Method", frame.Method)
	if frame.Params.URI != "" {
		req.Header.Set("Mcp-Name", frame.Params.URI)
	} else if frame.Params.Name != "" {
		req.Header.Set("Mcp-Name", frame.Params.Name)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	return unwrapRPC(raw)
}

func legacyRPC(t *testing.T, ts *httptest.Server, session, body string) rpcResponse {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return rpcResponse{header: resp.Header.Clone(), body: unwrapRPC(raw)}
}

func unwrapRPC(raw []byte) []byte {
	text := string(raw)
	if i := strings.Index(text, "data: "); i >= 0 {
		line := text[i+len("data: "):]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		return []byte(line)
	}
	return raw
}

func assertResultCache(t *testing.T, raw []byte, ttl int, scope string) {
	t.Helper()
	var envelope struct {
		Result struct {
			TTL   int    `json:"ttlMs"`
			Scope string `json:"cacheScope"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Result.TTL != ttl || envelope.Result.Scope != scope {
		t.Fatalf("cache = %d/%q, want %d/%q: %s", envelope.Result.TTL, envelope.Result.Scope, ttl, scope, raw)
	}
}

func assertErrorCode(t *testing.T, raw []byte, code int64) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code int64 `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != code {
		t.Fatalf("error code = %d, want %d: %s", envelope.Error.Code, code, raw)
	}
}

func callToolResult(t *testing.T, ts *httptest.Server, name string) map[string]json.RawMessage {
	t.Helper()
	raw := modernRPC(t, ts, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	var envelope struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Result
}

func assertTextFallback(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	var content []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 || content[len(content)-1].Text != want {
		t.Fatalf("content fallback = %s, want final text %q", raw, want)
	}
}

func assertNoTextFallback(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var content []json.RawMessage
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("unexpected content fallback: %s", raw)
	}
}

func assertError(t *testing.T, raw []byte, code int64, message string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != code || envelope.Error.Message != message {
		t.Fatalf("error = %d/%q, want %d/%q: %s", envelope.Error.Code, envelope.Error.Message, code, message, raw)
	}
}
