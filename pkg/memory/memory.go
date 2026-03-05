package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/llm"
	"github.com/blanergol/agent-core/pkg/redact"
	"github.com/blanergol/agent-core/pkg/telemetry"
)

// Item представляет единицу долговременной памяти с содержимым и метаданными.
type Item struct {
	// ID позволяет стабильно ссылаться на запись памяти.
	ID string
	// Text хранит основное содержимое наблюдения/сообщения.
	Text string
	// Metadata содержит служебные признаки (роль, инструмент и т.п.).
	Metadata map[string]string
	// CreatedAt фиксирует время записи для сортировки и отладки.
	CreatedAt time.Time
}

// LongTermMemory задаёт контракт для долговременного хранилища памяти.
type LongTermMemory interface {
	// Store сохраняет элемент в долговременную память.
	Store(ctx context.Context, item Item) error
	// Recall возвращает topK наиболее релевантных элементов по запросу.
	Recall(ctx context.Context, query string, topK int) ([]Item, error)
	// Get возвращает запись по идентификатору, если она существует.
	Get(ctx context.Context, id string) (Item, bool, error)
	// Delete удаляет запись из долговременной памяти по идентификатору.
	Delete(ctx context.Context, id string) error
}

// WritePolicy определяет, как данные проходят privacy/safety-подготовку перед записью в long-term.
type WritePolicy interface {
	Prepare(ctx context.Context, item Item) (prepared Item, allow bool)
}

// DefaultWritePolicy применяет минимальную редукцию секретов и ограничение размера записи.
type DefaultWritePolicy struct {
	MaxChars int
}

// Prepare редактирует чувствительные фрагменты и отбрасывает пустые элементы.
func (p DefaultWritePolicy) Prepare(_ context.Context, item Item) (Item, bool) {
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = 2000
	}
	text := strings.TrimSpace(item.Text)
	if text == "" {
		return Item{}, false
	}
	text = redact.Text(text)
	if len(text) > maxChars {
		text = text[:maxChars]
	}
	item.Text = text
	if item.Metadata == nil {
		item.Metadata = map[string]string{}
	}
	cleanMetadata := make(map[string]string, len(item.Metadata))
	for key, value := range item.Metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		cleanMetadata[key] = redact.Text(strings.TrimSpace(value))
	}
	item.Metadata = cleanMetadata
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	return item, true
}

// InMemoryLongTerm хранит долговременную память в рамках одного процесса.
type InMemoryLongTerm struct {
	// mu защищает коллекцию items при параллельных чтениях/записях.
	mu sync.RWMutex
	// items хранит все записи в оперативной памяти текущего процесса.
	items []Item
}

// NewInMemoryLongTerm создаёт простую in-memory реализацию долговременной памяти.
func NewInMemoryLongTerm() *InMemoryLongTerm {
	return &InMemoryLongTerm{items: make([]Item, 0, 64)}
}

// Store добавляет новую запись в конец списка без дополнительной обработки.
func (m *InMemoryLongTerm) Store(ctx context.Context, item Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.Metadata == nil {
		item.Metadata = map[string]string{}
	}
	if strings.TrimSpace(item.Metadata["session_id"]) == "" {
		item.Metadata["session_id"] = telemetry.SessionFromContext(ctx).SessionID
	}
	m.items = append(m.items, item)
	return nil
}

// Recall оценивает релевантность записей по пересечению токенов и возвращает topK.
func (m *InMemoryLongTerm) Recall(ctx context.Context, query string, topK int) ([]Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if topK <= 0 {
		topK = 5
	}

	// session фильтрует выдачу только текущим контекстом сессии.
	session := telemetry.SessionFromContext(ctx)
	// filtered хранит элементы только активной сессии, чтобы контекст не протекал между запросами.
	filtered := make([]Item, 0, len(m.items))
	for _, item := range m.items {
		if strings.TrimSpace(item.Metadata["session_id"]) == session.SessionID {
			filtered = append(filtered, item)
		}
	}

	// scored содержит пары "элемент + численная релевантность".
	scored := scoreItems(query, filtered)
	if len(scored) > topK {
		scored = scored[:topK]
	}

	// out формируется из отсортированных элементов без внутренних оценок.
	out := make([]Item, 0, len(scored))
	for _, v := range scored {
		out = append(out, v.item)
	}
	return out, nil
}

