package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Entry описывает cache-запись с явной границей TTL.
type Entry struct {
	Value     string
	ExpiresAt time.Time
}

// Backplane задаёт общий cache-контракт для multi-instance runtime.
type Backplane interface {
	Load(ctx context.Context, namespace, key string) (Entry, bool, error)
	Store(ctx context.Context, namespace, key string, entry Entry) error
}

// InMemoryBackplane реализует process-local backplane для single-instance сценариев.
type InMemoryBackplane struct {
	mu     sync.Mutex
	values map[string]Entry
}

// NewInMemoryBackplane создаёт process-local backplane.
func NewInMemoryBackplane() *InMemoryBackplane {
	return &InMemoryBackplane{values: map[string]Entry{}}
}

// Load читает запись из process-local хранилища и очищает протухшие значения.
func (b *InMemoryBackplane) Load(ctx context.Context, namespace, key string) (Entry, bool, error) {
	if err := contextErr(ctx); err != nil {
		return Entry{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	namespacedKey := namespace + ":" + key
	entry, ok := b.values[namespacedKey]
	if !ok {
		return Entry{}, false, nil
	}
	if isExpired(entry.ExpiresAt) {
		delete(b.values, namespacedKey)
		return Entry{}, false, nil
	}
	return entry, true, nil
}

// Store сохраняет запись в process-local хранилище или удаляет её, если TTL уже истёк.
func (b *InMemoryBackplane) Store(ctx context.Context, namespace, key string, entry Entry) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	namespacedKey := namespace + ":" + key
	if isExpired(entry.ExpiresAt) {
		delete(b.values, namespacedKey)
		return nil
	}
	b.values[namespacedKey] = entry
	return nil
}

// FileBackplane хранит cache в файловой системе, доступной нескольким инстансам процесса.
type FileBackplane struct {
	baseDir string
}

// NewFileBackplane создаёт file-backed backplane.
func NewFileBackplane(baseDir string) *FileBackplane {
	return &FileBackplane{baseDir: strings.TrimSpace(baseDir)}
}

// Load читает запись из файлового backplane и игнорирует устаревшие данные.
func (b *FileBackplane) Load(ctx context.Context, namespace, key string) (Entry, bool, error) {
	if err := contextErr(ctx); err != nil {
		return Entry{}, false, err
	}
	path, err := b.entryPath(namespace, key)
	if err != nil {
		return Entry{}, false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, false, nil
		}
		return Entry{}, false, err
	}
	var rec fileRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Entry{}, false, err
	}
	if rec.Key != key {
		return Entry{}, false, nil
	}
	entry := Entry{
		Value:     rec.Value,
		ExpiresAt: time.UnixMilli(rec.ExpiresAtUnixMS).UTC(),
	}
	if isExpired(entry.ExpiresAt) {
		_ = os.Remove(path)
		return Entry{}, false, nil
	}
	return entry, true, nil
}

// Store записывает запись в файловый backplane или удаляет файл для просроченного значения.
func (b *FileBackplane) Store(ctx context.Context, namespace, key string, entry Entry) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	path, err := b.entryPath(namespace, key)
	if err != nil {
		return err
	}
	if isExpired(entry.ExpiresAt) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(fileRecord{
		Key:             key,
		Value:           entry.Value,
		ExpiresAtUnixMS: entry.ExpiresAt.UTC().UnixMilli(),
	})
	if err != nil {
		return err
	}
	return writeFileAtomically(path, raw, 0o600)
}

// fileRecord описывает формат сериализации cache-записи на диске.
type fileRecord struct {
	Key             string `json:"key"`
	Value           string `json:"value"`
	ExpiresAtUnixMS int64  `json:"expires_at_unix_ms"`
}

// entryPath формирует путь файла для заданной пары namespace/key.
func (b *FileBackplane) entryPath(namespace, key string) (string, error) {
	if strings.TrimSpace(b.baseDir) == "" {
		return "", fmt.Errorf("cache backplane base dir is empty")
	}
	ns := sanitizeNamespace(namespace)
	if ns == "" {
		ns = "default"
	}
	return filepath.Join(b.baseDir, ns, hashKey(key)+".json"), nil
}

// hashKey возвращает короткий стабильный хеш ключа для имени файла.
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// namespaceCleaner удаляет из namespace символы, непригодные для имени директории.
var namespaceCleaner = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitizeNamespace нормализует namespace перед использованием в пути.
func sanitizeNamespace(in string) string {
	clean := strings.TrimSpace(in)
	if clean == "" {
		return ""
	}
	return namespaceCleaner.ReplaceAllString(clean, "_")
}

// isExpired проверяет, что время истечения уже наступило.
func isExpired(expiresAt time.Time) bool {
	if expiresAt.IsZero() {
		return false
	}
	return time.Now().After(expiresAt)
}

// contextErr безопасно возвращает ошибку контекста, учитывая nil-контекст.
func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// writeFileAtomically пишет данные во временный файл и атомарно заменяет целевой файл.
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

// replaceFile заменяет файл назначения, обходя платформенные ошибки "file exists".
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

// syncDir синхронизирует каталог на диск для повышения надёжности после rename.
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

// isRenameExistsError определяет ошибки rename, связанные с существующим файлом назначения.
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

// isSyncUnsupported определяет платформенные ошибки неподдерживаемого dir fsync.
func isSyncUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not supported") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "access is denied")
}
