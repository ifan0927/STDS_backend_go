package database

import (
	"database/sql"
	"testing"
	"time"
)

func TestApplyPoolConfigSetsMaxOpenConns(t *testing.T) {
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	applyPoolConfig(db, PoolConfig{
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: 5 * time.Minute,
	})

	if got := db.Stats().MaxOpenConnections; got != 5 {
		t.Fatalf("MaxOpenConnections = %d, want 5", got)
	}
}
