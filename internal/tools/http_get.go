package tools

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
)

// HTTPGetConfig задаёт ограничения безопасности и производительности для http.get.
type HTTPGetConfig struct {
	// AllowDomains ограничивает список доменов, куда разрешён исходящий GET.
	AllowDomains []string
	// MaxBodyBytes ограничивает размер читаемого HTTP-тела.
	MaxBodyBytes int64
	// Timeout ограничивает длительность сетевого запроса.
	Timeout time.Duration
	// CacheTTL задаёт TTL кэша для повторяющихся read-only вызовов.
	CacheTTL time.Duration
}

// HTTPGetTool выполняет безопасные HTTP GET-запросы по allowlist-доменам.
type HTTPGetTool struct {
	// client выполняет сетевой запрос с настроенным timeout.
	client *http.Client
	// cfg хранит политики доменов и лимиты чтения.
	cfg HTTPGetConfig
}

// NewHTTPGetTool создаёт безопасный HTTP GET инструмент с дефолтными лимитами.
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

// Name возвращает идентификатор инструмента в реестре.
func (t *HTTPGetTool) Name() string {
	return "http.get"
}

// Description кратко объясняет ограничения инструмента.
func (t *HTTPGetTool) Description() string {
	return "Performs a safe HTTP GET request to allowlisted domains"
}

// InputSchema требует URI в поле `url`.
func (t *HTTPGetTool) InputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["url"],"properties":{"url":{"type":"string","format":"uri"}}}`
}

// OutputSchema фиксирует JSON-структуру безопасного ответа инструмента.
func (t *HTTPGetTool) OutputSchema() string {
	return `{"type":"object","additionalProperties":false,"required":["status_code","body"],"properties":{"status_code":{"type":"integer"},"body":{"type":"string"}}}`
}

// IsReadOnly указывает, что HTTP GET не изменяет внутреннее состояние агента.
func (t *HTTPGetTool) IsReadOnly() bool { return true }

// IsSafeRetry указывает, что повтор GET-запроса допустим.
func (t *HTTPGetTool) IsSafeRetry() bool { return true }

// CacheTTL задаёт TTL кэша повторяющихся read-only вызовов.
func (t *HTTPGetTool) CacheTTL() time.Duration { return t.cfg.CacheTTL }

// Execute валидирует URL, выполняет GET и возвращает JSON со статусом и телом.
func (t *HTTPGetTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	// in содержит декодированные аргументы вызова.
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("decode args: %w", err)
	}
	if strings.TrimSpace(in.URL) == "" {
		return ToolResult{}, errors.New("url is required")
	}

	// u - распарсенный URL для последующих проверок и выполнения запроса.
	u, err := url.Parse(in.URL)
	if err != nil {
		return ToolResult{}, fmt.Errorf("parse url: %w", err)
	}
	if err := t.validateURL(ctx, u); err != nil {
		return ToolResult{}, err
	}

	// req описывает исходящий HTTP GET с ограниченным контекстом.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ToolResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "agent-core/1.0")

	// resp - HTTP-ответ удалённого сервера.
	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResult{}, fmt.Errorf("http get: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// body содержит не более MaxBodyBytes данных ответа.
	body, err := io.ReadAll(io.LimitReader(resp.Body, t.cfg.MaxBodyBytes))
	if err != nil {
		return ToolResult{}, fmt.Errorf("read response body: %w", err)
	}

	// out формирует безопасный JSON-результат с кодом и текстом тела.
	out := struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}{StatusCode: resp.StatusCode, Body: string(body)}
	// b - сериализованный JSON ответа инструмента.
	b, err := json.Marshal(out)
	if err != nil {
		return ToolResult{}, fmt.Errorf("encode output: %w", err)
	}
	return ToolResult{Output: string(b)}, nil
}

// validateURL проверяет схему, домен из allowlist и запрет внутренних адресов.
func (t *HTTPGetTool) validateURL(ctx context.Context, u *url.URL) error {
	if u == nil {
		return errors.New("url is nil")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	// host используется для проверок allowlist и DNS/IP безопасности.
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

// isAllowedDomain проверяет, что host равен allowlist-домену или его поддомену.
func isAllowedDomain(host string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return false
	}
	host = strings.ToLower(host)
	for _, domain := range allowlist {
		// d - нормализованное allowlist-значение без лишних пробелов.
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

// rejectInternalHost запрещает прямые и резолвленные внутренние IP-адреса.
func rejectInternalHost(ctx context.Context, host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		if isInternalIP(ip) {
			return fmt.Errorf("internal ip is not allowed: %s", host)
		}
		return nil
	}

	// addrs содержит результаты DNS-резолва хоста.
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no resolved ip for host: %s", host)
	}
	for _, addr := range addrs {
		// ip преобразуется в netip для унифицированной проверки внутренней сети.
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

// isInternalIP возвращает true для небезопасных адресных диапазонов.
func isInternalIP(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
