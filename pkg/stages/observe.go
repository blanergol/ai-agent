package stages

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ObserveStage собирает текущее наблюдение: память, состояние и каталог доступных инструментов.
type ObserveStage struct{}

// NewObserveStage создает стадию наблюдения для начала итерации цикла агента.
func NewObserveStage() core.Stage {
	return &ObserveStage{}
}

// Name возвращает стабильный идентификатор стадии наблюдения.
func (s *ObserveStage) Name() string { return "observe" }

// Run проверяет guardrails и формирует Observation для следующего шага планирования.
func (s *ObserveStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if run.PendingStop {
		return core.Continue(), nil
	}
	if err := run.ExecutePhase(ctx, core.PhaseGuardrails, func(_ context.Context) error {
		return run.Guardrails.BeforeStep()
	}); err != nil {
		steps, _, _ := run.Guardrails.Stats()
		run.CurrentStep = steps
		if run.State != nil {
			run.State.Iteration = steps
			run.State.Guardrails.Steps = steps
		}
		reason := strings.TrimSpace(err.Error())
		run.PendingStop = true
		run.PendingStopReason = reason
		run.PendingFinalResponse = core.FallbackFinalResponse(reason)
		return core.Continue(), nil
	}
	step, _, _ := run.Guardrails.Stats()
	run.CurrentStep = step
	if run.State != nil {
		run.State.Iteration = step
	}

	ctxMessages, err := run.Memory.BuildContext(ctx, run.Input.Text)
	if err != nil {
		return core.StageResult{}, err
	}
	if run.State != nil {
		run.State.Memory = copyMessages(ctxMessages)
	}
	snippets := make([]string, 0, len(ctxMessages)+len(run.Config.PromptHints)+1)
	for _, msg := range ctxMessages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		prefix := string(msg.Role)
		if msg.Name != "" {
			prefix += "[" + msg.Name + "]"
		}
		snippets = append(snippets, prefix+": "+content)
	}
	snippets = append(snippets, run.Config.PromptHints...)
	if strings.TrimSpace(run.Input.UserSub) != "" {
		snippets = append(snippets, "Authenticated subject: "+strings.TrimSpace(run.Input.UserSub))
	}
	var snapshot map[string]any
	if run.Deps.State != nil {
		snapshot = run.Deps.State.SnapshotForSession(ctx)
	}
	steps, toolCalls, elapsed := run.Guardrails.Stats()
	var contextData map[string]any
	var retrievedDocs []string
	if run.State != nil {
		run.State.Guardrails.Steps = steps
		run.State.Guardrails.ToolCalls = toolCalls
		run.State.Guardrails.Elapsed = elapsed
		contextData = coreCopyMap(run.State.Context)
		retrievedDocs = coreCopyStrings(run.State.RetrievedDocs)
	}
	run.Observation = core.Observation{
		UserInput:      run.Input.Text,
		StateSnapshot:  snapshot,
		Context:        contextData,
		RetrievedDocs:  retrievedDocs,
		MemorySnippets: snippets,
		ToolCatalog:    run.Deps.Tools.Specs(),
	}
	return core.Continue(), nil
}

func copyMessages(values []core.Message) []core.Message {
	if len(values) == 0 {
		return nil
	}
	out := make([]core.Message, len(values))
	copy(out, values)
	return out
}

func coreCopyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func coreCopyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
