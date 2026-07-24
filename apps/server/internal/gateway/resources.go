package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

type ContractError struct {
	Field   string
	Problem string
}

func (err *ContractError) Error() string {
	return fmt.Sprintf("agentgateway contract mismatch at %s: %s", err.Field, err.Problem)
}

type rawConfig struct {
	LLM *struct {
		Providers     []rawProvider     `json:"providers"`
		Models        []rawModel        `json:"models"`
		VirtualModels []rawVirtualModel `json:"virtualModels"`
	} `json:"llm"`
	MCP *struct {
		Targets []rawMCP `json:"targets"`
	} `json:"mcp"`
	Binds []rawBind `json:"binds"`
}

type rawProvider struct {
	Name     string          `json:"name"`
	Provider json.RawMessage `json:"provider"`
}

type rawModel struct {
	Name       string          `json:"name"`
	Provider   json.RawMessage `json:"provider"`
	Visibility string          `json:"visibility"`
	Params     struct {
		Model string `json:"model"`
	} `json:"params"`
}

type rawVirtualModel struct {
	Name    string `json:"name"`
	Routing struct {
		Weighted *struct {
			Targets []struct {
				Model string `json:"model"`
			} `json:"targets"`
		} `json:"weighted"`
		Failover *struct {
			Targets []struct {
				Model string `json:"model"`
			} `json:"targets"`
		} `json:"failover"`
		Conditional *struct {
			Targets []struct {
				Model string `json:"model"`
			} `json:"targets"`
		} `json:"conditional"`
	} `json:"routing"`
}

type rawMCP struct {
	Name    string          `json:"name"`
	SSE     json.RawMessage `json:"sse"`
	MCP     json.RawMessage `json:"mcp"`
	Stdio   json.RawMessage `json:"stdio"`
	OpenAPI json.RawMessage `json:"openapi"`
}

type rawBind struct {
	Port      int           `json:"port"`
	Listeners []rawListener `json:"listeners"`
}

type rawListener struct {
	Name      string     `json:"name"`
	Hostname  string     `json:"hostname"`
	Protocol  string     `json:"protocol"`
	Routes    []rawRoute `json:"routes"`
	TCPRoutes []rawRoute `json:"tcpRoutes"`
}

type rawRoute struct {
	Name      string                     `json:"name"`
	RuleName  string                     `json:"ruleName"`
	Hostnames []string                   `json:"hostnames"`
	Matches   []rawMatch                 `json:"matches"`
	Backends  []rawBackend               `json:"backends"`
	Policies  map[string]json.RawMessage `json:"policies"`
}

type rawMatch struct {
	Path *struct {
		Exact      string `json:"exact"`
		PathPrefix string `json:"pathPrefix"`
		Regex      string `json:"regex"`
	} `json:"path"`
}

type rawBackend struct {
	Host    json.RawMessage `json:"host"`
	Backend json.RawMessage `json:"backend"`
	Service *struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	} `json:"service"`
	RouteGroup json.RawMessage `json:"routeGroup"`
	AI         *struct {
		Name string `json:"name"`
	} `json:"ai"`
	MCP *struct {
		Targets []rawMCP `json:"targets"`
	} `json:"mcp"`
	Policies map[string]json.RawMessage `json:"policies"`
}

type rawAnalytics struct {
	BucketSeconds int64 `json:"bucketSeconds"`
	Buckets       []struct {
		Start       time.Time `json:"start"`
		Requests    int64     `json:"requests"`
		TotalTokens int64     `json:"totalTokens"`
		Cost        float64   `json:"cost"`
	} `json:"buckets"`
}

