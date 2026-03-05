package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/blanergol/agent-core/core"
)

// HTTPGetConfig задает ограничения безопасности и производительности для инструмента `http.get`.
type HTTPGetConfig struct {
	AllowDomains []string
	MaxBodyBytes int64
	Timeout      time.Duration
	CacheTTL     time.Duration
}

// HTTPGetTool выполняет HTTP GET только к разрешенным доменам и с защитными лимитами.
type HTTPGetTool struct {
	client *http.Client
	cfg    HTTPGetConfig
}

// NewHTTPGetTool создает безопасный инструмент `http.get` с дефолтными лимитами.
func NewHTTPGetTool(cfg HTTPGetConfig) *HTTPGetTool {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 64 * 1024
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &HTTPGetTool{
		client: &http.Client{Timeout: cfg.Timeout},
		cfg:    cfg,
	}
}

// Name возвращает идентификатор инструмента.
func (t *HTTPGetTool) Name() string {
	return "http.get"
}

// Description объясняет назначение инструмента для планировщика.
func (t *HTTPGetTool) Description() string {
	return "Performs a safe HTTP GET request to allowlisted domains"
}

// InputSchema задает JSON-схему входного payload.
func (t *HTTPGetTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["url"],"properties":{"url":{"type":"string","format":"uri"}}}`
}

// OutputSchema задает JSON-схему результата HTTP-запроса.
func (t *HTTPGetTool) OutputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["status_code","body"],"properties":{"status_code":{"type":"integer"},"body":{"type":"string"}}}`
}

// IsReadOnly отмечает, что инструмент не изменяет состояние системы.
func (t *HTTPGetTool) IsReadOnly() bool { return true }

// IsSafeRetry отмечает, что повторный вызов запроса допустим.
func (t *HTTPGetTool) IsSafeRetry() bool { return true }

// CacheTTL возвращает TTL кэша для ответов инструмента.
func (t *HTTPGetTool) CacheTTL() time.Duration { return t.cfg.CacheTTL }

// Execute валидирует URL, выполняет запрос и возвращает нормализованный JSON-ответ.
func (t *HTTPGetTool) Execute(ctx context.Context, args json.RawMessage) (core.ToolResult, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return core.ToolResult{}, fmt.Errorf("decode args: %w", err)
	}
	if strings.TrimSpace(in.URL) == "" {
		return core.ToolResult{}, errors.New("url is required")
	}

	u, err := url.Parse(in.URL)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("parse url: %w", err)
	}
	if err := t.validateURL(ctx, u); err != nil {
		return core.ToolResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "agent-core/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("http get: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.cfg.MaxBodyBytes))
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("read response body: %w", err)
	}

	out := struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}{StatusCode: resp.StatusCode, Body: string(body)}
	b, err := json.Marshal(out)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("encode output: %w", err)
	}
	return core.ToolResult{Output: string(b)}, nil
}

// validateURL проверяет схему, allowlist-домен и блокирует внутренние сети.
func (t *HTTPGetTool) validateURL(ctx context.Context, u *url.URL) error {
	if u == nil {
		return errors.New("url is nil")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("missing host")
	}
	if !isAllowedDomain(host, t.cfg.AllowDomains) {
		return fmt.Errorf("domain not allowlisted: %s", host)
	}
	if err := rejectInternalHost(ctx, host); err != nil {
		return err
	}
	return nil
}

// isAllowedDomain проверяет, что хост входит в allowlist напрямую или как поддомен.
func isAllowedDomain(host string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return false
	}
	host = strings.ToLower(host)
	for _, domain := range allowlist {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// rejectInternalHost отклоняет адреса, резолвящиеся во внутренние или loopback-сети.
func rejectInternalHost(ctx context.Context, host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		if isInternalIP(ip) {
			return fmt.Errorf("internal ip is not allowed: %s", host)
		}
		return nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no resolved ip for host: %s", host)
	}
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok {
			continue
		}
		if isInternalIP(ip) {
			return fmt.Errorf("resolved internal ip is not allowed: %s", ip.String())
		}
	}
	return nil
}

// isInternalIP определяет, относится ли IP к приватным/служебным диапазонам.
func isInternalIP(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
