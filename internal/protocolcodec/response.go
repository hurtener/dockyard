package protocolcodec

import (
	"encoding/json"
	"fmt"
)

const (
	legacyResourceNotFoundCode = int64(-32002)
	modernResourceNotFoundCode = int64(-32602)
)

// CacheMetadata is the protocol-agnostic input to versioned response encoding.
type CacheMetadata struct {
	TTLMs int64
	Scope string
}

// EncodeResultType applies the core result discriminator required by the
// selected protocol version. Modern results default to complete only when the
// producer omitted resultType; explicit task, input_required, complete, and
// extension-defined values are preserved. Legacy results remain unchanged.
func EncodeResultType(version ProtocolVersion, raw json.RawMessage) (json.RawMessage, error) {
	if version != VersionMCP20260728 {
		return raw, nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("protocolcodec: response result: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("protocolcodec: response result must be an object")
	}
	if _, present := result["resultType"]; present {
		return raw, nil
	}
	resultType, err := json.Marshal("complete")
	if err != nil {
		return nil, err
	}
	result["resultType"] = resultType
	return json.Marshal(result)
}

// EncodeCacheMetadata applies the selected version's top-level cache shape to
// an already encoded result. Legacy versions remove the fields; modern requires
// both fields. The returned JSON is fresh and safe for concurrent callers.
func EncodeCacheMetadata(version ProtocolVersion, raw json.RawMessage, cache CacheMetadata) (json.RawMessage, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("protocolcodec: response result: %w", err)
	}
	delete(result, "ttlMs")
	delete(result, "cacheScope")
	if version == VersionMCP20260728 {
		ttl, err := json.Marshal(cache.TTLMs)
		if err != nil {
			return nil, err
		}
		scope, err := json.Marshal(cache.Scope)
		if err != nil {
			return nil, err
		}
		result["ttlMs"] = ttl
		result["cacheScope"] = scope
	}
	return json.Marshal(result)
}

// metaKeyServerInfo is the SEP-2575 _meta key under which a modern-protocol
// server identifies itself on every result.
const metaKeyServerInfo = "io.modelcontextprotocol/serverInfo"

// ServerInfo is the protocol-agnostic server identity injected into modern
// responses per SEP-2575.
type ServerInfo struct {
	Name        string
	Title       string
	Version     string
	Description string
	WebsiteURL  string
	Icons       []Icon
}

// Icon is the SEP-973 server-icon wire shape carried in the modern-protocol
// serverInfo. protocolcodec owns this shape because it is the only package
// permitted to model MCP extension wire formats (RFC §5.4, P3). The JSON tags
// are the wire contract: src is required, the rest are omitted when empty.
type Icon struct {
	Src      string   `json:"src"`
	MIMEType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	Theme    string   `json:"theme,omitempty"`
}

// EncodeServerInfo annotates a modern-protocol result's _meta with the server
// identity SEP-2575 requires, unless the producer already set it. Legacy
// versions are unchanged. The returned JSON is fresh and safe for concurrent
// callers.
func EncodeServerInfo(version ProtocolVersion, raw json.RawMessage, info ServerInfo) (json.RawMessage, error) {
	if version != VersionMCP20260728 {
		return raw, nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("protocolcodec: response result: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("protocolcodec: response result must be an object")
	}
	var meta map[string]json.RawMessage
	if rawMeta, ok := result["_meta"]; ok {
		if err := json.Unmarshal(rawMeta, &meta); err != nil {
			return nil, fmt.Errorf("protocolcodec: response _meta: %w", err)
		}
	}
	if meta == nil {
		meta = map[string]json.RawMessage{}
	}
	if _, present := meta[metaKeyServerInfo]; present {
		return raw, nil
	}
	impl := map[string]any{"name": info.Name, "version": info.Version}
	if info.Title != "" {
		impl["title"] = info.Title
	}
	if info.Description != "" {
		impl["description"] = info.Description
	}
	if info.WebsiteURL != "" {
		impl["websiteUrl"] = info.WebsiteURL
	}
	if len(info.Icons) > 0 {
		impl["icons"] = info.Icons
	}
	encodedImpl, err := json.Marshal(impl)
	if err != nil {
		return nil, err
	}
	meta[metaKeyServerInfo] = encodedImpl
	encodedMeta, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	result["_meta"] = encodedMeta
	return json.Marshal(result)
}

// EncodeStructuredPresence applies Dockyard's typed presence decision after
// SDK output adaptation. It also maintains MCP's JSON text fallback for every
// non-object value, including explicit null.
func EncodeStructuredPresence(raw json.RawMessage, present, explicitNull bool) (json.RawMessage, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("protocolcodec: tool result: %w", err)
	}
	previous := result["structuredContent"]
	if !present {
		delete(result, "structuredContent")
		result["content"] = removeJSONFallback(result["content"], previous)
		return json.Marshal(result)
	}
	if explicitNull {
		result["structuredContent"] = json.RawMessage("null")
		result["content"] = replaceJSONFallback(result["content"], previous, "null")
	}
	return json.Marshal(result)
}

func removeJSONFallback(content, previous json.RawMessage) json.RawMessage {
	var blocks []map[string]json.RawMessage
	if len(content) == 0 || string(content) == "null" {
		return json.RawMessage("[]")
	}
	if json.Unmarshal(content, &blocks) != nil || len(previous) == 0 {
		return content
	}
	if len(blocks) == 0 {
		return content
	}
	want := string(previous)
	last := len(blocks) - 1
	var kind, text string
	_ = json.Unmarshal(blocks[last]["type"], &kind)
	_ = json.Unmarshal(blocks[last]["text"], &text)
	if kind == "text" && text == want {
		blocks = blocks[:last]
	}
	b, _ := json.Marshal(blocks)
	return b
}

func replaceJSONFallback(content, previous json.RawMessage, fallback string) json.RawMessage {
	content = removeJSONFallback(content, previous)
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(content, &blocks) != nil {
		blocks = nil
	}
	kind, _ := json.Marshal("text")
	text, _ := json.Marshal(fallback)
	blocks = append(blocks, map[string]json.RawMessage{"type": kind, "text": text})
	b, _ := json.Marshal(blocks)
	return b
}

// ResourceNotFoundCode returns the versioned JSON-RPC code for a missing URI.
func ResourceNotFoundCode(version ProtocolVersion) int64 {
	if version == VersionMCP20260728 {
		return modernResourceNotFoundCode
	}
	return legacyResourceNotFoundCode
}
