package cache

import (
	"context"
	"testing"
	"time"
)

// TestInMemoryBackplaneStoreLoad проверяет базовый цикл записи и чтения в InMemoryBackplane.
func TestInMemoryBackplaneStoreLoad(t *testing.T) {
	bp := NewInMemoryBackplane()
	err := bp.Store(context.Background(), "ns", "k", Entry{
		Value:     "v",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	entry, ok, err := bp.Load(context.Background(), "ns", "k")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if entry.Value != "v" {
		t.Fatalf("value = %s", entry.Value)
	}
}

// TestFileBackplaneSharedAcrossInstances проверяет совместное чтение кэша разными экземплярами FileBackplane.
func TestFileBackplaneSharedAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	first := NewFileBackplane(dir)
	second := NewFileBackplane(dir)

	err := first.Store(context.Background(), "ns", "k", Entry{
		Value:     "shared",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	entry, ok, err := second.Load(context.Background(), "ns", "k")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatalf("expected cache hit from second backplane instance")
	}
	if entry.Value != "shared" {
		t.Fatalf("value = %s", entry.Value)
	}
}
