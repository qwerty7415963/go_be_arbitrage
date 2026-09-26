package database

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/qwerty7415963/go_be_arbitrage/migrations"
)

// RunMigrate applies database migration commands using the SQL files embedded
// in the binary (see migrations/FS).
//
// Usage: migrate <command> [args]
//
//	up            apply all pending migrations
//	down [N|all]  roll back N migrations (default 1)
//	version       print current version and dirty flag
//	force V       set version V without running migrations (baseline a
//	              database that already has the schema applied manually)
//	goto V        migrate to version V (up or down)
//
// Returns a human-readable result message.
func RunMigrate(postgresURL string, args []string) (string, error) {
	if err := validateCommand(args); err != nil {
		return "", err
	}

	srcDrv, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return "", fmt.Errorf("load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", srcDrv, postgresURL)
	if err != nil {
		return "", fmt.Errorf("init migrate: %w", err)
	}

	msg, runErr := runCommand(m, args[0], args[1:])

	// Close releases the migration lock; surface its error only when the
	// command itself succeeded.
	if srcErr, dbErr := m.Close(); runErr == nil {
		if srcErr != nil && !errors.Is(srcErr, migrate.ErrNilVersion) {
			return msg, fmt.Errorf("close source: %w", srcErr)
		}
		if dbErr != nil {
			return msg, fmt.Errorf("close database: %w", dbErr)
		}
	}

	return msg, runErr
}

// validateCommand checks command name and argument shapes before any
// database connection is opened.
func validateCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: migrate <up|down|version|force|goto> [args]")
	}
	switch args[0] {
	case "up", "version":
		return nil
	case "down":
		if len(args) > 1 && args[1] != "all" {
			if n, err := strconv.Atoi(args[1]); err != nil || n < 1 {
				return fmt.Errorf("down steps must be a positive integer or \"all\", got %q", args[1])
			}
		}
		return nil
	case "force", "goto":
		_, err := parseTargetVersion(args[1:])
		return err
	default:
		return fmt.Errorf("unknown command %q (usage: migrate <up|down|version|force|goto>)", args[0])
	}
}

func runCommand(m *migrate.Migrate, cmd string, args []string) (string, error) {
	switch cmd {
	case "up":
		if err := m.Up(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				return "migrations up to date", nil
			}
			return "", fmt.Errorf("migrate up: %w", err)
		}
		return "migrations applied", nil

	case "down":
		if len(args) > 0 && args[0] == "all" {
			if err := m.Down(); err != nil {
				if errors.Is(err, migrate.ErrNoChange) {
					return "no migrations to roll back", nil
				}
				return "", fmt.Errorf("migrate down: %w", err)
			}
			return "all migrations rolled back", nil
		}
		steps := 1
		if len(args) > 0 {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return "", fmt.Errorf("down steps must be a positive integer or \"all\", got %q", args[0])
			}
			steps = n
		}
		if err := m.Steps(-steps); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				return "no migrations to roll back", nil
			}
			return "", fmt.Errorf("migrate down: %w", err)
		}
		return fmt.Sprintf("rolled back %d migration(s)", steps), nil

	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				return "no version (empty database)", nil
			}
			return "", fmt.Errorf("get version: %w", err)
		}
		return fmt.Sprintf("version %d (dirty=%v)", v, dirty), nil

	case "force":
		v, err := parseTargetVersion(args)
		if err != nil {
			return "", err
		}
		if err := m.Force(int(v)); err != nil {
			return "", fmt.Errorf("force version: %w", err)
		}
		return fmt.Sprintf("version forced to %d", v), nil

	case "goto":
		v, err := parseTargetVersion(args)
		if err != nil {
			return "", err
		}
		if err := m.Migrate(v); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				return "already at target version", nil
			}
			return "", fmt.Errorf("migrate goto: %w", err)
		}
		return fmt.Sprintf("migrated to version %d", v), nil

	default:
		return "", fmt.Errorf("unknown command %q (usage: migrate <up|down|version|force|goto>)", cmd)
	}
}

func parseTargetVersion(args []string) (uint, error) {
	if len(args) == 0 {
		return 0, errors.New("missing target version")
	}
	v, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid target version %q", args[0])
	}
	return uint(v), nil
}
