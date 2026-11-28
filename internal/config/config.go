package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server           ServerConfig   `yaml:"server"`
	Database         DatabaseConfig `yaml:"database"`
	Auth             AuthConfig     `yaml:"auth"`
	Log              LogConfig      `yaml:"log"`
	WS               WSConfig       `yaml:"ws"`
	CORS             CORSConfig     `yaml:"cors"`
	Telegram         TelegramConfig `yaml:"telegram"`
	LogoDir          string         `yaml:"logo_dir"`
	LogoRefreshDays  int            `yaml:"logo_refresh_days"`
	LogoNegativeDays int            `yaml:"logo_negative_days"`
	LogoWorkers      int            `yaml:"logo_workers"`
	LogoDevSecretKey string         `yaml:"logo_dev_secret_key"`

	// Security hardening
	AdminHost                  string   `yaml:"admin_host"`
	AdminPort                  int      `yaml:"admin_port"`
	TrustedProxies             []string `yaml:"trusted_proxies"`
	PublicOrigin               string   `yaml:"public_origin"`
	CookieSecure               bool     `yaml:"cookie_secure"`
	CookieName                 string   `yaml:"cookie_name"`
	CookieMaxAge               int      `yaml:"cookie_max_age"`
	CaptchaEnabled             bool     `yaml:"captcha_enabled"`
	TurnstileSecretKey         string   `yaml:"turnstile_secret_key"`
	TurnstileExpectedHostnames []string `yaml:"turnstile_expected_hostnames"`

	Cookie  CookieConfig  `yaml:"cookie"`
	Captcha CaptchaConfig `yaml:"captcha"`
}

type ServerConfig struct {
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	Mode           string   `yaml:"mode"`
	AdminHost      string   `yaml:"admin_host"`
	AdminPort      int      `yaml:"admin_port"`
	TrustedProxies []string `yaml:"trusted_proxies"`
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

	// Security hardening
	PublicOrigin               string   `yaml:"public_origin"`
	CookieSecure               bool     `yaml:"cookie_secure"`
	CookieName                 string   `yaml:"cookie_name"`
	CookieMaxAge               int      `yaml:"cookie_max_age"`
	CaptchaEnabled             bool     `yaml:"captcha_enabled"`
	TurnstileSecretKey         string   `yaml:"turnstile_secret_key"`
	TurnstileExpectedHostnames []string `yaml:"turnstile_expected_hostnames"`
	Mode                       string   `yaml:"mode"`
}

type CookieConfig struct {
	Secure bool   `yaml:"secure"`
	Name   string `yaml:"name"`
	MaxAge int    `yaml:"max_age"`
}

