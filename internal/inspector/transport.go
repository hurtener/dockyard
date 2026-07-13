package inspector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxDiscoveryResponse = 4 << 20

// modernFirstHTTPClient prevents the SDK from silently falling back after an
// unrelated server/discover failure without wrapping its connection type. The
// SDK relies on a private connection interface to install negotiated metadata.
func modernFirstHTTPClient(timeout time.Duration, base http.RoundTripper, allowLegacy bool) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &modernFirstRoundTripper{
			base: base, allowLegacy: allowLegacy,
		},
	}
}

type modernFirstRoundTripper struct {
	base        http.RoundTripper
	allowLegacy bool

	mu              sync.Mutex
	discoverSeen    bool
	fallbackAllowed bool
	discoverErr     error
}

func (t *modernFirstRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, method, requestID, err := readRequestEnvelope(req)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	t.mu.Lock()
	if method == "server/discover" {
		t.discoverSeen = true
		t.fallbackAllowed = false
		t.discoverErr = nil
	}
	if method == "initialize" && t.discoverSeen && (!t.allowLegacy || !t.fallbackAllowed) {
		err := t.discoverErr
		if err == nil {
			err = errors.New("server/discover did not return a recognized legacy fallback signal")
		}
		t.mu.Unlock()
		return nil, err
	}
	t.mu.Unlock()

	resp, err := t.base.RoundTrip(req)
	if method != "server/discover" {
		return resp, err
	}
	if err != nil {
		t.recordDiscovery(false, fmt.Errorf("server/discover transport failed: %w", err))
		return nil, err
	}
	allowed, inspectErr := inspectDiscoveryResponse(resp, requestID)
	t.recordDiscovery(t.allowLegacy && allowed, inspectErr)
	return resp, nil
}

func (t *modernFirstRoundTripper) recordDiscovery(allowed bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fallbackAllowed = allowed
	if !allowed {
		t.discoverErr = err
	}
}

func readRequestEnvelope(req *http.Request) ([]byte, string, json.RawMessage, error) {
	if req.Body == nil {
		return nil, "", nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, "", nil, fmt.Errorf("dockyard/internal/inspector: read MCP request: %w", err)
	}
	var envelope struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(body, &envelope)
	return body, envelope.Method, envelope.ID, nil
}

func inspectDiscoveryResponse(resp *http.Response, requestID json.RawMessage) (bool, error) {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Errorf("server/discover returned HTTP status %d", resp.StatusCode)
	}
	if resp.Body == nil {
		return false, errors.New("server/discover returned no response body")
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" && mediaType != "text/event-stream" {
		return false, errors.New("server/discover returned an unsupported Content-Type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryResponse+1))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("read server/discover response: %w", err)
	}
	if len(body) > maxDiscoveryResponse {
		return false, errors.New("server/discover response exceeds 4 MiB")
	}
	payload := body
	if mediaType == "text/event-stream" {
		payload = nil
		for _, line := range bytes.Split(body, []byte("\n")) {
			if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				payload = bytes.TrimSpace(data)
				break
			}
		}
		if len(payload) == 0 {
			return false, errors.New("server/discover SSE response carried no data event")
		}
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	if err := dec.Decode(&envelope); err != nil {
		return false, fmt.Errorf("malformed server/discover response %q: %w", truncate(body, 256), err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return false, errors.New("server/discover response contains trailing JSON")
	}
	if envelope.JSONRPC != "2.0" || len(requestID) == 0 || !bytes.Equal(bytes.TrimSpace(envelope.ID), bytes.TrimSpace(requestID)) {
		return false, errors.New("server/discover returned a mismatched JSON-RPC envelope")
	}
	hasResult, hasError := len(envelope.Result) != 0, len(envelope.Error) != 0
	if hasResult == hasError {
		return false, errors.New("server/discover response must contain exactly one of result or error")
	}
	if hasError {
		var rpcErr struct {
			Code    *int   `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(envelope.Error, &rpcErr); err != nil || rpcErr.Code == nil || rpcErr.Message == "" {
			return false, errors.New("server/discover returned a malformed JSON-RPC error")
		}
		allowed := *rpcErr.Code == int(jsonrpc.CodeMethodNotFound) ||
			*rpcErr.Code == int(mcpsdk.CodeUnsupportedProtocolVersion)
		if allowed {
			return true, nil
		}
		return false, fmt.Errorf("server/discover failed with JSON-RPC code %d", *rpcErr.Code)
	}
	var result struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil || len(result.SupportedVersions) == 0 {
		return false, errors.New("malformed server/discover result")
	}
	containsSupportedLegacy := false
	for _, version := range result.SupportedVersions {
		if !recognizedLegacyVersion(version) {
			return false, errors.New("server/discover did not advertise a supported modern version")
		}
		containsSupportedLegacy = containsSupportedLegacy || version == legacyProtocol
	}
	return containsSupportedLegacy, nil
}

func recognizedLegacyVersion(version string) bool {
	switch version {
	case "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05":
		return true
	default:
		return false
	}
}
