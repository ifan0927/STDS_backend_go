package firebase

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	firebaseauth "firebase.google.com/go/v4/auth"

	"stds_backend/internal/config"
)

func TestShouldLoadCredentials(t *testing.T) {
	t.Run("loads credentials in normal mode", func(t *testing.T) {
		cfg := config.FirebaseConfig{
			CredentialsFile: "firebase.json",
		}

		if !shouldLoadCredentials(cfg) {
			t.Fatal("expected credentials to be loaded outside emulator mode")
		}
	})

	t.Run("skips credentials in emulator mode", func(t *testing.T) {
		cfg := config.FirebaseConfig{
			CredentialsFile:  "firebase.json",
			AuthEmulatorHost: "127.0.0.1:9099",
		}

		if shouldLoadCredentials(cfg) {
			t.Fatal("expected credentials to be skipped in emulator mode")
		}
	})
}

func TestClientVerifyIDTokenParsesClaims(t *testing.T) {
	authClient := &fakeFirebaseAuthClient{
		token: &firebaseauth.Token{
			UID: "firebase-uid",
			Claims: map[string]interface{}{
				"role":                  "admin",
				"assigned_property_ids": []interface{}{"property-1", "", "property-2", 3},
			},
		},
	}
	client := &Client{auth: authClient}

	claims, err := client.VerifyIDToken(context.Background(), "id-token")
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}

	if authClient.verifyToken != "id-token" {
		t.Fatalf("expected token to be forwarded, got %q", authClient.verifyToken)
	}
	if claims.UID != "firebase-uid" {
		t.Fatalf("expected uid to be parsed, got %q", claims.UID)
	}
	if claims.Role != "admin" {
		t.Fatalf("expected role to be parsed, got %q", claims.Role)
	}
	if len(claims.AssignedPropertyIDs) != 2 || claims.AssignedPropertyIDs[0] != "property-1" || claims.AssignedPropertyIDs[1] != "property-2" {
		t.Fatalf("unexpected assigned property ids: %#v", claims.AssignedPropertyIDs)
	}
}

func TestClientSetCustomClaimsPayload(t *testing.T) {
	authClient := &fakeFirebaseAuthClient{}
	client := &Client{auth: authClient}

	err := client.SetCustomClaims(context.Background(), "firebase-uid", "staff", []string{"property-1", "property-2"})
	if err != nil {
		t.Fatalf("set custom claims: %v", err)
	}

	if authClient.claimsUID != "firebase-uid" {
		t.Fatalf("expected uid to be forwarded, got %q", authClient.claimsUID)
	}
	if authClient.claims["role"] != "staff" {
		t.Fatalf("expected role claim, got %#v", authClient.claims["role"])
	}
	propertyIDs, ok := authClient.claims["assigned_property_ids"].([]string)
	if !ok {
		t.Fatalf("expected assigned_property_ids []string, got %T", authClient.claims["assigned_property_ids"])
	}
	if len(propertyIDs) != 2 || propertyIDs[0] != "property-1" || propertyIDs[1] != "property-2" {
		t.Fatalf("unexpected assigned_property_ids claim: %#v", propertyIDs)
	}
}