// Get возвращает элемент долговременной памяти по ID.
func (m *InMemoryLongTerm) Get(_ context.Context, id string) (Item, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.items {
		if item.ID == id {
			return item, true, nil
		}
	}
	return Item{}, false, nil
}

// Delete удаляет элемент долговременной памяти по ID.
func (m *InMemoryLongTerm) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, item := range m.items {
		if item.ID != id {
			continue
		}
		m.items = append(m.items[:i], m.items[i+1:]...)
		return nil
	}
	return nil
}

// ShortTermMemory хранит короткое окно последних сообщений диалога.
type ShortTermMemory struct {
	// mu защищает буфер сообщений от гонок в многопоточном режиме.
	mu sync.RWMutex
	// maxMessages ограничивает размер окна истории перед свёрткой.
	maxMessages int
	// messages хранит текущий контекст диалога в порядке поступления.
	messages []llm.Message
}

// NewShortTermMemory создаёт буфер сообщений с дефолтной длиной окна.
func NewShortTermMemory(maxMessages int) *ShortTermMemory {
	if maxMessages <= 0 {
		maxMessages = 20
	}
	return &ShortTermMemory{
		maxMessages: maxMessages,
		messages:    make([]llm.Message, 0, maxMessages),
	}
}

// Add добавляет сообщение и запускает свёртку старых данных при переполнении.
func (m *ShortTermMemory) Add(message llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, message)
	if len(m.messages) > m.maxMessages {
		m.summarizeLocked()
	}
}

// Messages возвращает копию текущего буфера, чтобы вызывающий код не изменял внутреннее состояние.
func (m *ShortTermMemory) Messages() []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// out - изолированная копия истории сообщений.
	out := make([]llm.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

// Replace полностью заменяет содержимое short-term памяти на переданный снимок.
func (m *ShortTermMemory) Replace(messages []llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]llm.Message, len(messages))
	copy(m.messages, messages)
	if len(m.messages) > m.maxMessages {
		m.messages = m.messages[len(m.messages)-m.maxMessages:]
	}
}

// summarizeLocked сворачивает старую половину истории в одно безопасное резюме-данные.
func (m *ShortTermMemory) summarizeLocked() {
	if len(m.messages) < 2 {
		return
	}
	// cutoff делит историю на старую часть для свёртки и свежую часть для сохранения.
	cutoff := len(m.messages) / 2
	// old содержит сообщения, которые будут заменены summary.
	old := m.messages[:cutoff]
	// keep содержит более новые сообщения, которые сохраняются как есть.
	keep := m.messages[cutoff:]

	// parts аккумулирует компактные строки вида "role: content".
	parts := make([]string, 0, len(old))
	for _, msg := range old {
		role := string(msg.Role)
		content := truncate(msg.Content, 120)
		parts = append(parts, fmt.Sprintf("%s: %s", role, content))
	}
	// summary сохраняется как пользовательские данные, а не как системная инструкция.
	summary := llm.Message{
		Role:    llm.RoleUser,
		Content: "Conversation summary (data only): " + strings.Join(parts, " | "),
	}
	m.messages = append([]llm.Message{summary}, keep...)
}

// Manager объединяет short-term и long-term память для сборки контекста LLM.
type Manager struct {
	// shortTerm хранит недавний диалог для немедленного контекста.
	shortTerm *ShortTermMemory
	// longTerm хранит исторические наблюдения для семантического recall.
	longTerm LongTermMemory
	// recallTopK ограничивает число возвращаемых записей при recall.
	recallTopK int
	// shortTermMaxMessages нужен для создания изолированного run-scoped short-term буфера.
	shortTermMaxMessages int
	// tokenBudget ограничивает общий объём контекста, отправляемого в LLM.
	tokenBudget int
	// writePolicy контролирует privacy/safety-правила записи в long-term память.
	writePolicy WritePolicy
}

var _ core.Memory = (*Manager)(nil)

// NewManager объединяет кратковременную и долговременную память с дефолтами.
func NewManager(short *ShortTermMemory, long LongTermMemory, recallTopK int) *Manager {
	return NewManagerWithOptions(short, long, recallTopK, 2048)
}

