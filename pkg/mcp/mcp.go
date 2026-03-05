package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/retry"
	"github.com/blanergol/agent-core/pkg/tools"
)

// RemoteTool описывает инструмент, объявленный удалённым MCP-сервером.
type RemoteTool struct {
	// Name используется для вызова удалённого инструмента.
	Name string `json:"name"`
	// Description передаёт краткое назначение инструмента в каталог.
	Description string `json:"description"`
	// InputSchema описывает JSON-аргументы, которые ожидает инструмент.
	InputSchema string `json:"input_schema"`
	// OutputSchema описывает формат результата удалённого инструмента (если сервер предоставляет).
	OutputSchema string `json:"output_schema"`
}

// Client задаёт минимальный контракт клиента удалённого MCP API.
type Client interface {
	// ListTools запрашивает каталог инструментов у удалённого MCP-сервера.
	ListTools(ctx context.Context) ([]RemoteTool, error)
	// CallTool вызывает удалённый инструмент по имени с JSON-аргументами.
	CallTool(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error)
}

// HTTPClient реализует Client поверх HTTP-транспорта и retry-политики.
type HTTPClient struct {
	// baseURL хранит адрес MCP API без завершающего `/`.
	baseURL string
	// token используется для bearer-аутентификации к серверу.
	token string
	// client выполняет HTTP-запросы с заданным тайм-аутом.
	client *http.Client
	// retryPolicy задаёт единую стратегию повторов для list/call операций.
	retryPolicy retry.Policy
}

// NewHTTPClient создаёт HTTP-клиент MCP с нормализованным baseURL и дефолтным timeout.
func NewHTTPClient(baseURL, token string, timeout time.Duration) *HTTPClient {
	return NewHTTPClientWithPolicy(baseURL, token, timeout, retry.Policy{
		MaxRetries: 2,
		BaseDelay:  200 * time.Millisecond,
		MaxDelay:   2 * time.Second,
	})
}

// NewHTTPClientWithPolicy создаёт MCP-клиент с явной политикой retry/backoff.
func NewHTTPClientWithPolicy(baseURL, token string, timeout time.Duration, retryPolicy retry.Policy) *HTTPClient {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HTTPClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		token:       token,
		client:      &http.Client{Timeout: timeout},
		retryPolicy: retryPolicy,
	}
}

// ListTools получает список инструментов удалённого MCP-сервера.
func (c *HTTPClient) ListTools(ctx context.Context) ([]RemoteTool, error) {
	var out []RemoteTool
	err := retry.Do(ctx, c.retryPolicy, retry.DefaultClassifier, func(callCtx context.Context) error {
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, c.baseURL+"/tools", nil)
		if err != nil {
			return apperrors.Wrap(apperrors.CodeBadRequest, "build mcp list tools request", err, false)
		}
		c.setAuth(req)

		resp, err := c.client.Do(req)
		if err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "mcp list tools call failed", err, true)
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode >= 300 {
			return classifyMCPHTTPStatus("list_tools", resp.StatusCode, resp.Body)
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "decode mcp tools", err, true)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CallTool отправляет аргументы в удалённый инструмент и возвращает его результат.
func (c *HTTPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error) {
	var out tools.ToolResult
	err := retry.Do(ctx, c.retryPolicy, retry.DefaultClassifier, func(callCtx context.Context) error {
		payload := map[string]any{"args": json.RawMessage(args)}
		b, err := json.Marshal(payload)
		if err != nil {
			return apperrors.Wrap(apperrors.CodeValidation, "encode mcp tool args", err, false)
		}

		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.baseURL+"/tools/"+name, bytes.NewReader(b))
		if err != nil {
			return apperrors.Wrap(apperrors.CodeBadRequest, "build mcp tool request", err, false)
		}
		req.Header.Set("Content-Type", "application/json")
		c.setAuth(req)

		resp, err := c.client.Do(req)
		if err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "mcp call tool failed", err, true)
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode >= 300 {
			return classifyMCPHTTPStatus("call_tool", resp.StatusCode, resp.Body)
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "decode mcp tool result", err, true)
		}
		return nil
	})
	if err != nil {
		return tools.ToolResult{}, err
	}
	return out, nil
}

