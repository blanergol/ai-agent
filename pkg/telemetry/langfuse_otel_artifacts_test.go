package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestOTelArtifactSinkSetsLangfuseTraceAndObservationIOForAgentArtifacts проверяет,
// что корневые артефакты агента попадают в trace/observation поля Langfuse.
func TestOTelArtifactSinkSetsLangfuseTraceAndObservationIOForAgentArtifacts(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "agent.run")
	sink := NewOTelArtifactSink(1000)
	sink.Save(ctx, Artifact{
		Kind:    ArtifactPrompt,
		Name:    "agent.user_input",
		Payload: "hello",
	})
	sink.Save(ctx, Artifact{
		Kind:    ArtifactResponse,
		Name:    "agent.final_response",
		Payload: "world",
	})
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	values := attrMap(ended[0].Attributes())

	if got := values["langfuse.trace.input"]; got != "hello" {
		t.Fatalf("langfuse.trace.input = %q, want %q", got, "hello")
	}
	if got := values["langfuse.trace.output"]; got != "world" {
		t.Fatalf("langfuse.trace.output = %q, want %q", got, "world")
	}
	if got := values["langfuse.observation.input"]; got != "hello" {
		t.Fatalf("langfuse.observation.input = %q, want %q", got, "hello")
	}
	if got := values["langfuse.observation.output"]; got != "world" {
		t.Fatalf("langfuse.observation.output = %q, want %q", got, "world")
	}
}

// TestOTelArtifactSinkSetsObservationIOForLLMArtifactsOnly проверяет,
// что prompt/response для не-корневого span заполняют только observation-поля.
func TestOTelArtifactSinkSetsObservationIOForLLMArtifactsOnly(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "llm.chat")
	sink := NewOTelArtifactSink(1000)
	sink.Save(ctx, Artifact{
		Kind:    ArtifactPrompt,
		Name:    "llm.chat.messages",
		Payload: "prompt",
	})
	sink.Save(ctx, Artifact{
		Kind:    ArtifactResponse,
		Name:    "llm.chat.response",
		Payload: "answer",
	})
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	values := attrMap(ended[0].Attributes())

	if got := values["langfuse.observation.input"]; got != "prompt" {
		t.Fatalf("langfuse.observation.input = %q, want %q", got, "prompt")
	}
	if got := values["langfuse.observation.output"]; got != "answer" {
		t.Fatalf("langfuse.observation.output = %q, want %q", got, "answer")
	}
	if _, exists := values["langfuse.trace.input"]; exists {
		t.Fatalf("unexpected langfuse.trace.input attribute")
	}
	if _, exists := values["langfuse.trace.output"]; exists {
		t.Fatalf("unexpected langfuse.trace.output attribute")
	}
}

// TestOTelArtifactSinkSetsTraceOutputForRunResultState проверяет,
// что state-артефакт run_result заполняет trace output при незавершенном run.
func TestOTelArtifactSinkSetsTraceOutputForRunResultState(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "agent.run")
	sink := NewOTelArtifactSink(1000)
	sink.Save(ctx, Artifact{
		Kind:    ArtifactState,
		Name:    "agent.run_result",
		Payload: `{"stop_reason":"repeated_action_detected","final_response":""}`,
	})
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	values := attrMap(ended[0].Attributes())
	if got := values["langfuse.trace.output"]; got == "" {
		t.Fatalf("langfuse.trace.output is empty")
	}
	if got := values["langfuse.observation.output"]; got == "" {
		t.Fatalf("langfuse.observation.output is empty")
	}
}

// attrMap преобразует список OTel-атрибутов в map для проверок в тестах.
func attrMap(attrs []attribute.KeyValue) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.AsString()
	}
	return values
}
