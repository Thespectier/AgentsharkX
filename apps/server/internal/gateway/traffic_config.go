package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

var (
	ErrTrafficInvalidRequest     = errors.New("invalid traffic configuration request")
	ErrTrafficResourceNotFound   = errors.New("traffic configuration resource not found")
	ErrTrafficResourceConflict   = errors.New("traffic configuration resource conflict")
	ErrTrafficResourceReferenced = errors.New("traffic configuration resource has children")
	ErrTrafficWriteUnverified    = errors.New("agentgateway traffic configuration write could not be verified")
)

const (
	changeCreateTrafficBind     = "create-traffic-bind"
	changeUpdateTrafficBind     = "update-traffic-bind"
	changeDeleteTrafficBind     = "delete-traffic-bind"
	changeCreateTrafficListener = "create-traffic-listener"
	changeUpdateTrafficListener = "update-traffic-listener"
	changeDeleteTrafficListener = "delete-traffic-listener"
	changeCreateTrafficRoute    = "create-traffic-route"
	changeUpdateTrafficRoute    = "update-traffic-route"
	changeDeleteTrafficRoute    = "delete-traffic-route"
)

type trafficConfigDocument struct {
	root  map[string]json.RawMessage
	binds []json.RawMessage
}

type trafficListenerLocation struct {
	bindIndex     int
	listenerIndex int
	bind          map[string]json.RawMessage
	listeners     []json.RawMessage
	listener      map[string]json.RawMessage
}

type trafficRouteLocation struct {
	trafficListenerLocation
	kind       string
	routeIndex int
	routes     []json.RawMessage
	route      map[string]json.RawMessage
}

func (client *Client) TrafficConfiguration(ctx context.Context) (model.TrafficConfiguration, string, error) {
	document, revision, err := client.readTrafficDocument(ctx)
	if err != nil {
		return model.TrafficConfiguration{}, "", err
	}
	configuration, err := document.configuration(time.Now().UTC())
	return configuration, revision, err
}

func (client *Client) ApplyTrafficChange(ctx context.Context, expectedRevision string, change model.TrafficChange) (string, error) {
	document, revision, err := client.readTrafficDocument(ctx)
	if err != nil {
		return "", err
	}
	if revision != expectedRevision {
		return "", ErrConfigurationChanged
	}
	target, err := document.changeTarget(change)
	if err != nil {
		return "", err
	}
	if err := document.apply(change); err != nil {
		return "", err
	}
	expected, err := document.marshal()
	if err != nil {
		return "", &ContractError{Field: "/api/config", Problem: "configuration could not be encoded"}
	}
	var response struct {
		Status string `json:"status"`
	}
	if _, err := client.upstream.PostMutationJSON(ctx, "/api/config", json.RawMessage(expected), &response); err != nil {
		return "", err
	}
	if response.Status != "success" {
		return "", &ContractError{Field: "/api/config/status", Problem: "write success was not confirmed"}
	}
	actual, _, err := client.readTrafficDocument(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTrafficWriteUnverified, err)
	}
	actualPayload, err := actual.marshal()
	if err != nil || !bytes.Equal(expected, actualPayload) {
		return "", ErrTrafficWriteUnverified
	}
	return target, nil
}

func (client *Client) readTrafficDocument(ctx context.Context) (*trafficConfigDocument, string, error) {
	var payload json.RawMessage
	if _, err := client.upstream.GetJSON(ctx, "/api/config", &payload); err != nil {
		return nil, "", err
	}
	document, err := decodeTrafficDocument(payload)
	if err != nil {
		return nil, "", err
	}
	canonical, err := document.marshal()
	if err != nil {
		return nil, "", &ContractError{Field: "/api/config", Problem: "configuration could not be normalized"}
	}
	digest := sha256.Sum256(canonical)
	return document, hex.EncodeToString(digest[:]), nil
}

