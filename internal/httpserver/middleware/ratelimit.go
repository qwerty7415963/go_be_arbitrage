package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateBucket struct {
	count    int
	resetAt  time.Time
	mu       sync.Mutex
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

	// Background cleanup every window duration
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

// LoginRateLimit returns 429 if the IP exceeds `limit` login attempts within `window`.
func LoginRateLimit(limit int, window time.Duration) gin.HandlerFunc {
	limiter := NewRateLimiter(limit, window)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.isAllowed(ip) {
			retryAfter := time.Until(time.Now().Add(window)).Seconds()
			c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "RATE_LIMITED",
					"message": "too many login attempts, try again later",
				},
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