// NewManagerWithOptions создаёт менеджер памяти с настраиваемым token budget.
func NewManagerWithOptions(short *ShortTermMemory, long LongTermMemory, recallTopK int, tokenBudget int) *Manager {
	if short == nil {
		short = NewShortTermMemory(20)
	}
	if long == nil {
		long = NewInMemoryLongTerm()
	}
	if recallTopK <= 0 {
		recallTopK = 5
	}
	if tokenBudget <= 0 {
		tokenBudget = 2048
	}
	return &Manager{
		shortTerm:            short,
		longTerm:             long,
		recallTopK:           recallTopK,
		shortTermMaxMessages: short.maxMessages,
		tokenBudget:          tokenBudget,
		writePolicy:          DefaultWritePolicy{},
	}
}

// NewRun создаёт run-scoped копию менеджера с новым short-term буфером и общей long-term памятью.
func (m *Manager) NewRun() core.Memory {
	if m == nil {
		return NewManager(nil, nil, 5)
	}
	return &Manager{
		shortTerm:            NewShortTermMemory(m.shortTermMaxMessages),
		longTerm:             m.longTerm,
		recallTopK:           m.recallTopK,
		shortTermMaxMessages: m.shortTermMaxMessages,
		tokenBudget:          m.tokenBudget,
		writePolicy:          m.writePolicy,
	}
}

// ShortTermSnapshot возвращает снимок кратковременного контекста текущего запуска.
func (m *Manager) ShortTermSnapshot() []core.Message {
	if m == nil || m.shortTerm == nil {
		return nil
	}
	return llmToCoreMessages(m.shortTerm.Messages())
}

// RestoreShortTerm восстанавливает кратковременный контекст из снапшота.
func (m *Manager) RestoreShortTerm(messages []core.Message) {
	if m == nil || m.shortTerm == nil {
		return
	}
	m.shortTerm.Replace(coreToLLMMessages(messages))
}

// SetWritePolicy переопределяет privacy-политику long-term записи.
func (m *Manager) SetWritePolicy(policy WritePolicy) {
	if m == nil || policy == nil {
		return
	}
	m.writePolicy = policy
}

// AddUserMessage добавляет пользовательское сообщение в обе памяти.
func (m *Manager) AddUserMessage(ctx context.Context, text string) error {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "memory.add_user_message", nil)
	var spanErr error
	defer func() { span.End(spanErr) }()
	// msg хранит нормализованное представление сообщения для short-term истории.
	msg := llm.Message{Role: llm.RoleUser, Content: text}
	m.shortTerm.Add(msg)
	session := telemetry.SessionFromContext(ctx)
	if err := m.storeLongTerm(ctx, "user", Item{
		ID:        newID("user"),
		Text:      text,
		CreatedAt: time.Now(),
		Metadata:  map[string]string{"role": "user", "session_id": session.SessionID},
	}); err != nil {
		spanErr = err
		return err
	}
	return nil
}

// AddAssistantMessage добавляет ответ ассистента в обе памяти.
func (m *Manager) AddAssistantMessage(ctx context.Context, text string) error {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "memory.add_assistant_message", nil)
	var spanErr error
	defer func() { span.End(spanErr) }()
	// msg сохраняет ответ ассистента в кратковременном контексте.
	msg := llm.Message{Role: llm.RoleAssistant, Content: text}
	m.shortTerm.Add(msg)
	session := telemetry.SessionFromContext(ctx)
	if err := m.storeLongTerm(ctx, "assistant", Item{
		ID:        newID("assistant"),
		Text:      text,
		CreatedAt: time.Now(),
		Metadata:  map[string]string{"role": "assistant", "session_id": session.SessionID},
	}); err != nil {
		spanErr = err
		return err
	}
	return nil
}

// AddToolResult сохраняет вывод инструмента как отдельное событие памяти.
func (m *Manager) AddToolResult(ctx context.Context, toolName, result string) error {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "memory.add_tool_result", map[string]any{"tool": toolName})
	var spanErr error
	defer func() { span.End(spanErr) }()
	// text сохраняет вывод инструмента как JSON-строку, чтобы не смешивать его с инструкциями.
	text := fmt.Sprintf("Untrusted tool output JSON: %s", marshalToolResult(toolName, result))
	m.shortTerm.Add(llm.Message{Role: llm.RoleTool, Content: text, Name: toolName})
	session := telemetry.SessionFromContext(ctx)
	if err := m.storeLongTerm(ctx, "tool", Item{
		ID:        newID("tool"),
		Text:      text,
		CreatedAt: time.Now(),
		Metadata:  map[string]string{"role": "tool", "tool": toolName, "session_id": session.SessionID},
	}); err != nil {
		spanErr = err
		return err
	}
	return nil
}

