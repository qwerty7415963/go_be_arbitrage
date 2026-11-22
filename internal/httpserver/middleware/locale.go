package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/qwerty7415963/go_be_arbitrage/internal/i18n"
)

func LocaleMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		locale := i18n.LocaleVI
		if lang := c.Query("lang"); lang != "" {
			l := i18n.Locale(lang)
			if l.IsValid() {
				locale = l
			}
		} else {
			locale = i18n.ParseAcceptLanguage(c.GetHeader("Accept-Language"))
		}
		if !locale.IsValid() {
			locale = i18n.LocaleVI
		}
		c.Set("locale", locale)
		c.Next()
	}
}
