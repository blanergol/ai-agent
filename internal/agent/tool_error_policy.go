package agent

import (
	"context"
	"fmt"
	"strings"
)

// ToolErrorMode определяет стратегию реакции на ошибку вызова инструмента.
type ToolErrorMode string

const (
	// ToolErrorModeFail завершает выполнение при ошибке инструмента.
	ToolErrorModeFail ToolErrorMode = "fail"
	// ToolErrorModeContinue разрешает продолжить цикл после ошибки инструмента.
	ToolErrorModeContinue ToolErrorMode = "continue"
)

// ToolErrorDecision описывает результат policy-решения по ошибке tool-вызова.
type ToolErrorDecision struct {
	Mode ToolErrorMode
}

// Continue возвращает true, если policy разрешает продолжить цикл после tool-ошибки.
func (d ToolErrorDecision) Continue() bool {
	return d.Mode == ToolErrorModeContinue
}

// ToolErrorPolicy задаёт контракт graceful degradation при ошибках инструментов.
type ToolErrorPolicy interface {
	Decide(ctx context.Context, toolName string, err error) ToolErrorDecision
}

// StaticToolErrorPolicy поддерживает default-mode и точечные per-tool overrides.
type StaticToolErrorPolicy struct {
	defaultMode ToolErrorMode
	perTool     map[string]ToolErrorMode
}

// NewStaticToolErrorPolicy создаёт policy с явным default режимом и per-tool fallback картой.
func NewStaticToolErrorPolicy(defaultMode ToolErrorMode, perTool map[string]ToolErrorMode) *StaticToolErrorPolicy {
	mode := normalizeToolErrorMode(defaultMode)
	overrides := make(map[string]ToolErrorMode, len(perTool))
	for tool, rawMode := range perTool {
		key := strings.ToLower(strings.TrimSpace(tool))
		if key == "" {
			continue
		}
		overrides[key] = normalizeToolErrorMode(rawMode)
	}
	return &StaticToolErrorPolicy{
		defaultMode: mode,
		perTool:     overrides,
	}
}

// Decide возвращает режим обработки ошибки для конкретного инструмента.
func (p *StaticToolErrorPolicy) Decide(_ context.Context, toolName string, _ error) ToolErrorDecision {
	if p == nil {
		return ToolErrorDecision{Mode: ToolErrorModeFail}
	}
	key := strings.ToLower(strings.TrimSpace(toolName))
	if mode, ok := p.perTool[key]; ok {
		return ToolErrorDecision{Mode: mode}
	}
	return ToolErrorDecision{Mode: p.defaultMode}
}

// ParseToolErrorMode парсит строковый режим из конфигурации.
func ParseToolErrorMode(raw string) (ToolErrorMode, error) {
	mode := normalizeToolErrorMode(ToolErrorMode(raw))
	switch mode {
	case ToolErrorModeFail, ToolErrorModeContinue:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported tool error mode: %s", raw)
	}
}

// normalizeToolErrorMode приводит режим к поддерживаемому значению с fallback в fail.
func normalizeToolErrorMode(mode ToolErrorMode) ToolErrorMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(ToolErrorModeContinue):
		return ToolErrorModeContinue
	case string(ToolErrorModeFail):
		return ToolErrorModeFail
	default:
		return ToolErrorModeFail
	}
}
