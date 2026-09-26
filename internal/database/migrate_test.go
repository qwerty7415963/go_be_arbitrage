package database

import (
	"strings"
	"testing"
)

const fakeURL = "postgres://user:pass@localhost:59999/db?sslmode=disable"

// MIG usage errors (validation before any DB connection)

func TestRunMigrate_MissingArgs(t *testing.T) {
	_, err := RunMigrate(fakeURL, nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunMigrate_UnknownCommand(t *testing.T) {
	_, err := RunMigrate(fakeURL, []string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got %v", err)
	}
}

func TestRunMigrate_DownInvalidSteps(t *testing.T) {
	_, err := RunMigrate(fakeURL, []string{"down", "abc"})
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("expected invalid steps error, got %v", err)
	}
}

func TestRunMigrate_DownNegativeSteps(t *testing.T) {
	_, err := RunMigrate(fakeURL, []string{"down", "-2"})
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("expected invalid steps error, got %v", err)
	}
}

func TestRunMigrate_ForceMissingVersion(t *testing.T) {
	_, err := RunMigrate(fakeURL, []string{"force"})
	if err == nil || !strings.Contains(err.Error(), "missing target version") {
		t.Fatalf("expected missing version error, got %v", err)
	}
}

func TestRunMigrate_GotoInvalidVersion(t *testing.T) {
	_, err := RunMigrate(fakeURL, []string{"goto", "not-a-number"})
	if err == nil || !strings.Contains(err.Error(), "invalid target version") {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}
