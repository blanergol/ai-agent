package oauth21

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateEndpointURL validates OAuth endpoint URL and enforces HTTPS by default.
func ValidateEndpointURL(raw string, allowInsecureHTTP bool, field string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%s must include scheme and host", field)
	}
	if !allowInsecureHTTP && strings.EqualFold(parsed.Scheme, "http") && !isLocalhostHost(parsed.Host) {
		return "", fmt.Errorf("%s must use https", field)
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !strings.EqualFold(parsed.Scheme, "http") {
		return "", fmt.Errorf("%s scheme must be http or https", field)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLocalhostHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
