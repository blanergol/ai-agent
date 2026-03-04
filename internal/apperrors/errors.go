package apperrors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Code задаёт стабильный типизированный код ошибки приложения.
type Code string

const (
	// CodeBadRequest сигнализирует о некорректном формате входных данных.
	CodeBadRequest Code = "BAD_REQUEST"
	// CodeValidation сигнализирует о нарушении схем/бизнес-ограничений.
	CodeValidation Code = "VALIDATION"
	// CodeTimeout сигнализирует об истечении лимита времени.
	CodeTimeout Code = "TIMEOUT"
	// CodeRateLimit сигнализирует об ограничении частоты обращений.
	CodeRateLimit Code = "RATE_LIMIT"
	// CodeAuth сигнализирует об ошибке аутентификации/секрета.
	CodeAuth Code = "AUTH"
	// CodeForbidden сигнализирует о запрещённой операции политикой.
	CodeForbidden Code = "FORBIDDEN"
	// CodeNotFound сигнализирует об отсутствии сущности/ресурса.
	CodeNotFound Code = "NOT_FOUND"
	// CodeConflict сигнализирует о конфликте состояния.
	CodeConflict Code = "CONFLICT"
	// CodeTransient сигнализирует о временном сбое внешнего сервиса.
	CodeTransient Code = "TRANSIENT"
	// CodeCanceled сигнализирует о внешней отмене контекста.
	CodeCanceled Code = "CANCELED"
	// CodeInternal сигнализирует о внутренней непрогнозируемой ошибке.
	CodeInternal Code = "INTERNAL"
)

// Error представляет каноническую ошибку приложения.
type Error struct {
	// Code фиксирует машинно-обрабатываемую категорию ошибки.
	Code Code
	// Message хранит безопасное для пользователя сообщение.
	Message string
	// Cause хранит исходную ошибку для диагностики.
	Cause error
	// Retryable показывает, имеет ли смысл повторная попытка.
	Retryable bool
	// SafeDetails содержит опциональные безопасные атрибуты.
	SafeDetails map[string]any
}

// Error возвращает строковое представление ошибки.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
}

// Unwrap возвращает внутреннюю причину ошибки.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New создаёт typed-ошибку без вложенной причины.
func New(code Code, message string, retryable bool) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable}
}

// Wrap создаёт typed-ошибку и сохраняет исходную причину.
func Wrap(code Code, message string, cause error, retryable bool) *Error {
	return &Error{Code: code, Message: message, Cause: cause, Retryable: retryable}
}

// Normalize приводит произвольную ошибку к каноническому виду.
func Normalize(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	switch {
	case errors.Is(err, context.Canceled):
		return Wrap(CodeCanceled, "request canceled", err, false)
	case errors.Is(err, context.DeadlineExceeded):
		return Wrap(CodeTimeout, "request timeout", err, true)
	default:
		return Wrap(CodeInternal, "internal error", err, false)
	}
}

// CodeOf извлекает код ошибки из произвольной цепочки wrapped-ошибок.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	return Normalize(err).Code
}

// RetryableOf извлекает признак retryable из произвольной ошибки.
func RetryableOf(err error) bool {
	if err == nil {
		return false
	}
	return Normalize(err).Retryable
}

// UserMessage возвращает безопасное текстовое сообщение для клиента.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	appErr := Normalize(err)
	if appErr.Message == "" {
		return "unexpected error"
	}
	return appErr.Message
}

// HTTPStatus маппит код ошибки в HTTP-статус для API-ответов.
func HTTPStatus(err error) int {
	switch CodeOf(err) {
	case CodeBadRequest, CodeValidation:
		return http.StatusBadRequest
	case CodeAuth:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeRateLimit:
		return http.StatusTooManyRequests
	case CodeTimeout:
		return http.StatusGatewayTimeout
	case CodeTransient:
		return http.StatusBadGateway
	case CodeCanceled:
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}
