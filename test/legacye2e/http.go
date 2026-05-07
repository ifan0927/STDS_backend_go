//go:build legacye2e

package legacye2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

type legacyAPIClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newLegacyAPIClient(baseURL string, token string) legacyAPIClient {
	return legacyAPIClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c legacyAPIClient) getJSON(ctx context.Context, path string) (*http.Response, []byte, error) {
	return c.doJSON(ctx, http.MethodGet, path, nil)
}

func (c legacyAPIClient) doJSON(ctx context.Context, method string, path string, body any) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("call API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	return resp, respBody, nil
}

func requireStatus(t *testing.T, resp *http.Response, body []byte, expected int) {
	t.Helper()

	if resp.StatusCode != expected {
		t.Fatalf("expected HTTP %d, got %d: %s", expected, resp.StatusCode, string(body))
	}
}

func decodeJSON(t *testing.T, body []byte, target any) {
	t.Helper()

	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody: %s", err, string(body))
	}
}
