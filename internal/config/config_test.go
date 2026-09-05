package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	os.Setenv("ARBITRAGE_PORT", "9090")
	os.Setenv("ARBITRAGE_DB_HOST", "db.example.com")
	os.Setenv("ARBITRAGE_DB_NAME", "testdb")
	os.Setenv("ARBITRAGE_JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("ARBITRAGE_PORT")
	defer os.Unsetenv("ARBITRAGE_DB_HOST")
	defer os.Unsetenv("ARBITRAGE_DB_NAME")
	defer os.Unsetenv("ARBITRAGE_JWT_SECRET")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Errorf("expected host db.example.com, got %s", cfg.Database.Host)
	}
	if cfg.Database.Name != "testdb" {
		t.Errorf("expected name testdb, got %s", cfg.Database.Name)
	}
	if cfg.Auth.JWTSecret != "test-secret-key" {
		t.Errorf("expected JWT secret, got empty")
	}
}

func TestLoadDefaults(t *testing.T) {
	os.Unsetenv("ARBITRAGE_PORT")
	os.Unsetenv("ARBITRAGE_DB_HOST")
	os.Unsetenv("ARBITRAGE_DB_NAME")
	os.Setenv("ARBITRAGE_JWT_SECRET", "test-secret")
	defer os.Unsetenv("ARBITRAGE_JWT_SECRET")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default host 0.0.0.0, got %s", cfg.Server.Host)
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected default host localhost, got %s", cfg.Database.Host)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "localhost",
					Name: "testdb",
				},
				Auth: AuthConfig{
					JWTSecret: "secret",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			cfg: &Config{
				Server: ServerConfig{Port: 0},
				Database: DatabaseConfig{
					Host: "localhost",
					Name: "testdb",
				},
				Auth: AuthConfig{
					JWTSecret: "secret",
				},
			},
			wantErr: true,
		},
		{
			name: "missing database host",
			cfg: &Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "",
					Name: "testdb",
				},
				Auth: AuthConfig{
					JWTSecret: "secret",
				},
			},
			wantErr: true,
		},
		{
			name: "missing database name",
			cfg: &Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "localhost",
					Name: "",
				},
				Auth: AuthConfig{
					JWTSecret: "secret",
				},
			},
			wantErr: true,
		},
		{
			name: "missing JWT secret",
			cfg: &Config{
				Server: ServerConfig{Port: 8080},
				Database: DatabaseConfig{
					Host: "localhost",
					Name: "testdb",
				},
				Auth: AuthConfig{
					JWTSecret: "",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDSN(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "secret",
			Name:     "testdb",
			SSLMode:  "disable",
		},
	}

	expected := "host=localhost port=5432 user=postgres password=secret dbname=testdb sslmode=disable"
	if cfg.DSN() != expected {
		t.Errorf("DSN() = %s, want %s", cfg.DSN(), expected)
	}
}

func TestServerAddr(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
	}

	expected := "0.0.0.0:8080"
	if cfg.ServerAddr() != expected {
		t.Errorf("ServerAddr() = %s, want %s", cfg.ServerAddr(), expected)
	}
}

func TestGetEnvDuration(t *testing.T) {
	os.Setenv("TEST_DURATION", "5s")
	defer os.Unsetenv("TEST_DURATION")

	dur := getEnvDuration("TEST_DURATION", time.Second)
	if dur != 5*time.Second {
		t.Errorf("expected 5s, got %v", dur)
	}

	dur = getEnvDuration("NONEXISTENT", 10*time.Second)
	if dur != 10*time.Second {
		t.Errorf("expected default 10s, got %v", dur)
	}
}