func (client *Client) Snapshot(ctx context.Context) (model.GatewaySnapshot, error) {
	var payload json.RawMessage
	if _, err := client.upstream.GetJSON(ctx, "/api/config", &payload); err != nil {
		return model.GatewaySnapshot{}, err
	}
	var config rawConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		field := "/api/config"
		var fieldError *json.UnmarshalTypeError
		if errors.As(err, &fieldError) && fieldError.Field != "" {
			field += "/" + strings.ReplaceAll(fieldError.Field, ".", "/")
		}
		return model.GatewaySnapshot{}, &ContractError{Field: field, Problem: "unexpected field type"}
	}
	fetchedAt := time.Now().UTC()
	snapshot := model.GatewaySnapshot{FetchedAt: fetchedAt}
	if config.LLM != nil {
		for index, item := range config.LLM.Providers {
			field := fmt.Sprintf("/llm/providers/%d", index)
			if item.Name == "" {
				return model.GatewaySnapshot{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
			}
			kind, _, err := providerKind(item.Provider, field+"/provider")
			if err != nil {
				return model.GatewaySnapshot{}, err
			}
			snapshot.Providers = append(snapshot.Providers, model.GatewayProvider{
				ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, Kind: kind,
			})
		}
		providerIndexes := make(map[string]int, len(snapshot.Providers))
		for index := range snapshot.Providers {
			providerIndexes[snapshot.Providers[index].Name] = index
		}
		for index, item := range config.LLM.Models {
			field := fmt.Sprintf("/llm/models/%d", index)
			if item.Name == "" {
				return model.GatewaySnapshot{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
			}
			kind, reference, err := providerKind(item.Provider, field+"/provider")
			if err != nil {
				return model.GatewaySnapshot{}, err
			}
			if reference != "" {
				if providerIndex, ok := providerIndexes[reference]; ok {
					snapshot.Providers[providerIndex].ModelCount++
				}
			}
			snapshot.Models = append(snapshot.Models, model.GatewayModel{
				ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, Kind: "direct",
				Provider: kind, TargetModel: item.Params.Model,
			})
		}
		for index, item := range config.LLM.VirtualModels {
			field := fmt.Sprintf("/llm/virtualModels/%d", index)
			if item.Name == "" {
				return model.GatewaySnapshot{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
			}
			routing, targets, err := virtualRouting(item, field+"/routing")
			if err != nil {
				return model.GatewaySnapshot{}, err
			}
			snapshot.Models = append(snapshot.Models, model.GatewayModel{
				ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, Kind: "virtual",
				Routing: routing, Targets: targets,
			})
		}
	}
	if config.MCP != nil {
		for index, item := range config.MCP.Targets {
			server, err := mcpResource(item, fmt.Sprintf("/mcp/targets/%d", index), "gateway", fetchedAt)
			if err != nil {
				return model.GatewaySnapshot{}, err
			}
			snapshot.MCP = append(snapshot.MCP, server)
		}
	}
	for bindIndex, bind := range config.Binds {
		for listenerIndex, listener := range bind.Listeners {
			snapshot.Listeners++
			for routeIndex, route := range listener.Routes {
				field := fmt.Sprintf("/binds/%d/listeners/%d/routes/%d", bindIndex, listenerIndex, routeIndex)
				snapshot.Policies = append(snapshot.Policies, policySummaries(route.Policies, field+"/policies", field, fetchedAt)...)
				for backendIndex, backend := range route.Backends {
					backendField := fmt.Sprintf("%s/backends/%d", field, backendIndex)
					snapshot.Policies = append(snapshot.Policies, policySummaries(backend.Policies, backendField+"/policies", backendField, fetchedAt)...)
				}
				item, inlineMCP, err := routeResource(route, listener, bind.Port, field, false, fetchedAt)
				if err != nil {
					return model.GatewaySnapshot{}, err
				}
				snapshot.Backends += len(route.Backends)
				snapshot.Routes = append(snapshot.Routes, item)
				snapshot.MCP = append(snapshot.MCP, inlineMCP...)
			}
			for routeIndex, route := range listener.TCPRoutes {
				field := fmt.Sprintf("/binds/%d/listeners/%d/tcpRoutes/%d", bindIndex, listenerIndex, routeIndex)
				snapshot.Policies = append(snapshot.Policies, policySummaries(route.Policies, field+"/policies", field, fetchedAt)...)
				for backendIndex, backend := range route.Backends {
					backendField := fmt.Sprintf("%s/backends/%d", field, backendIndex)
					snapshot.Policies = append(snapshot.Policies, policySummaries(backend.Policies, backendField+"/policies", backendField, fetchedAt)...)
				}
				item, inlineMCP, err := routeResource(route, listener, bind.Port, field, true, fetchedAt)
				if err != nil {
					return model.GatewaySnapshot{}, err
				}
				snapshot.Backends += len(route.Backends)
				snapshot.Routes = append(snapshot.Routes, item)
				snapshot.MCP = append(snapshot.MCP, inlineMCP...)
			}
		}
	}
	return snapshot, nil
}

func (client *Client) Analytics(ctx context.Context) (model.GatewayAnalytics, error) {
	return client.AnalyticsWindow(ctx, model.CurrentTrendWindow(time.Now()))
}

func (client *Client) AnalyticsWindow(ctx context.Context, window model.TrendWindow) (model.GatewayAnalytics, error) {
	bucketSeconds := int64(window.BucketDuration / time.Second)
	var payload json.RawMessage
	_, err := client.upstream.PostJSON(ctx, "/api/logs/analytics/summary", struct {
		TimeRange     logTimeRange `json:"timeRange"`
		BucketSeconds int64        `json:"bucketSeconds"`
	}{
		TimeRange:     logTimeRange{From: window.From.UTC(), To: window.To.UTC()},
		BucketSeconds: bucketSeconds,
	}, &payload)
	if err != nil {
		var upstreamError *upstream.Error
		if errors.As(err, &upstreamError) && (upstreamError.Status == 404 || upstreamError.Status == 405 || upstreamError.Status >= 500) {
			reason := "request-log analytics storage is not configured or unavailable"
			if upstreamError.Status == 404 || upstreamError.Status == 405 {
				reason = "request-log analytics capability is unavailable"
			}
			return model.GatewayAnalytics{
				Status: "unavailable", Reason: reason, Buckets: []model.AnalyticsBucket{},
			}, nil
		}
		return model.GatewayAnalytics{}, err
	}
	var response rawAnalytics
	if err := json.Unmarshal(payload, &response); err != nil {
		field := "/api/logs/analytics/summary"
		var fieldError *json.UnmarshalTypeError
		if errors.As(err, &fieldError) && fieldError.Field != "" {
			field += "/" + strings.ReplaceAll(fieldError.Field, ".", "/")
		}
		return model.GatewayAnalytics{}, &ContractError{Field: field, Problem: "unexpected field type"}
	}
	if response.BucketSeconds <= 0 {
		return model.GatewayAnalytics{}, &ContractError{Field: "/api/logs/analytics/summary/bucketSeconds", Problem: "required positive field is missing"}
	}
	if response.BucketSeconds != bucketSeconds {
		return model.GatewayAnalytics{}, &ContractError{Field: "/api/logs/analytics/summary/bucketSeconds", Problem: "response does not match the requested bucket size"}
	}
	if response.Buckets == nil {
		return model.GatewayAnalytics{}, &ContractError{Field: "/api/logs/analytics/summary/buckets", Problem: "required array is missing"}
	}
	analytics := model.GatewayAnalytics{Status: "available", Buckets: make([]model.AnalyticsBucket, 0, len(response.Buckets))}
	requests := int64(0)
	tokens := int64(0)
	cost := float64(0)
	seenStarts := make(map[time.Time]struct{}, len(response.Buckets))
	for index, bucket := range response.Buckets {
		if bucket.Start.IsZero() {
			return model.GatewayAnalytics{}, &ContractError{Field: fmt.Sprintf("/api/logs/analytics/summary/buckets/%d/start", index), Problem: "required timestamp is missing"}
		}
		start := bucket.Start.UTC()
		if start.Before(window.From) || !start.Before(window.To) {
			return model.GatewayAnalytics{}, &ContractError{Field: fmt.Sprintf("/api/logs/analytics/summary/buckets/%d/start", index), Problem: "bucket is outside the requested time range"}
		}
		if _, exists := seenStarts[start]; exists {
			return model.GatewayAnalytics{}, &ContractError{Field: fmt.Sprintf("/api/logs/analytics/summary/buckets/%d/start", index), Problem: "duplicate bucket start"}
		}
		seenStarts[start] = struct{}{}
		requests += bucket.Requests
		tokens += bucket.TotalTokens
		cost += bucket.Cost
		analytics.Buckets = append(analytics.Buckets, model.AnalyticsBucket{
			Start: start, Requests: bucket.Requests, TotalTokens: bucket.TotalTokens, Cost: bucket.Cost,
		})
	}
	analytics.Requests = &requests
	analytics.TotalTokens = &tokens
	analytics.Cost = &cost
	analytics.BucketSeconds = &response.BucketSeconds
	return analytics, nil
}

func providerKind(raw json.RawMessage, field string) (string, string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", &ContractError{Field: field, Problem: "required field is missing"}
	}
	var kind string
	if json.Unmarshal(raw, &kind) == nil && kind != "" {
		return kind, "", nil
	}
	var reference struct {
		Reference string `json:"reference"`
	}
	if json.Unmarshal(raw, &reference) == nil && reference.Reference != "" {
		return "reference:" + reference.Reference, reference.Reference, nil
	}
	return "", "", &ContractError{Field: field, Problem: "expected provider name or reference"}
}

func virtualRouting(item rawVirtualModel, field string) (string, []string, error) {
	type routing struct {
		name    string
		targets []string
	}
	var choices []routing
	if item.Routing.Weighted != nil {
		values := make([]string, 0, len(item.Routing.Weighted.Targets))
		for _, target := range item.Routing.Weighted.Targets {
			values = append(values, target.Model)
		}
		choices = append(choices, routing{"weighted", values})
	}
	if item.Routing.Failover != nil {
		values := make([]string, 0, len(item.Routing.Failover.Targets))
		for _, target := range item.Routing.Failover.Targets {
			values = append(values, target.Model)
		}
		choices = append(choices, routing{"failover", values})
	}
	if item.Routing.Conditional != nil {
		values := make([]string, 0, len(item.Routing.Conditional.Targets))
		for _, target := range item.Routing.Conditional.Targets {
			values = append(values, target.Model)
		}
		choices = append(choices, routing{"conditional", values})
	}
	if len(choices) != 1 {
		return "", nil, &ContractError{Field: field, Problem: "expected exactly one verified routing strategy"}
	}
	return choices[0].name, choices[0].targets, nil
}

func mcpResource(item rawMCP, field, scope string, fetchedAt time.Time) (model.GatewayMCPServer, error) {
	if item.Name == "" {
		return model.GatewayMCPServer{}, &ContractError{Field: field + "/name", Problem: "required field is missing"}
	}
	var transports []string
	for name, value := range map[string]json.RawMessage{"sse": item.SSE, "mcp": item.MCP, "stdio": item.Stdio, "openapi": item.OpenAPI} {
		if len(value) > 0 && string(value) != "null" {
			transports = append(transports, name)
		}
	}
	if len(transports) != 1 {
		return model.GatewayMCPServer{}, &ContractError{Field: field, Problem: "expected exactly one verified transport"}
	}
	return model.GatewayMCPServer{
		ConnectResource: resource(field, item.Name, fetchedAt), Name: item.Name, Transport: transports[0], Scope: scope,
	}, nil
}

func routeResource(route rawRoute, listener rawListener, port int, field string, tcp bool, fetchedAt time.Time) (model.GatewayRoute, []model.GatewayMCPServer, error) {
	name := route.Name
	if name == "" {
		name = route.RuleName
	}
	protocol := listener.Protocol
	if protocol == "" {
		protocol = "HTTP"
	}
	if tcp {
		protocol = "TCP"
	}
	hostnames := append([]string{}, route.Hostnames...)
	if len(hostnames) == 0 && listener.Hostname != "" {
		hostnames = []string{listener.Hostname}
	}
	path := ""
	if !tcp {
		path = "/"
		if len(route.Matches) > 0 && route.Matches[0].Path != nil {
			pathMatch := route.Matches[0].Path
			switch {
			case pathMatch.Exact != "":
				path = pathMatch.Exact
			case pathMatch.PathPrefix != "":
				path = pathMatch.PathPrefix
			case pathMatch.Regex != "":
				path = pathMatch.Regex
			}
		}
	}
	targets := make([]string, 0, len(route.Backends))
	var inlineMCP []model.GatewayMCPServer
	unavailableBackends := 0
	for backendIndex, backend := range route.Backends {
		backendField := fmt.Sprintf("%s/backends/%d", field, backendIndex)
		if target := rawString(backend.Host); target != "" {
			targets = append(targets, safeTarget(target))
		} else if target := rawString(backend.Backend); target != "" {
			targets = append(targets, "backend:"+target)
		} else if backend.Service != nil && backend.Service.Name != "" {
			targets = append(targets, fmt.Sprintf("service:%s:%d", backend.Service.Name, backend.Service.Port))
		} else if target := rawString(backend.RouteGroup); target != "" {
			targets = append(targets, "route-group:"+target)
		} else if backend.AI != nil {
			if backend.AI.Name != "" {
				targets = append(targets, "ai:"+backend.AI.Name)
			} else {
				targets = append(targets, "ai")
			}
		} else if backend.MCP != nil {
			for targetIndex, item := range backend.MCP.Targets {
				mcpField := fmt.Sprintf("%s/mcp/targets/%d", backendField, targetIndex)
				server, err := mcpResource(item, mcpField, field, fetchedAt)
				if err != nil {
					return model.GatewayRoute{}, nil, err
				}
				inlineMCP = append(inlineMCP, server)
				targets = append(targets, "mcp:"+item.Name)
			}
		} else {
			unavailableBackends++
		}
	}
	listenerName := listener.Name
	if listenerName == "" {
		listenerName = "(unnamed)"
	}
	displayName := name
	if displayName == "" {
		displayName = "(unnamed)"
	}
	return model.GatewayRoute{
		ConnectResource: resource(field, name, fetchedAt), Name: displayName, Listener: listenerName,
		Protocol: protocol, Port: port, Hostnames: hostnames, Path: path, Targets: targets,
		BackendCount: len(route.Backends), UnavailableBackendCount: unavailableBackends,
	}, inlineMCP, nil
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func policySummaries(policies map[string]json.RawMessage, field, scope string, fetchedAt time.Time) []model.ProtectPolicy {
	keys := make([]string, 0, len(policies))
	for key := range policies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]model.ProtectPolicy, 0, len(keys))
	for _, key := range keys {
		if key == "ai" || key == "llm" {
			var nested map[string]json.RawMessage
			if json.Unmarshal(policies[key], &nested) == nil && len(nested) > 0 {
				nestedKeys := make([]string, 0, len(nested))
				for nestedKey := range nested {
					nestedKeys = append(nestedKeys, nestedKey)
				}
				sort.Strings(nestedKeys)
				for _, nestedKey := range nestedKeys {
					items = append(items, policySummary(key+"."+nestedKey, nested[nestedKey], field+"/"+key+"/"+nestedKey, scope, fetchedAt))
				}
				continue
			}
		}
		items = append(items, policySummary(key, policies[key], field+"/"+key, scope, fetchedAt))
	}
	return items
}

