package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/retry"
)

// TestHTTPClientListToolsRetriesTransient проверяет общий retry/backoff контракт для list-tools при 5xx.
func TestHTTPClientListToolsRetriesTransient(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools" {
			http.NotFound(w, r)
			return
		}
		attempt := atomic.AddInt32(&calls, 1)
		if attempt <= 2 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode([]RemoteTool{
			{
				Name:         "echo",
				Description:  "echo",
				InputSchema:  `{"type":"object"}`,
				OutputSchema: `{"type":"string"}`,
			},
		})
	}))
	defer srv.Close()

	client := NewHTTPClientWithPolicy(srv.URL, "", 500*time.Millisecond, retry.Policy{
		MaxRetries:    2,
		BaseDelay:     5 * time.Millisecond,
		MaxDelay:      20 * time.Millisecond,
		DisableJitter: true,
	})

	got, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "echo" {
		t.Fatalf("unexpected tools payload: %+v", got)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 retries + success)", calls)
	}
}

// TestHTTPClientCallToolAuthAndPayload проверяет передачу Authorization и JSON payload в MCP call-tool.
func TestHTTPClientCallToolAuthAndPayload(t *testing.T) {
	var (
		authHeader string
		gotArgs    json.RawMessage
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/echo" {
			http.NotFound(w, r)
			return
		}
		authHeader = r.Header.Get("Authorization")
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotArgs = body["args"]
		_ = json.NewEncoder(w).Encode(map[string]any{"output": "ok"})
	}))
	defer srv.Close()

	client := NewHTTPClientWithPolicy(srv.URL, "mcp-token", 500*time.Millisecond, retry.Policy{
		MaxRetries:    0,
		BaseDelay:     5 * time.Millisecond,
		MaxDelay:      20 * time.Millisecond,
		DisableJitter: true,
	})
	_, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"q":"hello"}`))
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if authHeader != "Bearer mcp-token" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
	if string(gotArgs) != `{"q":"hello"}` {
		t.Fatalf("args payload = %s", string(gotArgs))
	}
}

// TestHTTPClientCallToolDoesNotRetryAuthErrors проверяет, что 401 классифицируется как non-retryable Auth.
func TestHTTPClientCallToolDoesNotRetryAuthErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewHTTPClientWithPolicy(srv.URL, "bad-token", 500*time.Millisecond, retry.Policy{
		MaxRetries:    3,
		BaseDelay:     5 * time.Millisecond,
		MaxDelay:      20 * time.Millisecond,
		DisableJitter: true,
	})
	_, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"q":"hello"}`))
	if err == nil {
		t.Fatalf("expected auth error")
	}
	if apperrors.CodeOf(err) != apperrors.CodeAuth {
		t.Fatalf("error code = %s, want %s", apperrors.CodeOf(err), apperrors.CodeAuth)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 for non-retryable auth failure", calls)
	}
}
