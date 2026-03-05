package telemetry

import (
	"context"
	"strings"
	"testing"
)

// TestSessionScopeFromContextUsesSessionID проверяет приоритет явного session_id в scope.
func TestSessionScopeFromContextUsesSessionID(t *testing.T) {
	ctx := WithSession(context.Background(), SessionInfo{SessionID: "sess-1"})
	if got := SessionScopeFromContext(ctx); got != "sess-1" {
		t.Fatalf("scope = %s, want sess-1", got)
	}
}

// TestSessionScopeFromContextNoLongerFallsBackToGlobal проверяет новый формат fallback scope.
func TestSessionScopeFromContextNoLongerFallsBackToGlobal(t *testing.T) {
	first := SessionScopeFromContext(context.Background())
	second := SessionScopeFromContext(context.Background())
	if first == "global" || second == "global" {
		t.Fatalf("unexpected global fallback: %s / %s", first, second)
	}
	if !strings.HasPrefix(first, "missing-session-") || !strings.HasPrefix(second, "missing-session-") {
		t.Fatalf("unexpected fallback format: %s / %s", first, second)
	}
	if first == second {
		t.Fatalf("fallback scopes must be unique, got %s", first)
	}
}

// testScoreSink — простой счетчик вызовов для проверки передачи score sink через context.
type testScoreSink struct {
	count int
}

// Save увеличивает счетчик вызовов тестового sink-а.
func (s *testScoreSink) Save(_ context.Context, _ Score) {
	s.count++
}

// TestScoresFromContext проверяет возврат зарегистрированного sink-а и fallback на noop.
func TestScoresFromContext(t *testing.T) {
	sink := &testScoreSink{}
	ctx := WithScores(context.Background(), sink)
	got := ScoresFromContext(ctx)
	got.Save(ctx, Score{Name: "agent.run.success", Value: 1})
	if sink.count != 1 {
		t.Fatalf("score sink calls = %d, want 1", sink.count)
	}

	noop := ScoresFromContext(context.Background())
	noop.Save(context.Background(), Score{Name: "x", Value: 1})
}
