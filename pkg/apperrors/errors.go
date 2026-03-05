package apperrors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Code задает стабильный код ошибки приложения, используемый в API и логах.
type Code string

// Базовые коды ошибок, на которые опираются transport-слои и runtime-политики.
const (
	CodeBadRequest Code = "BAD_REQUEST"
	CodeValidation Code = "VALIDATION"
	CodeTimeout    Code = "TIMEOUT"
	CodeRateLimit  Code = "RATE_LIMIT"
	CodeAuth       Code = "AUTH"
	CodeForbidden  Code = "FORBIDDEN"
	CodeNotFound   Code = "NOT_FOUND"
	CodeConflict   Code = "CONFLICT"
	CodeTransient  Code = "TRANSIENT"
	CodeCanceled   Code = "CANCELED"
	CodeInternal   Code = "INTERNAL"
)

// Error — каноническая типизированная ошибка приложения.
type Error struct {
	Code        Code
	Message     string
	Cause       error
	Retryable   bool
	SafeDetails map[string]any
}

// Error возвращает строковое представление ошибки для логирования и трассировки.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
}

// Unwrap позволяет использовать errors.Is/errors.As для вложенной причины ошибки.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New создает типизированную ошибку без вложенной причины.
func New(code Code, message string, retryable bool) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable}
}

// Wrap создает типизированную ошибку и сохраняет исходную причину в цепочке.
func Wrap(code Code, message string, cause error, retryable bool) *Error {
	return &Error{Code: code, Message: message, Cause: cause, Retryable: retryable}
}

// Normalize приводит любую ошибку к каноническому типу Error.
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

// CodeOf извлекает код ошибки из любой цепочки обертывания.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	return Normalize(err).Code
}

// RetryableOf извлекает флаг retryable из любой цепочки обертывания.
func RetryableOf(err error) bool {
	if err == nil {
		return false
	}
	return Normalize(err).Retryable
}

// UserMessage возвращает безопасное сообщение для клиента.
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

// HTTPStatus сопоставляет типизированные коды ошибок HTTP-статусам.
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
