package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func JWT(authService *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "authorization header required"))
			c.Abort()
			return
		}
		if !applyBearer(c, authService, authHeader) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// OptionalJWT authenticates when an Authorization header is present (a
// malformed/invalid/expired token still aborts with 401) and otherwise
// passes the request through anonymously — for public endpoints that
// personalize per-user data when credentials are supplied.
func OptionalJWT(authService *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		if !applyBearer(c, authService, authHeader) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// applyBearer parses a Bearer header, validates the token and sets the
// auth context. On failure it writes the error response and returns false.
func applyBearer(c *gin.Context, authService *auth.Service, authHeader string) bool {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid authorization header format"))
		return false
	}

	claims, err := authService.ValidateToken(parts[1])
	if err != nil {
		if err == jwt.ErrTokenExpired {
			RespondError(c, domain.NewError(domain.ErrCodeAuthTokenExpired, "token expired"))
		} else {
			RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid token"))
		}
		return false
	}

	c.Set("user_id", claims.UserID)
	c.Set("tenant_id", claims.TenantID)
	c.Set("role", claims.Role)
	return true
}

func RespondError(c *gin.Context, err *domain.AppError) {
	statusCode := err.StatusCode
	if statusCode == 0 {
		statusCode = err.StatusCodeFromCode()
	}

	requestID, _ := c.Get("request_id")
	requestIDStr, _ := requestID.(string)

	c.JSON(statusCode, gin.H{
		"success": false,
		"error": gin.H{
			"code":       string(err.Code),
			"message":    err.Message,
			"details":    err.Details,
			"request_id": requestIDStr,
		},
	})
}