func decodeTrafficDocument(payload json.RawMessage) (*trafficConfigDocument, error) {
	root := make(map[string]json.RawMessage)
	if err := json.Unmarshal(payload, &root); err != nil || root == nil {
		return nil, &ContractError{Field: "/api/config", Problem: "expected configuration object"}
	}
	document := &trafficConfigDocument{root: root}
	if err := decodeArray(root["binds"], "/binds", &document.binds); err != nil {
		return nil, err
	}
	return document, nil
}

func (document *trafficConfigDocument) marshal() ([]byte, error) {
	binds, err := json.Marshal(document.binds)
	if err != nil {
		return nil, err
	}
	document.root["binds"] = binds
	return json.Marshal(document.root)
}

func (document *trafficConfigDocument) configuration(fetchedAt time.Time) (model.TrafficConfiguration, error) {
	configuration := model.TrafficConfiguration{
		Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
		Binds: []model.TrafficBindSetting{}, Listeners: []model.TrafficListenerSetting{}, Routes: []model.TrafficRouteSetting{},
	}
	for bindIndex, rawBind := range document.binds {
		bindField := fmt.Sprintf("/binds/%d", bindIndex)
		bind, listeners, port, err := decodeTrafficBind(rawBind, bindField)
		if err != nil {
			return model.TrafficConfiguration{}, err
		}
		bindResource := resource(bindField, fmt.Sprintf("%d", port), fetchedAt)
		bindSetting := model.TrafficBindSetting{
			ConnectResource: bindResource, Port: port, TunnelProtocol: optionalString(bind["tunnelProtocol"], "direct"),
			ListenerCount: len(listeners),
		}
		for listenerIndex, rawListener := range listeners {
			listenerField := fmt.Sprintf("%s/listeners/%d", bindField, listenerIndex)
			listener, httpRoutes, tcpRoutes, protocol, err := decodeTrafficListener(rawListener, listenerField)
			if err != nil {
				return model.TrafficConfiguration{}, err
			}
			name := optionalString(listener["name"], "")
			displayName := name
			if displayName == "" {
				displayName = fmt.Sprintf("Listener %d", listenerIndex+1)
			}
			listenerResource := resource(listenerField, name, fetchedAt)
			listenerConfiguration, err := trafficConfigObject(rawListener, listenerField)
			if err != nil {
				return model.TrafficConfiguration{}, err
			}
			listenerSetting := model.TrafficListenerSetting{
				ConnectResource: listenerResource, BindID: bindResource.ID, Port: port, Name: displayName,
				Hostname: optionalString(listener["hostname"], ""), Protocol: protocol,
				RouteCount: len(httpRoutes) + len(tcpRoutes), Configuration: listenerConfiguration,
			}
			groups := []struct {
				kind   string
				key    string
				routes []json.RawMessage
			}{{"http", "routes", httpRoutes}, {"tcp", "tcpRoutes", tcpRoutes}}
			for _, group := range groups {
				for routeIndex, rawRoute := range group.routes {
					routeField := fmt.Sprintf("%s/%s/%d", listenerField, group.key, routeIndex)
					routeSetting, err := decodeTrafficRoute(rawRoute, routeField, group.kind, listenerResource.ID, displayName, port, fetchedAt)
					if err != nil {
						return model.TrafficConfiguration{}, err
					}
					listenerSetting.BackendCount += routeSetting.BackendCount
					configuration.Routes = append(configuration.Routes, routeSetting)
				}
			}
			bindSetting.RouteCount += listenerSetting.RouteCount
			bindSetting.BackendCount += listenerSetting.BackendCount
			configuration.Listeners = append(configuration.Listeners, listenerSetting)
		}
		configuration.Binds = append(configuration.Binds, bindSetting)
	}
	return configuration, nil
}

