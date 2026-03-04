package output

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/blanergol/agent-core/internal/apperrors"
)

// secretPattern выявляет типичные маркеры утечки секретов в тексте ответа.
var secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|bearer|token)\s*[:=]\s*\S+`)

// Policy описывает минимальные ограничения на финальный ответ агента.
type Policy struct {
	// MaxChars ограничивает длину финального ответа.
	MaxChars int
	// ForbiddenSubstrings задаёт список запрещённых подстрок (регистр игнорируется).
	ForbiddenSubstrings []string
}

// Validator проверяет финальный ответ перед отдачей пользователю.
type Validator interface {
	// Validate возвращает ошибку при нарушении политики вывода.
	Validate(ctx context.Context, text string) error
}

// PolicyValidator реализует Validator на базе простой контентной политики.
type PolicyValidator struct {
	// policy хранит конфигурацию проверок.
	policy Policy
}

// NewPolicyValidator создаёт validator с безопасными дефолтами.
func NewPolicyValidator(policy Policy) *PolicyValidator {
	if policy.MaxChars <= 0 {
		policy.MaxChars = 8000
	}
	return &PolicyValidator{policy: policy}
}

// Validate проверяет размер ответа, запрещённые фрагменты и признаки утечки секретов.
func (v *PolicyValidator) Validate(_ context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return apperrors.New(apperrors.CodeValidation, "empty final response", false)
	}
	if len(text) > v.policy.MaxChars {
		return apperrors.Wrap(
			apperrors.CodeValidation,
			fmt.Sprintf("final response exceeds max chars: %d", v.policy.MaxChars),
			nil,
			false,
		)
	}
	normalized := strings.ToLower(text)
	for _, banned := range v.policy.ForbiddenSubstrings {
		if strings.TrimSpace(banned) == "" {
			continue
		}
		if strings.Contains(normalized, strings.ToLower(strings.TrimSpace(banned))) {
			return apperrors.New(apperrors.CodeForbidden, "final response contains forbidden content", false)
		}
	}
	if secretPattern.MatchString(text) {
		return apperrors.New(apperrors.CodeForbidden, "final response contains potential secret leakage", false)
	}
	return nil
}
