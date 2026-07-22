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
	return client.doJSON(ctx, http.MethodGet, path, nil, destination, true)
}

// PostJSON performs a bounded JSON POST which callers must use only for verified,
// side-effect-free upstream read contracts.
func (client *Client) PostJSON(ctx context.Context, path string, body, destination any) (time.Duration, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, &Error{Source: client.source, Method: http.MethodPost, Path: path, Kind: "request encoding failed"}
	}
	return client.doJSON(ctx, http.MethodPost, path, encoded, destination, true)
}

func (client *Client) doJSON(ctx context.Context, method, path string, body []byte, destination any, retrySafe bool) (time.Duration, error) {
	started := time.Now()
	response, err := client.do(ctx, method, path, body, retrySafe)
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

func (client *Client) do(ctx context.Context, method, path string, body []byte, retrySafe bool) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "?") {
		return nil, &Error{Source: client.source, Method: method, Path: "[invalid-path]", Kind: "invalid request path"}
	}
	endpoint := *client.baseURL
	endpoint.Path = strings.TrimRight(client.baseURL.Path, "/") + path
	endpoint.RawQuery = ""
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
