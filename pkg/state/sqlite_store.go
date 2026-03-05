package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore сохраняет key-value состояние в SQLite для durable-персистентности.
type SQLiteStore struct {
	db *sql.DB
}

var _ Store = (*SQLiteStore)(nil)
var _ StoreWithContext = (*SQLiteStore)(nil)

// NewSQLiteStore создает SQLite-backed state store и инициализирует схему хранения.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, errors.New("sqlite state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite state directory: %w", err)
	}

	dsn := "file:" + filepath.ToSlash(cleanPath) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite state db: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite state db: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close освобождает sqlite-ресурсы; безопасен для повторного вызова.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Put сохраняет значение в sqlite; перезаписывает существующий ключ.
func (s *SQLiteStore) Put(key string, value any) error {
	return s.PutWithContext(context.Background(), key, value)
}

// PutWithContext сохраняет значение с учетом отмены контекста.
func (s *SQLiteStore) PutWithContext(ctx context.Context, key string, value any) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return errors.New("sqlite state store is not initialized")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal sqlite state value: %w", err)
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO kv_state(key, value, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key,
		string(raw),
		time.Now().UTC().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("upsert sqlite state value: %w", err)
	}
	return nil
}

// Get читает значение из sqlite.
func (s *SQLiteStore) Get(key string) (any, bool) {
	value, ok, err := s.GetWithContext(context.Background(), key)
	if err != nil {
		return nil, false
	}
	return value, ok
}

// GetWithContext читает значение из sqlite с учетом контекста.
func (s *SQLiteStore) GetWithContext(ctx context.Context, key string) (any, bool, error) {
	if err := contextError(ctx); err != nil {
		return nil, false, err
	}
	if s == nil || s.db == nil {
		return nil, false, errors.New("sqlite state store is not initialized")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv_state WHERE key = ?`, key).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("query sqlite state value: %w", err)
	}
	value, err := decodeSQLiteValue(raw)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

// GetString возвращает строку при совместимом типе.
func (s *SQLiteStore) GetString(key string) (string, bool) {
	value, ok := s.Get(key)
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	return str, ok
}

// GetInt возвращает int при совместимом числовом типе.
func (s *SQLiteStore) GetInt(key string) (int, bool) {
	value, ok := s.Get(key)
	if !ok {
		return 0, false
	}
	switch vv := value.(type) {
	case int:
		return vv, true
	case int32:
		return int(vv), true
	case int64:
		return int(vv), true
	case float64:
		return int(vv), true
	case json.Number:
		i, err := vv.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// Delete удаляет ключ из sqlite store.
func (s *SQLiteStore) Delete(key string) error {
	return s.DeleteWithContext(context.Background(), key)
}

// DeleteWithContext удаляет ключ с учетом отмены контекста.
func (s *SQLiteStore) DeleteWithContext(ctx context.Context, key string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return errors.New("sqlite state store is not initialized")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kv_state WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete sqlite state value: %w", err)
	}
	return nil
}

// Snapshot возвращает копию всех записей sqlite-хранилища.
func (s *SQLiteStore) Snapshot() map[string]any {
	out, err := s.SnapshotWithContext(context.Background())
	if err != nil {
		return map[string]any{}
	}
	return out
}

// SnapshotWithContext возвращает копию всех записей с учетом контекста.
func (s *SQLiteStore) SnapshotWithContext(ctx context.Context) (map[string]any, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, errors.New("sqlite state store is not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM kv_state`)
	if err != nil {
		return nil, fmt.Errorf("query sqlite state snapshot: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make(map[string]any)
	for rows.Next() {
		var (
			key string
			raw string
		)
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("scan sqlite state snapshot row: %w", err)
		}
		value, err := decodeSQLiteValue(raw)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite state snapshot rows: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`CREATE TABLE IF NOT EXISTS kv_state(
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init sqlite state schema: %w", err)
		}
	}
	return nil
}

func decodeSQLiteValue(raw string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode sqlite state value: %w", err)
	}
	return out, nil
}
