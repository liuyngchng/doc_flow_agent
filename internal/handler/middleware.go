package handler

import (
	"log/slog"
	"time"

	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// CallLogMiddleware 对外 API 调用日志中间件
func CallLogMiddleware(st store.MetaStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只记录 /open_api 和 /api/v1 等对外调用
		path := c.Request.URL.Path
		if !isLoggablePath(path) {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		latency := time.Since(start).Milliseconds()
		status := c.Writer.Status()

		slog.Info("api_call_log",
			"path", path,
			"method", c.Request.Method,
			"status", status,
			"latency_ms", latency,
			"client_ip", c.ClientIP(),
		)
	}
}

func isLoggablePath(path string) bool {
	return len(path) > 4 && (path[:8] == "/open_api" || path[:11] == "/api/files/" || path[:8] == "/api/v1/")
}

// SecurityHeaders 安全头中间件
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Next()
	}
}