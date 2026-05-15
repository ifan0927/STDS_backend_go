package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	startupPingTimeout  = 10 * time.Second
	startupPingAttempts = 3
	startupPingDelay    = 2 * time.Second
)

type pingFunc func(context.Context) error
type sleepFunc func(context.Context, time.Duration) error

// PoolConfig contains optional database/sql pool limits.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Open creates a PostgreSQL connection pool and verifies connectivity with a
// bounded ping.
func Open(ctx context.Context, databaseURL string, poolConfigs ...PoolConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if len(poolConfigs) > 0 {
		applyPoolConfig(db, poolConfigs[0])
	}

	if err := pingWithRetry(ctx, db.PingContext, sleepContext); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func pingWithRetry(ctx context.Context, ping pingFunc, sleep sleepFunc) error {
	var attemptErrs []error

	for attempt := 1; attempt <= startupPingAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, startupPingTimeout)
		err := ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}

		attemptErrs = append(attemptErrs, fmt.Errorf("attempt %d/%d: %w", attempt, startupPingAttempts, err))
		if attempt == startupPingAttempts {
			break
		}
		if err := sleep(ctx, startupPingDelay); err != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("wait before retry: %w", err))
			break
		}
	}

	return errors.Join(attemptErrs...)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func applyPoolConfig(db *sql.DB, cfg PoolConfig) {
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
}
