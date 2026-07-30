package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadValidConfigurationAndRedactsSecrets(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":              "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":              "local",
		"AGENTSHARK_ADMIN_TOKEN":              "admin-token-with-enough-entropy",
		"AGENTSHARK_COOKIE_SECURE":            "false",
		"AGENTGATEWAY_BASE_URL":               "http://gateway.test:15000",
		"AGENTGATEWAY_ADMIN_TOKEN":            "gateway-secret",
		"AGENTGATEWAY_CONSOLE_URL":            "http://localhost:15000/ui",
		"AGENTGUARD_BASE_URL":                 "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":              "guard-secret-with-enough-entropy",
		"AGENTGUARD_CONSOLE_URL":              "http://localhost:38008",
		"AGENTSHARK_DEMO_GATEWAY_ADMIN_URL":   "http://demo-gateway.internal:15000",
		"AGENTSHARK_DEMO_GATEWAY_CONSOLE_URL": "http://127.0.0.1:15010",
		"AGENTSHARK_UPSTREAM_TIMEOUT":         "750ms",
		"AGENTSHARK_SCAN_TIMEOUT":             "45s",
		"AGENTSHARK_UPSTREAM_RETRY_MAX":       "2",
		"AGENTSHARK_POLL_INTERVAL":            "3s",
		"AGENTSHARK_DATABASE_URL":             "postgresql://agentshark:database-secret@postgres.test:5432/agentshark?sslmode=disable",
		"AGENTSHARK_DATABASE_AUTO_MIGRATE":    "false",
		"AGENTSHARK_DATABASE_MAX_CONNS":       "12",
		"AGENTSHARK_DATABASE_MIN_CONNS":       "2",
		"AGENTSHARK_DATABASE_CONNECT_TIMEOUT": "4s",
		"AGENTSHARK_EVENT_RETENTION":          "720h",
		"AGENTSHARK_TRACE_RETENTION":          "336h",
		"AGENTSHARK_PAYLOAD_RETENTION":        "24h",
		"AGENTSHARK_OUTBOX_RETENTION":         "12h",
	}

	cfg, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UpstreamTimeout != 750*time.Millisecond || cfg.ScanTimeout != 45*time.Second || cfg.UpstreamRetryMax != 2 {
		t.Fatalf("unexpected upstream policy: timeout=%s scan=%s retry=%d", cfg.UpstreamTimeout, cfg.ScanTimeout, cfg.UpstreamRetryMax)
	}
	if cfg.Database.TraceRetention != 14*24*time.Hour {
		t.Fatalf("trace retention = %s", cfg.Database.TraceRetention)
	}
	if cfg.Database.AutoMigrate {
		t.Fatal("database auto-migrate should honor the configured false value")
	}
	if cfg.Demo.GatewayAdminURL != values["AGENTSHARK_DEMO_GATEWAY_ADMIN_URL"] ||
		cfg.Demo.GatewayConsoleURL != values["AGENTSHARK_DEMO_GATEWAY_CONSOLE_URL"] {
		t.Fatalf("Demo gateway management and browser endpoints were conflated: %#v", cfg.Demo)
	}
	if got := cfg.AdminToken.Value(); got != values["AGENTSHARK_ADMIN_TOKEN"] {
		t.Fatalf("admin token did not round trip")
	}

	safe := cfg.SafeSummary()
	for _, secret := range []string{values["AGENTSHARK_ADMIN_TOKEN"], values["AGENTGATEWAY_ADMIN_TOKEN"], values["AGENTGUARD_ADMIN_TOKEN"], "database-secret"} {
		if strings.Contains(safe, secret) {
			t.Fatalf("SafeSummary leaked secret %q: %s", secret, safe)
		}
	}
	if strings.Contains(cfg.AdminToken.String(), values["AGENTSHARK_ADMIN_TOKEN"]) {
		t.Fatal("Secret.String leaked its value")
	}
}

func TestLoadRejectsMissingDatabaseAndInvalidRetention(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":      "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":      "local",
		"AGENTSHARK_ADMIN_TOKEN":      "admin-token-with-enough-entropy",
		"AGENTGUARD_ADMIN_TOKEN":      "guard-secret-with-enough-entropy",
		"AGENTSHARK_EVENT_RETENTION":  "1h",
		"AGENTSHARK_TRACE_RETENTION":  "30m",
		"AGENTSHARK_OUTBOX_RETENTION": "2h",
	}
	_, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil || !strings.Contains(err.Error(), "AGENTSHARK_DATABASE_URL") ||
		!strings.Contains(err.Error(), "AGENTSHARK_TRACE_RETENTION") ||
		!strings.Contains(err.Error(), "AGENTSHARK_OUTBOX_RETENTION") {
		t.Fatalf("expected database validation errors, got %v", err)
	}
}

