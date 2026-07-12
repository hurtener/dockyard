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
