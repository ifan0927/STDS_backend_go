package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadReadsDatabasePoolConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://stds:stds@localhost:5432/stds_backend?sslmode=disable")
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "2")
	t.Setenv("DB_CONN_MAX_LIFETIME", "5m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DB.MaxOpenConns != 5 {
		t.Fatalf("MaxOpenConns = %d, want 5", cfg.DB.MaxOpenConns)
	}
	if cfg.DB.MaxIdleConns != 2 {
		t.Fatalf("MaxIdleConns = %d, want 2", cfg.DB.MaxIdleConns)
	}
	if cfg.DB.ConnMaxLifetime != 5*time.Minute {
		t.Fatalf("ConnMaxLifetime = %s, want 5m", cfg.DB.ConnMaxLifetime)
	}
}

func TestLoadRejectsInvalidDatabasePoolConfig(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{
			name:    "max open must be integer",
			key:     "DB_MAX_OPEN_CONNS",
			value:   "many",
			wantErr: "DB_MAX_OPEN_CONNS must be an integer",
		},
		{
			name:    "max idle must be non-negative",
			key:     "DB_MAX_IDLE_CONNS",
			value:   "-1",
			wantErr: "DB_MAX_IDLE_CONNS must be non-negative",
		},
		{
			name:    "lifetime must be duration",
			key:     "DB_CONN_MAX_LIFETIME",
			value:   "soon",
			wantErr: "DB_CONN_MAX_LIFETIME must be a duration",
		},
		{
			name:    "lifetime must be non-negative",
			key:     "DB_CONN_MAX_LIFETIME",
			value:   "-5m",
			wantErr: "DB_CONN_MAX_LIFETIME must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://stds:stds@localhost:5432/stds_backend?sslmode=disable")
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %q, want contains %q", err.Error(), tt.wantErr)
			}
		})
	}
}
