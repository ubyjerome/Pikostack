package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func BasicAuth(username, password string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, p, ok := c.Request.BasicAuth()
		if !ok || u != username || p != password {
			c.Header("WWW-Authenticate", `Basic realm="Pikostack"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
