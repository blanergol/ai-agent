package guardrails

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blanergol/agent-core/core"
)

// Config задает лимиты безопасности для одного запуска агента.
type Config struct {
	MaxSteps           int
	MaxToolCalls       int
	MaxDuration        time.Duration
	MaxToolOutputBytes int
	ToolAllowlist      []string
}

// Guardrails хранит счетчики и проверяет безопасность действий в рамках одного run.
type Guardrails struct {
	cfg Config

	mu        sync.Mutex
	startTime time.Time
	steps     int
	toolCalls int
}

var _ core.Guardrails = (*Guardrails)(nil)

// New создает guardrails с дефолтами, если лимиты не заданы явно.
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

// NewRun создает новый экземпляр guardrails для отдельного запуска с теми же лимитами.
func (g *Guardrails) NewRun() core.Guardrails {
	if g == nil {
		return New(Config{})
	}
	return &Guardrails{
		cfg:       g.cfg,
		startTime: time.Now(),
	}
}

// BeforeStep проверяет лимиты времени и количества шагов перед новой итерацией цикла.
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

// ValidateAction проверяет, что выбранное действие допустимо с учетом текущей конфигурации.
func (g *Guardrails) ValidateAction(action core.Action) error {
	switch action.Type {
	case core.ActionTypeTool:
		if strings.TrimSpace(action.ToolName) == "" {
			return fmt.Errorf("tool action without tool name")
		}
		if len(g.cfg.ToolAllowlist) > 0 && !contains(g.cfg.ToolAllowlist, action.ToolName) {
			return fmt.Errorf("tool blocked by allowlist: %s", action.ToolName)
		}
	case core.ActionTypeFinal, core.ActionTypeNoop:
		return nil
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
	return nil
}

// RecordToolCall учитывает вызов инструмента и проверяет лимиты количества/размера вывода.
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

// Stats возвращает текущую статистику run: шаги, tool-вызовы и прошедшее время.
func (g *Guardrails) Stats() (steps int, toolCalls int, elapsed time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.steps, g.toolCalls, time.Since(g.startTime)
}

// Snapshot сериализует текущее состояние счетчиков для последующего восстановления.
func (g *Guardrails) Snapshot() core.GuardrailsSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return core.GuardrailsSnapshot{
		Steps:     g.steps,
		ToolCalls: g.toolCalls,
		Elapsed:   time.Since(g.startTime),
	}
}

// Restore восстанавливает состояние счетчиков из ранее сохраненного snapshot.
func (g *Guardrails) Restore(snapshot core.GuardrailsSnapshot) {
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

// contains выполняет регистронезависимую проверку значения в списке.
func contains(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}
