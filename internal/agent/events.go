package agent

import (
	"context"
	"time"
)

// EventType перечисляет типы событий жизненного цикла выполнения агента.
type EventType string

const (
	// EventRunStarted публикуется при старте обработки пользовательского запроса.
	EventRunStarted EventType = "run_started"
	// EventStepPlanned публикуется после выбора очередного действия планировщиком.
	EventStepPlanned EventType = "step_planned"
	// EventToolCompleted публикуется после успешного вызова инструмента.
	EventToolCompleted EventType = "tool_completed"
	// EventToolFailed публикуется при ошибке выполнения инструмента.
	EventToolFailed EventType = "tool_failed"
	// EventOutputInvalid публикуется при отклонении финального ответа валидатором.
	EventOutputInvalid EventType = "output_invalid"
	// EventRunCompleted публикуется при штатном завершении цикла выполнения.
	EventRunCompleted EventType = "run_completed"
	// EventRunFailed публикуется при аварийном завершении с ошибкой.
	EventRunFailed EventType = "run_failed"
)

// Event описывает единичное наблюдаемое событие выполнения агента.
type Event struct {
	Type      EventType
	Timestamp time.Time
	// SessionID связывает события внутри одной сессии.
	SessionID string
	// CorrelationID связывает события одного запроса/вызова.
	CorrelationID string
	// UserSub хранит subject пользователя для аудита.
	UserSub string

	Step       int
	ActionType string
	ToolName   string
	StopReason string

	InputHash  string
	OutputHash string
	Error      string
}

// Observer принимает события выполнения агента для внешнего аудита и метрик.
type Observer interface {
	OnEvent(ctx context.Context, event Event)
}

// noopObserver игнорирует все события и используется как безопасный fallback.
type noopObserver struct{}

// OnEvent в noop-реализации намеренно не выполняет действий.
func (noopObserver) OnEvent(_ context.Context, _ Event) {}
