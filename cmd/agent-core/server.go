package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/jsonx"
	"github.com/blanergol/agent-core/pkg/oauth21"
	"github.com/blanergol/agent-core/pkg/redact"
	"github.com/blanergol/agent-core/pkg/telemetry"
)

// agentRunner is minimal runtime contract required by HTTP transport.
type agentRunner interface {
	Run(ctx context.Context, in core.RunInput) (core.RunResult, error)
}

type accessTokenVerifier interface {
	Verify(ctx context.Context, authorizationHeader string) (oauth21.Principal, error)
}

type apiServer struct {
	runner         agentRunner
	logger         *slog.Logger
	userAuthHeader string
	oauthVerifier  accessTokenVerifier
	firstOnly      bool
	webUIEnabled   bool
	handled        atomic.Bool
	onFirstHandled func()
}

type runRequest struct {
	Input         string `json:"input"`
	UserSub       string `json:"user_sub,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

type runResponse struct {
	FinalResponse string              `json:"final_response"`
	Steps         int                 `json:"steps"`
	ToolCalls     int                 `json:"tool_calls"`
	StopReason    string              `json:"stop_reason"`
	SessionID     string              `json:"session_id"`
	CorrelationID string              `json:"correlation_id"`
	APIVersion    string              `json:"api_version"`
	PlanningSteps []core.PlanningStep `json:"planning_steps,omitempty"`
	CalledTools   []string            `json:"called_tools,omitempty"`
	MCPTools      []string            `json:"mcp_tools,omitempty"`
	Skills        []string            `json:"skills,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func newAPIServer(
	runner agentRunner,
	logger *slog.Logger,
	userAuthHeader string,
	oauthVerifier accessTokenVerifier,
	firstOnly bool,
	webUIEnabled bool,
) *apiServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &apiServer{
		runner:         runner,
		logger:         logger,
		userAuthHeader: userAuthHeader,
		oauthVerifier:  oauthVerifier,
		firstOnly:      firstOnly,
		webUIEnabled:   webUIEnabled,
	}
}

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

func (s *apiServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *apiServer) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runner == nil {
		writeError(w, http.StatusInternalServerError, "agent is not initialized")
		return
	}
	var principal oauth21.Principal
	if s.oauthVerifier != nil {
		verified, err := s.oauthVerifier.Verify(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			writeAuthFailure(w, err)
			return
		}
		principal = verified
	}

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
	if principal.Subject != "" {
		req.UserSub = principal.Subject
	} else if req.UserSub == "" && s.userAuthHeader != "" {
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

	claimed := false
	if s.firstOnly {
		if !s.handled.CompareAndSwap(false, true) {
			writeError(w, http.StatusConflict, "first request already processed")
			return
		}
		claimed = true
	}

	result, err := s.runner.Run(runCtx, core.RunInput{
		Text:          req.Input,
		SessionID:     req.SessionID,
		CorrelationID: req.CorrelationID,
		UserSub:       req.UserSub,
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
		PlanningSteps: result.PlanningSteps,
		CalledTools:   result.CalledTools,
		MCPTools:      result.MCPTools,
		Skills:        result.Skills,
	})

	if claimed && s.onFirstHandled != nil {
		go s.onFirstHandled()
	}
}

func decodeJSONBody(r *http.Request, out any) error {
	defer func() {
		_ = r.Body.Close()
	}()
	return jsonx.DecodeStrictLimit(r.Body, out, 1<<20)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeAuthFailure(w http.ResponseWriter, err error) {
	status := apperrors.HTTPStatus(err)
	switch status {
	case http.StatusUnauthorized:
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	case http.StatusForbidden:
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
	}
	writeError(w, status, apperrors.UserMessage(err))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
