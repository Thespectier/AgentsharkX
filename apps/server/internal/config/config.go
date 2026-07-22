// Package config loads and validates the server environment without exposing secrets.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type LookupFunc func(string) (string, bool)

type Secret struct{ value string }

func NewSecret(value string) Secret { return Secret{value: value} }
func (secret Secret) Value() string { return secret.value }
func (Secret) String() string       { return "[REDACTED]" }

type Upstream struct {
	BaseURL    string
	AdminToken Secret
	ConsoleURL string
}

type Config struct {
	ListenAddr       string
	Environment      string
	AdminToken       Secret
	AuthDisabled     bool
	CookieSecure     bool
	Gateway          Upstream
	Guard            Upstream
	GuardRelease     string
	UpstreamTimeout  time.Duration
	UpstreamRetryMax int
	PollInterval     time.Duration
	RedactPayloads   bool
}

func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		ListenAddr:   valueOr(lookup, "AGENTSHARK_LISTEN_ADDR", "0.0.0.0:8080"),
		Environment:  valueOr(lookup, "AGENTSHARK_ENVIRONMENT", "local"),
		AdminToken:   NewSecret(valueOr(lookup, "AGENTSHARK_ADMIN_TOKEN", "")),
		CookieSecure: true,
		Gateway: Upstream{
			BaseURL:    valueOr(lookup, "AGENTGATEWAY_BASE_URL", "http://agentgateway:15000"),
			AdminToken: NewSecret(valueOr(lookup, "AGENTGATEWAY_ADMIN_TOKEN", "")),
			ConsoleURL: valueOr(lookup, "AGENTGATEWAY_CONSOLE_URL", ""),
		},
		Guard: Upstream{
			BaseURL:    valueOr(lookup, "AGENTGUARD_BASE_URL", "http://agentguard:38080"),
			AdminToken: NewSecret(valueOr(lookup, "AGENTGUARD_ADMIN_TOKEN", "")),
			ConsoleURL: valueOr(lookup, "AGENTGUARD_CONSOLE_URL", ""),
		},
		GuardRelease:     valueOr(lookup, "AGENTGUARD_VERSION", ""),
		UpstreamTimeout:  3 * time.Second,
		UpstreamRetryMax: 1,
		PollInterval:     2 * time.Second,
		RedactPayloads:   true,
	}

	var err error
	if cfg.AuthDisabled, err = boolValue(lookup, "AGENTSHARK_AUTH_DISABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.CookieSecure, err = boolValue(lookup, "AGENTSHARK_COOKIE_SECURE", true); err != nil {
		return Config{}, err
	}
	if cfg.RedactPayloads, err = boolValue(lookup, "AGENTSHARK_REDACT_PAYLOADS", true); err != nil {
		return Config{}, err
	}
	if cfg.UpstreamTimeout, err = durationValue(lookup, "AGENTSHARK_UPSTREAM_TIMEOUT", cfg.UpstreamTimeout); err != nil {
		return Config{}, err
	}
	if cfg.PollInterval, err = durationValue(lookup, "AGENTSHARK_POLL_INTERVAL", cfg.PollInterval); err != nil {
		return Config{}, err
	}
	if cfg.UpstreamRetryMax, err = intValue(lookup, "AGENTSHARK_UPSTREAM_RETRY_MAX", cfg.UpstreamRetryMax); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	var validationErrors []error
	host, _, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_LISTEN_ADDR must be host:port"))
	}
	loopback := isLoopback(host)

	if cfg.AuthDisabled {
		if !loopback || (cfg.Environment != "local" && cfg.Environment != "development") {
			validationErrors = append(validationErrors, errors.New("disabled authentication is allowed only in a local environment bound to loopback"))
		}
	} else if !validSecret(cfg.AdminToken.Value()) {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_ADMIN_TOKEN must be a non-placeholder value of at least 16 characters"))
	}
	if !cfg.CookieSecure && (!loopback || (cfg.Environment != "local" && cfg.Environment != "development")) {
		validationErrors = append(validationErrors, errors.New("insecure cookies are allowed only in a local environment bound to loopback"))
	}
	if !cfg.RedactPayloads {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_REDACT_PAYLOADS must remain enabled"))
	}
	if !validSecret(cfg.Guard.AdminToken.Value()) {
		validationErrors = append(validationErrors, errors.New("AGENTGUARD_ADMIN_TOKEN must be a non-placeholder value of at least 16 characters"))
	}
	for name, rawURL := range map[string]string{
		"AGENTGATEWAY_BASE_URL": cfg.Gateway.BaseURL,
		"AGENTGUARD_BASE_URL":   cfg.Guard.BaseURL,
	} {
		if err := validateURL(rawURL); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("%s is invalid: %w", name, err))
		}
	}
	for name, rawURL := range map[string]string{
		"AGENTGATEWAY_CONSOLE_URL": cfg.Gateway.ConsoleURL,
		"AGENTGUARD_CONSOLE_URL":   cfg.Guard.ConsoleURL,
	} {
		if rawURL != "" {
			if err := validateURL(rawURL); err != nil {
				validationErrors = append(validationErrors, fmt.Errorf("%s is invalid: %w", name, err))
			}
		}
	}
	if cfg.UpstreamTimeout < 100*time.Millisecond || cfg.UpstreamTimeout > 30*time.Second {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_UPSTREAM_TIMEOUT must be between 100ms and 30s"))
	}
	if cfg.UpstreamRetryMax < 0 || cfg.UpstreamRetryMax > 3 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_UPSTREAM_RETRY_MAX must be between 0 and 3"))
	}
	if cfg.PollInterval < time.Second || cfg.PollInterval > time.Minute {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_POLL_INTERVAL must be between 1s and 1m"))
	}
	return errors.Join(validationErrors...)
}

func (cfg Config) SafeSummary() string {
	return fmt.Sprintf("listen=%s environment=%s auth_disabled=%t cookie_secure=%t gateway=%s guard=%s timeout=%s retries=%d poll=%s redact_payloads=%t",
		cfg.ListenAddr, cfg.Environment, cfg.AuthDisabled, cfg.CookieSecure, safeEndpoint(cfg.Gateway.BaseURL),
		safeEndpoint(cfg.Guard.BaseURL), cfg.UpstreamTimeout, cfg.UpstreamRetryMax, cfg.PollInterval, cfg.RedactPayloads)
}

func safeEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid]"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func validSecret(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return len(value) >= 16 && !strings.HasPrefix(normalized, "change-me") && !strings.HasPrefix(normalized, "replace-me")
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("must be an absolute http or https URL")
	}
	if parsed.User != nil {
		return errors.New("must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must not contain a query or fragment")
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func valueOr(lookup LookupFunc, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func boolValue(lookup LookupFunc, key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}

func durationValue(lookup LookupFunc, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration", key)
	}
	return parsed, nil
}

func intValue(lookup LookupFunc, key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}
