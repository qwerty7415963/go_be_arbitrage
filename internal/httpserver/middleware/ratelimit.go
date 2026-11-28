package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateBucket struct {
	count   int
	resetAt time.Time
	mu      sync.Mutex
}

type RateLimiter struct {
	buckets map[string]*rateBucket
	mu      sync.Mutex
	limit   int
	window  time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		limit:   limit,
		window:  window,
	}

	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for ip, b := range rl.buckets {
				b.mu.Lock()
				if now.After(b.resetAt) {
					delete(rl.buckets, ip)
				}
				b.mu.Unlock()
			}
			rl.mu.Unlock()
		}
	}()

	return rl
}

func (rl *RateLimiter) isAllowed(key string) bool {
	now := time.Now()

	rl.mu.Lock()
	b, exists := rl.buckets[key]
	if !exists {
		b = &rateBucket{resetAt: now.Add(rl.window)}
		rl.buckets[key] = b
	}
	rl.mu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	if now.After(b.resetAt) {
		b.count = 0
		b.resetAt = now.Add(rl.window)
	}

	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

func (rl *RateLimiter) retryAfter(key string) time.Duration {
	rl.mu.Lock()
	b, exists := rl.buckets[key]
	rl.mu.Unlock()
	if !exists {
		return rl.window
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	d := time.Until(b.resetAt)
	if d < time.Second {
		d = time.Second
	}
	return d
}

// LoginRateLimit returns 429 if the IP exceeds `limit` attempts within `window`.
func LoginRateLimit(limit int, window time.Duration) gin.HandlerFunc {
	limiter := NewRateLimiter(limit, window)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.isAllowed(ip) {
			ra := limiter.retryAfter(ip)
			c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "RATE_LIMITED",
					"message": "too many requests, try again later",
				},
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func readBodyField(c *gin.Context, field string) string {
	if c.Request.Body == nil {
		return ""
	}
	data, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	if v, ok := m[field].(string); ok {
		return v
	}
	return ""
}

// RegisterRateLimit: 3/hour/IP + 10/day/IP
func RegisterRateLimit() gin.HandlerFunc {
	hourly := NewRateLimiter(3, time.Hour)
	daily := NewRateLimiter(10, 24*time.Hour)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !hourly.isAllowed(ip) {
			ra := hourly.retryAfter(ip)
			c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
			c.Abort()
			return
		}
		if !daily.isAllowed(ip) {
			ra := daily.retryAfter(ip)
			c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
			c.Abort()
			return
		}
		c.Next()
	}
}

// LoginRateLimitFull: 5/min/IP + 10/hour/email
func LoginRateLimitFull() gin.HandlerFunc {
	ipLimiter := NewRateLimiter(5, time.Minute)
	emailLimiter := NewRateLimiter(10, time.Hour)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !ipLimiter.isAllowed(ip) {
			ra := ipLimiter.retryAfter(ip)
			c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
			c.Abort()
			return
		}
		email := readBodyField(c, "email")
		if email != "" {
			key := "email:" + email
			if !emailLimiter.isAllowed(key) {
				ra := emailLimiter.retryAfter(key)
				c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
				c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// WalletRateLimit: 10/min/IP + 5/min/address
func WalletRateLimit() gin.HandlerFunc {
	ipLimiter := NewRateLimiter(10, time.Minute)
	addrLimiter := NewRateLimiter(5, time.Minute)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !ipLimiter.isAllowed(ip) {
			ra := ipLimiter.retryAfter(ip)
			c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
			c.Abort()
			return
		}
		address := readBodyField(c, "address")
		if address == "" {
			address = readBodyField(c, "message") // fallback
		}
		if address != "" {
			key := "addr:" + address
			if !addrLimiter.isAllowed(key) {
				ra := addrLimiter.retryAfter(key)
				c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
				c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// RefreshRateLimit: 30/min/IP
func RefreshRateLimit() gin.HandlerFunc {
	return LoginRateLimit(30, time.Minute)
}

// ScannerRateLimit: 60/min/IP
func ScannerRateLimit() gin.HandlerFunc {
	return LoginRateLimit(60, time.Minute)
}

// GeneralRateLimit: 120/min/user
func GeneralRateLimit() gin.HandlerFunc {
	limiter := NewRateLimiter(120, time.Minute)
	return func(c *gin.Context) {
		key := c.ClientIP()
		if v, ok := c.Get("user_id"); ok {
			key = fmt.Sprintf("user:%v", v)
		}
		if !limiter.isAllowed(key) {
			ra := limiter.retryAfter(key)
			c.Header("Retry-After", fmt.Sprintf("%.0f", ra.Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": gin.H{"code": "RATE_LIMITED", "message": "too many requests"}})
			c.Abort()
			return
		}
		c.Next()
	}
}
