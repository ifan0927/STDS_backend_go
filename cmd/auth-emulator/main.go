package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"

	"stds_backend/internal/config"
)

const emulatorAPIKey = "fake-api-key"

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: go run ./cmd/auth-emulator [upsert-user|issue-token]")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if !cfg.Firebase.UsesAuthEmulator() {
		log.Fatalf("FIREBASE_AUTH_EMULATOR_HOST is required")
	}

	switch os.Args[1] {
	case "upsert-user":
		if err := runUpsertUser(cfg.Firebase, os.Args[2:]); err != nil {
			log.Fatalf("upsert emulator user: %v", err)
		}
	case "issue-token":
		if err := runIssueToken(cfg.Firebase, os.Args[2:]); err != nil {
			log.Fatalf("issue emulator token: %v", err)
		}
	default:
		log.Fatalf("unsupported command %q", os.Args[1])
	}
}

func runUpsertUser(firebaseCfg config.FirebaseConfig, args []string) error {
	fs := flag.NewFlagSet("upsert-user", flag.ExitOnError)
	uid := fs.String("uid", firebaseCfg.TestUID, "Firebase UID")
	email := fs.String("email", "testuser@example.com", "Emulator email")
	password := fs.String("password", "", "Emulator password")
	fs.Parse(args)

	if *uid == "" || *email == "" || *password == "" {
		return errors.New("uid, email, and password are required")
	}

	authClient, err := newAuthClient(context.Background(), firebaseCfg)
	if err != nil {
		return err
	}

	params := (&firebaseauth.UserToCreate{}).
		UID(*uid).
		Email(*email).
		Password(*password)

	if _, err := authClient.GetUser(context.Background(), *uid); err != nil {
		if _, err := authClient.CreateUser(context.Background(), params); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
	} else {
		updateParams := (&firebaseauth.UserToUpdate{}).
			Email(*email).
			Password(*password)

		if _, err := authClient.UpdateUser(context.Background(), *uid, updateParams); err != nil {
			return fmt.Errorf("update user: %w", err)
		}
	}

	fmt.Printf("emulator user ready: uid=%s email=%s\n", *uid, *email)

	return nil
}

func runIssueToken(firebaseCfg config.FirebaseConfig, args []string) error {
	fs := flag.NewFlagSet("issue-token", flag.ExitOnError)
	email := fs.String("email", "testuser@example.com", "Emulator email")
	password := fs.String("password", "", "Emulator password")
	fs.Parse(args)

	if *email == "" || *password == "" {
		return errors.New("email and password are required")
	}

	payload := map[string]any{
		"email":             *email,
		"password":          *password,
		"returnSecureToken": true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sign-in payload: %w", err)
	}

	url := fmt.Sprintf("http://%s/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=%s", firebaseCfg.AuthEmulatorHost, emulatorAPIKey)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build sign-in request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call emulator sign-in: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read sign-in response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("emulator sign-in failed: %s", strings.TrimSpace(string(respBody)))
	}

	var result struct {
		IDToken string `json:"idToken"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode sign-in response: %w", err)
	}
	if result.IDToken == "" {
		return errors.New("emulator response missing idToken")
	}

	fmt.Println(result.IDToken)

	return nil
}

func newAuthClient(ctx context.Context, firebaseCfg config.FirebaseConfig) (*firebaseauth.Client, error) {
	if err := os.Setenv("FIREBASE_AUTH_EMULATOR_HOST", firebaseCfg.AuthEmulatorHost); err != nil {
		return nil, fmt.Errorf("set FIREBASE_AUTH_EMULATOR_HOST: %w", err)
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: firebaseCfg.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("create firebase app: %w", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("create auth client: %w", err)
	}

	return authClient, nil
}
