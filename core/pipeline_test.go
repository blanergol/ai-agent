package core

import (
	"context"
	"testing"
)

type testStage struct {
	name   string
	result StageResult
	runFn  func(ctx context.Context, run *RunContext)
}

func (s testStage) Name() string { return s.name }

func (s testStage) Run(ctx context.Context, run *RunContext) (StageResult, error) {
	if s.runFn != nil {
		s.runFn(ctx, run)
	}
	if s.result.Control == "" {
		return Continue(), nil
	}
	return s.result, nil
}

func TestPipelineRunOrderAndStop(t *testing.T) {
	order := make([]string, 0, 3)
	pipeline := NewPipeline(
		testStage{name: "a", runFn: func(_ context.Context, _ *RunContext) { order = append(order, "a") }},
		testStage{name: "b", runFn: func(_ context.Context, _ *RunContext) { order = append(order, "b") }, result: Stop("done")},
		testStage{name: "c", runFn: func(_ context.Context, _ *RunContext) { order = append(order, "c") }},
	)

	result, err := pipeline.RunCycle(context.Background(), &RunContext{})
	if err != nil {
		t.Fatalf("run cycle failed: %v", err)
	}
	if result.Control != StageControlStop {
		t.Fatalf("control = %s, want %s", result.Control, StageControlStop)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %#v", order)
	}
}

func TestPipelineMutations(t *testing.T) {
	pipeline := NewPipeline(
		testStage{name: "observe"},
		testStage{name: "plan"},
		testStage{name: "act"},
	)
	if ok := pipeline.InsertBefore("plan", testStage{name: "sanitize"}); !ok {
		t.Fatalf("insert before failed")
	}
	if ok := pipeline.Replace("act", testStage{name: "execute"}); !ok {
		t.Fatalf("replace failed")
	}
	if ok := pipeline.Remove("observe"); !ok {
		t.Fatalf("remove failed")
	}
	names := pipeline.StageNames()
	want := []string{"sanitize", "plan", "execute"}
	if len(names) != len(want) {
		t.Fatalf("names len = %d, want %d", len(names), len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names[%d] = %s, want %s", i, names[i], want[i])
		}
	}
}
