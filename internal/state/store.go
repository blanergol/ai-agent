package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// stateFileVersion фиксирует текущую версию формата persisted state.
	stateFileVersion = 1
)

// persistedState описывает versioned-формат сохранения KV-состояния на диск.
type persistedState struct {
	// Version позволяет эволюционировать формат state-файла без поломки старых данных.
	Version int `json:"version"`
	// Values содержит фактический snapshot key-value состояния.
	Values map[string]any `json:"values"`
}

// Store задаёт базовый контракт key-value хранилища состояния агента.
type Store interface {
	// Put сохраняет значение по ключу.
	Put(key string, value any) error
	// Get возвращает сырое значение и признак существования ключа.
	Get(key string) (any, bool)
	// GetString возвращает значение как строку при корректном типе.
	GetString(key string) (string, bool)
	// GetInt возвращает значение как int с поддержкой нескольких числовых типов.
	GetInt(key string) (int, bool)
	// Delete удаляет ключ из хранилища.
	Delete(key string) error
	// Snapshot возвращает копию текущего состояния для безопасного чтения.
	Snapshot() map[string]any
}

// StoreWithContext расширяет Store контекстными операциями для внешних backend'ов.
type StoreWithContext interface {
	PutWithContext(ctx context.Context, key string, value any) error
	GetWithContext(ctx context.Context, key string) (any, bool, error)
	DeleteWithContext(ctx context.Context, key string) error
	SnapshotWithContext(ctx context.Context) (map[string]any, error)
}

// PutWithContext выполняет Put с поддержкой context-aware backend'ов.
func PutWithContext(ctx context.Context, store Store, key string, value any) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if withContext, ok := store.(StoreWithContext); ok {
		return withContext.PutWithContext(ctx, key, value)
	}
	return store.Put(key, value)
}

// GetWithContext выполняет Get с поддержкой context-aware backend'ов.
func GetWithContext(ctx context.Context, store Store, key string) (any, bool, error) {
	if err := contextError(ctx); err != nil {
		return nil, false, err
	}
	if withContext, ok := store.(StoreWithContext); ok {
		return withContext.GetWithContext(ctx, key)
	}
	value, ok := store.Get(key)
	return value, ok, nil
}

// DeleteWithContext выполняет Delete с поддержкой context-aware backend'ов.
func DeleteWithContext(ctx context.Context, store Store, key string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if withContext, ok := store.(StoreWithContext); ok {
		return withContext.DeleteWithContext(ctx, key)
	}
	return store.Delete(key)
}

// SnapshotWithContext выполняет Snapshot с поддержкой context-aware backend'ов.
func SnapshotWithContext(ctx context.Context, store Store) (map[string]any, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if withContext, ok := store.(StoreWithContext); ok {
		return withContext.SnapshotWithContext(ctx)
	}
	return store.Snapshot(), nil
}

// KVStore реализует in-memory key-value хранилище с опциональной файловой персистентностью.
type KVStore struct {
	// mu защищает map values и операции записи на диск.
	mu sync.RWMutex
	// values хранит актуальные ключ-значение пары в памяти.
	values map[string]any
	// persistPath задаёт путь к JSON-файлу для персистентности.
	persistPath string
}

// NewKVStore создаёт хранилище и при необходимости подгружает состояние с диска.
func NewKVStore(persistPath string) (*KVStore, error) {
	// s - рабочий экземпляр хранилища с пустой map.
	s := &KVStore{
		values:      make(map[string]any),
		persistPath: persistPath,
	}
	if persistPath != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Put сохраняет значение и синхронизирует его на диск при включённой персистентности.
func (s *KVStore) Put(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.values[key] = value
	return s.persistLocked()
}

// PutWithContext выполняет Put с предварительной проверкой отмены контекста.
func (s *KVStore) PutWithContext(ctx context.Context, key string, value any) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return s.Put(key, value)
}

// Get возвращает значение из памяти без преобразования типа.
func (s *KVStore) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// v - найденное значение по ключу.
	v, ok := s.values[key]
	return v, ok
}

// GetWithContext выполняет Get с предварительной проверкой отмены контекста.
func (s *KVStore) GetWithContext(ctx context.Context, key string) (any, bool, error) {
	if err := contextError(ctx); err != nil {
		return nil, false, err
	}
	value, ok := s.Get(key)
	return value, ok, nil
}