func decodeTrafficBind(raw json.RawMessage, field string) (map[string]json.RawMessage, []json.RawMessage, int, error) {
	bind, err := trafficRawObject(raw, field)
	if err != nil {
		return nil, nil, 0, err
	}
	var port int
	if json.Unmarshal(bind["port"], &port) != nil || port < 1 || port > 65535 {
		return nil, nil, 0, &ContractError{Field: field + "/port", Problem: "expected port between 1 and 65535"}
	}
	var listeners []json.RawMessage
	if err := decodeArray(bind["listeners"], field+"/listeners", &listeners); err != nil {
		return nil, nil, 0, err
	}
	return bind, listeners, port, nil
}

func decodeTrafficListener(raw json.RawMessage, field string) (map[string]json.RawMessage, []json.RawMessage, []json.RawMessage, string, error) {
	listener, err := trafficRawObject(raw, field)
	if err != nil {
		return nil, nil, nil, "", err
	}
	protocol := optionalString(listener["protocol"], "HTTP")
	if !validTrafficProtocol(protocol) {
		return nil, nil, nil, "", &ContractError{Field: field + "/protocol", Problem: "expected verified listener protocol"}
	}
	var routes, tcpRoutes []json.RawMessage
	if err := decodeArray(listener["routes"], field+"/routes", &routes); err != nil {
		return nil, nil, nil, "", err
	}
	if err := decodeArray(listener["tcpRoutes"], field+"/tcpRoutes", &tcpRoutes); err != nil {
		return nil, nil, nil, "", err
	}
	return listener, routes, tcpRoutes, protocol, nil
}

func decodeTrafficRoute(raw json.RawMessage, field, kind, listenerID, listenerName string, port int, fetchedAt time.Time) (model.TrafficRouteSetting, error) {
	route, err := trafficRawObject(raw, field)
	if err != nil {
		return model.TrafficRouteSetting{}, err
	}
	name := optionalString(route["name"], optionalString(route["ruleName"], ""))
	displayName := name
	if displayName == "" {
		displayName = "(unnamed)"
	}
	hostnames := []string{}
	if rawHostnames := route["hostnames"]; len(rawHostnames) > 0 && string(rawHostnames) != "null" {
		if json.Unmarshal(rawHostnames, &hostnames) != nil || hostnames == nil {
			return model.TrafficRouteSetting{}, &ContractError{Field: field + "/hostnames", Problem: "expected string array"}
		}
	}
	var backends []json.RawMessage
	if err := decodeArray(route["backends"], field+"/backends", &backends); err != nil {
		return model.TrafficRouteSetting{}, err
	}
	configuration, err := trafficConfigObject(raw, field)
	if err != nil {
		return model.TrafficRouteSetting{}, err
	}
	return model.TrafficRouteSetting{
		ConnectResource: resource(field, name, fetchedAt), ListenerID: listenerID, Listener: listenerName,
		Port: port, Kind: kind, Name: displayName, Hostnames: hostnames, BackendCount: len(backends), Configuration: configuration,
	}, nil
}

func trafficRawObject(raw json.RawMessage, field string) (map[string]json.RawMessage, error) {
	value := make(map[string]json.RawMessage)
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, &ContractError{Field: field, Problem: "expected object"}
	}
	return value, nil
}

func trafficConfigObject(raw json.RawMessage, field string) (model.TrafficConfigObject, error) {
	value := make(model.TrafficConfigObject)
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, &ContractError{Field: field, Problem: "expected object"}
	}
	return value, nil
}

func optionalString(raw json.RawMessage, fallback string) string {
	var value string
	if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &value) == nil {
		return value
	}
	return fallback
}

func validTrafficProtocol(protocol string) bool {
	switch protocol {
	case "HTTP", "HTTPS", "TLS", "TCP", "HBONE":
		return true
	default:
		return false
	}
}

func listenerRouteKind(protocol string) string {
	if protocol == "TCP" || protocol == "TLS" {
		return "tcp"
	}
	return "http"
}

