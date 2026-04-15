package firebase

import (
	"context"
	"errors"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"

	"stds_backend/internal/config"
)

type Claims struct {
	UID                 string
	Role                string
	AssignedPropertyIDs []string
}

type Authenticator interface {
	VerifyIDToken(ctx context.Context, token string) (*Claims, error)
}

type Client struct {
	auth *firebaseauth.Client
}

func New(ctx context.Context, cfg config.FirebaseConfig) (*Client, error) {
	if cfg.ProjectID == "" {
		return nil, errors.New("FIREBASE_PROJECT_ID is required")
	}

	if cfg.AuthEmulatorHost != "" {
		if err := os.Setenv("FIREBASE_AUTH_EMULATOR_HOST", cfg.AuthEmulatorHost); err != nil {
			return nil, fmt.Errorf("set FIREBASE_AUTH_EMULATOR_HOST: %w", err)
		}
	}

	options := []option.ClientOption{}
	if shouldLoadCredentials(cfg) {
		if _, err := os.Stat(cfg.CredentialsFile); err != nil {
			return nil, fmt.Errorf("stat firebase credentials: %w", err)
		}
		options = append(options, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: cfg.ProjectID,
	}, options...)
	if err != nil {
		return nil, fmt.Errorf("create firebase app: %w", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("create firebase auth client: %w", err)
	}

	return &Client{auth: authClient}, nil
}

func shouldLoadCredentials(cfg config.FirebaseConfig) bool {
	return cfg.CredentialsFile != "" && !cfg.UsesAuthEmulator()
}

func (c *Client) VerifyIDToken(ctx context.Context, token string) (*Claims, error) {
	verified, err := c.auth.VerifyIDToken(ctx, token)
	if err != nil {
		return nil, err
	}

	return &Claims{
		UID:                 verified.UID,
		Role:                getStringClaim(verified.Claims, "role"),
		AssignedPropertyIDs: getStringSliceClaim(verified.Claims, "assigned_property_ids"),
	}, nil
}

func getStringClaim(claims map[string]interface{}, key string) string {
	value, ok := claims[key]
	if !ok {
		return ""
	}

	text, _ := value.(string)

	return text
}

func getStringSliceClaim(claims map[string]interface{}, key string) []string {
	value, ok := claims[key]
	if !ok {
		return nil
	}

	values, ok := value.([]interface{})
	if !ok {
		return nil
	}

	result := make([]string, 0, len(values))
	for _, item := range values {
		text, ok := item.(string)
		if ok && text != "" {
			result = append(result, text)
		}
	}

	return result
}