func TestLoadRejectsUnsafeDevelopmentAuth(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":   "0.0.0.0:8080",
		"AGENTSHARK_ENVIRONMENT":   "local",
		"AGENTSHARK_AUTH_DISABLED": "true",
		"AGENTSHARK_COOKIE_SECURE": "false",
		"AGENTGATEWAY_BASE_URL":    "http://gateway.test:15000",
		"AGENTGUARD_BASE_URL":      "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":   "guard-secret-with-enough-entropy",
	}

	_, err := Load(func(key string) (string, bool) {
		value, ok := base[key]
		return value, ok
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback validation error, got %v", err)
	}
}

func TestLoadRejectsMissingTokensBeforeNonLoopbackStartup(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR": "0.0.0.0:8080",
		"AGENTSHARK_ENVIRONMENT": "preview",
		"AGENTGATEWAY_BASE_URL":  "http://gateway.test:15000",
		"AGENTGUARD_BASE_URL":    "http://guard.test:38080",
	}
	_, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil {
		t.Fatal("expected missing startup credentials to be rejected")
	}
	message := err.Error()
	for _, name := range []string{"AGENTSHARK_ADMIN_TOKEN", "AGENTGUARD_ADMIN_TOKEN"} {
		if !strings.Contains(message, name) {
			t.Fatalf("missing validation for %s: %s", name, message)
		}
	}
}

func TestLoadRejectsPlaceholderAndURLCredentials(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR": "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT": "local",
		"AGENTSHARK_ADMIN_TOKEN": "change-me-before-use",
		"AGENTGATEWAY_BASE_URL":  "http://admin:secret@gateway.test:15000",
		"AGENTGUARD_BASE_URL":    "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN": "guard-secret-with-enough-entropy",
	}

	_, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil {
		t.Fatal("expected unsafe configuration to be rejected")
	}
	message := err.Error()
	if strings.Contains(message, "secret") || strings.Contains(message, "change-me-before-use") {
		t.Fatalf("validation error leaked configuration values: %s", message)
	}
}

func TestLoadRejectsUnsafeConsoleURL(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":   "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":   "local",
		"AGENTSHARK_ADMIN_TOKEN":   "admin-token-with-enough-entropy",
		"AGENTGATEWAY_BASE_URL":    "http://gateway.test:15000",
		"AGENTGATEWAY_CONSOLE_URL": "https://user:secret@gateway.test/ui",
		"AGENTGUARD_BASE_URL":      "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":   "guard-secret-with-enough-entropy",
	}

	_, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil || !strings.Contains(err.Error(), "AGENTGATEWAY_CONSOLE_URL") || strings.Contains(err.Error(), "user:secret") {
		t.Fatalf("expected secret-safe console URL rejection, got %v", err)
	}
}

func TestLoadDemoDefaultsDisabledAndValidatesEnabledToken(t *testing.T) {
	t.Parallel()
	base := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":   "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":   "local",
		"AGENTSHARK_ADMIN_TOKEN":   "admin-token-with-enough-entropy",
		"AGENTGUARD_ADMIN_TOKEN":   "guard-secret-with-enough-entropy",
		"AGENTSHARK_DATABASE_URL":  "postgresql://agentshark:secret@postgres.test:5432/agentshark",
		"AGENTSHARK_COOKIE_SECURE": "false",
	}
	lookup := func(key string) (string, bool) { value, ok := base[key]; return value, ok }
	cfg, err := Load(lookup)
	if err != nil || cfg.Demo.Enabled {
		t.Fatalf("disabled Demo defaults: cfg=%#v err=%v", cfg.Demo, err)
	}
	if cfg.Demo.LLMModel != "agentshark-demo-model-v1" || cfg.Demo.GatewayAdminURL != "http://agentshark-demo-gateway:15000" {
		t.Fatalf("Demo model/admin defaults = %#v", cfg.Demo)
	}
	base["AGENTSHARK_DEMO_ENABLED"] = "true"
	if _, err := Load(lookup); err == nil || !strings.Contains(err.Error(), "AGENTSHARK_DEMO_RUNNER_TOKEN") {
		t.Fatalf("missing enabled Runner token error = %v", err)
	}
	base["AGENTSHARK_DEMO_RUNNER_TOKEN"] = "0123456789abcdef0123456789abcdef"
	base["AGENTSHARK_DEMO_LLM_MODEL"] = "custom-demo-model"
	base["AGENTSHARK_DEMO_GATEWAY_ADMIN_URL"] = "http://demo-gateway.test:15000"
	if cfg, err = Load(lookup); err != nil || !cfg.Demo.Enabled || cfg.Demo.LLMModel != "custom-demo-model" || cfg.Demo.GatewayAdminURL != "http://demo-gateway.test:15000" {
		t.Fatalf("enabled Demo configuration: cfg=%#v err=%v", cfg.Demo, err)
	}
}
