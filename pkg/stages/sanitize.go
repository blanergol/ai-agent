package stages

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// Sanitizer нормализует пользовательский ввод перед построением наблюдения и планированием.
type Sanitizer interface {
	Sanitize(text string) (string, error)
}

// SanitizeStage применяет Sanitizer к входному тексту run-контекста.
type SanitizeStage struct {
	sanitizer Sanitizer
}

// NewSanitizeStage создает стадию нормализации пользовательского ввода.
func NewSanitizeStage(sanitizer Sanitizer) core.Stage {
	return &SanitizeStage{sanitizer: sanitizer}
}

// Name возвращает стабильный идентификатор стадии нормализации.
func (s *SanitizeStage) Name() string { return "sanitize" }

// Run нормализует входной текст и записывает его обратно в run-контекст.
func (s *SanitizeStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if s.sanitizer == nil || run.PendingStop {
		return core.Continue(), nil
	}
	if err := run.ExecutePhase(ctx, core.PhaseNormalize, func(_ context.Context) error {
		normalized, err := s.sanitizer.Sanitize(run.Input.Text)
		if err != nil {
			return err
		}
		run.Input.Text = strings.TrimSpace(normalized)
		if run.State != nil {
			run.State.NormalizedInput = run.Input.Text
		}
		return nil
	}); err != nil {
		return core.StageResult{}, err
	}
	return core.Continue(), nil
}
