package firebase

import (
	"testing"

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