// setAuth добавляет bearer-токен, если он настроен.
func (c *HTTPClient) setAuth(req *http.Request) {
	if c.token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
}

// classifyMCPHTTPStatus сопоставляет HTTP-статус MCP-сервера с типизированной ошибкой приложения.
func classifyMCPHTTPStatus(operation string, status int, body io.Reader) error {
	details, _ := io.ReadAll(io.LimitReader(body, 512))
	message := fmt.Sprintf("mcp %s status: %d", operation, status)
	if trimmed := strings.TrimSpace(string(details)); trimmed != "" {
		message = message + ": " + trimmed
	}

	switch {
	case status == http.StatusTooManyRequests:
		return apperrors.New(apperrors.CodeRateLimit, message, true)
	case status == http.StatusRequestTimeout, status == http.StatusTooEarly:
		return apperrors.New(apperrors.CodeTimeout, message, true)
	case status >= 500:
		return apperrors.New(apperrors.CodeTransient, message, true)
	case status == http.StatusUnauthorized:
		return apperrors.New(apperrors.CodeAuth, message, false)
	case status == http.StatusForbidden:
		return apperrors.New(apperrors.CodeForbidden, message, false)
	case status == http.StatusNotFound:
		return apperrors.New(apperrors.CodeNotFound, message, false)
	default:
		return apperrors.New(apperrors.CodeBadRequest, message, false)
	}
}

// Bridge импортирует удалённые MCP-инструменты в локальный реестр.
type Bridge struct {
	// ServerName становится частью итогового имени импортированного инструмента.
	ServerName string
	// Client выполняет операции list/call к удалённому MCP API.
	Client Client
}

// Import загружает удалённые инструменты и регистрирует локальные адаптеры в registry.
func (b Bridge) Import(ctx context.Context, registry *tools.Registry) error {
	// remoteTools - каталог инструментов, полученный с удалённого сервера.
	remoteTools, err := b.Client.ListTools(ctx)
	if err != nil {
		return err
	}
	for _, rt := range remoteTools {
		// adapter связывает формат локального инструмента с удалённым MCP-вызовом.
		adapter := &remoteToolAdapter{
			server: b.ServerName,
			tool:   rt,
			client: b.Client,
		}
		if err := registry.Register(adapter); err != nil {
			return err
		}
	}
	return nil
}

// remoteToolAdapter адаптирует удалённый MCP-инструмент к локальному интерфейсу tools.Tool.
type remoteToolAdapter struct {
	// server хранит имя MCP-сервера для построения префикса инструмента.
	server string
	// tool содержит описание конкретного удалённого инструмента.
	tool RemoteTool
	// client выполняет реальный HTTP-вызов удалённого инструмента.
	client Client
}

// Name формирует имя локального инструмента в формате `mcp.<server>.<tool>`.
func (r *remoteToolAdapter) Name() string {
	return "mcp." + r.server + "." + r.tool.Name
}

// Description возвращает описание удалённого инструмента для каталога.
func (r *remoteToolAdapter) Description() string {
	return r.tool.Description
}

// InputSchema возвращает схему аргументов или безопасный объект по умолчанию.
func (r *remoteToolAdapter) InputSchema() string {
	if strings.TrimSpace(r.tool.InputSchema) == "" {
		return `{"type":"object"}`
	}
	return r.tool.InputSchema
}

// OutputSchema возвращает схему результата удалённого инструмента либо permissive-схему.
func (r *remoteToolAdapter) OutputSchema() string {
	if strings.TrimSpace(r.tool.OutputSchema) == "" {
		return `{}`
	}
	return r.tool.OutputSchema
}

// IsReadOnly по умолчанию считает удалённые MCP-инструменты потенциально изменяющими состояние.
func (r *remoteToolAdapter) IsReadOnly() bool { return false }

// IsSafeRetry по умолчанию запрещает автоматические повторы для удалённых MCP-инструментов.
func (r *remoteToolAdapter) IsSafeRetry() bool { return false }

// Execute проксирует вызов в исходный MCP-инструмент.
func (r *remoteToolAdapter) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
	return r.client.CallTool(ctx, r.tool.Name, args)
}
