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

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid authorization header format"))
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims, err := authService.ValidateToken(tokenString)
		if err != nil {
			if err == jwt.ErrTokenExpired {
				RespondError(c, domain.NewError(domain.ErrCodeAuthTokenExpired, "token expired"))
			} else {
				RespondError(c, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid token"))
			}
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("tenant_id", claims.TenantID)
		c.Next()
	}
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
			"code":      string(err.Code),
			"message":   err.Message,
			"details":   err.Details,
			"request_id": requestIDStr,
		},
	})
}