func (document *trafficConfigDocument) changeTarget(change model.TrafficChange) (string, error) {
	switch change.Operation {
	case changeCreateTrafficBind, changeUpdateTrafficBind:
		return fmt.Sprintf("Port %d", change.Bind.Port), nil
	case changeCreateTrafficListener, changeUpdateTrafficListener:
		return configDisplayName(change.Listener.Configuration, "Listener"), nil
	case changeCreateTrafficRoute, changeUpdateTrafficRoute:
		return configDisplayName(change.Route.Configuration, "Route"), nil
	case changeDeleteTrafficBind:
		_, _, port, err := document.bindByID(change.ResourceID)
		return fmt.Sprintf("Port %d", port), err
	case changeDeleteTrafficListener:
		location, err := document.listenerByID(change.ResourceID)
		if err != nil {
			return "", err
		}
		return rawDisplayName(location.listener, "Listener"), nil
	case changeDeleteTrafficRoute:
		location, err := document.routeByID(change.ResourceID)
		if err != nil {
			return "", err
		}
		return rawDisplayName(location.route, "Route"), nil
	default:
		return "", ErrTrafficInvalidRequest
	}
}

func (document *trafficConfigDocument) apply(change model.TrafficChange) error {
	switch change.Operation {
	case changeCreateTrafficBind:
		if !validTrafficPort(change.Bind.Port) {
			return ErrTrafficInvalidRequest
		}
		if document.portExists(change.Bind.Port, -1) {
			return ErrTrafficResourceConflict
		}
		raw, _ := json.Marshal(map[string]any{"port": change.Bind.Port, "listeners": []any{}})
		document.binds = append(document.binds, raw)
		return nil
	case changeUpdateTrafficBind:
		if !validTrafficPort(change.Bind.Port) {
			return ErrTrafficInvalidRequest
		}
		index, bind, _, err := document.bindByID(change.ResourceID)
		if err != nil {
			return err
		}
		if document.portExists(change.Bind.Port, index) {
			return ErrTrafficResourceConflict
		}
		bind["port"], _ = json.Marshal(change.Bind.Port)
		document.binds[index], _ = json.Marshal(bind)
		return nil
	case changeDeleteTrafficBind:
		index, _, _, err := document.bindByID(change.ResourceID)
		if err != nil {
			return err
		}
		_, listeners, _, err := decodeTrafficBind(document.binds[index], fmt.Sprintf("/binds/%d", index))
		if err != nil {
			return err
		}
		if len(listeners) > 0 && !change.DeleteChildren {
			return ErrTrafficResourceReferenced
		}
		document.binds = append(document.binds[:index], document.binds[index+1:]...)
		return nil
	case changeCreateTrafficListener:
		return document.createListener(change.BindID, change.Listener)
	case changeUpdateTrafficListener:
		return document.updateListener(change.ResourceID, change.Listener)
	case changeDeleteTrafficListener:
		return document.deleteListener(change.ResourceID, change.DeleteChildren)
	case changeCreateTrafficRoute:
		return document.createRoute(change.ListenerID, change.Route)
	case changeUpdateTrafficRoute:
		return document.updateRoute(change.ResourceID, change.Route)
	case changeDeleteTrafficRoute:
		return document.deleteRoute(change.ResourceID)
	default:
		return ErrTrafficInvalidRequest
	}
}

func validTrafficPort(port int) bool { return port >= 1 && port <= 65535 }

func (document *trafficConfigDocument) portExists(port, except int) bool {
	for index, raw := range document.binds {
		if index == except {
			continue
		}
		_, _, candidate, err := decodeTrafficBind(raw, fmt.Sprintf("/binds/%d", index))
		if err == nil && candidate == port {
			return true
		}
	}
	return false
}

func (document *trafficConfigDocument) bindByID(id string) (int, map[string]json.RawMessage, int, error) {
	for index, raw := range document.binds {
		field := fmt.Sprintf("/binds/%d", index)
		bind, _, port, err := decodeTrafficBind(raw, field)
		if err != nil {
			return -1, nil, 0, err
		}
		if resource(field, "", time.Time{}).ID == id {
			return index, bind, port, nil
		}
	}
	return -1, nil, 0, ErrTrafficResourceNotFound
}

