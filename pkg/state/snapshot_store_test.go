package state

import (
	"context"
	"testing"

	"github.com/blanergol/agent-core/core"
)

// TestSnapshotStoreSaveLoad проверяет полный цикл сохранения и загрузки runtime snapshot.
func TestSnapshotStoreSaveLoad(t *testing.T) {
	store, err := NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	snapshots := NewSnapshotStore(store)

	want := "session-1"
	if err := snapshots.Save(context.Background(), coreSnapshot(want)); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	got, ok, err := snapshots.Load(context.Background(), want)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !ok {
		t.Fatalf("expected snapshot to be found")
	}
	if got.SessionID != want {
		t.Fatalf("session_id = %s, want %s", got.SessionID, want)
	}
	if len(got.ShortTermMessages) != 1 || got.ShortTermMessages[0].Content != "hello" {
		t.Fatalf("messages = %#v", got.ShortTermMessages)
	}
}

// TestSnapshotStoreLoadTypeMismatch проверяет ошибку при несовместимом типе сохраненного значения.
func TestSnapshotStoreLoadTypeMismatch(t *testing.T) {
	store, err := NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	if err := store.Put(runtimeSnapshotPrefix+"session-x", 123); err != nil {
		t.Fatalf("put raw value: %v", err)
	}
	snapshots := NewSnapshotStore(store)
	if _, _, err := snapshots.Load(context.Background(), "session-x"); err == nil {
		t.Fatalf("expected type mismatch error")
	}
}

// coreSnapshot создает минимальный runtime snapshot для тестовых сценариев.
func coreSnapshot(sessionID string) core.RuntimeSnapshot {
	return core.RuntimeSnapshot{
		SessionID: sessionID,
		ShortTermMessages: []core.Message{
			{Role: core.RoleUser, Content: "hello"},
		},
	}
}
