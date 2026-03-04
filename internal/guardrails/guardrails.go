package guardrails

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blanergol/agent-core/internal/planner"
)

// Config задаёт лимиты и политики безопасности для выполнения агента.
type Config struct {
	// MaxSteps ограничивает количество шагов в одной сессии выполнения.
	MaxSteps int
	// MaxToolCalls ограничивает число вызовов инструментов.
	MaxToolCalls int
	// MaxDuration ограничивает общее время выполнения задачи.
	MaxDuration time.Duration
	// MaxToolOutputBytes ограничивает размер ответа одного инструмента.
	MaxToolOutputBytes int
	// ToolAllowlist содержит явно разрешённые имена инструментов.
	ToolAllowlist []string
}

// UserAuthContext несёт идентификатор субъекта для авторизационных проверок.
type UserAuthContext struct {
	// Subject хранит идентификатор пользователя/субъекта для контекстной авторизации.
	Subject string
}

// Guardrails хранит состояние лимитов и счётчиков во время выполнения одного запуска.
type Guardrails struct {
	// cfg хранит статические лимиты и политики безопасности.
	cfg Config

	// mu защищает счётчики и время старта от гонок между шагами.
	mu sync.Mutex
	// startTime фиксирует момент начала сессии для контроля общего времени.
	startTime time.Time
	// steps считает уже выполненные шаги цикла.
	steps int
	// toolCalls считает количество вызовов инструментов.
	toolCalls int
}

// RuntimeSnapshot хранит минимальное сериализуемое состояние guardrails для продолжения выполнения.
type RuntimeSnapshot struct {
	// Steps хранит уже выполненное количество шагов.
	Steps int
	// ToolCalls хранит количество совершённых tool-вызовов.
	ToolCalls int
	// Elapsed хранит уже потраченное время выполнения.
	Elapsed time.Duration
}

// New создаёт guardrails и нормализует дефолтные лимиты, если они не заданы.
func New(cfg Config) *Guardrails {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 8
	}
	if cfg.MaxToolCalls <= 0 {
		cfg.MaxToolCalls = cfg.MaxSteps
	}
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = 2 * time.Minute
	}
	if cfg.MaxToolOutputBytes <= 0 {
		cfg.MaxToolOutputBytes = 64 * 1024
	}
	return &Guardrails{cfg: cfg, startTime: time.Now()}
}

// NewRun создаёт изолированный экземпляр guardrails для одного запуска агента.
func (g *Guardrails) NewRun() *Guardrails {
	if g == nil {
		return New(Config{})
	}
	return &Guardrails{
		cfg:       g.cfg,
		startTime: time.Now(),
	}
}

// BeforeStep проверяет лимиты времени/шагов и увеличивает счётчик шага.
func (g *Guardrails) BeforeStep() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if time.Since(g.startTime) > g.cfg.MaxDuration {
		return fmt.Errorf("time limit exceeded: %s", g.cfg.MaxDuration)
	}
	if g.steps >= g.cfg.MaxSteps {
		return fmt.Errorf("max steps exceeded: %d", g.cfg.MaxSteps)
	}
	g.steps++
	return nil
}

// ValidateAction фильтрует недопустимые типы действий и блокирует запрещённые инструменты.
func (g *Guardrails) ValidateAction(action planner.Action) error {
	switch action.Type {
	case "tool":
		if action.ToolName == "" {
			return fmt.Errorf("tool action without tool name")
		}
		if len(g.cfg.ToolAllowlist) > 0 && !contains(g.cfg.ToolAllowlist, action.ToolName) {
			return fmt.Errorf("tool blocked by allowlist: %s", action.ToolName)
		}
	case "final", "noop":
		return nil
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
	return nil
}

// RecordToolCall фиксирует вызов инструмента и валидирует лимиты по количеству и размеру вывода.
func (g *Guardrails) RecordToolCall(outputSize int) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.toolCalls++
	if g.toolCalls > g.cfg.MaxToolCalls {
		return fmt.Errorf("max tool calls exceeded: %d", g.cfg.MaxToolCalls)
	}
	if outputSize > g.cfg.MaxToolOutputBytes {
		return fmt.Errorf("tool output exceeds max size: %d", outputSize)
	}
	return nil
}

// Stats возвращает текущие счётчики и прошедшее время выполнения.
func (g *Guardrails) Stats() (steps int, toolCalls int, elapsed time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.steps, g.toolCalls, time.Since(g.startTime)
}

// Snapshot возвращает снимок счётчиков guardrails для последующего восстановления.
func (g *Guardrails) Snapshot() RuntimeSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return RuntimeSnapshot{
		Steps:     g.steps,
		ToolCalls: g.toolCalls,
		Elapsed:   time.Since(g.startTime),
	}
}

// Restore восстанавливает счётчики guardrails из ранее сохранённого снапшота.
func (g *Guardrails) Restore(snapshot RuntimeSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if snapshot.Steps < 0 {
		snapshot.Steps = 0
	}
	if snapshot.ToolCalls < 0 {
		snapshot.ToolCalls = 0
	}
	if snapshot.Elapsed < 0 {
		snapshot.Elapsed = 0
	}
	g.steps = snapshot.Steps
	g.toolCalls = snapshot.ToolCalls
	g.startTime = time.Now().Add(-snapshot.Elapsed)
}

// contains выполняет регистронезависимую проверку вхождения значения в список.
func contains(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}