func TestClientCreateEmailPasswordUser(t *testing.T) {
	t.Run("returns created uid", func(t *testing.T) {
		authClient := &fakeFirebaseAuthClient{
			userRecord: &firebaseauth.UserRecord{
				UserInfo: &firebaseauth.UserInfo{UID: "firebase-uid"},
			},
		}
		client := &Client{auth: authClient}

		uid, err := client.CreateEmailPasswordUser(context.Background(), "user@example.com", "User Name")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		if uid != "firebase-uid" {
			t.Fatalf("expected created uid, got %q", uid)
		}
		if authClient.createUser == nil {
			t.Fatal("expected create user request")
		}
	})

	t.Run("maps duplicate email", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"EMAIL_EXISTS"}}`))
		}))
		defer server.Close()
		t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "")

		client, err := New(context.Background(), config.FirebaseConfig{
			ProjectID:        "test-project",
			AuthEmulatorHost: strings.TrimPrefix(server.URL, "http://"),
		})
		if err != nil {
			t.Fatalf("new firebase client: %v", err)
		}

		uid, err := client.CreateEmailPasswordUser(context.Background(), "duplicate@example.com", "Duplicate User")
		if uid != "" {
			t.Fatalf("expected empty uid, got %q", uid)
		}
		if !errors.Is(err, ErrEmailAlreadyExists) {
			t.Fatalf("expected duplicate email error, got %v", err)
		}
	})

	t.Run("wraps generic create error", func(t *testing.T) {
		createErr := errors.New("provider unavailable")
		client := &Client{auth: &fakeFirebaseAuthClient{createUserErr: createErr}}

		uid, err := client.CreateEmailPasswordUser(context.Background(), "user@example.com", "User Name")
		if uid != "" {
			t.Fatalf("expected empty uid, got %q", uid)
		}
		if !errors.Is(err, createErr) {
			t.Fatalf("expected provider error to be wrapped, got %v", err)
		}
		if err == createErr {
			t.Fatal("expected generic create error to be wrapped")
		}
	})
}

func TestClientGeneratePasswordResetLink(t *testing.T) {
	t.Run("uses plain reset link without redirect url", func(t *testing.T) {
		authClient := &fakeFirebaseAuthClient{passwordResetLink: "https://reset.example/link"}
		client := &Client{auth: authClient}

		link, err := client.GeneratePasswordResetLink(context.Background(), "user@example.com")
		if err != nil {
			t.Fatalf("generate reset link: %v", err)
		}

		if link != "https://reset.example/link" {
			t.Fatalf("unexpected reset link: %q", link)
		}
		if authClient.passwordResetEmail != "user@example.com" {
			t.Fatalf("expected email to be forwarded, got %q", authClient.passwordResetEmail)
		}
		if authClient.passwordResetSettings != nil {
			t.Fatalf("expected no action code settings, got %#v", authClient.passwordResetSettings)
		}
	})

	t.Run("uses settings when redirect url is configured", func(t *testing.T) {
		authClient := &fakeFirebaseAuthClient{passwordResetLink: "https://reset.example/link"}
		client := &Client{
			auth:                     authClient,
			passwordResetRedirectURL: "https://app.example/reset",
		}

		link, err := client.GeneratePasswordResetLink(context.Background(), "user@example.com")
		if err != nil {
			t.Fatalf("generate reset link: %v", err)
		}

		if link != "https://reset.example/link" {
			t.Fatalf("unexpected reset link: %q", link)
		}
		if authClient.passwordResetEmail != "user@example.com" {
			t.Fatalf("expected email to be forwarded, got %q", authClient.passwordResetEmail)
		}
		if authClient.passwordResetSettings == nil || authClient.passwordResetSettings.URL != "https://app.example/reset" {
			t.Fatalf("unexpected action code settings: %#v", authClient.passwordResetSettings)
		}
	})

	t.Run("wraps provider error", func(t *testing.T) {
		resetErr := errors.New("reset unavailable")
		client := &Client{auth: &fakeFirebaseAuthClient{passwordResetErr: resetErr}}

		link, err := client.GeneratePasswordResetLink(context.Background(), "user@example.com")
		if link != "" {
			t.Fatalf("expected empty link, got %q", link)
		}
		if !errors.Is(err, resetErr) {
			t.Fatalf("expected reset error to be wrapped, got %v", err)
		}
		if err == resetErr {
			t.Fatal("expected reset error to be wrapped")
		}
	})
}

func TestClientDeleteUserWrapsProviderError(t *testing.T) {
	deleteErr := errors.New("delete unavailable")
	client := &Client{auth: &fakeFirebaseAuthClient{deleteErr: deleteErr}}

	err := client.DeleteUser(context.Background(), "firebase-uid")
	if !errors.Is(err, deleteErr) {
		t.Fatalf("expected delete error to be wrapped, got %v", err)
	}
	if err == deleteErr {
		t.Fatal("expected delete error to be wrapped")
	}
}

type fakeFirebaseAuthClient struct {
	token                 *firebaseauth.Token
	verifyToken           string
	claimsUID             string
	claims                map[string]interface{}
	userRecord            *firebaseauth.UserRecord
	createUser            *firebaseauth.UserToCreate
	createUserErr         error
	passwordResetEmail    string
	passwordResetSettings *firebaseauth.ActionCodeSettings
	passwordResetLink     string
	passwordResetErr      error
	deleteErr             error
}

func (f *fakeFirebaseAuthClient) VerifyIDToken(_ context.Context, token string) (*firebaseauth.Token, error) {
	f.verifyToken = token
	return f.token, nil
}

func (f *fakeFirebaseAuthClient) SetCustomUserClaims(_ context.Context, uid string, customClaims map[string]interface{}) error {
	f.claimsUID = uid
	f.claims = customClaims
	return nil
}

func (f *fakeFirebaseAuthClient) CreateUser(_ context.Context, user *firebaseauth.UserToCreate) (*firebaseauth.UserRecord, error) {
	f.createUser = user
	if f.createUserErr != nil {
		return nil, f.createUserErr
	}

	return f.userRecord, nil
}

func (f *fakeFirebaseAuthClient) PasswordResetLink(_ context.Context, email string) (string, error) {
	f.passwordResetEmail = email
	if f.passwordResetErr != nil {
		return "", f.passwordResetErr
	}

	return f.passwordResetLink, nil
}

func (f *fakeFirebaseAuthClient) PasswordResetLinkWithSettings(_ context.Context, email string, settings *firebaseauth.ActionCodeSettings) (string, error) {
	f.passwordResetEmail = email
	f.passwordResetSettings = settings
	if f.passwordResetErr != nil {
		return "", f.passwordResetErr
	}

	return f.passwordResetLink, nil
}

func (f *fakeFirebaseAuthClient) DeleteUser(_ context.Context, _ string) error {
	return f.deleteErr
}
