package telemetry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
)

type langfuseScoreSink struct {
	endpoint   string
	authHeader string
	client     *http.Client
	logger     *slog.Logger
}

type langfuseScoreRequest struct {
	TraceID       string  `json:"traceId,omitempty"`
	ObservationID string  `json:"observationId,omitempty"`
	Name          string  `json:"name"`
	Value         float64 `json:"value"`
	Comment       string  `json:"comment,omitempty"`
	ConfigID      string  `json:"configId,omitempty"`
	DataType      string  `json:"dataType,omitempty"`
}

func newLangfuseScoreSink(host, publicKey, secretKey string, timeout time.Duration, logger *slog.Logger) (ScoreSink, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(host))
	if err != nil {
		return nil, fmt.Errorf("parse langfuse host for scores: %w", err)
	}
	if strings.TrimSpace(parsedURL.Scheme) == "" || strings.TrimSpace(parsedURL.Host) == "" {
		return nil, fmt.Errorf("invalid langfuse host for scores: %s", host)
	}
	path := "/api/public/scores"
	if strings.TrimSpace(parsedURL.Path) != "" && parsedURL.Path != "/" {
		path = strings.TrimRight(parsedURL.Path, "/") + path
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &langfuseScoreSink{
		endpoint: parsedURL.Scheme + "://" + parsedURL.Host + path,
		authHeader: "Basic " +
			base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(publicKey)+":"+strings.TrimSpace(secretKey))),
		client: &http.Client{Timeout: timeout},
		logger: logger,
	}, nil
}

func (s *langfuseScoreSink) Save(ctx context.Context, score Score) {
	if s == nil || s.client == nil {
		return
	}
	name := strings.TrimSpace(score.Name)
	if name == "" || math.IsNaN(score.Value) || math.IsInf(score.Value, 0) {
		return
	}

	traceID := strings.TrimSpace(score.TraceID)
	if traceID == "" {
		traceID = traceIDFromContext(ctx)
	}
	if traceID == "" {
		return
	}

	reqPayload := langfuseScoreRequest{
		TraceID:       traceID,
		ObservationID: strings.TrimSpace(score.ObservationID),
		Name:          name,
		Value:         score.Value,
		Comment:       strings.TrimSpace(score.Comment),
		ConfigID:      strings.TrimSpace(score.ConfigID),
		DataType:      strings.TrimSpace(score.DataType),
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", s.authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Warn("langfuse score emit failed", slog.String("error", err.Error()), slog.String("name", name))
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	s.logger.Warn(
		"langfuse score rejected",
		slog.Int("status", resp.StatusCode),
		slog.String("name", name),
		slog.String("body", strings.TrimSpace(string(raw))),
	)
}

func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := oteltrace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() || !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