// GetString читает значение и проверяет, что это строка.
func (s *KVStore) GetString(key string) (string, bool) {
	// v - исходное значение до приведения типа.
	v, ok := s.Get(key)
	if !ok {
		return "", false
	}
	// sv хранит строковое значение после type assertion.
	sv, ok := v.(string)
	return sv, ok
}

// GetInt читает значение и приводит поддерживаемые числовые типы к int.
func (s *KVStore) GetInt(key string) (int, bool) {
	// v - исходное значение до определения конкретного типа.
	v, ok := s.Get(key)
	if !ok {
		return 0, false
	}
	// vv - значение в конкретном типе внутри type switch.
	switch vv := v.(type) {
	case int:
		return vv, true
	case int32:
		return int(vv), true
	case int64:
		return int(vv), true
	case float64:
		return int(vv), true
	default:
		return 0, false
	}
}

// Delete удаляет ключ и сохраняет обновлённое состояние.
func (s *KVStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.values, key)
	return s.persistLocked()
}

// DeleteWithContext выполняет Delete с предварительной проверкой отмены контекста.
func (s *KVStore) DeleteWithContext(ctx context.Context, key string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return s.Delete(key)
}

// Snapshot создаёт изолированную копию map, безопасную для чтения извне.
func (s *KVStore) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// clone используется, чтобы вызывающий код не модифицировал внутреннюю map.
	clone := make(map[string]any, len(s.values))
	for k, v := range s.values {
		clone[k] = v
	}
	return clone
}

// SnapshotWithContext выполняет Snapshot с предварительной проверкой отмены контекста.
func (s *KVStore) SnapshotWithContext(ctx context.Context) (map[string]any, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return s.Snapshot(), nil
}

// load загружает состояние из JSON-файла, если файл существует.
func (s *KVStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// b содержит сырое содержимое state-файла.
	b, err := os.ReadFile(s.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read state file: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	// raw позволяет определить, записан ли файл в новом versioned-формате.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("unmarshal state file: %w", err)
	}

	if _, hasVersion := raw["version"]; hasVersion {
		if _, hasValues := raw["values"]; hasValues {
			// version нужен для проверки совместимости формата.
			var version int
			if err := json.Unmarshal(raw["version"], &version); err != nil {
				return fmt.Errorf("decode state version: %w", err)
			}
			if version <= 0 || version > stateFileVersion {
				return fmt.Errorf("unsupported state version: %d", version)
			}
			// values декодируется из versioned envelope.
			values := make(map[string]any)
			if len(raw["values"]) != 0 && string(raw["values"]) != "null" {
				if err := json.Unmarshal(raw["values"], &values); err != nil {
					return fmt.Errorf("decode state values: %w", err)
				}
			}
			s.values = values
			return nil
		}
	}

	// legacy поддерживает обратную совместимость со старым форматом: plain map[string]any.
	legacy := make(map[string]any)
	if err := json.Unmarshal(b, &legacy); err != nil {
		return fmt.Errorf("unmarshal legacy state file: %w", err)
	}
	s.values = legacy
	return nil
}

// persistLocked сохраняет текущее состояние на диск; вызывается только под mutex.
func (s *KVStore) persistLocked() error {
	if s.persistPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.persistPath), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	// snapshot упаковывает значения с номером версии для будущих миграций формата.
	snapshot := persistedState{
		Version: stateFileVersion,
		Values:  s.values,
	}
	// b - форматированный JSON для удобной диагностики и восстановления.
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := writeFileAtomically(s.persistPath, b, 0o600); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

// writeFileAtomically записывает файл через временный путь и rename для защиты от частичных записей.
func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if perm != 0 {
		if err := tmp.Chmod(perm); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// replaceFile заменяет целевой файл, учитывая различия поведения os.Rename на разных платформах.
func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isRenameExistsError(err) {
		return err
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}

// syncDir синхронизирует директорию, чтобы зафиксировать атомарный rename на файловой системе.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = dir.Close()
	}()
	if err := dir.Sync(); err != nil && !isSyncUnsupported(err) {
		return err
	}
	return nil
}

// isRenameExistsError определяет ошибку конфликта назначения при rename.
func isRenameExistsError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "file exists")
}

// isSyncUnsupported распознаёт платформенные ошибки, где fsync директории не поддерживается.
func isSyncUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not supported") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "access is denied")
}

// contextError возвращает ошибку отмены/дедлайна контекста или nil для пустого/активного контекста.
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
