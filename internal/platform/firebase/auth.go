package firebase

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"

	"stds_backend/internal/config"
)

// ErrEmailAlreadyExists indicates the Firebase Auth email is already in use.
var ErrEmailAlreadyExists = errors.New("firebase auth email already exists")

// Claims contains the authenticated identity fields consumed by the API.
type Claims struct {
	UID                 string
	Role                string
	AssignedPropertyIDs []string
}

// Authenticator verifies Firebase ID tokens and returns normalized claims.
type Authenticator interface {
	VerifyIDToken(ctx context.Context, token string) (*Claims, error)
}

// ClaimsWriter updates Firebase custom claims for a user.
type ClaimsWriter interface {
	SetCustomClaims(ctx context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error
}

// UserProvisioner creates and deletes Firebase Auth users for backend-managed onboarding.
type UserProvisioner interface {
	CreateEmailPasswordUser(ctx context.Context, email string, name string) (string, error)
	GeneratePasswordResetLink(ctx context.Context, email string) (string, error)
	DeleteUser(ctx context.Context, firebaseUID string) error
}

// Client is the Firebase-backed implementation of Authenticator.
type Client struct {
	auth                     firebaseAuthClient
	passwordResetRedirectURL string
}

type firebaseAuthClient interface {
	VerifyIDToken(ctx context.Context, token string) (*firebaseauth.Token, error)
	SetCustomUserClaims(ctx context.Context, uid string, customClaims map[string]interface{}) error
	CreateUser(ctx context.Context, user *firebaseauth.UserToCreate) (*firebaseauth.UserRecord, error)
	PasswordResetLink(ctx context.Context, email string) (string, error)
	PasswordResetLinkWithSettings(ctx context.Context, email string, settings *firebaseauth.ActionCodeSettings) (string, error)
	DeleteUser(ctx context.Context, uid string) error
}

// New creates a Firebase auth client from the provided configuration.
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

	return &Client{
		auth:                     authClient,
		passwordResetRedirectURL: cfg.PasswordResetRedirectURL,
	}, nil
}

func shouldLoadCredentials(cfg config.FirebaseConfig) bool {
	return cfg.CredentialsFile != "" && !cfg.UsesAuthEmulator()
}

// VerifyIDToken verifies the bearer token with Firebase and maps selected
// custom claims into the local Claims shape.
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

// SetCustomClaims writes role and assigned property IDs to Firebase custom claims.
func (c *Client) SetCustomClaims(ctx context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error {
	claims := map[string]interface{}{
		"role":                  role,
		"assigned_property_ids": assignedPropertyIDs,
	}

	if err := c.auth.SetCustomUserClaims(ctx, firebaseUID, claims); err != nil {
		return fmt.Errorf("set custom user claims: %w", err)
	}

	return nil
}

// CreateEmailPasswordUser provisions a Firebase Auth user with a temporary password.
func (c *Client) CreateEmailPasswordUser(ctx context.Context, email string, name string) (string, error) {
	temporaryPassword, err := generateTemporaryPassword()
	if err != nil {
		return "", fmt.Errorf("generate temporary password: %w", err)
	}

	record, err := c.auth.CreateUser(ctx, (&firebaseauth.UserToCreate{}).
		Email(email).
		DisplayName(name).
		EmailVerified(false).
		Password(temporaryPassword))
	if err != nil {
		if firebaseauth.IsEmailAlreadyExists(err) {
			return "", ErrEmailAlreadyExists
		}

		return "", fmt.Errorf("create firebase auth user: %w", err)
	}

	return record.UID, nil
}

// GeneratePasswordResetLink returns the Firebase-generated password reset URL.
func (c *Client) GeneratePasswordResetLink(ctx context.Context, email string) (string, error) {
	if c.passwordResetRedirectURL == "" {
		link, err := c.auth.PasswordResetLink(ctx, email)
		if err != nil {
			return "", fmt.Errorf("generate password reset link: %w", err)
		}

		return link, nil
	}

	link, err := c.auth.PasswordResetLinkWithSettings(ctx, email, &firebaseauth.ActionCodeSettings{
		URL: c.passwordResetRedirectURL,
	})
	if err != nil {
		return "", fmt.Errorf("generate password reset link with settings: %w", err)
	}

	return link, nil
}

// DeleteUser removes a Firebase Auth user by UID.
func (c *Client) DeleteUser(ctx context.Context, firebaseUID string) error {
	if err := c.auth.DeleteUser(ctx, firebaseUID); err != nil {
		return fmt.Errorf("delete firebase auth user: %w", err)
	}

	return nil
}

func generateTemporaryPassword() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
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
