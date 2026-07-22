// Package upstream provides a bounded, secret-safe HTTP transport for adapters.
package upstream

import (
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
	Path      string
	Status    int
	Retryable bool
	Kind      string
}

func (err *Error) Error() string {
	if err.Status > 0 {
		return fmt.Sprintf("%s GET %s returned status %d", err.Source, err.Path, err.Status)
	}
	return fmt.Sprintf("%s GET %s failed: %s", err.Source, err.Path, err.Kind)
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
	started := time.Now()
	response, err := client.get(ctx, path)
	duration := time.Since(started)
	if err != nil {
		return duration, err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(destination); err != nil {
		return duration, &Error{Source: client.source, Path: path, Kind: "invalid JSON response"}
	}
	return duration, nil
}

func (client *Client) ProbeJSON(ctx context.Context, path string) error {
	var payload any
	_, err := client.GetJSON(ctx, path, &payload)
	return err
}

func (client *Client) get(ctx context.Context, path string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "?") {
		return nil, &Error{Source: client.source, Path: "[invalid-path]", Kind: "invalid request path"}
	}
	endpoint := *client.baseURL
	endpoint.Path = strings.TrimRight(client.baseURL.Path, "/") + path
	endpoint.RawQuery = ""
	endpoint.Fragment = ""

	for attempt := 0; attempt <= client.retryMax; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, &Error{Source: client.source, Path: path, Kind: "request creation failed"}
		}
		request.Header.Set("Accept", "application/json")
		if client.authName != "" && client.authValue != "" {
			request.Header.Set(client.authName, client.authValue)
		}
		response, err := client.http.Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}

		retryable := err != nil
		status := 0
		kind := "request failed"
		if response != nil {
			status = response.StatusCode
			retryable = status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			kind = "upstream response"
		}
		if !retryable || attempt == client.retryMax {
			return nil, &Error{Source: client.source, Path: path, Status: status, Retryable: retryable, Kind: kind}
		}
		select {
		case <-ctx.Done():
			return nil, &Error{Source: client.source, Path: path, Retryable: true, Kind: "request canceled"}
		case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
		}
	}
	return nil, &Error{Source: client.source, Path: path, Kind: "request failed"}
}
