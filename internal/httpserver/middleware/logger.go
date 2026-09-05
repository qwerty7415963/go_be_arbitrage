package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
)

func Logger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()
		method := c.Request.Method
		clientIP := c.ClientIP()
		requestID, _ := c.Get("request_id")
		requestIDStr, _ := requestID.(string)

		logFields := map[string]interface{}{
			"status":     statusCode,
			"method":     method,
			"path":       path,
			"query":      query,
			"latency_ms": latency.Milliseconds(),
			"client_ip":  clientIP,
			"request_id": requestIDStr,
		}

		if len(c.Errors) > 0 {
			logFields["errors"] = c.Errors.String()
		}

		log.WithFields(logFields).Info("request completed")
	}
}
