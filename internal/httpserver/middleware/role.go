package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "role not found in token"))
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "invalid role type"))
			c.Abort()
			return
		}

		for _, allowed := range roles {
			if roleStr == allowed {
				c.Next()
				return
			}
		}

		RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "insufficient permissions"))
		c.Abort()
	}
}