func (document *trafficConfigDocument) listenerByID(id string) (trafficListenerLocation, error) {
	for bindIndex, rawBind := range document.binds {
		bindField := fmt.Sprintf("/binds/%d", bindIndex)
		bind, listeners, _, err := decodeTrafficBind(rawBind, bindField)
		if err != nil {
			return trafficListenerLocation{}, err
		}
		for listenerIndex, rawListener := range listeners {
			field := fmt.Sprintf("%s/listeners/%d", bindField, listenerIndex)
			if resource(field, "", time.Time{}).ID != id {
				continue
			}
			listener, _, _, _, err := decodeTrafficListener(rawListener, field)
			if err != nil {
				return trafficListenerLocation{}, err
			}
			return trafficListenerLocation{bindIndex, listenerIndex, bind, listeners, listener}, nil
		}
	}
	return trafficListenerLocation{}, ErrTrafficResourceNotFound
}

func (document *trafficConfigDocument) routeByID(id string) (trafficRouteLocation, error) {
	for bindIndex, rawBind := range document.binds {
		bindField := fmt.Sprintf("/binds/%d", bindIndex)
		bind, listeners, _, err := decodeTrafficBind(rawBind, bindField)
		if err != nil {
			return trafficRouteLocation{}, err
		}
		for listenerIndex, rawListener := range listeners {
			listenerField := fmt.Sprintf("%s/listeners/%d", bindField, listenerIndex)
			listener, routes, tcpRoutes, _, err := decodeTrafficListener(rawListener, listenerField)
			if err != nil {
				return trafficRouteLocation{}, err
			}
			groups := []struct {
				kind, key string
				routes    []json.RawMessage
			}{{"http", "routes", routes}, {"tcp", "tcpRoutes", tcpRoutes}}
			for _, group := range groups {
				for routeIndex, rawRoute := range group.routes {
					field := fmt.Sprintf("%s/%s/%d", listenerField, group.key, routeIndex)
					if resource(field, "", time.Time{}).ID != id {
						continue
					}
					route, err := trafficRawObject(rawRoute, field)
					if err != nil {
						return trafficRouteLocation{}, err
					}
					return trafficRouteLocation{trafficListenerLocation{bindIndex, listenerIndex, bind, listeners, listener}, group.kind, routeIndex, group.routes, route}, nil
				}
			}
		}
	}
	return trafficRouteLocation{}, ErrTrafficResourceNotFound
}

func (document *trafficConfigDocument) createListener(bindID string, draft model.TrafficListenerDraft) error {
	bindIndex, bind, _, err := document.bindByID(bindID)
	if err != nil {
		return err
	}
	_, listeners, _, err := decodeTrafficBind(document.binds[bindIndex], fmt.Sprintf("/binds/%d", bindIndex))
	if err != nil {
		return err
	}
	listener, protocol, err := encodeListenerDraft(draft, nil)
	if err != nil {
		return err
	}
	key := "routes"
	if listenerRouteKind(protocol) == "tcp" {
		key = "tcpRoutes"
	}
	listener[key] = json.RawMessage("[]")
	raw, _ := json.Marshal(listener)
	listeners = append(listeners, raw)
	return document.storeListeners(bindIndex, bind, listeners)
}

