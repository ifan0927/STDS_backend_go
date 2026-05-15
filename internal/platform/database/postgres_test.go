package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

func TestPingWithRetryReturnsAfterFirstSuccess(t *testing.T) {
	var pingCalls int
	var sleepCalls int

	err := pingWithRetry(context.Background(), func(ctx context.Context) error {
		pingCalls++
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ping context has no deadline")
		}
		if got := time.Until(deadline); got <= 0 || got > startupPingTimeout {
			t.Fatalf("ping timeout = %v, want up to %v", got, startupPingTimeout)
		}
		return nil
	}, func(ctx context.Context, d time.Duration) error {
		sleepCalls++
		return nil
	})

	if err != nil {
		t.Fatalf("pingWithRetry() error = %v", err)
	}
	if pingCalls != 1 {
		t.Fatalf("ping calls = %d, want 1", pingCalls)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d, want 0", sleepCalls)
	}
}

func TestPingWithRetryRetriesTemporaryFailure(t *testing.T) {
	var pingCalls int
	var sleepDelays []time.Duration
	temporaryErr := errors.New("temporary unavailable")

	err := pingWithRetry(context.Background(), func(ctx context.Context) error {
		pingCalls++
		if pingCalls < 3 {
			return temporaryErr
		}
		return nil
	}, func(ctx context.Context, d time.Duration) error {
		sleepDelays = append(sleepDelays, d)
		return nil
	})

	if err != nil {
		t.Fatalf("pingWithRetry() error = %v", err)
	}
	if pingCalls != 3 {
		t.Fatalf("ping calls = %d, want 3", pingCalls)
	}
	if len(sleepDelays) != 2 {
		t.Fatalf("sleep calls = %d, want 2", len(sleepDelays))
	}
	for i, got := range sleepDelays {
		if got != startupPingDelay {
			t.Fatalf("sleep delay %d = %v, want %v", i, got, startupPingDelay)
		}
	}
}

func TestPingWithRetryReturnsErrorAfterAttempts(t *testing.T) {
	var pingCalls int
	var sleepCalls int
	pingErr := errors.New("connection timeout")

	err := pingWithRetry(context.Background(), func(ctx context.Context) error {
		pingCalls++
		return pingErr
	}, func(ctx context.Context, d time.Duration) error {
		sleepCalls++
		return nil
	})

	if err == nil {
		t.Fatal("pingWithRetry() error = nil, want error")
	}
	if pingCalls != startupPingAttempts {
		t.Fatalf("ping calls = %d, want %d", pingCalls, startupPingAttempts)
	}
	if sleepCalls != startupPingAttempts-1 {
		t.Fatalf("sleep calls = %d, want %d", sleepCalls, startupPingAttempts-1)
	}
	if !strings.Contains(err.Error(), "attempt 3/3") {
		t.Fatalf("error = %q, want final attempt context", err.Error())
	}
	if !errors.Is(err, pingErr) {
		t.Fatalf("errors.Is(error, pingErr) = false, want true")
	}
}