func policySummary(name string, raw json.RawMessage, field, scope string, fetchedAt time.Time) model.ProtectPolicy {
	policyType := "Gateway Policy"
	phase := "unknown"
	normalized := strings.ToLower(name)
	if strings.Contains(normalized, "guardrail") || strings.Contains(normalized, "promptguard") {
		policyType = "Content Guardrail"
		var phases struct {
			Request  json.RawMessage `json:"request"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(raw, &phases) == nil {
			values := make([]string, 0, 2)
			if len(phases.Request) > 0 && string(phases.Request) != "null" {
				values = append(values, "Request")
			}
			if len(phases.Response) > 0 && string(phases.Response) != "null" {
				values = append(values, "Response")
			}
			if len(values) > 0 {
				phase = strings.Join(values, " + ")
			}
		}
	}
	return model.ProtectPolicy{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: base64.RawURLEncoding.EncodeToString([]byte(field)), UpstreamID: field,
			Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: field},
		},
		Name: name, Type: policyType, Scope: scope, Phase: phase, Action: "Configured", Status: "read-only",
	}
}

func safeTarget(target string) string {
	if strings.Contains(target, "@") {
		return "[redacted endpoint]"
	}
	return target
}

func resource(rawID, upstreamID string, fetchedAt time.Time) model.ConnectResource {
	return model.ConnectResource{
		ID:         base64.RawURLEncoding.EncodeToString([]byte(rawID)),
		UpstreamID: upstreamID,
		Source:     model.SourceAgentGateway,
		FetchedAt:  fetchedAt,
		RawRef:     model.RawRef{Source: model.SourceAgentGateway, ID: rawID},
	}
}