func (document *trafficConfigDocument) updateListener(id string, draft model.TrafficListenerDraft) error {
	location, err := document.listenerByID(id)
	if err != nil {
		return err
	}
	field := fmt.Sprintf("/binds/%d/listeners/%d", location.bindIndex, location.listenerIndex)
	_, routes, tcpRoutes, currentProtocol, err := decodeTrafficListener(document.bindsListenerRaw(location), field)
	if err != nil {
		return err
	}
	listener, nextProtocol, err := encodeListenerDraft(draft, location.listener)
	if err != nil {
		return err
	}
	if listenerRouteKind(currentProtocol) != listenerRouteKind(nextProtocol) {
		if len(routes)+len(tcpRoutes) > 0 && !draft.DeleteIncompatibleRoutes {
			return ErrTrafficResourceReferenced
		}
		routes, tcpRoutes = nil, nil
	}
	if listenerRouteKind(nextProtocol) == "tcp" {
		delete(listener, "routes")
		listener["tcpRoutes"], _ = json.Marshal(tcpRoutes)
	} else {
		delete(listener, "tcpRoutes")
		listener["routes"], _ = json.Marshal(routes)
	}
	location.listeners[location.listenerIndex], _ = json.Marshal(listener)
	return document.storeListeners(location.bindIndex, location.bind, location.listeners)
}

func (document *trafficConfigDocument) deleteListener(id string, deleteChildren bool) error {
	location, err := document.listenerByID(id)
	if err != nil {
		return err
	}
	field := fmt.Sprintf("/binds/%d/listeners/%d", location.bindIndex, location.listenerIndex)
	_, routes, tcpRoutes, _, err := decodeTrafficListener(document.bindsListenerRaw(location), field)
	if err != nil {
		return err
	}
	if len(routes)+len(tcpRoutes) > 0 && !deleteChildren {
		return ErrTrafficResourceReferenced
	}
	location.listeners = append(location.listeners[:location.listenerIndex], location.listeners[location.listenerIndex+1:]...)
	return document.storeListeners(location.bindIndex, location.bind, location.listeners)
}

func (document *trafficConfigDocument) createRoute(listenerID string, draft model.TrafficRouteDraft) error {
	location, err := document.listenerByID(listenerID)
	if err != nil {
		return err
	}
	protocol := optionalString(location.listener["protocol"], "HTTP")
	if listenerRouteKind(protocol) != draft.Kind {
		return ErrTrafficInvalidRequest
	}
	route, err := encodeRouteDraft(draft)
	if err != nil {
		return err
	}
	key := routeKey(draft.Kind)
	var routes []json.RawMessage
	if err := decodeArray(location.listener[key], key, &routes); err != nil {
		return err
	}
	raw, _ := json.Marshal(route)
	routes = append(routes, raw)
	location.listener[key], _ = json.Marshal(routes)
	location.listeners[location.listenerIndex], _ = json.Marshal(location.listener)
	return document.storeListeners(location.bindIndex, location.bind, location.listeners)
}

func (document *trafficConfigDocument) updateRoute(id string, draft model.TrafficRouteDraft) error {
	location, err := document.routeByID(id)
	if err != nil {
		return err
	}
	if location.kind != draft.Kind {
		return ErrTrafficInvalidRequest
	}
	route, err := encodeRouteDraft(draft)
	if err != nil {
		return err
	}
	location.routes[location.routeIndex], _ = json.Marshal(route)
	location.listener[routeKey(location.kind)], _ = json.Marshal(location.routes)
	location.listeners[location.listenerIndex], _ = json.Marshal(location.listener)
	return document.storeListeners(location.bindIndex, location.bind, location.listeners)
}

func (document *trafficConfigDocument) deleteRoute(id string) error {
	location, err := document.routeByID(id)
	if err != nil {
		return err
	}
	location.routes = append(location.routes[:location.routeIndex], location.routes[location.routeIndex+1:]...)
	location.listener[routeKey(location.kind)], _ = json.Marshal(location.routes)
	location.listeners[location.listenerIndex], _ = json.Marshal(location.listener)
	return document.storeListeners(location.bindIndex, location.bind, location.listeners)
}

func (document *trafficConfigDocument) bindsListenerRaw(location trafficListenerLocation) json.RawMessage {
	return location.listeners[location.listenerIndex]
}

