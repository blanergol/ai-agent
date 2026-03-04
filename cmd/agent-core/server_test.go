package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blanergol/agent-core/internal/agent"
)

// fakeRunner имитирует выполнение агента в тестах HTTP-сервера.
type fakeRunner struct {
	// result имитирует успешный результат выполнения агента.
	result agent.RunResult
	// err позволяет проверить обработку ошибок раннера.
	err error
	// lastSub запоминает переданный subject для проверки прокидывания контекста.
	lastSub string
	// calls считает количество вызовов Run.
	calls int
}

// RunWithInput реализует минимальный раннер для тестов API.
func (f *fakeRunner) RunWithInput(_ context.Context, in agent.RunInput) (agent.RunResult, error) {
	f.calls++
	f.lastSub = in.Auth.Subject
	if f.err != nil {
		return agent.RunResult{}, f.err
	}
	return f.result, nil
}

// TestAPIServerFirstOnly проверяет, что сервер принимает только первый запрос в режиме first-only.
func TestAPIServerFirstOnly(t *testing.T) {
	// runner эмулирует стабильно успешное выполнение агента.
	runner := &fakeRunner{result: agent.RunResult{
		FinalResponse: "ok",
		Steps:         1,
		ToolCalls:     1,
		StopReason:    "planner_done",
		PlanningSteps: []agent.PlanningStep{
			{Step: 1, ActionType: "tool", ToolName: "time.now", Done: false},
		},
		CalledTools: []string{"time.now"},
		MCPTools:    []string{"mcp.remote.lookup"},
		Skills:      []string{"ops"},
	}}
	// srv настраивается в first-only режиме для проверки блокировки повторного запроса.
	srv := newAPIServer(runner, nil, "", true, false)
	// h - готовый HTTP-обработчик маршрутов API.
	h := srv.routes()

	// req1 имитирует первый корректный запрос.
	req1 := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello"}`))
	// w1 собирает ответ сервера без реального сетевого сокета.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first status = %d", w1.Code)
	}

	// resp нужен для проверки структуры и значения JSON-ответа.
	var resp runResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.FinalResponse != "ok" {
		t.Fatalf("final_response = %s", resp.FinalResponse)
	}
	if len(resp.PlanningSteps) != 1 || resp.PlanningSteps[0].ToolName != "time.now" {
		t.Fatalf("planning_steps = %#v", resp.PlanningSteps)
	}
	if len(resp.CalledTools) != 1 || resp.CalledTools[0] != "time.now" {
		t.Fatalf("called_tools = %#v", resp.CalledTools)
	}
	if len(resp.MCPTools) != 1 || resp.MCPTools[0] != "mcp.remote.lookup" {
		t.Fatalf("mcp_tools = %#v", resp.MCPTools)
	}
	if len(resp.Skills) != 1 || resp.Skills[0] != "ops" {
		t.Fatalf("skills = %#v", resp.Skills)
	}

	// req2 имитирует второй запрос, который должен быть отклонён.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello2"}`))
	// w2 хранит ответ на второй запрос.
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second status = %d", w2.Code)
	}
}

// TestAPIServerReadsUserSubHeader проверяет чтение subject из HTTP-заголовка.
func TestAPIServerReadsUserSubHeader(t *testing.T) {
	// runner сохраняет последний полученный subject.
	runner := &fakeRunner{result: agent.RunResult{FinalResponse: "ok", StopReason: "planner_done"}}
	// srv ожидает subject в заголовке X-User-Sub.
	srv := newAPIServer(runner, nil, "X-User-Sub", false, false)
	h := srv.routes()

	// req не передаёт user_sub в JSON, чтобы использовать заголовок.
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("X-User-Sub", "user-123")
	// w собирает HTTP-ответ для ассертов.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if runner.lastSub != "user-123" {
		t.Fatalf("user_sub = %s", runner.lastSub)
	}
}

// TestAPIServerWebUIDisabled проверяет, что UI не отдается, когда он выключен в конфиге.
func TestAPIServerWebUIDisabled(t *testing.T) {
	srv := newAPIServer(&fakeRunner{}, nil, "", false, false)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
}

// TestAPIServerWebUIEnabled проверяет отдачу одностраничного интерфейса при включенном флаге.
func TestAPIServerWebUIEnabled(t *testing.T) {
	srv := newAPIServer(&fakeRunner{}, nil, "", false, true)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "/v1/agent/run") {
		t.Fatalf("ui body missing run endpoint")
	}
	if !strings.Contains(body, "textarea") {
		t.Fatalf("ui body missing textarea")
	}
}
