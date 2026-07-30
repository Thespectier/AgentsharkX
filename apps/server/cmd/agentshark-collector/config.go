package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

const (
	defaultCollectorAddress       = "0.0.0.0:4318"
	defaultMaxCompressedBytes     = int64(4 << 20)
	defaultMaxDecompressedBytes   = int64(16 << 20)
	defaultTracePayloadLimitBytes = int64(256 << 10)
	defaultMaxSpansPerRequest     = 2048
)

type lookupFunc func(string) (string, bool)

type collectorConfig struct {
	listenAddress        string
	databaseURL          string
	databaseMaxConns     int
	databaseMinConns     int
	databaseConnect      time.Duration
	ingestToken          string
	requestTimeout       time.Duration
	maxCompressedBytes   int64
	maxDecompressedBytes int64
	maxSpansPerRequest   int
	contentMode          telemetry.ContentMode
	payloadLimitBytes    int64
	payloadRetention     time.Duration
	traceRetention       time.Duration
	outboxRetention      time.Duration
}

func loadCollectorConfig(lookup lookupFunc) (collectorConfig, error) {
	databaseURL := valueOr(lookup, "AGENTSHARK_COLLECTOR_DATABASE_URL", "")
	if databaseURL == "" {
		// Local preview may share the BFF account. A dedicated variable takes
		// precedence so production can grant the Collector only Trace-table
		// read/write/retention and payload-free Trace-outbox capabilities.
		databaseURL = valueOr(lookup, "AGENTSHARK_DATABASE_URL", "")
	}
	config := collectorConfig{
		listenAddress: defaultCollectorAddress, databaseURL: databaseURL,
		databaseMaxConns: 10, databaseMinConns: 1, databaseConnect: 5 * time.Second,
		ingestToken: valueOr(lookup, "AGENTSHARK_TRACE_INGEST_TOKEN", ""), requestTimeout: 10 * time.Second,
		maxCompressedBytes: defaultMaxCompressedBytes, maxDecompressedBytes: defaultMaxDecompressedBytes,
		maxSpansPerRequest: defaultMaxSpansPerRequest,
		contentMode:        telemetry.ContentModeMetadata, payloadLimitBytes: defaultTracePayloadLimitBytes,
		payloadRetention: 24 * time.Hour, traceRetention: 30 * 24 * time.Hour,
		outboxRetention: 24 * time.Hour,
	}
	config.listenAddress = valueOr(lookup, "AGENTSHARK_COLLECTOR_LISTEN_ADDR", config.listenAddress)

	var err error
	if config.databaseMaxConns, err = intValue(lookup, "AGENTSHARK_COLLECTOR_DATABASE_MAX_CONNS", config.databaseMaxConns); err != nil {
		return collectorConfig{}, err
	}
	if config.databaseMinConns, err = intValue(lookup, "AGENTSHARK_COLLECTOR_DATABASE_MIN_CONNS", config.databaseMinConns); err != nil {
		return collectorConfig{}, err
	}
	if config.databaseConnect, err = durationValue(lookup, "AGENTSHARK_COLLECTOR_DATABASE_CONNECT_TIMEOUT", config.databaseConnect); err != nil {
		return collectorConfig{}, err
	}
	if config.requestTimeout, err = durationValue(lookup, "AGENTSHARK_COLLECTOR_REQUEST_TIMEOUT", config.requestTimeout); err != nil {
		return collectorConfig{}, err
	}
	if config.maxCompressedBytes, err = int64Value(lookup, "AGENTSHARK_COLLECTOR_MAX_COMPRESSED_BYTES", config.maxCompressedBytes); err != nil {
		return collectorConfig{}, err
	}
	if config.maxDecompressedBytes, err = int64Value(lookup, "AGENTSHARK_COLLECTOR_MAX_DECOMPRESSED_BYTES", config.maxDecompressedBytes); err != nil {
		return collectorConfig{}, err
	}
	if config.maxSpansPerRequest, err = intValue(lookup, "AGENTSHARK_COLLECTOR_MAX_SPANS_PER_REQUEST", config.maxSpansPerRequest); err != nil {
		return collectorConfig{}, err
	}
	if config.payloadLimitBytes, err = int64Value(lookup, "AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES", config.payloadLimitBytes); err != nil {
		return collectorConfig{}, err
	}
	if config.payloadRetention, err = durationValue(lookup, "AGENTSHARK_TRACE_PAYLOAD_RETENTION", config.payloadRetention); err != nil {
		return collectorConfig{}, err
	}
	if config.traceRetention, err = durationValue(lookup, "AGENTSHARK_TRACE_RETENTION", config.traceRetention); err != nil {
		return collectorConfig{}, err
	}
	if config.outboxRetention, err = durationValue(lookup, "AGENTSHARK_OUTBOX_RETENTION", config.outboxRetention); err != nil {
		return collectorConfig{}, err
	}
	config.contentMode = telemetry.ContentMode(strings.ToLower(valueOr(lookup, "AGENTSHARK_TRACE_CONTENT_MODE", string(config.contentMode))))
	if err := config.validate(); err != nil {
		return collectorConfig{}, err
	}
	return config, nil
}

