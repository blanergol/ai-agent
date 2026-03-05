package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/redact"
	"github.com/blanergol/agent-core/pkg/telemetry"
	"github.com/spf13/cobra"
)

func main() {
	root := newRootCmd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-core",
		Short: "Base AI agent core",
	}
	root.AddCommand(newRunCmd())
	root.AddCommand(newServeCmd())
	return root
}

func newRunCmd() *cobra.Command {
	var (
		inputText        string
		providerOverride string
		modelOverride    string
		debugOverride    bool
		userSub          string
		sessionID        string
		correlationID    string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single agent task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serveSession := telemetry.EnsureSession(telemetry.SessionInfo{})
			serveCtx := telemetry.WithSession(cmd.Context(), serveSession)
			rt, err := BuildRuntime(serveCtx, Overrides{
				Provider: providerOverride,
				Model:    modelOverride,
				Debug:    optionalDebugOverride(cmd, debugOverride),
			})
			if err != nil {
				return err
			}
			defer shutdownRuntime(rt.Logger, rt.Shutdown)

			if strings.TrimSpace(inputText) == "" {
				reader := bufio.NewReader(os.Stdin)
				if _, err := fmt.Fprint(os.Stdout, "input> "); err != nil {
					return err
				}
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
					return err
				}
				inputText = strings.TrimSpace(line)
			}
			if inputText == "" {
				return fmt.Errorf("empty input")
			}

			result, err := rt.Runner.Run(cmd.Context(), core.RunInput{
				Text:          inputText,
				SessionID:     sessionID,
				CorrelationID: correlationID,
				UserSub:       userSub,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout, result.FinalResponse); err != nil {
				return err
			}
			runLogCtx := telemetry.WithSession(cmd.Context(), telemetry.SessionInfo{
				SessionID:     result.SessionID,
				CorrelationID: result.CorrelationID,
			})
			telemetry.NewContextLogger(runLogCtx, rt.Logger).Info("agent run finished",
				slog.Int("steps", result.Steps),
				slog.Int("tool_calls", result.ToolCalls),
				slog.String("stop_reason", result.StopReason),
				slog.String("api_version", result.APIVersion),
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&providerOverride, "provider", "", "LLM provider override: openai|openrouter|ollama|lmstudio")
	cmd.Flags().StringVar(&modelOverride, "model", "", "LLM model override")
	cmd.Flags().StringVar(&inputText, "input", "", "Task input (if empty, read from stdin)")
	cmd.Flags().BoolVar(&debugOverride, "debug", false, "Enable debug logging")
	cmd.Flags().StringVar(&userSub, "user-sub", "", "Optional user auth subject context")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Optional session id for context continuity")
	cmd.Flags().StringVar(&correlationID, "correlation-id", "", "Optional request correlation id")
	return cmd
}

func newServeCmd() *cobra.Command {
	var (
		addr              string
		firstOnly         bool
		shutdownTimeoutMs int
		providerOverride  string
		modelOverride     string
		debugOverride     bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server for agent requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serveSession := telemetry.EnsureSession(telemetry.SessionInfo{})
			serveCtx := telemetry.WithSession(cmd.Context(), serveSession)
			rt, err := BuildRuntime(serveCtx, Overrides{
				Provider: providerOverride,
				Model:    modelOverride,
				Debug:    optionalDebugOverride(cmd, debugOverride),
			})
			if err != nil {
				return err
			}
			defer shutdownRuntime(rt.Logger, rt.Shutdown)

			api := newAPIServer(rt.Runner, rt.Logger, rt.UserAuthHeader, rt.OAuthVerifier, firstOnly, rt.WebUIEnabled)
			srv := &http.Server{
				Addr:              addr,
				Handler:           api.routes(),
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      2 * time.Minute,
				IdleTimeout:       60 * time.Second,
			}
			firstHandled := make(chan struct{})
			if firstOnly {
				var firstHandledOnce sync.Once
				api.onFirstHandled = func() {
					firstHandledOnce.Do(func() { close(firstHandled) })
				}
			} else {
				firstHandled = nil
			}
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			shutdownTimeout := time.Duration(shutdownTimeoutMs) * time.Millisecond
			return runHTTPServer(serveCtx, rt.Logger, srv, listener, shutdownTimeout, firstHandled, firstOnly)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8080", "HTTP listen address")
	cmd.Flags().BoolVar(&firstOnly, "first-only", true, "Process only the first successful request")
	cmd.Flags().IntVar(&shutdownTimeoutMs, "shutdown-timeout-ms", 5000, "Graceful shutdown timeout in milliseconds")
	cmd.Flags().StringVar(&providerOverride, "provider", "", "LLM provider override: openai|openrouter|ollama|lmstudio")
	cmd.Flags().StringVar(&modelOverride, "model", "", "LLM model override")
	cmd.Flags().BoolVar(&debugOverride, "debug", false, "Enable debug logging")
	return cmd
}

func optionalDebugOverride(cmd *cobra.Command, value bool) *bool {
	if !cmd.Flags().Changed("debug") {
		return nil
	}
	v := value
	return &v
}

func shutdownRuntime(logger *slog.Logger, shutdown func(context.Context) error) {
	if shutdown == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		telemetry.NewContextLogger(ctx, logger).Warn(
			"runtime shutdown failed",
			slog.String("error", redact.Error(err)),
		)
	}
}

func runHTTPServer(
	serveCtx context.Context,
	logger *slog.Logger,
	srv *http.Server,
	listener net.Listener,
	shutdownTimeout time.Duration,
	firstHandled <-chan struct{},
	firstOnly bool,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}

	var shutdownOnce sync.Once
	shutdownServer := func(reason string) {
		shutdownOnce.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(serveCtx, shutdownTimeout)
			defer cancel()
			telemetry.NewContextLogger(shutdownCtx, logger).Info(
				"http server shutdown requested",
				slog.String("reason", reason),
			)
			if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				telemetry.NewContextLogger(shutdownCtx, logger).Error(
					"http shutdown failed",
					slog.String("error", redact.Error(err)),
				)
			}
		})
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-serveCtx.Done():
			shutdownServer("context_canceled")
		case <-firstHandled:
			shutdownServer("first_request_handled")
		case <-done:
		}
	}()

	addr := srv.Addr
	if listener != nil {
		addr = listener.Addr().String()
	}
	startAttrs := []slog.Attr{
		slog.String("addr", addr),
		slog.Bool("first_only", firstOnly),
		slog.String("endpoint", "/v1/agent/run"),
	}
	telemetry.NewContextLogger(serveCtx, logger).Info("http server started", startAttrs...)

	var err error
	if listener != nil {
		err = srv.Serve(listener)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	telemetry.NewContextLogger(serveCtx, logger).Info("http server stopped")
	return nil
}
