package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadValidConfigurationAndRedactsSecrets(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":        "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":        "local",
		"AGENTSHARK_ADMIN_TOKEN":        "admin-token-with-enough-entropy",
		"AGENTSHARK_COOKIE_SECURE":      "false",
		"AGENTGATEWAY_BASE_URL":         "http://gateway.test:15000",
		"AGENTGATEWAY_ADMIN_TOKEN":      "gateway-secret",
		"AGENTGATEWAY_CONSOLE_URL":      "http://localhost:15000/ui",
		"AGENTGUARD_BASE_URL":           "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":        "guard-secret-with-enough-entropy",
		"AGENTGUARD_CONSOLE_URL":        "http://localhost:38008",
		"AGENTSHARK_UPSTREAM_TIMEOUT":   "750ms",
		"AGENTSHARK_UPSTREAM_RETRY_MAX": "2",
		"AGENTSHARK_POLL_INTERVAL":      "3s",
		"AGENTSHARK_REDACT_PAYLOADS":    "true",
	}

	cfg, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UpstreamTimeout != 750*time.Millisecond || cfg.UpstreamRetryMax != 2 {
		t.Fatalf("unexpected upstream policy: timeout=%s retry=%d", cfg.UpstreamTimeout, cfg.UpstreamRetryMax)
	}
	if got := cfg.AdminToken.Value(); got != values["AGENTSHARK_ADMIN_TOKEN"] {
		t.Fatalf("admin token did not round trip")
	}

	safe := cfg.SafeSummary()
	for _, secret := range []string{values["AGENTSHARK_ADMIN_TOKEN"], values["AGENTGATEWAY_ADMIN_TOKEN"], values["AGENTGUARD_ADMIN_TOKEN"]} {
		if strings.Contains(safe, secret) {
			t.Fatalf("SafeSummary leaked secret %q: %s", secret, safe)
		}
	}
	if strings.Contains(cfg.AdminToken.String(), values["AGENTSHARK_ADMIN_TOKEN"]) {
		t.Fatal("Secret.String leaked its value")
	}
}

func TestLoadRejectsUnsafeDevelopmentAuth(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":     "0.0.0.0:8080",
		"AGENTSHARK_ENVIRONMENT":     "local",
		"AGENTSHARK_AUTH_DISABLED":   "true",
		"AGENTSHARK_COOKIE_SECURE":   "false",
		"AGENTGATEWAY_BASE_URL":      "http://gateway.test:15000",
		"AGENTGUARD_BASE_URL":        "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":     "guard-secret-with-enough-entropy",
		"AGENTSHARK_REDACT_PAYLOADS": "true",
	}

	_, err := Load(func(key string) (string, bool) {
		value, ok := base[key]
		return value, ok
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback validation error, got %v", err)
	}
}

func TestLoadRejectsPlaceholderAndURLCredentials(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_LISTEN_ADDR":     "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":     "local",
		"AGENTSHARK_ADMIN_TOKEN":     "change-me-before-use",
		"AGENTGATEWAY_BASE_URL":      "http://admin:secret@gateway.test:15000",
		"AGENTGUARD_BASE_URL":        "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":     "guard-secret-with-enough-entropy",
		"AGENTSHARK_REDACT_PAYLOADS": "true",
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
		"AGENTSHARK_LISTEN_ADDR":     "127.0.0.1:8080",
		"AGENTSHARK_ENVIRONMENT":     "local",
		"AGENTSHARK_ADMIN_TOKEN":     "admin-token-with-enough-entropy",
		"AGENTGATEWAY_BASE_URL":      "http://gateway.test:15000",
		"AGENTGATEWAY_CONSOLE_URL":   "https://user:secret@gateway.test/ui",
		"AGENTGUARD_BASE_URL":        "http://guard.test:38080",
		"AGENTGUARD_ADMIN_TOKEN":     "guard-secret-with-enough-entropy",
		"AGENTSHARK_REDACT_PAYLOADS": "true",
	}

	_, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil || !strings.Contains(err.Error(), "AGENTGATEWAY_CONSOLE_URL") || strings.Contains(err.Error(), "user:secret") {
		t.Fatalf("expected secret-safe console URL rejection, got %v", err)
	}
}
