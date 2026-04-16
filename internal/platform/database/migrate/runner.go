package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Runner applies embedded SQL migrations to the configured database.
type Runner struct {
	db *sql.DB
}

type migration struct {
	Version   string
	Direction string
	Name      string
	SQL       string
}

// NewRunner returns a migration runner for the provided database handle.
func NewRunner(db *sql.DB) *Runner {
	return &Runner{db: db}
}

// Up applies unapplied upward migrations in ascending version order.
func (r *Runner) Up(ctx context.Context) error {
	migrations, err := loadMigrations("up")
	if err != nil {
		return err
	}

	return r.apply(ctx, migrations, true)
}

// Down rolls back applied downward migrations in descending version order.
func (r *Runner) Down(ctx context.Context) error {
	migrations, err := loadMigrations("down")
	if err != nil {
		return err
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version > migrations[j].Version
	})

	return r.apply(ctx, migrations, false)
}

func (r *Runner) apply(ctx context.Context, migrations []migration, isUp bool) error {
	if err := r.ensureSchemaMigrations(ctx); err != nil {
		return err
	}

	applied, err := r.appliedVersions(ctx)
	if err != nil {
		return err
	}

	for _, item := range migrations {
		_, exists := applied[item.Version]
		if isUp && exists {
			continue
		}
		if !isUp && !exists {
			continue
		}

		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration tx: %w", err)
		}

		if _, err := tx.ExecContext(ctx, item.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec migration %s_%s: %w", item.Version, item.Direction, err)
		}

		if isUp {
			if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, item.Version); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("record migration version %s: %w", item.Version, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = $1`, item.Version); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("delete migration version %s: %w", item.Version, err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", item.Version, err)
		}
	}

	return nil
}

func (r *Runner) ensureSchemaMigrations(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version VARCHAR(255) PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	return nil
}

func (r *Runner) appliedVersions(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	result := map[string]struct{}{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		result[version] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}

	return result, nil
}

func loadMigrations(direction string) ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "."+direction+".sql") {
			continue
		}

		item, err := parseMigration(entry.Name())
		if err != nil {
			return nil, err
		}

		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		item.SQL = string(content)
		result = append(result, item)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Version < result[j].Version
	})

	return result, nil
}

func parseMigration(filename string) (migration, error) {
	parts := strings.Split(filename, ".")
	if len(parts) != 3 {
		return migration{}, errors.New("invalid migration filename: " + filename)
	}

	nameParts := strings.SplitN(parts[0], "_", 2)
	if len(nameParts) != 2 {
		return migration{}, errors.New("invalid migration version: " + filename)
	}

	return migration{
		Version:   nameParts[0],
		Name:      nameParts[1],
		Direction: parts[1],
	}, nil
}
