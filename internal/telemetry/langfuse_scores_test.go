package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestLangfuseScoreSinkSendsScoreForActiveTrace(t *testing.T) {
	var (
		gotPath string
		gotAuth string
		gotReq  langfuseScoreRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	sink, err := newLangfuseScoreSink(server.URL, "lf_pk_test", "lf_sk_test", time.Second, nil)
	if err != nil {
		t.Fatalf("newLangfuseScoreSink error: %v", err)
	}

	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "agent.run")
	traceID := oteltrace.SpanFromContext(ctx).SpanContext().TraceID().String()
	sink.Save(ctx, Score{
		Name:    "agent.run.success",
		Value:   1,
		Comment: "ok",
	})
	span.End()

	if gotPath != "/api/public/scores" {
		t.Fatalf("path = %q, want /api/public/scores", gotPath)
	}
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("lf_pk_test:lf_sk_test"))
	if gotAuth != expectedAuth {
		t.Fatalf("authorization = %q, want %q", gotAuth, expectedAuth)
	}
	if gotReq.TraceID != traceID {
		t.Fatalf("traceId = %q, want %q", gotReq.TraceID, traceID)
	}
	if gotReq.Name != "agent.run.success" {
		t.Fatalf("name = %q, want agent.run.success", gotReq.Name)
	}
	if gotReq.Value != 1 {
		t.Fatalf("value = %v, want 1", gotReq.Value)
	}
}

func TestLangfuseScoreSinkSkipsWhenTraceIDMissing(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	sink, err := newLangfuseScoreSink(server.URL+"/langfuse", "lf_pk_test", "lf_sk_test", time.Second, nil)
	if err != nil {
		t.Fatalf("newLangfuseScoreSink error: %v", err)
	}
	sink.Save(context.Background(), Score{
		Name:  "agent.run.success",
		Value: 1,
	})

	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestTraceIDFromContext(t *testing.T) {
	if got := traceIDFromContext(context.Background()); strings.TrimSpace(got) != "" {
		t.Fatalf("traceIDFromContext(background) = %q, want empty", got)
	}
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "agent.run")
	defer span.End()
	if got := traceIDFromContext(ctx); strings.TrimSpace(got) == "" {
		t.Fatalf("traceIDFromContext(span ctx) is empty")
	}
}
