package inspector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestModernFirstRoundTripperFallbackPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		allowLegacy bool
		status      int
		contentType string
		discover    string
		wantInitErr bool
	}{
		{
			name:        "recognized compatibility signal permits ordinary fallback",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`,
		},
		{
			name:        "advertised legacy version permits ordinary fallback",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":1,"result":{"supportedVersions":["2025-11-25"]}}`,
		},
		{
			name:        "unrelated error cannot downgrade",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"broken"}}`,
			wantInitErr: true,
		},
		{
			name:        "caller-selected modern task cannot downgrade",
			allowLegacy: false,
			discover:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`,
			wantInitErr: true,
		},
		{
			name:        "unknown future advertisement cannot downgrade",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":1,"result":{"supportedVersions":["2027-01-01"]}}`,
			wantInitErr: true,
		},
		{
			name:        "malformed response cannot downgrade",
			allowLegacy: true,
			discover:    `{`,
			wantInitErr: true,
		},
		{
			name:        "authorization status cannot downgrade",
			allowLegacy: true,
			status:      http.StatusUnauthorized,
			discover:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`,
			wantInitErr: true,
		},
		{
			name:        "unrecognized content type cannot downgrade",
			allowLegacy: true,
			contentType: "text/plain",
			discover:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`,
			wantInitErr: true,
		},
		{
			name:        "missing jsonrpc cannot downgrade",
			allowLegacy: true,
			discover:    `{"id":1,"error":{"code":-32601,"message":"method not found"}}`,
			wantInitErr: true,
		},
		{
			name:        "mismatched id cannot downgrade",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`,
			wantInitErr: true,
		},
		{
			name:        "result and error cannot downgrade",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-32601,"message":"method not found"}}`,
			wantInitErr: true,
		},
		{
			name:        "trailing json cannot downgrade",
			allowLegacy: true,
			discover:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}} {}`,
			wantInitErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := tt.status
			if status == 0 {
				status = http.StatusOK
			}
			contentType := tt.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Type": []string{contentType}},
					Body:       io.NopCloser(bytes.NewBufferString(tt.discover)),
				}, nil
			})
			transport := &modernFirstRoundTripper{base: base, allowLegacy: tt.allowLegacy}
			discoverReq := requestWithMethod(t, "server/discover")
			resp, err := transport.RoundTrip(discoverReq)
			if err != nil {
				t.Fatalf("discover RoundTrip: %v", err)
			}
			_ = resp.Body.Close()
			_, err = transport.RoundTrip(requestWithMethod(t, "initialize"))
			if (err != nil) != tt.wantInitErr {
				t.Fatalf("initialize error = %v, wantErr %v", err, tt.wantInitErr)
			}
		})
	}
}

func TestModernFirstHTTPClientBoundsCleanupRequests(t *testing.T) {
	t.Parallel()
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	client := modernFirstHTTPClient(25*time.Millisecond, base, true)
	req, err := http.NewRequest(http.MethodDelete, "http://127.0.0.1/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = client.Do(req)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup request error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded cleanup took %v", elapsed)
	}
}

func requestWithMethod(t *testing.T, method string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1/mcp",
		bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	return req
}