// AddSystemMessage добавляет системную подсказку в short-term память текущего запуска.
func (m *Manager) AddSystemMessage(_ context.Context, text string) error {
	m.shortTerm.Add(llm.Message{Role: llm.RoleSystem, Content: text})
	return nil
}

// Recall делегирует запрос долговременной памяти с заранее настроенным topK.
func (m *Manager) Recall(ctx context.Context, query string) ([]Item, error) {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "memory.recall", map[string]any{"top_k": m.recallTopK})
	var spanErr error
	defer func() { span.End(spanErr) }()

	items, err := m.longTerm.Recall(ctx, query, m.recallTopK)
	if err != nil {
		telemetry.MetricsFromContext(ctx).IncCounter("memory.recall", 1, map[string]string{"status": "error"})
		spanErr = err
		return nil, err
	}
	telemetry.MetricsFromContext(ctx).IncCounter("memory.recall", 1, map[string]string{"status": "ok"})
	telemetry.MetricsFromContext(ctx).ObserveHistogram("memory.recall.items", float64(len(items)), nil)
	return items, nil
}

// BuildContext строит контекст LLM из short-term истории и релевантных memory hints.
func (m *Manager) BuildContext(ctx context.Context, userInput string) ([]core.Message, error) {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "memory.build_context", nil)
	var spanErr error
	defer func() { span.End(spanErr) }()

	// messages содержит текущий "живой" диалог.
	messages := m.shortTerm.Messages()
	// recalled - найденные релевантные элементы из long-term памяти.
	recalled, err := m.Recall(ctx, userInput)
	if err != nil {
		telemetry.MetricsFromContext(ctx).IncCounter("memory.build_context", 1, map[string]string{"status": "error"})
		spanErr = err
		return nil, err
	}
	finalMessages := m.trimToTokenBudget(messages)
	if len(recalled) == 0 {
		telemetry.MetricsFromContext(ctx).IncCounter("memory.build_context", 1, map[string]string{"status": "ok"})
		telemetry.MetricsFromContext(ctx).ObserveHistogram("memory.context.tokens", float64(totalEstimatedTokens(finalMessages)), nil)
		return llmToCoreMessages(finalMessages), nil
	}

	// parts собирает укороченные фрагменты recall для передачи модели как недоверенные данные.
	parts := make([]string, 0, len(recalled))
	for _, item := range recalled {
		parts = append(parts, truncate(item.Text, 180))
	}
	// payload сериализует recall в JSON, чтобы модель трактовала его как данные, а не инструкцию.
	payload, _ := json.Marshal(map[string]any{
		"source":   "long_term_memory",
		"snippets": parts,
	})
	memoryHint := llm.Message{Role: llm.RoleTool, Name: "memory.recall", Content: string(payload)}
	finalMessages = m.trimToTokenBudget(append(messages, memoryHint))
	telemetry.MetricsFromContext(ctx).IncCounter("memory.build_context", 1, map[string]string{"status": "ok"})
	telemetry.MetricsFromContext(ctx).ObserveHistogram("memory.context.tokens", float64(totalEstimatedTokens(finalMessages)), nil)
	return llmToCoreMessages(finalMessages), nil
}

// storeLongTerm применяет policy и записывает элемент в долговременное хранилище с метриками.
func (m *Manager) storeLongTerm(ctx context.Context, role string, item Item) error {
	policy := m.writePolicy
	if policy == nil {
		policy = DefaultWritePolicy{}
	}
	prepared, allow := policy.Prepare(ctx, item)
	if !allow {
		telemetry.MetricsFromContext(ctx).IncCounter("memory.write", 1, map[string]string{"role": role, "status": "skipped"})
		return nil
	}
	if err := m.longTerm.Store(ctx, prepared); err != nil {
		telemetry.MetricsFromContext(ctx).IncCounter("memory.write", 1, map[string]string{"role": role, "status": "error"})
		return err
	}
	telemetry.MetricsFromContext(ctx).IncCounter("memory.write", 1, map[string]string{"role": role, "status": "ok"})
	telemetry.MetricsFromContext(ctx).ObserveHistogram("memory.write.chars", float64(len(prepared.Text)), map[string]string{"role": role})
	return nil
}

