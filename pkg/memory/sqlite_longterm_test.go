package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/blanergol/agent-core/pkg/telemetry"
)

// TestSQLiteLongTermRecallIsSessionScoped проверяет, что recall не протекает между сессиями.
func TestSQLiteLongTermRecallIsSessionScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.sqlite")
	mem, err := NewSQLiteLongTerm(path)
	if err != nil {
		t.Fatalf("new sqlite long-term: %v", err)
	}
	defer func() {
		_ = mem.Close()
	}()

	ctxA := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-a",
		CorrelationID: "corr-a",
	})
	ctxB := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-b",
		CorrelationID: "corr-b",
	})

	if err := mem.Store(ctxA, Item{
		ID:        "a-1",
		Text:      "database migration failed for payments service",
		Metadata:  map[string]string{"session_id": "session-a"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store a-1: %v", err)
	}
	if err := mem.Store(ctxB, Item{
		ID:        "b-1",
		Text:      "incident update for checkout service",
		Metadata:  map[string]string{"session_id": "session-b"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store b-1: %v", err)
	}

	recalled, err := mem.Recall(ctxA, "payments migration", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(recalled) != 1 {
		t.Fatalf("recall len = %d, want 1", len(recalled))
	}
	if recalled[0].ID != "a-1" {
		t.Fatalf("recalled id = %s, want a-1", recalled[0].ID)
	}
}

// TestSQLiteLongTermPersistsAcrossReopen проверяет durable-персистентность long-term памяти.
func TestSQLiteLongTermPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.sqlite")
	ctx := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-1",
		CorrelationID: "corr-1",
	})

	first, err := NewSQLiteLongTerm(path)
	if err != nil {
		t.Fatalf("new sqlite long-term: %v", err)
	}
	if err := first.Store(ctx, Item{
		ID:        "persist-1",
		Text:      "remember to notify oncall team",
		Metadata:  map[string]string{"session_id": "session-1"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store persist-1: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := NewSQLiteLongTerm(path)
	if err != nil {
		t.Fatalf("reopen sqlite long-term: %v", err)
	}
	defer func() {
		_ = second.Close()
	}()

	item, ok, err := second.Get(ctx, "persist-1")
	if err != nil {
		t.Fatalf("get persist-1: %v", err)
	}
	if !ok {
		t.Fatalf("expected persisted item to exist")
	}
	if item.ID != "persist-1" {
		t.Fatalf("item id = %s, want persist-1", item.ID)
	}
}
