//go:build legacye2e

package legacye2e

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

const legacyEmulatorAPIKey = "fake-api-key"

type legacyEmulatorToken struct {
	IDToken string
	UID     string
}

func issueLegacyFirebaseEmulatorToken(ctx context.Context, cfg legacyE2EConfig) (legacyEmulatorToken, error) {
	token, err := signUpLegacyFirebaseEmulatorUser(ctx, cfg)
	if err == nil {
		return token, nil
	}
	if !strings.Contains(err.Error(), "EMAIL_EXISTS") {
		return legacyEmulatorToken{}, err
	}

	return signInLegacyFirebaseEmulatorUser(ctx, cfg)
}

func signUpLegacyFirebaseEmulatorUser(ctx context.Context, cfg legacyE2EConfig) (legacyEmulatorToken, error) {
	payload := map[string]any{
		"email":             cfg.TestEmail,
		"password":          cfg.TestPassword,
		"returnSecureToken": true,
	}

	return callLegacyFirebaseEmulatorAuth(ctx, cfg.FirebaseEmulatorHost, "accounts:signUp", payload)
}

func signInLegacyFirebaseEmulatorUser(ctx context.Context, cfg legacyE2EConfig) (legacyEmulatorToken, error) {
	payload := map[string]any{
		"email":             cfg.TestEmail,
		"password":          cfg.TestPassword,
		"returnSecureToken": true,
	}

	return callLegacyFirebaseEmulatorAuth(ctx, cfg.FirebaseEmulatorHost, "accounts:signInWithPassword", payload)
}

func callLegacyFirebaseEmulatorAuth(ctx context.Context, host string, method string, payload map[string]any) (legacyEmulatorToken, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return legacyEmulatorToken{}, fmt.Errorf("marshal Firebase emulator payload: %w", err)
	}

	url := fmt.Sprintf("http://%s/identitytoolkit.googleapis.com/v1/%s?key=%s", host, method, legacyEmulatorAPIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return legacyEmulatorToken{}, fmt.Errorf("build Firebase emulator request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return legacyEmulatorToken{}, fmt.Errorf("call Firebase Auth Emulator: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return legacyEmulatorToken{}, fmt.Errorf("read Firebase emulator response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return legacyEmulatorToken{}, fmt.Errorf("Firebase emulator auth failed: %s", strings.TrimSpace(string(respBody)))
	}

	var result struct {
		IDToken string `json:"idToken"`
		LocalID string `json:"localId"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return legacyEmulatorToken{}, fmt.Errorf("decode Firebase emulator response: %w", err)
	}
	if result.IDToken == "" || result.LocalID == "" {
		return legacyEmulatorToken{}, fmt.Errorf("Firebase emulator response missing idToken or localId")
	}

	return legacyEmulatorToken{IDToken: result.IDToken, UID: result.LocalID}, nil
}
