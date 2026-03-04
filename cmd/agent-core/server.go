package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/blanergol/agent-core/internal/agent"
	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/blanergol/agent-core/internal/guardrails"
	"github.com/blanergol/agent-core/internal/redact"
	"github.com/blanergol/agent-core/internal/telemetry"
)

// agentRunner описывает минимальный контракт запуска агента, необходимый HTTP-слою.
type agentRunner interface {
	// RunWithInput выполняет один полный цикл обработки пользовательского запроса агентом.
	RunWithInput(ctx context.Context, in agent.RunInput) (agent.RunResult, error)
}

// apiServer инкапсулирует HTTP-обработчики поверх раннера агента.
type apiServer struct {
	// runner абстрагирует движок агента, чтобы сервер не зависел от конкретной реализации.
	runner agentRunner
	// logger фиксирует ошибки выполнения и служебные события HTTP-слоя.
	logger *slog.Logger
	// userAuthHeader определяет заголовок, из которого извлекается subject пользователя.
	userAuthHeader string
	// firstOnly включает режим единственного успешно обработанного запроса.
	firstOnly bool
	// webUIEnabled включает встроенный web-интерфейс для ручного тестирования.
	webUIEnabled bool
	// handled защищает first-only режим от повторных конкурентных запусков.
	handled atomic.Bool
	// onFirstHandled вызывается после первого успешного запроса (например, для graceful shutdown).
	onFirstHandled func()
}

// runRequest представляет входной JSON-контракт эндпоинта запуска агента.
type runRequest struct {
	// Input содержит пользовательскую задачу для агента.
	Input string `json:"input"`
	// UserSub передаёт идентификатор субъекта; может быть пустым и взятым из заголовка.
	UserSub string `json:"user_sub,omitempty"`
	// SessionID позволяет продолжить существующую сессию.
	SessionID string `json:"session_id,omitempty"`
	// CorrelationID позволяет передать внешний request id для трассировки.
	CorrelationID string `json:"correlation_id,omitempty"`
}

// runResponse представляет успешный JSON-ответ эндпоинта запуска агента.
type runResponse struct {
	// FinalResponse содержит финальный текстовый ответ агента.
	FinalResponse string `json:"final_response"`
	// Steps показывает, сколько шагов цикла агент успел выполнить.
	Steps int `json:"steps"`
	// ToolCalls отражает число вызовов инструментов в рамках задачи.
	ToolCalls int `json:"tool_calls"`
	// StopReason объясняет причину завершения выполнения.
	StopReason string `json:"stop_reason"`
	// SessionID возвращает идентификатор активной сессии.
	SessionID string `json:"session_id"`
	// CorrelationID возвращает идентификатор запроса.
	CorrelationID string `json:"correlation_id"`
	// APIVersion возвращает версию публичного контракта результата агента.
	APIVersion string `json:"api_version"`
}

// errorResponse унифицирует JSON-формат ошибок API.
type errorResponse struct {
	// Error хранит человекочитаемое описание ошибки API.
	Error string `json:"error"`
}

// newAPIServer создаёт HTTP-обёртку вокруг раннера агента и дефолтных настроек логирования.
func newAPIServer(runner agentRunner, logger *slog.Logger, userAuthHeader string, firstOnly bool, webUIEnabled bool) *apiServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &apiServer{
		runner:         runner,
		logger:         logger,
		userAuthHeader: userAuthHeader,
		firstOnly:      firstOnly,
		webUIEnabled:   webUIEnabled,
	}
}

// routes регистрирует маршруты health-check и основного запуска агента.
func (s *apiServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/agent/run", s.handleRun)
	if s.webUIEnabled {
		mux.HandleFunc("/", s.handleWebUI)
		mux.HandleFunc("/ui", s.handleWebUI)
	}
	return mux
}

// handleHealth отвечает на проверку доступности сервиса.
func (s *apiServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRun валидирует вход, запускает агента и отдаёт структурированный JSON-ответ.
func (s *apiServer) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runner == nil {
		writeError(w, http.StatusInternalServerError, "agent is not initialized")
		return
	}

	// req содержит уже декодированный JSON-запрос с пользовательскими параметрами.
	var req runRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Input = strings.TrimSpace(req.Input)
	if req.Input == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}
	if req.UserSub == "" && s.userAuthHeader != "" {
		req.UserSub = strings.TrimSpace(r.Header.Get(s.userAuthHeader))
	}
	session := telemetry.EnsureSession(telemetry.SessionInfo{
		SessionID:     req.SessionID,
		CorrelationID: req.CorrelationID,
		UserSub:       req.UserSub,
	})
	req.SessionID = session.SessionID
	req.CorrelationID = session.CorrelationID
	runCtx := telemetry.WithSession(r.Context(), session)

	// claimed показывает, заняли ли мы "слот" первого запроса в режиме first-only.
	claimed := false
	if s.firstOnly {
		if !s.handled.CompareAndSwap(false, true) {
			writeError(w, http.StatusConflict, "first request already processed")
			return
		}
		claimed = true
	}

	result, err := s.runner.RunWithInput(runCtx, agent.RunInput{
		Text:          req.Input,
		SessionID:     req.SessionID,
		CorrelationID: req.CorrelationID,
		Auth:          guardrails.UserAuthContext{Subject: req.UserSub},
	})
	if err != nil {
		if claimed {
			s.handled.Store(false)
		}
		telemetry.NewContextLogger(runCtx, s.logger).Error(
			"http agent run failed",
			slog.String("error", redact.Error(err)),
		)
		writeError(w, apperrors.HTTPStatus(err), apperrors.UserMessage(err))
		return
	}

	writeJSON(w, http.StatusOK, runResponse{
		FinalResponse: result.FinalResponse,
		Steps:         result.Steps,
		ToolCalls:     result.ToolCalls,
		StopReason:    result.StopReason,
		SessionID:     result.SessionID,
		CorrelationID: result.CorrelationID,
		APIVersion:    result.APIVersion,
	})

	if claimed && s.onFirstHandled != nil {
		go s.onFirstHandled()
	}
}

// decodeJSONBody ограничивает размер тела, запрещает лишние поля и гарантирует один JSON-объект.
func decodeJSONBody(r *http.Request, out any) error {
	defer func() {
		_ = r.Body.Close()
	}()
	// dec читает не больше 1 МБ, чтобы исключить избыточные тела запроса.
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return errors.New("invalid JSON body")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid JSON body")
	}
	return nil
}

// writeError формирует единый формат ошибки API для всех хендлеров.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

// writeJSON сериализует значение в JSON и выставляет код ответа.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
