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

type Database struct {
	URL              Secret
	AutoMigrate      bool
	MaxConnections   int
	MinConnections   int
	ConnectTimeout   time.Duration
	EventRetention   time.Duration
	TraceRetention   time.Duration
	PayloadRetention time.Duration
	OutboxRetention  time.Duration
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
	Database         Database
	UpstreamTimeout  time.Duration
	ScanTimeout      time.Duration
	UpstreamRetryMax int
	PollInterval     time.Duration
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
		GuardRelease: valueOr(lookup, "AGENTGUARD_VERSION", ""),
		Database: Database{
			URL: NewSecret(valueOr(lookup, "AGENTSHARK_DATABASE_URL", "")), AutoMigrate: true,
			MaxConnections: 10, MinConnections: 1, ConnectTimeout: 5 * time.Second,
			EventRetention: 30 * 24 * time.Hour, TraceRetention: 30 * 24 * time.Hour,
			PayloadRetention: 0, OutboxRetention: 24 * time.Hour,
		},
		UpstreamTimeout:  3 * time.Second,
		ScanTimeout:      90 * time.Second,
		UpstreamRetryMax: 1,
		PollInterval:     2 * time.Second,
	}

	var err error
	if cfg.Database.AutoMigrate, err = boolValue(lookup, "AGENTSHARK_DATABASE_AUTO_MIGRATE", cfg.Database.AutoMigrate); err != nil {
		return Config{}, err
	}
	if cfg.AuthDisabled, err = boolValue(lookup, "AGENTSHARK_AUTH_DISABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.CookieSecure, err = boolValue(lookup, "AGENTSHARK_COOKIE_SECURE", true); err != nil {
		return Config{}, err
	}
	if cfg.UpstreamTimeout, err = durationValue(lookup, "AGENTSHARK_UPSTREAM_TIMEOUT", cfg.UpstreamTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ScanTimeout, err = durationValue(lookup, "AGENTSHARK_SCAN_TIMEOUT", cfg.ScanTimeout); err != nil {
		return Config{}, err
	}
	if cfg.PollInterval, err = durationValue(lookup, "AGENTSHARK_POLL_INTERVAL", cfg.PollInterval); err != nil {
		return Config{}, err
	}
	if cfg.UpstreamRetryMax, err = intValue(lookup, "AGENTSHARK_UPSTREAM_RETRY_MAX", cfg.UpstreamRetryMax); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxConnections, err = intValue(lookup, "AGENTSHARK_DATABASE_MAX_CONNS", cfg.Database.MaxConnections); err != nil {
		return Config{}, err
	}
	if cfg.Database.MinConnections, err = intValue(lookup, "AGENTSHARK_DATABASE_MIN_CONNS", cfg.Database.MinConnections); err != nil {
		return Config{}, err
	}
	if cfg.Database.ConnectTimeout, err = durationValue(lookup, "AGENTSHARK_DATABASE_CONNECT_TIMEOUT", cfg.Database.ConnectTimeout); err != nil {
		return Config{}, err
	}
	if cfg.Database.EventRetention, err = durationValue(lookup, "AGENTSHARK_EVENT_RETENTION", cfg.Database.EventRetention); err != nil {
		return Config{}, err
	}
	if cfg.Database.TraceRetention, err = durationValue(lookup, "AGENTSHARK_TRACE_RETENTION", cfg.Database.TraceRetention); err != nil {
		return Config{}, err
	}
	if cfg.Database.PayloadRetention, err = durationValue(lookup, "AGENTSHARK_PAYLOAD_RETENTION", cfg.Database.PayloadRetention); err != nil {
		return Config{}, err
	}
	if cfg.Database.OutboxRetention, err = durationValue(lookup, "AGENTSHARK_OUTBOX_RETENTION", cfg.Database.OutboxRetention); err != nil {
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
	if err := validateDatabaseURL(cfg.Database.URL.Value()); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AGENTSHARK_DATABASE_URL is invalid: %w", err))
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
	if cfg.ScanTimeout < 5*time.Second || cfg.ScanTimeout > 5*time.Minute {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_SCAN_TIMEOUT must be between 5s and 5m"))
	}
	if cfg.UpstreamRetryMax < 0 || cfg.UpstreamRetryMax > 3 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_UPSTREAM_RETRY_MAX must be between 0 and 3"))
	}
	if cfg.PollInterval < time.Second || cfg.PollInterval > time.Minute {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_POLL_INTERVAL must be between 1s and 1m"))
	}
	if cfg.Database.MaxConnections < 1 || cfg.Database.MaxConnections > 100 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DATABASE_MAX_CONNS must be between 1 and 100"))
	}
	if cfg.Database.MinConnections < 0 || cfg.Database.MinConnections > cfg.Database.MaxConnections {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DATABASE_MIN_CONNS must be between 0 and AGENTSHARK_DATABASE_MAX_CONNS"))
	}
	if cfg.Database.ConnectTimeout < 100*time.Millisecond || cfg.Database.ConnectTimeout > 30*time.Second {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DATABASE_CONNECT_TIMEOUT must be between 100ms and 30s"))
	}
	if cfg.Database.EventRetention < time.Hour {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_EVENT_RETENTION must be at least 1h"))
	}
	if cfg.Database.TraceRetention < time.Hour {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_TRACE_RETENTION must be at least 1h"))
	}
	if cfg.Database.PayloadRetention < 0 || (cfg.Database.PayloadRetention > 0 && cfg.Database.PayloadRetention < time.Hour) || cfg.Database.PayloadRetention > cfg.Database.EventRetention {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_PAYLOAD_RETENTION must be 0 or between 1h and AGENTSHARK_EVENT_RETENTION"))
	}
	if cfg.Database.OutboxRetention < time.Minute || cfg.Database.OutboxRetention > cfg.Database.EventRetention {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_OUTBOX_RETENTION must be between 1m and AGENTSHARK_EVENT_RETENTION"))
	}
	return errors.Join(validationErrors...)
}

func (cfg Config) SafeSummary() string {
	return fmt.Sprintf("listen=%s environment=%s auth_disabled=%t cookie_secure=%t gateway=%s guard=%s database=%s database_auto_migrate=%t timeout=%s scan_timeout=%s retries=%d poll=%s",
		cfg.ListenAddr, cfg.Environment, cfg.AuthDisabled, cfg.CookieSecure, safeEndpoint(cfg.Gateway.BaseURL),
		safeEndpoint(cfg.Guard.BaseURL), safeDatabaseEndpoint(cfg.Database.URL.Value()), cfg.Database.AutoMigrate,
		cfg.UpstreamTimeout, cfg.ScanTimeout, cfg.UpstreamRetryMax, cfg.PollInterval)
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

func validateDatabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return errors.New("must be an absolute postgres or postgresql URL")
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return errors.New("must name a database")
	}
	if parsed.Fragment != "" {
		return errors.New("must not contain a fragment")
	}
	return nil
}

func safeDatabaseEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "[invalid]"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
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
