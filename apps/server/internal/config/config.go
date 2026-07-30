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

type Demo struct {
	Enabled           bool
	RunnerURL         string
	RunnerToken       Secret
	MaxConcurrency    int
	DefaultDelayMS    int
	RunTimeout        time.Duration
	MonitorInterval   time.Duration
	CollectorURL      string
	LLMBaseURL        string
	LLMModel          string
	MCPURL            string
	GatewayAdminURL   string
	GatewayConsoleURL string
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
	Demo             Demo
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
		Demo: Demo{
			RunnerURL: "http://demo-runner:39100", MaxConcurrency: 1,
			DefaultDelayMS: 700, RunTimeout: 10 * time.Minute, MonitorInterval: 750 * time.Millisecond,
			CollectorURL:      "http://agentshark-collector:4318/readyz",
			LLMBaseURL:        "http://agentshark-demo-gateway:39000/v1",
			LLMModel:          "agentshark-demo-model-v1",
			MCPURL:            "http://demo-fixtures:39200/mcp",
			GatewayAdminURL:   "http://agentshark-demo-gateway:15000",
			GatewayConsoleURL: "",
		},
		UpstreamTimeout:  3 * time.Second,
		ScanTimeout:      90 * time.Second,
		UpstreamRetryMax: 1,
		PollInterval:     2 * time.Second,
	}

	var err error
	if cfg.Demo.Enabled, err = boolValue(lookup, "AGENTSHARK_DEMO_ENABLED", false); err != nil {
		return Config{}, err
	}
	cfg.Demo.RunnerURL = valueOr(lookup, "AGENTSHARK_DEMO_RUNNER_URL", cfg.Demo.RunnerURL)
	cfg.Demo.RunnerToken = NewSecret(valueOr(lookup, "AGENTSHARK_DEMO_RUNNER_TOKEN", ""))
	cfg.Demo.CollectorURL = valueOr(lookup, "AGENTSHARK_DEMO_COLLECTOR_READY_URL", cfg.Demo.CollectorURL)
	cfg.Demo.LLMBaseURL = valueOr(lookup, "AGENTSHARK_DEMO_LLM_BASE_URL", cfg.Demo.LLMBaseURL)
	cfg.Demo.LLMModel = valueOr(lookup, "AGENTSHARK_DEMO_LLM_MODEL", cfg.Demo.LLMModel)
	cfg.Demo.MCPURL = valueOr(lookup, "AGENTSHARK_DEMO_MCP_URL", cfg.Demo.MCPURL)
	cfg.Demo.GatewayAdminURL = valueOr(lookup, "AGENTSHARK_DEMO_GATEWAY_ADMIN_URL", cfg.Demo.GatewayAdminURL)
	cfg.Demo.GatewayConsoleURL = valueOr(lookup, "AGENTSHARK_DEMO_GATEWAY_CONSOLE_URL", cfg.Demo.GatewayConsoleURL)
	if cfg.Demo.MaxConcurrency, err = intValue(lookup, "AGENTSHARK_DEMO_MAX_CONCURRENCY", cfg.Demo.MaxConcurrency); err != nil {
		return Config{}, err
	}
	if cfg.Demo.DefaultDelayMS, err = intValue(lookup, "AGENTSHARK_DEMO_DEFAULT_DELAY_MS", cfg.Demo.DefaultDelayMS); err != nil {
		return Config{}, err
	}
	if cfg.Demo.RunTimeout, err = durationValue(lookup, "AGENTSHARK_DEMO_RUN_TIMEOUT", cfg.Demo.RunTimeout); err != nil {
		return Config{}, err
	}
	if cfg.Demo.MonitorInterval, err = durationValue(lookup, "AGENTSHARK_DEMO_MONITOR_INTERVAL", cfg.Demo.MonitorInterval); err != nil {
		return Config{}, err
	}
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
	for name, rawURL := range map[string]string{
		"AGENTSHARK_DEMO_RUNNER_URL":          cfg.Demo.RunnerURL,
		"AGENTSHARK_DEMO_COLLECTOR_READY_URL": cfg.Demo.CollectorURL,
		"AGENTSHARK_DEMO_LLM_BASE_URL":        cfg.Demo.LLMBaseURL,
		"AGENTSHARK_DEMO_MCP_URL":             cfg.Demo.MCPURL,
		"AGENTSHARK_DEMO_GATEWAY_ADMIN_URL":   cfg.Demo.GatewayAdminURL,
	} {
		if err := validateURL(rawURL); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("%s is invalid: %w", name, err))
		}
	}
	if cfg.Demo.Enabled && (len([]byte(cfg.Demo.RunnerToken.Value())) < 32 || !validSecret(cfg.Demo.RunnerToken.Value())) {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DEMO_RUNNER_TOKEN must be a non-placeholder value of at least 32 bytes when Demo Lab is enabled"))
	}
	if cfg.Demo.MaxConcurrency != 1 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DEMO_MAX_CONCURRENCY must be 1"))
	}
	if cfg.Demo.DefaultDelayMS < 0 || cfg.Demo.DefaultDelayMS > 2000 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DEMO_DEFAULT_DELAY_MS must be between 0 and 2000"))
	}
	if strings.TrimSpace(cfg.Demo.LLMModel) == "" || len(cfg.Demo.LLMModel) > 128 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DEMO_LLM_MODEL must contain between 1 and 128 characters"))
	}
	if cfg.Demo.RunTimeout < time.Minute || cfg.Demo.RunTimeout > time.Hour {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DEMO_RUN_TIMEOUT must be between 1m and 1h"))
	}
	if cfg.Demo.MonitorInterval < 500*time.Millisecond || cfg.Demo.MonitorInterval > time.Second {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_DEMO_MONITOR_INTERVAL must be between 500ms and 1s"))
	}
	if err := validateDatabaseURL(cfg.Database.URL.Value()); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("AGENTSHARK_DATABASE_URL is invalid: %w", err))
	}
	for name, rawURL := range map[string]string{
		"AGENTGATEWAY_CONSOLE_URL":            cfg.Gateway.ConsoleURL,
		"AGENTGUARD_CONSOLE_URL":              cfg.Guard.ConsoleURL,
		"AGENTSHARK_DEMO_GATEWAY_CONSOLE_URL": cfg.Demo.GatewayConsoleURL,
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
	return fmt.Sprintf("listen=%s environment=%s auth_disabled=%t cookie_secure=%t gateway=%s guard=%s database=%s database_auto_migrate=%t timeout=%s scan_timeout=%s retries=%d poll=%s demo_enabled=%t demo_runner=%s demo_gateway_admin=%s demo_model=%s",
		cfg.ListenAddr, cfg.Environment, cfg.AuthDisabled, cfg.CookieSecure, safeEndpoint(cfg.Gateway.BaseURL),
		safeEndpoint(cfg.Guard.BaseURL), safeDatabaseEndpoint(cfg.Database.URL.Value()), cfg.Database.AutoMigrate,
		cfg.UpstreamTimeout, cfg.ScanTimeout, cfg.UpstreamRetryMax, cfg.PollInterval,
		cfg.Demo.Enabled, safeEndpoint(cfg.Demo.RunnerURL), safeEndpoint(cfg.Demo.GatewayAdminURL), cfg.Demo.LLMModel)
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
