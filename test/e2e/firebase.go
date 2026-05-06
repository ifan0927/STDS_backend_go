//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const emulatorAPIKey = "fake-api-key"

type emulatorToken struct {
	IDToken string
	UID     string
}

func issueFirebaseEmulatorToken(ctx context.Context, cfg e2eConfig) (emulatorToken, error) {
	token, err := signUpFirebaseEmulatorUser(ctx, cfg)
	if err == nil {
		return token, nil
	}
	if !strings.Contains(err.Error(), "EMAIL_EXISTS") {
		return emulatorToken{}, err
	}

	return signInFirebaseEmulatorUser(ctx, cfg)
}

func signUpFirebaseEmulatorUser(ctx context.Context, cfg e2eConfig) (emulatorToken, error) {
	payload := map[string]any{
		"email":             cfg.TestEmail,
		"password":          cfg.TestPassword,
		"returnSecureToken": true,
	}

	return callFirebaseEmulatorAuth(ctx, cfg.FirebaseEmulatorHost, "accounts:signUp", payload)
}

func signInFirebaseEmulatorUser(ctx context.Context, cfg e2eConfig) (emulatorToken, error) {
	payload := map[string]any{
		"email":             cfg.TestEmail,
		"password":          cfg.TestPassword,
		"returnSecureToken": true,
	}

	return callFirebaseEmulatorAuth(ctx, cfg.FirebaseEmulatorHost, "accounts:signInWithPassword", payload)
}

func callFirebaseEmulatorAuth(ctx context.Context, host string, method string, payload map[string]any) (emulatorToken, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return emulatorToken{}, fmt.Errorf("marshal Firebase emulator payload: %w", err)
	}

	url := fmt.Sprintf("http://%s/identitytoolkit.googleapis.com/v1/%s?key=%s", host, method, emulatorAPIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return emulatorToken{}, fmt.Errorf("build Firebase emulator request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return emulatorToken{}, fmt.Errorf("call Firebase Auth Emulator: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return emulatorToken{}, fmt.Errorf("read Firebase emulator response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return emulatorToken{}, fmt.Errorf("Firebase emulator auth failed: %s", strings.TrimSpace(string(respBody)))
	}

	var result struct {
		IDToken string `json:"idToken"`
		LocalID string `json:"localId"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return emulatorToken{}, fmt.Errorf("decode Firebase emulator response: %w", err)
	}
	if result.IDToken == "" || result.LocalID == "" {
		return emulatorToken{}, fmt.Errorf("Firebase emulator response missing idToken or localId")
	}

	return emulatorToken{IDToken: result.IDToken, UID: result.LocalID}, nil
}