func (config collectorConfig) validate() error {
	var validationErrors []error
	if _, _, err := net.SplitHostPort(config.listenAddress); err != nil {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_LISTEN_ADDR must be host:port"))
	}
	if !validCollectorSecret(config.ingestToken) {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_TRACE_INGEST_TOKEN must be a non-placeholder value of at least 16 characters"))
	}
	if err := validatePostgresURL(config.databaseURL); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("Collector database URL is invalid: %w", err))
	}
	if config.databaseMaxConns < 1 || config.databaseMaxConns > 100 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_DATABASE_MAX_CONNS must be between 1 and 100"))
	}
	if config.databaseMinConns < 0 || config.databaseMinConns > config.databaseMaxConns {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_DATABASE_MIN_CONNS must be between 0 and AGENTSHARK_COLLECTOR_DATABASE_MAX_CONNS"))
	}
	if config.databaseConnect < 100*time.Millisecond || config.databaseConnect > 30*time.Second {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_DATABASE_CONNECT_TIMEOUT must be between 100ms and 30s"))
	}
	if config.requestTimeout < 100*time.Millisecond || config.requestTimeout > 2*time.Minute {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_REQUEST_TIMEOUT must be between 100ms and 2m"))
	}
	if config.maxCompressedBytes < 1024 || config.maxCompressedBytes > 64<<20 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_MAX_COMPRESSED_BYTES must be between 1024 and 67108864"))
	}
	if config.maxDecompressedBytes < 1024 || config.maxDecompressedBytes > 256<<20 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_MAX_DECOMPRESSED_BYTES must be between 1024 and 268435456"))
	}
	if config.maxDecompressedBytes < config.maxCompressedBytes {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_MAX_DECOMPRESSED_BYTES must not be smaller than the compressed limit"))
	}
	if config.maxSpansPerRequest < 1 || config.maxSpansPerRequest > 100000 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_COLLECTOR_MAX_SPANS_PER_REQUEST must be between 1 and 100000"))
	}
	if !config.contentMode.Valid() {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_TRACE_CONTENT_MODE must be none, metadata, or full"))
	}
	if config.payloadLimitBytes < 1024 || config.payloadLimitBytes > 16<<20 {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES must be between 1024 and 16777216"))
	}
	if config.payloadRetention < time.Minute || config.payloadRetention > 30*24*time.Hour {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_TRACE_PAYLOAD_RETENTION must be between 1m and 720h"))
	}
	if config.traceRetention < time.Hour {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_TRACE_RETENTION must be at least 1h"))
	}
	if config.outboxRetention < time.Minute {
		validationErrors = append(validationErrors, errors.New("AGENTSHARK_OUTBOX_RETENTION must be at least 1m"))
	}
	return errors.Join(validationErrors...)
}

func validCollectorSecret(value string) bool {
	normalized := strings.ToLower(value)
	return len(value) >= 16 && !strings.ContainsAny(value, " \t\r\n") && !strings.HasPrefix(normalized, "change-me") &&
		!strings.HasPrefix(normalized, "replace-me") && !strings.HasPrefix(normalized, "example")
}

func validatePostgresURL(value string) error {
	parsed, err := url.Parse(value)
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

func valueOr(lookup lookupFunc, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func intValue(lookup lookupFunc, key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func int64Value(lookup lookupFunc, key string, fallback int64) (int64, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of bytes", key)
	}
	return parsed, nil
}

func durationValue(lookup lookupFunc, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration", key)
	}
	return parsed, nil
}
