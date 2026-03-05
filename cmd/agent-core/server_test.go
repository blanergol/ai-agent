package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/oauth21"
)

type fakeRunner struct {
	result  core.RunResult
	err     error
	lastSub string
	calls   int
}

func (f *fakeRunner) Run(_ context.Context, in core.RunInput) (core.RunResult, error) {
	f.calls++
	f.lastSub = in.UserSub
	if f.err != nil {
		return core.RunResult{}, f.err
	}
	return f.result, nil
}

type fakeVerifier struct {
	principal oauth21.Principal
	err       error
	calls     int
}

type sequenceRunner struct {
	results []core.RunResult
	calls   int
}

func (s *sequenceRunner) Run(_ context.Context, _ core.RunInput) (core.RunResult, error) {
	s.calls++
	if s.calls <= len(s.results) {
		return s.results[s.calls-1], nil
	}
	if len(s.results) == 0 {
		return core.RunResult{}, nil
	}
	return s.results[len(s.results)-1], nil
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (oauth21.Principal, error) {
	f.calls++
	if f.err != nil {
		return oauth21.Principal{}, f.err
	}
	return f.principal, nil
}

func TestAPIServerFirstOnly(t *testing.T) {
	runner := &fakeRunner{result: core.RunResult{
		FinalResponse: "ok",
		Steps:         1,
		ToolCalls:     1,
		StopReason:    "planner_done",
		PlanningSteps: []core.PlanningStep{
			{Step: 1, ActionType: "tool", ToolName: "time.now", Done: false},
		},
		CalledTools: []string{"time.now"},
		MCPTools:    []string{"mcp.remote.lookup"},
		Skills:      []string{"ops"},
	}}
	srv := newAPIServer(runner, nil, "", nil, true, false)
	h := srv.routes()

	req1 := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello"}`))
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first status = %d", w1.Code)
	}

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

	req2 := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello2"}`))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second status = %d", w2.Code)
	}
}

func TestAPIServerReadsUserSubHeader(t *testing.T) {
	runner := &fakeRunner{result: core.RunResult{FinalResponse: "ok", StopReason: "planner_done"}}
	srv := newAPIServer(runner, nil, "X-User-Sub", nil, false, false)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("X-User-Sub", "user-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if runner.lastSub != "user-123" {
		t.Fatalf("user_sub = %s", runner.lastSub)
	}
}

func TestAPIServerWebUIDisabled(t *testing.T) {
	srv := newAPIServer(&fakeRunner{}, nil, "", nil, false, false)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestAPIServerWebUIEnabled(t *testing.T) {
	srv := newAPIServer(&fakeRunner{}, nil, "", nil, false, true)
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

func TestAPIServerRejectsWhenOAuthVerificationFails(t *testing.T) {
	runner := &fakeRunner{result: core.RunResult{FinalResponse: "ok", StopReason: "planner_done"}}
	verifier := &fakeVerifier{err: apperrors.New(apperrors.CodeAuth, "missing bearer token", false)}
	srv := newAPIServer(runner, nil, "X-User-Sub", verifier, false, false)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "invalid_token") {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	if verifier.calls != 1 {
		t.Fatalf("verifier calls = %d, want 1", verifier.calls)
	}
}

func TestAPIServerUsesOAuthSubject(t *testing.T) {
	runner := &fakeRunner{result: core.RunResult{FinalResponse: "ok", StopReason: "planner_done"}}
	verifier := &fakeVerifier{principal: oauth21.Principal{Subject: "oauth-user"}}
	srv := newAPIServer(runner, nil, "X-User-Sub", verifier, false, false)
	h := srv.routes()

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"hello","user_sub":"body-user"}`))
	req.Header.Set("X-User-Sub", "header-user")
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if runner.lastSub != "oauth-user" {
		t.Fatalf("user_sub = %s, want oauth-user", runner.lastSub)
	}
}

func TestAPIServerFirstOnlyAllowsApprovalContinuation(t *testing.T) {
	runner := &sequenceRunner{
		results: []core.RunResult{
			{
				FinalResponse: "approval required",
				StopReason:    core.StopReasonAwaitingHumanApproval,
				SessionID:     "session-1",
				PendingApproval: &core.PendingToolApproval{
					RequestID: "apr-1",
					Action: core.Action{
						Type:     core.ActionTypeTool,
						ToolName: "kv.put",
					},
				},
			},
			{
				FinalResponse: "done",
				StopReason:    "planner_done",
				SessionID:     "session-1",
			},
		},
	}
	srv := newAPIServer(runner, nil, "", nil, true, false)
	h := srv.routes()

	req1 := httptest.NewRequest(http.MethodPost, "/v1/agent/run", strings.NewReader(`{"input":"start","session_id":"session-1"}`))
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first status = %d", w1.Code)
	}

	req2 := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent/run",
		strings.NewReader(`{"session_id":"session-1","approval":{"request_id":"apr-1","decision":"approve"}}`),
	)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second status = %d", w2.Code)
	}
	if runner.calls != 2 {
		t.Fatalf("runner calls = %d, want 2", runner.calls)
	}
}

func TestAPIServerRejectsApprovalWithoutSessionID(t *testing.T) {
	srv := newAPIServer(&fakeRunner{}, nil, "", nil, false, false)
	h := srv.routes()

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent/run",
		strings.NewReader(`{"approval":{"request_id":"apr-1","decision":"approve"}}`),
	)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
