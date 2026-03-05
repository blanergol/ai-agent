package skills

import (
	"fmt"
	"strings"

	"github.com/blanergol/agent-core/pkg/stages"
	"github.com/blanergol/agent-core/pkg/tools"
)

// Skill описывает бизнес-примитив, который регистрирует инструменты и подсказки.
type Skill interface {
	Name() string
	Register(registry *tools.Registry) error
	PromptAdditions() []string
}

// PipelineExtender позволяет skill-у дополнительно изменять базовый pipeline.
type PipelineExtender interface {
	PipelineMutations() []stages.PipelineMutation
}

// ApplyResult аккумулирует эффекты применения skills для runtime wiring.
type ApplyResult struct {
	PromptAdditions []string
	Pipeline        []stages.PipelineMutation
}

// Registry хранит доступные реализации skills и применяет их по имени.
type Registry struct {
	skills map[string]Skill
}

// NewRegistry создает пустой реестр skills.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Register добавляет навык в реестр и проверяет уникальность его имени.
func (r *Registry) Register(skill Skill) error {
	if skill == nil {
		return fmt.Errorf("nil skill")
	}
	name := strings.ToLower(strings.TrimSpace(skill.Name()))
	if name == "" {
		return fmt.Errorf("skill name is empty")
	}
	if _, exists := r.skills[name]; exists {
		return fmt.Errorf("skill already registered: %s", name)
	}
	r.skills[name] = skill
	return nil
}

// Apply активирует выбранные навыки, регистрирует их инструменты и собирает мутации пайплайна.
func (r *Registry) Apply(names []string, toolRegistry *tools.Registry) (ApplyResult, error) {
	result := ApplyResult{}
	for _, name := range names {
		skill, ok := r.skills[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return ApplyResult{}, fmt.Errorf("skill not found: %s", name)
		}
		if err := skill.Register(toolRegistry); err != nil {
			return ApplyResult{}, fmt.Errorf("register skill %s: %w", name, err)
		}
		result.PromptAdditions = append(result.PromptAdditions, skill.PromptAdditions()...)
		if extender, ok := skill.(PipelineExtender); ok {
			result.Pipeline = append(result.Pipeline, extender.PipelineMutations()...)
		}
	}
	return result, nil
}
