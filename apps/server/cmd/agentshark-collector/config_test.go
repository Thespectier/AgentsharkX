package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

func TestCollectorConfigDefaultsAndDedicatedDatabasePrecedence(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"AGENTSHARK_TRACE_INGEST_TOKEN":      "collector-token-with-enough-entropy",
		"AGENTSHARK_DATABASE_URL":            "postgresql://shared:secret@database/shared",
		"AGENTSHARK_COLLECTOR_DATABASE_URL":  "postgresql://collector:secret@database/traces",
		"AGENTSHARK_TRACE_PAYLOAD_RETENTION": "24h",
		"AGENTSHARK_TRACE_CONTENT_MODE":      "FULL",
	}
	config, err := loadCollectorConfig(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	if config.databaseURL != values["AGENTSHARK_COLLECTOR_DATABASE_URL"] {
		t.Fatalf("database URL = %q", config.databaseURL)
	}
	if config.listenAddress != defaultCollectorAddress || config.contentMode != telemetry.ContentModeFull ||
		config.payloadRetention != 24*time.Hour || config.traceRetention != 30*24*time.Hour ||
		config.maxSpansPerRequest != defaultMaxSpansPerRequest {
		t.Fatalf("config = %#v", config)
	}
}

func TestCollectorConfigAllowsSharedLocalDatabaseFallback(t *testing.T) {
	t.Parallel()

	config, err := loadCollectorConfig(mapLookup(map[string]string{
		"AGENTSHARK_TRACE_INGEST_TOKEN": "collector-token-with-enough-entropy",
		"AGENTSHARK_DATABASE_URL":       "postgresql://shared:secret@database/agentshark",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config.databaseURL, "/agentshark") {
		t.Fatalf("database URL = %q", config.databaseURL)
	}
}

func TestCollectorConfigRejectsUnsafeOrUnboundedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "token", key: "AGENTSHARK_TRACE_INGEST_TOKEN", value: "change-me-collector-token", want: "INGEST_TOKEN"},
		{name: "database", key: "AGENTSHARK_COLLECTOR_DATABASE_URL", value: "https://database/traces", want: "database URL"},
		{name: "content", key: "AGENTSHARK_TRACE_CONTENT_MODE", value: "everything", want: "CONTENT_MODE"},
		{name: "request timeout", key: "AGENTSHARK_COLLECTOR_REQUEST_TIMEOUT", value: "10ms", want: "REQUEST_TIMEOUT"},
		{name: "compressed size", key: "AGENTSHARK_COLLECTOR_MAX_COMPRESSED_BYTES", value: "100", want: "MAX_COMPRESSED_BYTES"},
		{name: "expanded size", key: "AGENTSHARK_COLLECTOR_MAX_DECOMPRESSED_BYTES", value: "1024", want: "must not be smaller"},
		{name: "span batch", key: "AGENTSHARK_COLLECTOR_MAX_SPANS_PER_REQUEST", value: "0", want: "MAX_SPANS_PER_REQUEST"},
		{name: "payload", key: "AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES", value: "12", want: "PAYLOAD_LIMIT_BYTES"},
		{name: "trace retention", key: "AGENTSHARK_TRACE_RETENTION", value: "30m", want: "TRACE_RETENTION"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{
				"AGENTSHARK_TRACE_INGEST_TOKEN":     "collector-token-with-enough-entropy",
				"AGENTSHARK_COLLECTOR_DATABASE_URL": "postgresql://collector:secret@database/traces",
			}
			values[test.key] = test.value
			_, err := loadCollectorConfig(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func mapLookup(values map[string]string) lookupFunc {
	return func(key string) (string, bool) {
		value, found := values[key]
		return value, found
	}
}
