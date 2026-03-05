package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// TestSQLiteStorePutGetDelete проверяет базовый CRUD цикл SQLite-backed store.
func TestSQLiteStorePutGetDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	if err := store.Put("key", map[string]any{"name": "alice", "count": 3}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok := store.Get("key")
	if !ok {
		t.Fatalf("expected key to exist")
	}
	asMap, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("value type = %T, want map[string]any", got)
	}
	if asMap["name"] != "alice" {
		t.Fatalf("name = %#v, want alice", asMap["name"])
	}
	if count, ok := store.GetInt("missing-count"); ok || count != 0 {
		t.Fatalf("missing int = %d, ok=%t", count, ok)
	}

	if err := store.Delete("key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := store.Get("key"); ok {
		t.Fatalf("expected key to be deleted")
	}
}

// TestSQLiteStorePersistsAcrossReopen проверяет durable-персистентность между открытиями.
func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")

	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	if err := first.Put("persisted", "ok"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer func() {
		_ = second.Close()
	}()
	value, ok := second.GetString("persisted")
	if !ok || value != "ok" {
		t.Fatalf("persisted value = %q, ok=%t", value, ok)
	}
}

// TestSQLiteStoreContextCancellation проверяет fail-fast при отмененном контексте.
func TestSQLiteStoreContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.PutWithContext(ctx, "k", "v"); !errors.Is(err, context.Canceled) {
		t.Fatalf("put err = %v, want context canceled", err)
	}
}
