//go:build e2e

package e2e

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

type apiClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newAPIClient(baseURL string, token string) apiClient {
	return apiClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c apiClient) getJSON(ctx context.Context, path string) (*http.Response, []byte, error) {
	return c.doJSON(ctx, http.MethodGet, path, nil)
}

func (c apiClient) postJSON(ctx context.Context, path string, body any) (*http.Response, []byte, error) {
	return c.doJSON(ctx, http.MethodPost, path, body)
}

func (c apiClient) patchJSON(ctx context.Context, path string, body any) (*http.Response, []byte, error) {
	return c.doJSON(ctx, http.MethodPatch, path, body)
}

func (c apiClient) doJSON(ctx context.Context, method string, path string, body any) (*http.Response, []byte, error) {
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

func requireAPIError(t *testing.T, body []byte, expectedCode string, expectedMessage string) {
	t.Helper()

	var payload struct {
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
	}
	decodeJSON(t, body, &payload)
	if payload.ErrorCode != expectedCode {
		t.Fatalf("expected error_code %q, got %q: %s", expectedCode, payload.ErrorCode, string(body))
	}
	if payload.Message != expectedMessage {
		t.Fatalf("expected message %q, got %q: %s", expectedMessage, payload.Message, string(body))
	}
}
