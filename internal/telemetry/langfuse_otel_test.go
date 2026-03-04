package telemetry

import (
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// TestSessionOTelAttrsIncludesLangfuseSessionAndUserIDs verifies Langfuse-native
// fields are emitted for session/user correlation.
func TestSessionOTelAttrsIncludesLangfuseSessionAndUserIDs(t *testing.T) {
	attrs := sessionOTelAttrs(SessionInfo{
		SessionID:     "session-1",
		CorrelationID: "corr-1",
		UserSub:       "  user-42  ",
	})
	values := otelAttrMap(attrs)

	if got := values["langfuse.session.id"]; got != "session-1" {
		t.Fatalf("langfuse.session.id = %q, want %q", got, "session-1")
	}
	if got := values["langfuse.user.id"]; got != "user-42" {
		t.Fatalf("langfuse.user.id = %q, want %q", got, "user-42")
	}
	if got := values["agent.session_id"]; got != "session-1" {
		t.Fatalf("agent.session_id = %q, want %q", got, "session-1")
	}
	if got := values["agent.correlation_id"]; got != "corr-1" {
		t.Fatalf("agent.correlation_id = %q, want %q", got, "corr-1")
	}
	userHash := values["agent.user_sub_hash"]
	if strings.TrimSpace(userHash) == "" {
		t.Fatalf("agent.user_sub_hash is missing")
	}
	if strings.Contains(userHash, "user-42") {
		t.Fatalf("raw user leaked into hash: %q", userHash)
	}
}

// TestSessionOTelAttrsOmitsUserAttrsWhenMissing verifies that empty user subject
// does not create Langfuse user id or hashed user attribute.
func TestSessionOTelAttrsOmitsUserAttrsWhenMissing(t *testing.T) {
	attrs := sessionOTelAttrs(SessionInfo{
		SessionID:     "session-1",
		CorrelationID: "corr-1",
	})
	values := otelAttrMap(attrs)

	if _, exists := values["langfuse.user.id"]; exists {
		t.Fatalf("unexpected langfuse.user.id attribute")
	}
	if _, exists := values["agent.user_sub_hash"]; exists {
		t.Fatalf("unexpected agent.user_sub_hash attribute")
	}
}

func otelAttrMap(attrs []attribute.KeyValue) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.AsString()
	}
	return values
}
