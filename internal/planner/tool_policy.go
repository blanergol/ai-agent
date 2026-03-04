package planner

import (
	"fmt"
	"strings"
)

// ToolSelectionPolicy задаёт контракт стратегии выбора инструмента в planner.
type ToolSelectionPolicy interface {
	// StrategyPrompt возвращает короткое правило, которое planner добавляет в system prompt.
	StrategyPrompt(obs Observation) string
	// ValidateSelection проверяет соответствие выбранного action внутренней tool-политике.
	ValidateSelection(obs Observation, action Action) error
}

// StrictToolSelectionPolicy требует, чтобы planner выбирал только инструменты из текущего каталога.
type StrictToolSelectionPolicy struct{}

// StrategyPrompt фиксирует базовую стратегию безопасного выбора инструмента.
func (StrictToolSelectionPolicy) StrategyPrompt(_ Observation) string {
	return "When action.type=tool, choose tool_name strictly from Available tools JSON and keep tool_args minimal. Use kv.get/kv.put only for explicit state-management requests from the user."
}

// ValidateSelection проверяет, что tool_name существует в ToolCatalog observation.
func (StrictToolSelectionPolicy) ValidateSelection(obs Observation, action Action) error {
	if action.Type != "tool" {
		return nil
	}
	toolName := strings.TrimSpace(action.ToolName)
	if toolName == "" {
		return fmt.Errorf("tool_name is empty")
	}
	matchedToolName := ""
	for _, spec := range obs.ToolCatalog {
		if strings.EqualFold(strings.TrimSpace(spec.Name), toolName) {
			matchedToolName = strings.TrimSpace(spec.Name)
			break
		}
	}
	if matchedToolName == "" {
		return fmt.Errorf("tool is not present in planner catalog: %s", toolName)
	}
	if isKVToolName(matchedToolName) && !looksLikeStateManagementIntent(obs.UserInput) {
		return fmt.Errorf("kv tools require explicit state-management intent in user request")
	}
	return nil
}

func isKVToolName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(normalized, "kv.")
}

func looksLikeStateManagementIntent(userInput string) bool {
	normalized := strings.ToLower(strings.TrimSpace(userInput))
	if normalized == "" {
		return false
	}
	keywords := []string{
		"save", "store", "remember", "recall", "persist", "state", "key", "value",
		"lookup", "retrieve", "read from state", "write to state",
		"сохрани", "запомни", "запиши", "в state", "из state", "ключ", "значение", "память",
	}
	for _, kw := range keywords {
		if strings.Contains(normalized, kw) {
			return true
		}
	}
	return false
}
