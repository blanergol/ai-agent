package planner

import (
	"fmt"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ToolSelectionPolicy определяет стратегию и валидацию выбора инструментов planner-ом.
type ToolSelectionPolicy interface {
	StrategyPrompt(obs Observation) string
	ValidateSelection(obs Observation, action Action) error
}

// StrictToolSelectionPolicy требует, чтобы tool_name был из каталога доступных инструментов.
type StrictToolSelectionPolicy struct{}

// StrategyPrompt возвращает подсказку для LLM о правилах выбора инструментов.
func (StrictToolSelectionPolicy) StrategyPrompt(_ Observation) string {
	return "When action.type=tool, choose tool_name strictly from Available tools JSON and keep tool_args minimal. Use kv.get/kv.put only for explicit state-management requests from the user."
}

// ValidateSelection проверяет, что выбранный инструмент допустим в текущем каталоге и контексте.
func (StrictToolSelectionPolicy) ValidateSelection(obs Observation, action Action) error {
	if action.Type != core.ActionTypeTool {
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

// isKVToolName проверяет, относится ли имя инструмента к state-хранилищу KV.
func isKVToolName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(normalized, "kv.")
}

// looksLikeStateManagementIntent определяет, просит ли пользователь работу с сохраненным state.
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
