package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestContextAttrsHashesUserSub проверяет, что user-sub попадает в логи только в хэшированном виде.
func TestContextAttrsHashesUserSub(t *testing.T) {
	ctx := WithSession(context.Background(), SessionInfo{
		SessionID:     "session-1",
		CorrelationID: "corr-1",
		UserSub:       "user-123",
	})
	attrs := ContextAttrs(ctx)

	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[attr.Key] = slogValueToString(attr.Value)
	}

	if values["session_id"] != "session-1" {
		t.Fatalf("session_id = %s", values["session_id"])
	}
	if values["correlation_id"] != "corr-1" {
		t.Fatalf("correlation_id = %s", values["correlation_id"])
	}
	if _, exists := values["user_sub"]; exists {
		t.Fatalf("unexpected raw user_sub attribute")
	}
	userSubHash, ok := values["user_sub_hash"]
	if !ok || strings.TrimSpace(userSubHash) == "" {
		t.Fatalf("user_sub_hash is missing")
	}
	if strings.Contains(userSubHash, "user-123") {
		t.Fatalf("raw user_sub leaked into hash attribute: %s", userSubHash)
	}
}

// TestUserSubForLogsIsStable проверяет детерминированность хэширования user-sub.
func TestUserSubForLogsIsStable(t *testing.T) {
	a := UserSubForLogs("alice")
	b := UserSubForLogs("alice")
	c := UserSubForLogs("bob")
	if a == "" {
		t.Fatalf("hash is empty")
	}
	if a != b {
		t.Fatalf("hash is not stable: %s != %s", a, b)
	}
	if a == c {
		t.Fatalf("different user_sub values produced identical hash: %s", a)
	}
}

// TestContextLoggerEnforcesSessionCorrelationAttrs проверяет обязательные session/correlation атрибуты.
func TestContextLoggerEnforcesSessionCorrelationAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := WithSession(context.Background(), SessionInfo{
		SessionID:     "session-enforced",
		CorrelationID: "corr-enforced",
	})
	clog := NewContextLogger(ctx, logger)
	clog.Info("hello", slog.String("key", "value"))

	raw := buf.String()
	if !strings.Contains(raw, `"session_id":"session-enforced"`) {
		t.Fatalf("missing session_id in log: %s", raw)
	}
	if !strings.Contains(raw, `"correlation_id":"corr-enforced"`) {
		t.Fatalf("missing correlation_id in log: %s", raw)
	}
	if !strings.Contains(raw, `"key":"value"`) {
		t.Fatalf("missing custom attr in log: %s", raw)
	}
}

// slogValueToString нормализует извлечение строкового представления slog.Value в тестах.
func slogValueToString(v slog.Value) string {
	if v.Kind() == slog.KindString {
		return v.String()
	}
	return v.String()
}