func (document *trafficConfigDocument) storeListeners(bindIndex int, bind map[string]json.RawMessage, listeners []json.RawMessage) error {
	bind["listeners"], _ = json.Marshal(listeners)
	raw, err := json.Marshal(bind)
	if err != nil {
		return err
	}
	document.binds[bindIndex] = raw
	return nil
}

func encodeListenerDraft(draft model.TrafficListenerDraft, current map[string]json.RawMessage) (map[string]json.RawMessage, string, error) {
	raw, err := json.Marshal(draft.Configuration)
	if err != nil || len(draft.Configuration) == 0 {
		return nil, "", ErrTrafficInvalidRequest
	}
	listener, err := trafficRawObject(raw, "/listener/configuration")
	if err != nil {
		return nil, "", ErrTrafficInvalidRequest
	}
	for key := range listener {
		if !trafficListenerField(key) {
			return nil, "", ErrTrafficInvalidRequest
		}
	}
	delete(listener, "routes")
	delete(listener, "tcpRoutes")
	protocol := optionalString(listener["protocol"], "HTTP")
	if !validTrafficProtocol(protocol) {
		return nil, "", ErrTrafficInvalidRequest
	}
	if _, exists := listener["protocol"]; !exists {
		listener["protocol"], _ = json.Marshal(protocol)
	}
	if current != nil {
		for _, key := range []string{"namespace"} {
			if _, supplied := listener[key]; !supplied {
				if value, exists := current[key]; exists {
					listener[key] = value
				}
			}
		}
	}
	return listener, protocol, nil
}

func encodeRouteDraft(draft model.TrafficRouteDraft) (map[string]json.RawMessage, error) {
	if draft.Kind != "http" && draft.Kind != "tcp" {
		return nil, ErrTrafficInvalidRequest
	}
	raw, err := json.Marshal(draft.Configuration)
	if err != nil || len(draft.Configuration) == 0 {
		return nil, ErrTrafficInvalidRequest
	}
	route, err := trafficRawObject(raw, "/route/configuration")
	if err != nil {
		return nil, ErrTrafficInvalidRequest
	}
	for key := range route {
		if !trafficRouteField(key) {
			return nil, ErrTrafficInvalidRequest
		}
	}
	if draft.Kind == "tcp" {
		if _, exists := route["matches"]; exists {
			return nil, ErrTrafficInvalidRequest
		}
	} else if _, exists := route["matches"]; !exists {
		route["matches"] = json.RawMessage(`[{"path":{"pathPrefix":"/"}}]`)
	}
	if err := validateRawStringArray(route["hostnames"]); err != nil {
		return nil, err
	}
	if err := validateRawObjectArray(route["backends"]); err != nil {
		return nil, err
	}
	if draft.Kind == "http" {
		if err := validateRawObjectArray(route["matches"]); err != nil {
			return nil, err
		}
	}
	return route, nil
}

func validateRawStringArray(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return ErrTrafficInvalidRequest
	}
	return nil
}

func validateRawObjectArray(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values []map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return ErrTrafficInvalidRequest
	}
	return nil
}

func trafficListenerField(key string) bool {
	switch key {
	case "name", "namespace", "hostname", "protocol", "tls", "routes", "tcpRoutes", "policies":
		return true
	default:
		return false
	}
}

func trafficRouteField(key string) bool {
	switch key {
	case "name", "namespace", "ruleName", "hostnames", "matches", "policies", "backends":
		return true
	default:
		return false
	}
}

func routeKey(kind string) string {
	if kind == "tcp" {
		return "tcpRoutes"
	}
	return "routes"
}

func configDisplayName(configuration model.TrafficConfigObject, fallback string) string {
	for _, key := range []string{"name", "ruleName"} {
		if value, ok := configuration[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}

func rawDisplayName(configuration map[string]json.RawMessage, fallback string) string {
	for _, key := range []string{"name", "ruleName"} {
		if value := optionalString(configuration[key], ""); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}
