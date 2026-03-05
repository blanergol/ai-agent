package stages

import (
	"fmt"

	"github.com/blanergol/agent-core/core"
)

// FactoryConfig задает зависимости, которые передаются фабрикам стадий при сборке пайплайна.
type FactoryConfig struct {
	Sanitizer Sanitizer
}

// Factory создает экземпляр стадии по конфигурации сборки.
type Factory func(cfg FactoryConfig) core.Stage

// PipelineMutation описывает изменение уже собранного пайплайна стадий.
type PipelineMutation interface {
	Apply(p *core.Pipeline) error
}

// PipelineMutationFunc позволяет использовать обычную функцию как mutation-объект.
type PipelineMutationFunc func(p *core.Pipeline) error

// Apply применяет mutation-функцию к пайплайну.
func (f PipelineMutationFunc) Apply(p *core.Pipeline) error {
	if f == nil {
		return nil
	}
	return f(p)
}

// Registry хранит фабрики стадий и собирает пайплайн из именованных этапов.
type Registry struct {
	factories map[string]Factory
}

// DefaultOrder определяет стандартный порядок стадий базового цикла агента.
var DefaultOrder = []string{"observe", "enrich_context", "plan", "act", "reflect", "stop"}

// NewRegistry создает реестр стадий с базовым набором фабрик.
func NewRegistry() *Registry {
	r := &Registry{factories: map[string]Factory{}}
	r.Register("observe", func(_ FactoryConfig) core.Stage { return NewObserveStage() })
	r.Register("enrich_context", func(_ FactoryConfig) core.Stage { return NewEnrichContextStage() })
	r.Register("plan", func(_ FactoryConfig) core.Stage { return NewPlanStage() })
	r.Register("act", func(_ FactoryConfig) core.Stage { return NewActStage() })
	r.Register("reflect", func(_ FactoryConfig) core.Stage { return NewReflectStage() })
	r.Register("stop", func(_ FactoryConfig) core.Stage { return NewStopStage() })
	r.Register("sanitize", func(cfg FactoryConfig) core.Stage { return NewSanitizeStage(cfg.Sanitizer) })
	return r
}

// BuildDefaultPipeline собирает стандартный пайплайн и применяет переданные мутации.
func BuildDefaultPipeline(cfg FactoryConfig, mutations ...PipelineMutation) (*core.Pipeline, error) {
	registry := NewRegistry()
	return registry.Build(DefaultOrder, cfg, mutations...)
}

// Register регистрирует фабрику стадии по имени.
func (r *Registry) Register(name string, factory Factory) {
	if r == nil || name == "" || factory == nil {
		return
	}
	r.factories[name] = factory
}

// Build собирает пайплайн в заданном порядке и применяет все мутации по очереди.
func (r *Registry) Build(order []string, cfg FactoryConfig, mutations ...PipelineMutation) (*core.Pipeline, error) {
	stages := make([]core.Stage, 0, len(order))
	for _, name := range order {
		factory, ok := r.factories[name]
		if !ok {
			return nil, fmt.Errorf("stage is not registered: %s", name)
		}
		stage := factory(cfg)
		if stage == nil {
			return nil, fmt.Errorf("stage factory returned nil: %s", name)
		}
		stages = append(stages, stage)
	}
	pipeline := core.NewPipeline(stages...)
	for _, mutation := range mutations {
		if mutation == nil {
			continue
		}
		if err := mutation.Apply(pipeline); err != nil {
			return nil, err
		}
	}
	return pipeline, nil
}

// Append добавляет стадию в конец пайплайна.
func Append(stage core.Stage) PipelineMutation {
	return PipelineMutationFunc(func(p *core.Pipeline) error {
		if stage == nil {
			return fmt.Errorf("append stage is nil")
		}
		p.Append(stage)
		return nil
	})
}

// InsertBefore вставляет стадию перед указанной целевой стадией.
func InsertBefore(target string, stage core.Stage) PipelineMutation {
	return PipelineMutationFunc(func(p *core.Pipeline) error {
		if stage == nil {
			return fmt.Errorf("insert-before stage is nil")
		}
		if ok := p.InsertBefore(target, stage); !ok {
			return fmt.Errorf("insert before failed, target not found: %s", target)
		}
		return nil
	})
}

// InsertAfter вставляет стадию сразу после указанной целевой стадии.
func InsertAfter(target string, stage core.Stage) PipelineMutation {
	return PipelineMutationFunc(func(p *core.Pipeline) error {
		if stage == nil {
			return fmt.Errorf("insert-after stage is nil")
		}
		if ok := p.InsertAfter(target, stage); !ok {
			return fmt.Errorf("insert after failed, target not found: %s", target)
		}
		return nil
	})
}

// Replace заменяет целевую стадию новым экземпляром.
func Replace(target string, stage core.Stage) PipelineMutation {
	return PipelineMutationFunc(func(p *core.Pipeline) error {
		if stage == nil {
			return fmt.Errorf("replace stage is nil")
		}
		if ok := p.Replace(target, stage); !ok {
			return fmt.Errorf("replace failed, target not found: %s", target)
		}
		return nil
	})
}

// Remove удаляет целевую стадию из пайплайна.
func Remove(target string) PipelineMutation {
	return PipelineMutationFunc(func(p *core.Pipeline) error {
		if ok := p.Remove(target); !ok {
			return fmt.Errorf("remove failed, target not found: %s", target)
		}
		return nil
	})
}