type CaptchaConfig struct {
	Enabled                    bool     `yaml:"enabled"`
	TurnstileSecretKey         string   `yaml:"turnstile_secret_key"`
	TurnstileExpectedHostnames []string `yaml:"turnstile_expected_hostnames"`
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

type TelegramConfig struct {
	BotToken     string `yaml:"bot_token"`
	BotUsername  string `yaml:"bot_username"`
	AppPublicURL string `yaml:"app_public_url"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host:           getEnv("ARBITRAGE_HOST", "0.0.0.0"),
			Port:           getEnvInt("ARBITRAGE_PORT", 8080),
			Mode:           getEnv("ARBITRAGE_MODE", "debug"),
			AdminHost:      getEnv("ARBITRAGE_ADMIN_HOST", "0.0.0.0"),
			AdminPort:      getEnvInt("ARBITRAGE_ADMIN_PORT", 8081),
			TrustedProxies: getEnvStringSlice("ARBITRAGE_TRUSTED_PROXIES", nil),
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

			PublicOrigin:               getEnv("ARBITRAGE_PUBLIC_ORIGIN", ""),
			CookieSecure:               getEnvBool("ARBITRAGE_COOKIE_SECURE", false),
			CookieName:                 getEnv("ARBITRAGE_COOKIE_NAME", "__Host-refresh"),
			CookieMaxAge:               getEnvInt("ARBITRAGE_COOKIE_MAX_AGE", 604800),
			CaptchaEnabled:             getEnvBool("ARBITRAGE_CAPTCHA_ENABLED", false),
			TurnstileSecretKey:         getEnv("TURNSTILE_SECRET_KEY", ""),
			TurnstileExpectedHostnames: getEnvStringSlice("TURNSTILE_EXPECTED_HOSTNAMES", nil),
			Mode:                       getEnv("ARBITRAGE_MODE", "debug"),
		},
		CORS: CORSConfig{
			AllowOrigins: getEnvStringSlice("ARBITRAGE_CORS_ORIGINS", []string{"*"}),
		},
		Telegram: TelegramConfig{
			BotToken:     getEnv("TELEGRAM_BOT_TOKEN", ""),
			BotUsername:  getEnv("TELEGRAM_BOT_USERNAME", ""),
			AppPublicURL: getEnv("APP_PUBLIC_URL", "http://localhost:3000"),
		},
		LogoDir:          getEnv("ARBITRAGE_LOGO_DIR", "./data/logos"),
		LogoRefreshDays:  getEnvInt("ARBITRAGE_LOGO_REFRESH_DAYS", 30),
		LogoNegativeDays: getEnvInt("ARBITRAGE_LOGO_NEGATIVE_DAYS", 7),
		LogoWorkers:      getEnvInt("ARBITRAGE_LOGO_WORKERS", 2),
		LogoDevSecretKey: getEnv("LOGO_DEV_SECRET_KEY", ""),

		// Security hardening top-level
		AdminHost:                  getEnv("ARBITRAGE_ADMIN_HOST", "0.0.0.0"),
		AdminPort:                  getEnvInt("ARBITRAGE_ADMIN_PORT", 8081),
		TrustedProxies:             getEnvStringSlice("ARBITRAGE_TRUSTED_PROXIES", nil),
		PublicOrigin:               getEnv("ARBITRAGE_PUBLIC_ORIGIN", ""),
		CookieSecure:               getEnvBool("ARBITRAGE_COOKIE_SECURE", false),
		CookieName:                 getEnv("ARBITRAGE_COOKIE_NAME", "__Host-refresh"),
		CookieMaxAge:               getEnvInt("ARBITRAGE_COOKIE_MAX_AGE", 604800),
		CaptchaEnabled:             getEnvBool("ARBITRAGE_CAPTCHA_ENABLED", false),
		TurnstileSecretKey:         getEnv("TURNSTILE_SECRET_KEY", ""),
		TurnstileExpectedHostnames: getEnvStringSlice("TURNSTILE_EXPECTED_HOSTNAMES", nil),

		Cookie: CookieConfig{
			Secure: getEnvBool("ARBITRAGE_COOKIE_SECURE", false),
			Name:   getEnv("ARBITRAGE_COOKIE_NAME", "__Host-refresh"),
			MaxAge: getEnvInt("ARBITRAGE_COOKIE_MAX_AGE", 604800),
		},
		Captcha: CaptchaConfig{
			Enabled:                    getEnvBool("ARBITRAGE_CAPTCHA_ENABLED", false),
			TurnstileSecretKey:         getEnv("TURNSTILE_SECRET_KEY", ""),
			TurnstileExpectedHostnames: getEnvStringSlice("TURNSTILE_EXPECTED_HOSTNAMES", nil),
		},
		Log: LogConfig{
			Level:  getEnv("ARBITRAGE_LOG_LEVEL", "info"),
			Format: getEnv("ARBITRAGE_LOG_FORMAT", "json"),
		},
		WS: WSConfig{
			ReadBufferSize:  getEnvInt("ARBITRAGE_WS_READ_BUFFER", 1024),
			WriteBufferSize: getEnvInt("ARBITRAGE_WS_WRITE_BUFFER", 1024),
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
	if c.Server.AdminPort < 1 || c.Server.AdminPort > 65535 {
		if c.Server.AdminPort == 0 {
			c.Server.AdminPort = 8081
		} else {
			return fmt.Errorf("invalid admin port: %d", c.Server.AdminPort)
		}
	}
	if c.Server.AdminHost == "" {
		c.Server.AdminHost = "0.0.0.0"
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
	// secret length
	minSecret := 32
	if c.Server.Mode == "release" {
		minSecret = 64
	}
	if len(c.Auth.JWTSecret) < minSecret {
		return fmt.Errorf("JWT secret too short: need at least %d", minSecret)
	}
	// sync top-level defaults
	if c.AdminHost == "" {
		c.AdminHost = c.Server.AdminHost
	}
	if c.AdminPort == 0 {
		c.AdminPort = c.Server.AdminPort
	}
	if c.TrustedProxies == nil {
		c.TrustedProxies = c.Server.TrustedProxies
	}
	if c.PublicOrigin == "" {
		c.PublicOrigin = c.Auth.PublicOrigin
	}
	if c.CookieName == "" {
		c.CookieName = c.Auth.CookieName
		if c.CookieName == "" {
			c.CookieName = "__Host-refresh"
		}
	}
	if c.CookieMaxAge == 0 {
		c.CookieMaxAge = c.Auth.CookieMaxAge
		if c.CookieMaxAge == 0 {
			c.CookieMaxAge = 604800
		}
	}
	// fail-closed in release
	if c.Server.Mode == "release" {
		if !c.Auth.CookieSecure && !c.CookieSecure && !c.Cookie.Secure {
			return fmt.Errorf("cookie secure required in release")
		}
		if !c.Auth.CaptchaEnabled && !c.CaptchaEnabled && !c.Captcha.Enabled {
			return fmt.Errorf("captcha must be enabled in release")
		}
		if c.Auth.SIWEDomain == "localhost" {
			return fmt.Errorf("SIWE domain must not be localhost in release")
		}
		// CORS wildcard check
		for _, o := range c.CORS.AllowOrigins {
			if o == "*" {
				return fmt.Errorf("CORS wildcard not allowed")
			}
		}
	}
	if c.Telegram.AppPublicURL == "" {
		c.Telegram.AppPublicURL = "http://localhost:3000"
	}
	// sync nested security fields
	c.Server.AdminHost = c.AdminHost
	c.Server.AdminPort = c.AdminPort
	if c.Server.TrustedProxies == nil {
		c.Server.TrustedProxies = c.TrustedProxies
	}
	c.Auth.PublicOrigin = c.PublicOrigin
	c.Auth.CookieSecure = c.CookieSecure || c.Cookie.Secure
	c.Auth.CookieName = c.CookieName
	c.Auth.CookieMaxAge = c.CookieMaxAge
	c.Auth.CaptchaEnabled = c.CaptchaEnabled || c.Captcha.Enabled
	c.Auth.Mode = c.Server.Mode
	if c.Auth.Mode == "" {
		c.Auth.Mode = "debug"
	}
	c.Auth.TurnstileSecretKey = c.TurnstileSecretKey
	if c.Auth.TurnstileSecretKey == "" {
		c.Auth.TurnstileSecretKey = c.Captcha.TurnstileSecretKey
	}
	if c.Captcha.TurnstileSecretKey == "" {
		c.Captcha.TurnstileSecretKey = c.Auth.TurnstileSecretKey
	}
	if c.TurnstileSecretKey == "" {
		c.TurnstileSecretKey = c.Auth.TurnstileSecretKey
	}
	c.Auth.TurnstileExpectedHostnames = c.TurnstileExpectedHostnames
	if c.Auth.TurnstileExpectedHostnames == nil {
		c.Auth.TurnstileExpectedHostnames = c.Captcha.TurnstileExpectedHostnames
	}
	if c.Captcha.TurnstileExpectedHostnames == nil {
		c.Captcha.TurnstileExpectedHostnames = c.Auth.TurnstileExpectedHostnames
	}
	if c.TurnstileExpectedHostnames == nil {
		c.TurnstileExpectedHostnames = c.Auth.TurnstileExpectedHostnames
	}
	c.Cookie.Secure = c.Auth.CookieSecure
	c.Cookie.Name = c.Auth.CookieName
	c.Cookie.MaxAge = c.Auth.CookieMaxAge
	c.Captcha.Enabled = c.Auth.CaptchaEnabled
	if c.Captcha.TurnstileSecretKey == "" {
		c.Captcha.TurnstileSecretKey = c.TurnstileSecretKey
	}
	if c.TurnstileSecretKey == "" {
		c.TurnstileSecretKey = c.Captcha.TurnstileSecretKey
	}
	if c.Captcha.TurnstileExpectedHostnames == nil {
		c.Captcha.TurnstileExpectedHostnames = c.TurnstileExpectedHostnames
	}
	if c.TurnstileExpectedHostnames == nil {
		c.TurnstileExpectedHostnames = c.Captcha.TurnstileExpectedHostnames
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

// PostgresURL returns the database connection string as a postgres:// URL
// (required by the golang-migrate postgres driver).
func (c *Config) PostgresURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		url.PathEscape(c.Database.User),
		url.QueryEscape(c.Database.Password),
		c.Database.Host,
		c.Database.Port,
		c.Database.Name,
		c.Database.SSLMode,
	)
}

func (c *Config) ServerAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func (c *Config) AdminServerAddr() string {
	host := c.Server.AdminHost
	if host == "" {
		host = c.AdminHost
	}
	if host == "" {
		host = "0.0.0.0"
	}
	port := c.Server.AdminPort
	if port == 0 {
		port = c.AdminPort
	}
	if port == 0 {
		port = 8081
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return defaultVal
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