// totalEstimatedTokens суммирует приблизительную токен-оценку по всем сообщениям.
func llmToCoreMessages(messages []llm.Message) []core.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]core.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, core.Message{
			Role:    core.MessageRole(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		})
	}
	return out
}

// coreToLLMMessages конвертирует универсальные core-сообщения в формат LLM-провайдера.
func coreToLLMMessages(messages []core.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		})
	}
	return out
}

// totalEstimatedTokens оценивает суммарный token budget по набору сообщений.
func totalEstimatedTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg.Content)
	}
	return total
}

// trimToTokenBudget ограничивает размер контекста приблизительной токен-оценкой.
func (m *Manager) trimToTokenBudget(messages []llm.Message) []llm.Message {
	if m.tokenBudget <= 0 {
		return messages
	}
	// total оценивает суммарный объём токенов текущего контекста.
	total := 0
	for _, msg := range messages {
		total += estimateTokens(msg.Content)
	}
	if total <= m.tokenBudget {
		return messages
	}

	// kept накапливает наиболее релевантный хвост контекста (с конца).
	kept := make([]llm.Message, 0, len(messages))
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := estimateTokens(messages[i].Content)
		if used+msgTokens > m.tokenBudget {
			continue
		}
		used += msgTokens
		kept = append(kept, messages[i])
	}
	// Разворачиваем массив обратно в исходный порядок.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	// summary объясняет модели, что часть контекста была удалена политикой бюджета.
	summary := llm.Message{
		Role:    llm.RoleSystem,
		Content: "Context was truncated to fit token budget.",
	}
	return append([]llm.Message{summary}, kept...)
}

// estimateTokens грубо оценивает токены как четверть длины текста.
func estimateTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return len(text)/4 + 1
}

// scoreItems считает пересечение токенов запроса и текста элемента и сортирует по релевантности.
func scoreItems(query string, items []Item) []struct {
	item  Item
	score int
} {
	// queryTokens - множество нормализованных токенов пользовательского запроса.
	queryTokens := tokenize(query)
	// scored аккумулирует элементы с положительной оценкой релевантности.
	scored := make([]struct {
		item  Item
		score int
	}, 0, len(items))
	for _, item := range items {
		// tokens - токены текста конкретного элемента памяти.
		tokens := tokenize(item.Text)
		// score - количество общих токенов между запросом и элементом.
		score := overlap(queryTokens, tokens)
		if score == 0 {
			continue
		}
		scored = append(scored, struct {
			item  Item
			score int
		}{item: item, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].item.CreatedAt.After(scored[j].item.CreatedAt)
		}
		return scored[i].score > scored[j].score
	})
	return scored
}

// tokenize разбивает строку на нормализованные токены для грубого семантического поиска.
func tokenize(s string) map[string]struct{} {
	// tokens содержит слова в нижнем регистре до фильтрации пунктуации.
	tokens := strings.Fields(strings.ToLower(s))
	// out хранит уникальные токены как множество.
	out := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		t = strings.Trim(t, ".,:;!?()[]{}\"'`")
		if len(t) < 2 {
			continue
		}
		out[t] = struct{}{}
	}
	return out
}

// overlap возвращает размер пересечения двух множеств токенов.
func overlap(a, b map[string]struct{}) int {
	// score увеличивается на каждый общий токен.
	score := 0
	for k := range a {
		if _, ok := b[k]; ok {
			score++
		}
	}
	return score
}

// truncate обрезает строку до n символов и добавляет многоточие при необходимости.
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// newID генерирует простой time-based идентификатор записи памяти.
func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// marshalToolResult сериализует tool-output в JSON, чтобы безопасно передавать его как данные.
func marshalToolResult(toolName, result string) string {
	// payload хранит строгую структуру недоверенного ответа инструмента.
	payload := struct {
		Tool   string `json:"tool"`
		Output string `json:"output"`
	}{
		Tool:   toolName,
		Output: result,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return `{"tool":"unknown","output":"<serialization_error>"}`
	}
	return string(raw)
}
