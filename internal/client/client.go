package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultEndpoint = "https://api.parasail.io/api/v1"

type Client struct {
	endpoint   *url.URL
	apiKey     string
	httpClient *http.Client
}

func New(endpoint string, apiKey string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultEndpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("endpoint must be an absolute URL")
	}

	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	return &Client{
		endpoint: parsed,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("parasail api returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("parasail api returned HTTP %d: %s", e.StatusCode, e.Body)
}

func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method string, path string, query url.Values, body any, out any) error {
	endpoint := *c.endpoint
	basePath := strings.TrimRight(endpoint.Path, "/")
	requestPath := strings.TrimLeft(path, "/")
	endpoint.Path = basePath + "/" + requestPath
	endpoint.RawQuery = query.Encode()

	var requestBody io.Reader
	if body != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		requestBody = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}

	if out == nil || len(responseBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}

	return nil
}
