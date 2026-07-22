// Package upstream provides a bounded, secret-safe HTTP transport for adapters.
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const maxResponseBytes = 1 << 20

type Error struct {
	Source    model.Source
	Method    string
	Path      string
	Status    int
	Retryable bool
	Kind      string
}

func (err *Error) Error() string {
	method := err.Method
	if method == "" {
		method = http.MethodGet
	}
	if err.Status > 0 {
		return fmt.Sprintf("%s %s %s returned status %d", err.Source, method, err.Path, err.Status)
	}
	return fmt.Sprintf("%s %s %s failed: %s", err.Source, method, err.Path, err.Kind)
}

type Client struct {
	source    model.Source
	baseURL   *url.URL
	http      *http.Client
	retryMax  int
	authName  string
	authValue string
}

func New(source model.Source, rawBaseURL, authName, authValue string, client *http.Client, retryMax int) (*Client, error) {
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("invalid upstream base URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &Client{source: source, baseURL: baseURL, http: client, retryMax: retryMax, authName: authName, authValue: authValue}, nil
}

func (client *Client) GetJSON(ctx context.Context, path string, destination any) (time.Duration, error) {
	return client.doJSONQuery(ctx, http.MethodGet, path, nil, nil, destination, true)
}

// GetJSONQuery performs a bounded read with query parameters encoded by
// net/url. Adapters must use this instead of interpolating query strings into
// upstream paths.
func (client *Client) GetJSONQuery(ctx context.Context, path string, query url.Values, destination any) (time.Duration, error) {
	return client.doJSONQuery(ctx, http.MethodGet, path, query, nil, destination, true)
}

// PostJSON performs a bounded JSON POST which callers must use only for verified,
// side-effect-free upstream read contracts.
func (client *Client) PostJSON(ctx context.Context, path string, body, destination any) (time.Duration, error) {
	return client.writeJSON(ctx, http.MethodPost, path, body, destination, true)
}

// PostMutationJSON performs a non-retried mutation against a verified upstream
// contract. Callers remain responsible for idempotency and response validation.
func (client *Client) PostMutationJSON(ctx context.Context, path string, body, destination any) (time.Duration, error) {
	return client.writeJSON(ctx, http.MethodPost, path, body, destination, false)
}

// PatchJSON performs a non-retried PATCH against a verified upstream contract.
func (client *Client) PatchJSON(ctx context.Context, path string, body, destination any) (time.Duration, error) {
	return client.writeJSON(ctx, http.MethodPatch, path, body, destination, false)
}

// DeleteMutationJSON performs a non-retried DELETE against a verified upstream
// contract. A nil body sends an empty request body.
func (client *Client) DeleteMutationJSON(ctx context.Context, path string, body, destination any) (time.Duration, error) {
	if body == nil {
		return client.doJSON(ctx, http.MethodDelete, path, nil, destination, false)
	}
	return client.writeJSON(ctx, http.MethodDelete, path, body, destination, false)
}

func (client *Client) writeJSON(ctx context.Context, method, path string, body, destination any, retrySafe bool) (time.Duration, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, &Error{Source: client.source, Method: method, Path: path, Kind: "request encoding failed"}
	}
	return client.doJSON(ctx, method, path, encoded, destination, retrySafe)
}

func (client *Client) doJSON(ctx context.Context, method, path string, body []byte, destination any, retrySafe bool) (time.Duration, error) {
	return client.doJSONQuery(ctx, method, path, nil, body, destination, retrySafe)
}

func (client *Client) doJSONQuery(ctx context.Context, method, path string, query url.Values, body []byte, destination any, retrySafe bool) (time.Duration, error) {
	started := time.Now()
	response, err := client.do(ctx, method, path, query, body, retrySafe)
	duration := time.Since(started)
	if err != nil {
		return duration, err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(destination); err != nil {
		return duration, &Error{Source: client.source, Method: method, Path: path, Kind: "invalid JSON response"}
	}
	return duration, nil
}

func (client *Client) ProbeJSON(ctx context.Context, path string) error {
	var payload any
	_, err := client.GetJSON(ctx, path, &payload)
	return err
}

func (client *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, retrySafe bool) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "?") {
		return nil, &Error{Source: client.source, Method: method, Path: "[invalid-path]", Kind: "invalid request path"}
	}
	endpoint := *client.baseURL
	rawPath := strings.TrimRight(client.baseURL.EscapedPath(), "/") + path
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return nil, &Error{Source: client.source, Method: method, Path: "[invalid-path]", Kind: "invalid request path"}
	}
	endpoint.Path = decodedPath
	endpoint.RawPath = rawPath
	endpoint.RawQuery = query.Encode()
	endpoint.Fragment = ""

	for attempt := 0; attempt <= client.retryMax; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
		if err != nil {
			return nil, &Error{Source: client.source, Method: method, Path: path, Kind: "request creation failed"}
		}
		request.Header.Set("Accept", "application/json")
		if len(body) > 0 {
			request.Header.Set("Content-Type", "application/json")
		}
		if client.authName != "" && client.authValue != "" {
			request.Header.Set(client.authName, client.authValue)
		}
		response, err := client.http.Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}

		retryable := retrySafe && err != nil
		status := 0
		kind := "request failed"
		if response != nil {
			status = response.StatusCode
			retryable = retrySafe && (status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500)
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			kind = "upstream response"
		}
		if !retryable || attempt == client.retryMax {
			return nil, &Error{Source: client.source, Method: method, Path: path, Status: status, Retryable: retryable, Kind: kind}
		}
		select {
		case <-ctx.Done():
			return nil, &Error{Source: client.source, Method: method, Path: path, Retryable: true, Kind: "request canceled"}
		case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
		}
	}
	return nil, &Error{Source: client.source, Method: method, Path: path, Kind: "request failed"}
}
