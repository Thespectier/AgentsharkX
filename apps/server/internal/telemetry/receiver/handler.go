package receiver

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/normalize"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	tracesPath              = "/v1/traces"
	protobufContentType     = "application/x-protobuf"
	defaultCompressedLimit  = int64(4 << 20)
	defaultExpandedLimit    = int64(16 << 20)
	defaultRequestTimeout   = 10 * time.Second
	defaultMaxSpans         = 2048
	minimumIngestTokenBytes = 16
	maximumCompressedLimit  = int64(64 << 20)
	maximumExpandedLimit    = int64(256 << 20)
	maximumRequestTimeout   = 2 * time.Minute
	maximumMaxSpans         = 100000
)

var (
	errExpandedBodyTooLarge = errors.New("expanded request body exceeds limit")
	errWriterRequired       = errors.New("trace writer is required")
	errTokenRequired        = errors.New("ingest token must contain at least 16 characters")
	errInvalidLimits        = errors.New("trace receiver limits are invalid")
)

type Options struct {
	IngestToken          string
	MaxCompressedBytes   int64
	MaxDecompressedBytes int64
	RequestTimeout       time.Duration
	MaxSpansPerRequest   int
	Normalize            normalize.Options
	Metrics              *Metrics
	Logger               *slog.Logger
}

type Handler struct {
	writer               storage.TraceWriter
	tokenDigest          [sha256.Size]byte
	maxCompressedBytes   int64
	maxDecompressedBytes int64
	requestTimeout       time.Duration
	maxSpansPerRequest   int
	normalizeOptions     normalize.Options
	metrics              *Metrics
	logger               *slog.Logger
}

func New(writer storage.TraceWriter, options Options) (*Handler, error) {
	if writer == nil {
		return nil, errWriterRequired
	}
	if len(options.IngestToken) < minimumIngestTokenBytes {
		return nil, errTokenRequired
	}
	if options.MaxCompressedBytes <= 0 {
		options.MaxCompressedBytes = defaultCompressedLimit
	}
	if options.MaxDecompressedBytes <= 0 {
		options.MaxDecompressedBytes = defaultExpandedLimit
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaultRequestTimeout
	}
	if options.MaxSpansPerRequest <= 0 {
		options.MaxSpansPerRequest = defaultMaxSpans
	}
	if options.MaxCompressedBytes > maximumCompressedLimit ||
		options.MaxDecompressedBytes > maximumExpandedLimit ||
		options.MaxDecompressedBytes < options.MaxCompressedBytes ||
		options.RequestTimeout > maximumRequestTimeout ||
		options.MaxSpansPerRequest > maximumMaxSpans {
		return nil, errInvalidLimits
	}
	if options.Metrics == nil {
		options.Metrics = &Metrics{}
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{
		writer: writer, tokenDigest: sha256.Sum256([]byte(options.IngestToken)),
		maxCompressedBytes: options.MaxCompressedBytes, maxDecompressedBytes: options.MaxDecompressedBytes,
		requestTimeout: options.RequestTimeout, normalizeOptions: options.Normalize,
		maxSpansPerRequest: options.MaxSpansPerRequest,
		metrics:            options.Metrics, logger: options.Logger,
	}, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.metrics.requests.Add(1)
	if request.URL.Path != tracesPath {
		handler.reject(response, http.StatusNotFound, "endpoint not found")
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		handler.reject(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !handler.authorized(request.Header.Values("Authorization")) {
		response.Header().Set("WWW-Authenticate", `Bearer realm="agentshark-traces"`)
		handler.reject(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !isProtobuf(request.Header.Get("Content-Type")) {
		handler.reject(response, http.StatusUnsupportedMediaType, "Content-Type must be application/x-protobuf")
		return
	}
	encoding, accepted := contentEncoding(request.Header.Get("Content-Encoding"))
	if !accepted {
		handler.reject(response, http.StatusUnsupportedMediaType, "Content-Encoding must be identity or gzip")
		return
	}
	if request.ContentLength > handler.maxCompressedBytes {
		handler.reject(response, http.StatusRequestEntityTooLarge, "request body exceeds compressed size limit")
		return
	}

	deadline := time.Now().Add(handler.requestTimeout)
	ctx, cancel := context.WithDeadline(request.Context(), deadline)
	defer cancel()
	_ = http.NewResponseController(response).SetReadDeadline(deadline)
	request = request.WithContext(ctx)

	document, err := handler.readBody(response, request, encoding)
	if err != nil {
		handler.rejectBodyError(response, ctx, err)
		return
	}
	if err := ctx.Err(); err != nil {
		handler.reject(response, http.StatusRequestTimeout, "trace ingest request timed out")
		return
	}
	var exportRequest collectortracev1.ExportTraceServiceRequest
	if err := proto.Unmarshal(document, &exportRequest); err != nil {
		handler.reject(response, http.StatusBadRequest, "request body is not valid OTLP protobuf")
		return
	}
	if countSpans(&exportRequest) > handler.maxSpansPerRequest {
		handler.reject(response, http.StatusRequestEntityTooLarge, "request contains too many spans")
		return
	}

	batch, report := normalize.Traces(&exportRequest, handler.normalizeOptions)
	rejected := len(report.Rejections)
	handler.metrics.observeNormalization(report.Received, report.Accepted, rejected)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		handler.reject(response, http.StatusRequestTimeout, "trace ingest request timed out")
		return
	}
	if report.Accepted > 0 {
		startedAt := time.Now()
		result, err := handler.writer.WriteBatch(ctx, batch)
		handler.metrics.observeWrite(startedAt, result.Inserted, result.Updated, result.Duplicates, err != nil)
		if err != nil {
			// A storage error can contain driver context. Never copy it to logs or
			// the response because values may originate in span attributes.
			handler.logger.Error("trace batch persistence failed")
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				handler.reject(response, http.StatusGatewayTimeout, "trace ingest request timed out")
				return
			}
			handler.reject(response, http.StatusServiceUnavailable, "trace storage is unavailable")
			return
		}
	}

	exportResponse := &collectortracev1.ExportTraceServiceResponse{}
	if rejected > 0 {
		exportResponse.PartialSuccess = &collectortracev1.ExportTracePartialSuccess{
			RejectedSpans: int64(rejected),
			ErrorMessage:  rejectionDiagnostic(report.Rejections),
		}
	}
	handler.writeProto(response, exportResponse)
}

func countSpans(request *collectortracev1.ExportTraceServiceRequest) int {
	count := 0
	for _, resourceSpans := range request.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			count += len(scopeSpans.GetSpans())
		}
	}
	return count
}

func (handler *Handler) authorized(values []string) bool {
	presented := ""
	validShape := len(values) == 1
	if validShape {
		parts := strings.Fields(values[0])
		validShape = len(parts) == 2 && strings.EqualFold(parts[0], "Bearer")
		if validShape {
			presented = parts[1]
		}
	}
	presentedDigest := sha256.Sum256([]byte(presented))
	matched := subtle.ConstantTimeCompare(presentedDigest[:], handler.tokenDigest[:]) == 1
	return validShape && matched
}

func (handler *Handler) readBody(response http.ResponseWriter, request *http.Request, encoding string) ([]byte, error) {
	compressed := http.MaxBytesReader(response, request.Body, handler.maxCompressedBytes)
	decompressed := io.Reader(compressed)
	if encoding == "gzip" {
		gzipReader, err := gzip.NewReader(compressed)
		if err != nil {
			return nil, err
		}
		decompressed = gzipReader
		defer gzipReader.Close()
	}
	limited := &io.LimitedReader{R: decompressed, N: handler.maxDecompressedBytes + 1}
	document, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(document)) > handler.maxDecompressedBytes {
		return nil, errExpandedBodyTooLarge
	}
	return document, nil
}

