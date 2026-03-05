package llm

import (
	"context"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blanergol/agent-core/pkg/cache"
	"github.com/tmc/langchaingo/llms"
)

// cacheAwareExecutor — тестовый executor, считающий обращения и поддерживающий стрим-фрагменты.
type cacheAwareExecutor struct {
	mu      sync.Mutex
	calls   int
	text    string
	streams []string
}

// GenerateContent имитирует обычный и streaming-ответы модели для тестов кэширования.
func (e *cacheAwareExecutor) GenerateContent(ctx context.Context, _ []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()

	var callOpts llms.CallOptions
	for _, option := range options {
		option(&callOpts)
	}
	if callOpts.StreamingFunc != nil {
		for _, chunk := range e.streams {
			if err := callOpts.StreamingFunc(ctx, []byte(chunk)); err != nil {
				return nil, err
			}
		}
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: e.text},
		},
	}, nil
}

// Calls возвращает количество обращений к тестовому executor.
func (e *cacheAwareExecutor) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// TestLangChainProviderChatUsesCache проверяет общий LLM-cache для одинакового prompt/options.
func TestLangChainProviderChatUsesCache(t *testing.T) {
	exec := &cacheAwareExecutor{text: "cached-answer"}
	provider := newLangChainProvider(
		"test",
		"test-model",
		exec,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Second,
		0,
		time.Millisecond,
		1,
		5,
		time.Second,
		true,
		time.Minute,
		nil,
		nil,
	)

	messages := []Message{{Role: RoleUser, Content: "hello"}}
	opts := ChatOptions{Temperature: math.NaN(), TopP: math.NaN()}

	first, err := provider.Chat(context.Background(), messages, opts)
	if err != nil {
		t.Fatalf("first Chat error: %v", err)
	}
	second, err := provider.Chat(context.Background(), messages, opts)
	if err != nil {
		t.Fatalf("second Chat error: %v", err)
	}
	if first != "cached-answer" || second != "cached-answer" {
		t.Fatalf("unexpected chat outputs: first=%q second=%q", first, second)
	}
	if exec.Calls() != 1 {
		t.Fatalf("executor calls = %d, want 1 due cache hit", exec.Calls())
	}
}

// TestLangChainProviderChatStreamUsesProviderStreamingAndCache проверяет provider-native stream + повторный cache hit.
func TestLangChainProviderChatStreamUsesProviderStreamingAndCache(t *testing.T) {
	exec := &cacheAwareExecutor{
		text:    "fallback-text",
		streams: []string{"hel", "lo"},
	}
	provider := newLangChainProvider(
		"test",
		"test-model",
		exec,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Second,
		0,
		time.Millisecond,
		1,
		5,
		time.Second,
		true,
		time.Minute,
		nil,
		nil,
	)

	messages := []Message{{Role: RoleUser, Content: "stream hello"}}
	streamOut, streamErr := collectStream(provider.ChatStream(context.Background(), messages, ChatOptions{}))
	if streamErr != nil {
		t.Fatalf("first ChatStream error: %v", streamErr)
	}
	if streamOut != "hello" {
		t.Fatalf("first stream output = %q, want %q", streamOut, "hello")
	}

	cachedOut, cachedErr := collectStream(provider.ChatStream(context.Background(), messages, ChatOptions{}))
	if cachedErr != nil {
		t.Fatalf("second ChatStream error: %v", cachedErr)
	}
	if cachedOut != "hello" {
		t.Fatalf("second stream output = %q, want %q", cachedOut, "hello")
	}
	if exec.Calls() != 1 {
		t.Fatalf("executor calls = %d, want 1 due cache hit", exec.Calls())
	}
}

// TestLangChainProviderSharesCacheAcrossInstancesViaBackplane проверяет общий кэш между разными инстансами provider.
func TestLangChainProviderSharesCacheAcrossInstancesViaBackplane(t *testing.T) {
	backplane := cache.NewFileBackplane(t.TempDir())
	execA := &cacheAwareExecutor{text: "shared-answer"}
	execB := &cacheAwareExecutor{text: "should-not-be-used"}
	providerA := newLangChainProvider(
		"test",
		"test-model",
		execA,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Second,
		0,
		time.Millisecond,
		1,
		5,
		time.Second,
		true,
		time.Minute,
		backplane,
		nil,
	)
	providerB := newLangChainProvider(
		"test",
		"test-model",
		execB,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Second,
		0,
		time.Millisecond,
		1,
		5,
		time.Second,
		true,
		time.Minute,
		backplane,
		nil,
	)
	messages := []Message{{Role: RoleUser, Content: "hello"}}
	opts := ChatOptions{Temperature: math.NaN(), TopP: math.NaN()}
	if _, err := providerA.Chat(context.Background(), messages, opts); err != nil {
		t.Fatalf("providerA Chat error: %v", err)
	}
	out, err := providerB.Chat(context.Background(), messages, opts)
	if err != nil {
		t.Fatalf("providerB Chat error: %v", err)
	}
	if out != "shared-answer" {
		t.Fatalf("providerB output = %q, want shared-answer", out)
	}
	if execA.Calls() != 1 {
		t.Fatalf("executor A calls = %d, want 1", execA.Calls())
	}
	if execB.Calls() != 0 {
		t.Fatalf("executor B calls = %d, want 0 due shared backplane cache", execB.Calls())
	}
}

// collectStream собирает текст из stream-каналов и возвращает первую ошибку, если она возникла.
func collectStream(chunks <-chan StreamChunk, errs <-chan error) (string, error) {
	var out strings.Builder
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()

	for chunks != nil || errs != nil {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			out.WriteString(chunk.Delta)
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return out.String(), err
			}
		case <-timeout.C:
			return out.String(), context.DeadlineExceeded
		}
	}
	return out.String(), nil
}
