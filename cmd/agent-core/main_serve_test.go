package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/blanergol/agent-core/pkg/telemetry"
)

// TestRunHTTPServerStopsOnContextCancel проверяет путь graceful shutdown при отмене serve-контекста (SIGINT/SIGTERM).
func TestRunHTTPServerStopsOnContextCancel(t *testing.T) {
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveCtx := telemetry.WithSession(baseCtx, telemetry.SessionInfo{
		SessionID:     "serve-test-session",
		CorrelationID: "serve-test-correlation",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	srv := &http.Server{Handler: mux}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan error, 1)
	go func() {
		done <- runHTTPServer(serveCtx, logger, srv, listener, 1500*time.Millisecond, nil, false)
	}()

	healthURL := "http://" + listener.Addr().String() + "/healthz"
	if err := waitHTTPReady(healthURL, 2*time.Second); err != nil {
		t.Fatalf("server was not ready: %v", err)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHTTPServer returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for graceful shutdown")
	}
}

// waitHTTPReady опрашивает health-endpoint до готовности сервера или истечения тайм-аута.
func waitHTTPReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return context.DeadlineExceeded
}
