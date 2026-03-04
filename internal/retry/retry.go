package retry

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net"
	"time"

	"github.com/blanergol/agent-core/internal/apperrors"
)

// Policy задаёт единую стратегию retry/backoff для внешних интеграций.
type Policy struct {
	// MaxRetries задаёт число повторов после первой попытки.
	MaxRetries int
	// BaseDelay задаёт базовую экспоненциальную задержку.
	BaseDelay time.Duration
	// MaxDelay ограничивает верхнюю границу задержки.
	MaxDelay time.Duration
	// DisableJitter отключает случайную компоненту backoff.
	DisableJitter bool
}

// Classifier определяет, нужно ли повторять ошибку.
type Classifier func(err error) bool

// DefaultClassifier считает retryable контекстные timeout/transient ошибки.
func DefaultClassifier(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if apperrors.RetryableOf(err) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// Do выполняет операцию с retry/backoff по заданной политике.
func Do(ctx context.Context, policy Policy, classifier Classifier, fn func(context.Context) error) error {
	p := normalize(policy)
	if classifier == nil {
		classifier = DefaultClassifier
	}

	var lastErr error
	for attempt := 0; attempt <= p.MaxRetries; attempt++ {
		if err := contextErr(ctx); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt >= p.MaxRetries || !classifier(err) {
			break
		}
		if err := sleep(ctx, p, attempt); err != nil {
			return err
		}
	}
	return lastErr
}

// normalize применяет безопасные значения по умолчанию к retry-политике.
func normalize(policy Policy) Policy {
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = 200 * time.Millisecond
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 5 * time.Second
	}
	return policy
}

// sleep выдерживает backoff-паузу между попытками с учётом jitter и отмены контекста.
func sleep(ctx context.Context, policy Policy, attempt int) error {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 8 {
		attempt = 8
	}
	delay := time.Duration(float64(policy.BaseDelay) * math.Pow(2, float64(attempt)))
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}
	jitter := time.Duration(0)
	if !policy.DisableJitter {
		jitter = time.Duration(rand.Int63n(int64(100 * time.Millisecond)))
	}
	timer := time.NewTimer(delay + jitter)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// contextErr возвращает текущую ошибку контекста и безопасно обрабатывает nil-контекст.
func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
