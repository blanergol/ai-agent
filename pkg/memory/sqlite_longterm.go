package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/telemetry"
	_ "modernc.org/sqlite"
)

// SQLiteLongTerm хранит долговременную память в SQLite с BM25-подобным recall scoring.
type SQLiteLongTerm struct {
	db *sql.DB
}

var _ LongTermMemory = (*SQLiteLongTerm)(nil)

// NewSQLiteLongTerm создает durable SQLite-backed long-term memory.
func NewSQLiteLongTerm(path string) (*SQLiteLongTerm, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, errors.New("sqlite memory path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite memory directory: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(cleanPath) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory db: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite memory db: %w", err)
	}

	mem := &SQLiteLongTerm{db: db}
	if err := mem.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return mem, nil
}

// Close закрывает sqlite-подключение памяти.
func (m *SQLiteLongTerm) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

// Store сохраняет запись памяти в sqlite (upsert по ID).
func (m *SQLiteLongTerm) Store(ctx context.Context, item Item) error {
	if m == nil || m.db == nil {
		return errors.New("sqlite long-term memory is not initialized")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(item.ID) == "" {
		return errors.New("memory item id is empty")
	}
	if item.Metadata == nil {
		item.Metadata = map[string]string{}
	}
	sessionID := strings.TrimSpace(item.Metadata["session_id"])
	if sessionID == "" {
		sessionID = telemetry.SessionFromContext(ctx).SessionID
		item.Metadata["session_id"] = sessionID
	}
	createdAt := item.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	metaRaw, err := json.Marshal(item.Metadata)
	if err != nil {
		return fmt.Errorf("marshal memory metadata: %w", err)
	}
	_, err = m.db.ExecContext(
		ctx,
		`INSERT INTO long_term_memory(id, text, metadata, session_id, created_at_unix, created_at_rfc3339)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   text=excluded.text,
		   metadata=excluded.metadata,
		   session_id=excluded.session_id,
		   created_at_unix=excluded.created_at_unix,
		   created_at_rfc3339=excluded.created_at_rfc3339`,
		item.ID,
		item.Text,
		string(metaRaw),
		sessionID,
		createdAt.UnixMilli(),
		createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert memory item: %w", err)
	}
	return nil
}

// Recall возвращает наиболее релевантные элементы по BM25-подобной оценке в рамках текущей сессии.
func (m *SQLiteLongTerm) Recall(ctx context.Context, query string, topK int) ([]Item, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("sqlite long-term memory is not initialized")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 5
	}
	sessionID := telemetry.SessionFromContext(ctx).SessionID
	rows, err := m.db.QueryContext(
		ctx,
		`SELECT id, text, metadata, created_at_unix
		   FROM long_term_memory
		  WHERE session_id = ?
		  ORDER BY created_at_unix DESC
		  LIMIT 500`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query memory recall candidates: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	candidates := make([]Item, 0, 64)
	for rows.Next() {
		var (
			id            string
			text          string
			metadataRaw   string
			createdAtUnix int64
		)
		if err := rows.Scan(&id, &text, &metadataRaw, &createdAtUnix); err != nil {
			return nil, fmt.Errorf("scan memory candidate: %w", err)
		}
		candidates = append(candidates, Item{
			ID:        id,
			Text:      text,
			Metadata:  decodeStringMetadata(metadataRaw),
			CreatedAt: time.UnixMilli(createdAtUnix).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory candidates: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return limitItems(candidates, topK), nil
	}
	scored := bm25Score(queryTokens, candidates)
	if len(scored) == 0 {
		return limitItems(candidates, topK), nil
	}
	if len(scored) > topK {
		scored = scored[:topK]
	}
	out := make([]Item, 0, len(scored))
	for _, v := range scored {
		out = append(out, v.item)
	}
	return out, nil
}

// Get возвращает запись памяти по ID.
func (m *SQLiteLongTerm) Get(ctx context.Context, id string) (Item, bool, error) {
	if m == nil || m.db == nil {
		return Item{}, false, errors.New("sqlite long-term memory is not initialized")
	}
	if err := contextError(ctx); err != nil {
		return Item{}, false, err
	}
	var (
		text          string
		metadataRaw   string
		createdAtUnix int64
	)
	err := m.db.QueryRowContext(
		ctx,
		`SELECT text, metadata, created_at_unix FROM long_term_memory WHERE id = ?`,
		id,
	).Scan(&text, &metadataRaw, &createdAtUnix)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, false, nil
		}
		return Item{}, false, fmt.Errorf("query memory item: %w", err)
	}
	return Item{
		ID:        id,
		Text:      text,
		Metadata:  decodeStringMetadata(metadataRaw),
		CreatedAt: time.UnixMilli(createdAtUnix).UTC(),
	}, true, nil
}

// Delete удаляет запись по ID.
func (m *SQLiteLongTerm) Delete(ctx context.Context, id string) error {
	if m == nil || m.db == nil {
		return errors.New("sqlite long-term memory is not initialized")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, `DELETE FROM long_term_memory WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete memory item: %w", err)
	}
	return nil
}

func (m *SQLiteLongTerm) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`CREATE TABLE IF NOT EXISTS long_term_memory(
			id TEXT PRIMARY KEY,
			text TEXT NOT NULL,
			metadata TEXT NOT NULL,
			session_id TEXT NOT NULL,
			created_at_unix INTEGER NOT NULL,
			created_at_rfc3339 TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_long_term_memory_session_created
			ON long_term_memory(session_id, created_at_unix DESC)`,
	}
	for _, stmt := range statements {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init sqlite long-term schema: %w", err)
		}
	}
	return nil
}

func bm25Score(queryTokens map[string]struct{}, items []Item) []struct {
	item  Item
	score float64
} {
	if len(items) == 0 || len(queryTokens) == 0 {
		return nil
	}
	type stats struct {
		tf     map[string]int
		docLen int
	}
	perDoc := make([]stats, len(items))
	df := make(map[string]int, len(queryTokens))
	totalDocLen := 0

	for i := range items {
		tf, docLen := termFrequency(items[i].Text)
		perDoc[i] = stats{tf: tf, docLen: docLen}
		totalDocLen += docLen
		for token := range queryTokens {
			if tf[token] > 0 {
				df[token]++
			}
		}
	}

	n := float64(len(items))
	avgDocLen := float64(totalDocLen) / math.Max(n, 1)
	if avgDocLen <= 0 {
		avgDocLen = 1
	}
	const k1 = 1.2
	const b = 0.75

	now := time.Now().UTC()
	out := make([]struct {
		item  Item
		score float64
	}, 0, len(items))
	for i := range items {
		docTF := perDoc[i].tf
		docLen := float64(perDoc[i].docLen)
		if docLen <= 0 {
			docLen = 1
		}
		score := 0.0
		for token := range queryTokens {
			tf := float64(docTF[token])
			if tf == 0 {
				continue
			}
			dfv := float64(df[token])
			if dfv == 0 {
				continue
			}
			idf := math.Log(1 + (n-dfv+0.5)/(dfv+0.5))
			denom := tf + k1*(1-b+b*(docLen/avgDocLen))
			score += idf * ((tf * (k1 + 1)) / denom)
		}
		if score <= 0 {
			continue
		}
		ageHours := now.Sub(items[i].CreatedAt).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		score += 0.05 / (1 + ageHours/24.0)
		out = append(out, struct {
			item  Item
			score float64
		}{item: items[i], score: score})
	}

	sort.Slice(out, func(i, j int) bool {
		if math.Abs(out[i].score-out[j].score) < 1e-9 {
			return out[i].item.CreatedAt.After(out[j].item.CreatedAt)
		}
		return out[i].score > out[j].score
	})
	return out
}

func termFrequency(text string) (map[string]int, int) {
	parts := strings.Fields(strings.ToLower(text))
	out := make(map[string]int, len(parts))
	docLen := 0
	for _, part := range parts {
		token := strings.Trim(part, ".,:;!?()[]{}\"'`")
		if len(token) < 2 {
			continue
		}
		out[token]++
		docLen++
	}
	return out, docLen
}

func limitItems(items []Item, topK int) []Item {
	if topK <= 0 || len(items) <= topK {
		out := make([]Item, len(items))
		copy(out, items)
		return out
	}
	out := make([]Item, topK)
	copy(out, items[:topK])
	return out
}

func decodeStringMetadata(raw string) map[string]string {
	decoded := make(map[string]any)
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(decoded))
	for key, value := range decoded {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		out[trimmedKey] = strings.TrimSpace(fmt.Sprint(value))
	}
	return out
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