func (handler *Handler) rejectBodyError(response http.ResponseWriter, ctx context.Context, err error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		handler.reject(response, http.StatusRequestTimeout, "trace ingest request timed out")
		return
	}
	var maximumBytesError *http.MaxBytesError
	if errors.As(err, &maximumBytesError) {
		handler.reject(response, http.StatusRequestEntityTooLarge, "request body exceeds compressed size limit")
		return
	}
	if errors.Is(err, errExpandedBodyTooLarge) {
		handler.reject(response, http.StatusRequestEntityTooLarge, "request body exceeds decompressed size limit")
		return
	}
	handler.reject(response, http.StatusBadRequest, "request body is not valid for its declared encoding")
}

func (handler *Handler) reject(response http.ResponseWriter, status int, message string) {
	handler.metrics.rejectRequest()
	http.Error(response, message, status)
}

func (*Handler) writeProto(response http.ResponseWriter, message proto.Message) {
	document, err := proto.Marshal(message)
	if err != nil {
		http.Error(response, "trace response encoding failed", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", protobufContentType)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(document)
}

func isProtobuf(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, protobufContentType) && len(parameters) == 0
}

func contentEncoding(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "identity":
		return "identity", true
	case "gzip":
		return "gzip", true
	default:
		return "", false
	}
}

func rejectionDiagnostic(rejections []normalize.Rejection) string {
	counts := make(map[string]int)
	for _, rejection := range rejections {
		counts[diagnosticCategory(rejection.Reason)]++
	}
	categories := make([]string, 0, len(counts))
	for category := range counts {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	parts := make([]string, 0, len(categories))
	for _, category := range categories {
		parts = append(parts, fmt.Sprintf("%s=%d", category, counts[category]))
	}
	return fmt.Sprintf("rejected %d invalid span(s): %s", len(rejections), strings.Join(parts, ", "))
}

func diagnosticCategory(reason string) string {
	normalized := strings.ToLower(reason)
	switch {
	case strings.Contains(normalized, "parent") && strings.Contains(normalized, "span"):
		return "invalid_parent_span_id"
	case strings.Contains(normalized, "link") && strings.Contains(normalized, "id"):
		return "invalid_link_id"
	case strings.Contains(normalized, "trace") && strings.Contains(normalized, "id"):
		return "invalid_trace_id"
	case strings.Contains(normalized, "span") && strings.Contains(normalized, "id"):
		return "invalid_span_id"
	case strings.Contains(normalized, "time"):
		return "invalid_timestamp"
	case strings.Contains(normalized, "retention"):
		return "outside_retention"
	case strings.Contains(normalized, "name"):
		return "missing_span_name"
	default:
		return "invalid_span"
	}
}
