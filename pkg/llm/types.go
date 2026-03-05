package llm

import (
	"context"
	"encoding/json"
)

// Role задаёт роль сообщения в истории диалога с моделью.
type Role string

// Роли сообщений, из которых строится диалог с моделью.
const (
	// RoleSystem используется для инструкций, задающих правила поведения модели.
	RoleSystem Role = "system"
	// RoleUser представляет прямой ввод от пользователя.
	RoleUser Role = "user"
	// RoleAssistant представляет ответы модели/агента.
	RoleAssistant Role = "assistant"
	// RoleTool представляет недоверенный вывод инструмента в истории.
	RoleTool Role = "tool"
)

// Message описывает одно сообщение в запросе к LLM.
type Message struct {
	// Role определяет тип сообщения в диалоге с моделью.
	Role Role
	// Content содержит текст сообщения.
	Content string
	// Name опционально хранит имя инструмента или источника сообщения.
	Name string
}

// ChatOptions описывает параметры генерации ответа модели.
type ChatOptions struct {
	// Temperature регулирует случайность генерации.
	Temperature float64
	// TopP задаёт top-p sampling для генерации.
	TopP float64
	// Seed фиксирует детерминизм генерации (если поддерживается провайдером).
	Seed int
	// MaxTokens ограничивает размер генерируемого ответа.
	MaxTokens int
}

// StreamChunk представляет фрагмент потокового ответа модели.
type StreamChunk struct {
	// Delta содержит новую часть текста ответа.
	Delta string
	// Done сигнализирует о завершении стрима.
	Done bool
}

// Provider определяет единый контракт для LLM-провайдеров.
type Provider interface {
	// Name возвращает идентификатор подключённого провайдера.
	Name() string
	// Chat выполняет обычный текстовый запрос к модели.
	Chat(ctx context.Context, messages []Message, opts ChatOptions) (string, error)
	// ChatStream возвращает поток частичных фрагментов ответа с поддержкой отмены контекста.
	ChatStream(ctx context.Context, messages []Message, opts ChatOptions) (<-chan StreamChunk, <-chan error)
	// ChatJSON запрашивает JSON-ответ, валидируемый по переданной схеме.
	ChatJSON(ctx context.Context, messages []Message, jsonSchema string, opts ChatOptions) (json.RawMessage, error)
}
