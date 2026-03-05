package llm

import (
	"context"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestSetLangfuseGenerationStaticAttributes проверяет запись статических атрибутов генерации.
func TestSetLangfuseGenerationStaticAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "llm.chat")
	setLangfuseGenerationStaticAttributes(ctx, "openai/gpt-4o-mini", ChatOptions{
		Temperature: 0.2,
		TopP:        0.9,
		MaxTokens:   256,
		Seed:        42,
	})
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	values := attrMapLangfuse(ended[0].Attributes())
	if got := values["langfuse.observation.type"]; got != "generation" {
		t.Fatalf("langfuse.observation.type = %q, want generation", got)
	}
	if got := values["langfuse.observation.model.name"]; got != "openai/gpt-4o-mini" {
		t.Fatalf("langfuse.observation.model.name = %q, want openai/gpt-4o-mini", got)
	}
	if got := values["langfuse.observation.model.parameters"]; got == "" {
		t.Fatalf("langfuse.observation.model.parameters is empty")
	}
}

// TestSetLangfuseGenerationUsageAndCostAttributes проверяет запись usage/cost атрибутов.
func TestSetLangfuseGenerationUsageAndCostAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider()
	tp.RegisterSpanProcessor(recorder)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "llm.chat")
	setLangfuseGenerationUsageAndCostAttributes(ctx, tokenUsage{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		InputCost:        0.0015,
		OutputCost:       0.0020,
		TotalCost:        0.0035,
		CostSource:       "provider_native",
	})
	setLangfuseCompletionStartTime(ctx, time.Unix(1700000000, 123456789).UTC())
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	values := attrMapLangfuse(ended[0].Attributes())
	if got := values["langfuse.observation.usage_details"]; got == "" {
		t.Fatalf("langfuse.observation.usage_details is empty")
	}
	if got := values["langfuse.observation.cost_details"]; got == "" {
		t.Fatalf("langfuse.observation.cost_details is empty")
	}
	if got := values["langfuse.observation.completion_start_time"]; got == "" {
		t.Fatalf("langfuse.observation.completion_start_time is empty")
	}
}

// TestEnrichUsageWithCostFromModelPrices проверяет расчет стоимости из локальной таблицы цен.
func TestEnrichUsageWithCostFromModelPrices(t *testing.T) {
	provider := &langChainProvider{
		modelName:   "openai/gpt-4o-mini",
		modelPrices: map[string]ModelPrice{"openai/gpt-4o-mini": {InputPer1M: 0.15, OutputPer1M: 0.6}},
	}
	usage := provider.enrichUsageWithCost(tokenUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		Source:           "estimated",
	})

	if usage.CostSource != "model_pricing" {
		t.Fatalf("cost source = %q, want model_pricing", usage.CostSource)
	}
	if usage.InputCost <= 0 || usage.OutputCost <= 0 || usage.TotalCost <= 0 {
		t.Fatalf("unexpected costs: input=%f output=%f total=%f", usage.InputCost, usage.OutputCost, usage.TotalCost)
	}
}

// TestProviderNativeUsageParsesCosts проверяет извлечение usage/cost из provider-native ответа.
func TestProviderNativeUsageParsesCosts(t *testing.T) {
	resp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			GenerationInfo: map[string]any{
				"PromptTokens":     100,
				"CompletionTokens": 20,
				"TotalTokens":      120,
				"cost_details": map[string]any{
					"input":  0.001,
					"output": 0.002,
					"total":  0.003,
				},
			},
		}},
	}
	usage, ok := providerNativeUsage(resp)
	if !ok {
		t.Fatalf("providerNativeUsage did not parse usage")
	}
	if usage.TotalTokens != 120 {
		t.Fatalf("total tokens = %d, want 120", usage.TotalTokens)
	}
	if usage.TotalCost <= 0 {
		t.Fatalf("total cost = %f, want > 0", usage.TotalCost)
	}
}

// attrMapLangfuse преобразует список OTel-атрибутов в map для удобных ассертов.
func attrMapLangfuse(attrs []attribute.KeyValue) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.AsString()
	}
	return values
}
