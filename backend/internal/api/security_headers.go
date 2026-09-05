package api

import "github.com/gin-gonic/gin"

// SecurityHeaders sets headers that are meaningful even for a direct (non-
// nginx-proxied) request to the API e.g. if BACKEND_PORT is exposed to the
// host, or during local development against the backend directly. The full
// CSP lives in nginx.conf, since it only makes sense for HTML responses.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "same-origin")
		c.Next()
	}
}
