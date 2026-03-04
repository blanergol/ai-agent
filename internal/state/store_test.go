package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKVStorePersistsVersionedFormat проверяет запись state в versioned envelope.
func TestKVStorePersistsVersionedFormat(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")

	store, err := NewKVStore(statePath)
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	if err := store.Put("foo", "bar"); err != nil {
		t.Fatalf("put: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}

	// snapshot позволяет проверить метаданные версии и сохранённые значения.
	var snapshot persistedState
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("unmarshal persisted state: %v", err)
	}
	if snapshot.Version != stateFileVersion {
		t.Fatalf("version = %d, want %d", snapshot.Version, stateFileVersion)
	}
	if got, ok := snapshot.Values["foo"].(string); !ok || got != "bar" {
		t.Fatalf("snapshot value = %#v", snapshot.Values["foo"])
	}
}

// TestKVStoreLoadsLegacyFormat проверяет обратную совместимость со старым map-форматом.
func TestKVStoreLoadsLegacyFormat(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state-legacy.json")
	if err := os.WriteFile(statePath, []byte(`{"foo":"bar"}`), 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	store, err := NewKVStore(statePath)
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	got, ok := store.GetString("foo")
	if !ok || got != "bar" {
		t.Fatalf("legacy value = %q, ok=%t", got, ok)
	}
}

// TestKVStoreLoadsVersionedFormat проверяет чтение нового versioned-формата state.
func TestKVStoreLoadsVersionedFormat(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state-v1.json")
	payload := `{"version":1,"values":{"count":7}}`
	if err := os.WriteFile(statePath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	store, err := NewKVStore(statePath)
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	got, ok := store.GetInt("count")
	if !ok || got != 7 {
		t.Fatalf("versioned value = %d, ok=%t", got, ok)
	}
}

// TestKVStoreRejectsUnsupportedVersion проверяет fail-fast на неизвестной версии state.
func TestKVStoreRejectsUnsupportedVersion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state-invalid-version.json")
	if err := os.WriteFile(statePath, []byte(`{"version":999,"values":{"k":"v"}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	if _, err := NewKVStore(statePath); err == nil {
		t.Fatalf("expected unsupported version error")
	}
}

// TestPutWithContextHonorsCancellation проверяет fail-fast при отменённом контексте.
func TestPutWithContextHonorsCancellation(t *testing.T) {
	store, err := NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := PutWithContext(ctx, store, "k", "v"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if _, ok := store.Get("k"); ok {
		t.Fatalf("unexpected write despite canceled context")
	}
}

// fakeContextStore имитирует context-aware backend и считает вызовы его методов.
type fakeContextStore struct {
	values         map[string]any
	putCtxCalls    int
	getCtxCalls    int
	deleteCtxCalls int
	snapCtxCalls   int
}

// Put сохраняет значение в локальной map фейкового store.
func (s *fakeContextStore) Put(key string, value any) error {
	if s.values == nil {
		s.values = map[string]any{}
	}
	s.values[key] = value
	return nil
}

// Get читает значение по ключу из локальной map.
func (s *fakeContextStore) Get(key string) (any, bool) {
	if s.values == nil {
		return nil, false
	}
	v, ok := s.values[key]
	return v, ok
}

// GetString возвращает строковое представление значения при корректном типе.
func (s *fakeContextStore) GetString(key string) (string, bool) {
	v, ok := s.Get(key)
	if !ok {
		return "", false
	}
	out, ok := v.(string)
	return out, ok
}

// GetInt возвращает целочисленное значение при корректном типе.
func (s *fakeContextStore) GetInt(key string) (int, bool) {
	v, ok := s.Get(key)
	if !ok {
		return 0, false
	}
	out, ok := v.(int)
	return out, ok
}

// Delete удаляет значение по ключу.
func (s *fakeContextStore) Delete(key string) error {
	delete(s.values, key)
	return nil
}

// Snapshot возвращает копию текущего состояния.
func (s *fakeContextStore) Snapshot() map[string]any {
	out := map[string]any{}
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// PutWithContext считает вызов context-aware метода и делегирует в Put.
func (s *fakeContextStore) PutWithContext(_ context.Context, key string, value any) error {
	s.putCtxCalls++
	return s.Put(key, value)
}

// GetWithContext считает вызов context-aware метода и делегирует в Get.
func (s *fakeContextStore) GetWithContext(_ context.Context, key string) (any, bool, error) {
	s.getCtxCalls++
	v, ok := s.Get(key)
	return v, ok, nil
}

// DeleteWithContext считает вызов context-aware метода и делегирует в Delete.
func (s *fakeContextStore) DeleteWithContext(_ context.Context, key string) error {
	s.deleteCtxCalls++
	return s.Delete(key)
}

// SnapshotWithContext считает вызов context-aware метода и делегирует в Snapshot.
func (s *fakeContextStore) SnapshotWithContext(_ context.Context) (map[string]any, error) {
	s.snapCtxCalls++
	return s.Snapshot(), nil
}

// TestContextHelpersPreferContextStore проверяет, что helper'ы используют context-aware контракт при наличии.
func TestContextHelpersPreferContextStore(t *testing.T) {
	store := &fakeContextStore{values: map[string]any{}}
	ctx := context.Background()

	if err := PutWithContext(ctx, store, "x", "y"); err != nil {
		t.Fatalf("put with context: %v", err)
	}
	if store.putCtxCalls != 1 {
		t.Fatalf("putCtx calls = %d, want 1", store.putCtxCalls)
	}

	if _, _, err := GetWithContext(ctx, store, "x"); err != nil {
		t.Fatalf("get with context: %v", err)
	}
	if store.getCtxCalls != 1 {
		t.Fatalf("getCtx calls = %d, want 1", store.getCtxCalls)
	}

	if err := DeleteWithContext(ctx, store, "x"); err != nil {
		t.Fatalf("delete with context: %v", err)
	}
	if store.deleteCtxCalls != 1 {
		t.Fatalf("deleteCtx calls = %d, want 1", store.deleteCtxCalls)
	}

	if _, err := SnapshotWithContext(ctx, store); err != nil {
		t.Fatalf("snapshot with context: %v", err)
	}
	if store.snapCtxCalls != 1 {
		t.Fatalf("snapshotCtx calls = %d, want 1", store.snapCtxCalls)
	}
}

// TestKVStoreAtomicWriteLeavesNoTempFiles проверяет отсутствие временных файлов после атомарной записи.
func TestKVStoreAtomicWriteLeavesNoTempFiles(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store, err := NewKVStore(statePath)
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	if err := store.Put("k", "v1"); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if err := store.Put("k", "v2"); err != nil {
		t.Fatalf("put v2: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(statePath))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("unexpected temp file left after atomic write: %s", entry.Name())
		}
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var snapshot persistedState
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("unmarshal persisted state: %v", err)
	}
	if got := snapshot.Values["k"]; got != "v2" {
		t.Fatalf("state value = %#v, want v2", got)
	}
}
