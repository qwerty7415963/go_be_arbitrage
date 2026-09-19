package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Log      LogConfig      `yaml:"log"`
	WS       WSConfig       `yaml:"ws"`
	CORS     CORSConfig     `yaml:"cors"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	User            string        `yaml:"user"`
	Password        string        `yaml:"password"`
	Name            string        `yaml:"name"`
	SSLMode         string        `yaml:"sslmode"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

type AuthConfig struct {
	JWTSecret         string        `yaml:"jwt_secret"`
	JWTExpiration     time.Duration `yaml:"jwt_expiration"`
	RefreshExpiration time.Duration `yaml:"refresh_expiration"`
	AdminEmail        string        `yaml:"admin_email"`
	AdminPassword     string        `yaml:"admin_password"`
	SIWEDomain        string        `yaml:"siwe_domain"`
	SIWENonceTTL      time.Duration `yaml:"siwe_nonce_ttl"`
	SupportedChains   []int64       `yaml:"supported_chains"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type WSConfig struct {
	ReadBufferSize  int `yaml:"read_buffer_size"`
	WriteBufferSize int `yaml:"write_buffer_size"`
}

type CORSConfig struct {
	AllowOrigins []string `yaml:"allow_origins"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host: getEnv("ARBITRAGE_HOST", "0.0.0.0"),
			Port: getEnvInt("ARBITRAGE_PORT", 8080),
			Mode: getEnv("ARBITRAGE_MODE", "debug"),
		},
		Database: DatabaseConfig{
			Host:            getEnv("ARBITRAGE_DB_HOST", "localhost"),
			Port:            getEnvInt("ARBITRAGE_DB_PORT", 5432),
			User:            getEnv("ARBITRAGE_DB_USER", "arbitrage"),
			Password:        getEnv("ARBITRAGE_DB_PASSWORD", ""),
			Name:            getEnv("ARBITRAGE_DB_NAME", "arbitrage"),
			SSLMode:         getEnv("ARBITRAGE_DB_SSLMODE", "disable"),
			MaxOpenConns:    getEnvInt("ARBITRAGE_DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("ARBITRAGE_DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime: getEnvDuration("ARBITRAGE_DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Auth: AuthConfig{
			JWTSecret:         getEnv("ARBITRAGE_JWT_SECRET", ""),
			JWTExpiration:     getEnvDuration("ARBITRAGE_JWT_EXPIRATION", 15*time.Minute),
			RefreshExpiration: getEnvDuration("ARBITRAGE_REFRESH_EXPIRATION", 7*24*time.Hour),
			AdminEmail:        getEnv("ARBITRAGE_ADMIN_EMAIL", ""),
			AdminPassword:     getEnv("ARBITRAGE_ADMIN_PASSWORD", ""),
			SIWEDomain:        getEnv("ARBITRAGE_SIWE_DOMAIN", "localhost"),
			SIWENonceTTL:      getEnvDuration("ARBITRAGE_SIWE_NONCE_TTL", 5*time.Minute),
			SupportedChains:   getEnvInt64Slice("ARBITRAGE_SIWE_CHAINS", []int64{1, 42161, 10, 137, 8453}),
		},
		Log: LogConfig{
			Level:  getEnv("ARBITRAGE_LOG_LEVEL", "info"),
			Format: getEnv("ARBITRAGE_LOG_FORMAT", "json"),
		},
		WS: WSConfig{
			ReadBufferSize:  getEnvInt("ARBITRAGE_WS_READ_BUFFER", 1024),
			WriteBufferSize: getEnvInt("ARBITRAGE_WS_WRITE_BUFFER", 1024),
		},
		CORS: CORSConfig{
			AllowOrigins: getEnvStringSlice("ARBITRAGE_CORS_ORIGINS", []string{"*"}),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Database.Name == "" {
		return fmt.Errorf("database name is required")
	}
	if c.Auth.JWTSecret == "" {
		return fmt.Errorf("JWT secret is required")
	}
	return nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Database.Host,
		c.Database.Port,
		c.Database.User,
		c.Database.Password,
		c.Database.Name,
		c.Database.SSLMode,
	)
}

func (c *Config) ServerAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if dur, err := time.ParseDuration(val); err == nil {
			return dur
		}
	}
	return defaultVal
}

func getEnvInt64Slice(key string, defaultVal []int64) []int64 {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		result := make([]int64, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if id, err := strconv.ParseInt(p, 10, 64); err == nil {
				result = append(result, id)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultVal
}

func getEnvStringSlice(key string, defaultVal []string) []string {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultVal
}
