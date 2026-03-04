package skills

import (
	"fmt"
	"strings"

	"github.com/blanergol/agent-core/internal/tools"
)

// Skill описывает подключаемый модуль, который добавляет инструменты и подсказки.
type Skill interface {
	// Name возвращает идентификатор навыка, используемый в конфигурации.
	Name() string
	// Register добавляет инструменты навыка в общий реестр.
	Register(registry *tools.Registry) error
	// PromptAdditions возвращает дополнительные системные подсказки для планировщика.
	PromptAdditions() []string
}

// Registry хранит доступные навыки и применяет их к общему реестру инструментов.
type Registry struct {
	// skills индексирует зарегистрированные навыки по нормализованному имени.
	skills map[string]Skill
}

// NewRegistry создаёт пустой реестр доступных навыков.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Register валидирует и добавляет навык, предотвращая дубли.
func (r *Registry) Register(skill Skill) error {
	if skill == nil {
		return fmt.Errorf("nil skill")
	}
	// name нормализуется в lower-case для регистронезависимого обращения.
	name := strings.ToLower(skill.Name())
	if name == "" {
		return fmt.Errorf("skill name is empty")
	}
	if _, exists := r.skills[name]; exists {
		return fmt.Errorf("skill already registered: %s", name)
	}
	r.skills[name] = skill
	return nil
}

// Apply включает перечисленные навыки, регистрирует их инструменты и собирает prompt additions.
func (r *Registry) Apply(names []string, toolRegistry *tools.Registry) ([]string, error) {
	// prompts аккумулирует подсказки от каждого успешно применённого навыка.
	prompts := make([]string, 0)
	for _, name := range names {
		skill, ok := r.skills[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("skill not found: %s", name)
		}
		if err := skill.Register(toolRegistry); err != nil {
			return nil, fmt.Errorf("register skill %s: %w", name, err)
		}
		prompts = append(prompts, skill.PromptAdditions()...)
	}
	return prompts, nil
}
