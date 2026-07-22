// Package connect implements the read-only agentgateway resource BFF.
package connect

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const (
	defaultLimit = 25
	maxLimit     = 100
)

var (
	ErrInvalidCursor = errors.New("invalid pagination cursor")
	ErrNotFound      = errors.New("resource not found")
)

type Gateway interface {
	Health(context.Context) model.SourceHealth
	Snapshot(context.Context) (model.GatewaySnapshot, error)
	Analytics(context.Context) (model.GatewayAnalytics, error)
}

type Service struct {
	gateway Gateway
	links   model.ConsoleLinks
}

func New(gateway Gateway, consoleURL string) *Service {
	return &Service{gateway: gateway, links: consoleLinks(consoleURL)}
}

func (service *Service) Summary(ctx context.Context) (model.ConnectSummaryEnvelope, error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ConnectSummaryEnvelope{}, err
	}
	analytics, err := service.gateway.Analytics(ctx)
	if err != nil {
		return model.ConnectSummaryEnvelope{}, err
	}
	health := service.gateway.Health(ctx)
	counts := []model.ConnectCount{
		count("listeners", "Listeners", snapshot.Listeners),
		count("routes", "Routes", len(snapshot.Routes)),
		count("backends", "Backends", snapshot.Backends),
		count("mcp-targets", "MCP targets", len(snapshot.MCP)),
	}
	return model.ConnectSummaryEnvelope{
		Data: model.ConnectSummary{Health: health, Counts: counts, Analytics: analytics, Links: service.links},
		Meta: gatewayMeta(snapshot.FetchedAt, analytics.Status == "unavailable"),
	}, nil
}

func (service *Service) Providers(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayProvider], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayProvider]{}, err
	}
	items := filter(snapshot.Providers, query, func(item model.GatewayProvider) string {
		return item.Name + " " + item.Kind
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayProvider]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) Models(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayModel], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayModel]{}, err
	}
	items := filter(snapshot.Models, query, func(item model.GatewayModel) string {
		return item.Name + " " + item.Provider + " " + item.Routing + " " + strings.Join(item.Targets, " ")
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayModel]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) MCPServers(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayMCPServer], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayMCPServer]{}, err
	}
	items := filter(snapshot.MCP, query, func(item model.GatewayMCPServer) string {
		return item.Name + " " + item.Transport + " " + item.Scope
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayMCPServer]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) Routes(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayRoute], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayRoute]{}, err
	}
	items := filter(snapshot.Routes, query, func(item model.GatewayRoute) string {
		return item.Name + " " + item.Listener + " " + item.Protocol + " " + strings.Join(item.Hostnames, " ") + " " + strings.Join(item.Targets, " ")
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayRoute]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) Provider(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayProvider], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayProvider]{}, err
	}
	return detail(snapshot.Providers, id, snapshot.FetchedAt, func(item model.GatewayProvider) string { return item.ID })
}

func (service *Service) Model(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayModel], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayModel]{}, err
	}
	return detail(snapshot.Models, id, snapshot.FetchedAt, func(item model.GatewayModel) string { return item.ID })
}

func (service *Service) MCPServer(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayMCPServer], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayMCPServer]{}, err
	}
	return detail(snapshot.MCP, id, snapshot.FetchedAt, func(item model.GatewayMCPServer) string { return item.ID })
}

func (service *Service) Route(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayRoute], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayRoute]{}, err
	}
	return detail(snapshot.Routes, id, snapshot.FetchedAt, func(item model.GatewayRoute) string { return item.ID })
}

func (service *Service) Analytics(ctx context.Context) (model.ResourceEnvelope[model.GatewayAnalytics], error) {
	analytics, err := service.gateway.Analytics(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayAnalytics]{}, err
	}
	return model.ResourceEnvelope[model.GatewayAnalytics]{
		Data: analytics, Meta: gatewayMeta(service.gateway.Health(ctx).CheckedAt, analytics.Status == "unavailable"),
	}, nil
}

func (service *Service) Setup(ctx context.Context) model.ResourceEnvelope[model.ConnectSetup] {
	health := service.gateway.Health(ctx)
	snapshot, err := service.gateway.Snapshot(ctx)
	readable := err == nil
	status := health.Status
	message := health.Message
	if !readable {
		if status == model.HealthHealthy {
			status = model.HealthDegraded
		}
		message = "management runtime responded but configuration could not be read"
	}
	fetchedAt := health.CheckedAt
	if readable {
		fetchedAt = snapshot.FetchedAt
	}
	return model.ResourceEnvelope[model.ConnectSetup]{
		Data: model.ConnectSetup{
			Source: model.SourceAgentGateway, ManagementConfigured: true, ConfigurationReadable: readable,
			Status: status, Version: health.Version, LatencyMS: health.LatencyMS, CheckedAt: health.CheckedAt,
			Message: message, Links: service.links,
		},
		Meta: gatewayMeta(fetchedAt, !readable || status != model.HealthHealthy),
	}
}

func count(id, label string, value int) model.ConnectCount {
	return model.ConnectCount{ID: id, Label: label, Value: &value, Status: "configured"}
}

func gatewayMeta(fetchedAt time.Time, partial bool) model.Meta {
	return model.Meta{Source: model.SourceAgentGateway, FetchedAt: fetchedAt, Stale: false, Partial: partial}
}

func detail[T any](items []T, id string, fetchedAt time.Time, idOf func(T) string) (model.ResourceEnvelope[T], error) {
	for _, item := range items {
		if idOf(item) == id {
			return model.ResourceEnvelope[T]{Data: item, Meta: gatewayMeta(fetchedAt, false)}, nil
		}
	}
	return model.ResourceEnvelope[T]{}, ErrNotFound
}

func filter[T any](items []T, query string, text func(T) string) []T {
	normalized := strings.ToLower(strings.TrimSpace(query))
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if normalized == "" || strings.Contains(strings.ToLower(text(item)), normalized) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func paginate[T any](items []T, cursor string, limit int) (model.ResourcePage[T], error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return model.ResourcePage[T]{}, fmt.Errorf("limit must be between 1 and %d", maxLimit)
	}
	offset, err := decodeCursor(cursor)
	if err != nil || offset > len(items) {
		return model.ResourcePage[T]{}, ErrInvalidCursor
	}
	end := min(offset+limit, len(items))
	page := model.ResourcePage[T]{Items: items[offset:end], Total: len(items)}
	if end < len(items) {
		next := encodeCursor(end)
		page.NextCursor = &next
	}
	return page, nil
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, ErrInvalidCursor
	}
	return offset, nil
}

func consoleLinks(raw string) model.ConsoleLinks {
	if raw == "" {
		return model.ConsoleLinks{}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return model.ConsoleLinks{}
	}
	base := strings.TrimRight(parsed.String(), "/")
	return model.ConsoleLinks{
		Console: base, RawConfig: base + "/raw-config", CEL: base + "/cel",
		LLMPlayground: base + "/llm/playground", MCPPlayground: base + "/mcp/playground",
	}
}
