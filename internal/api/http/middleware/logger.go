package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/gin-gonic/gin"
)

// RequestLogger 用项目统一的 logger 记录每个请求，替代 gin.Logger()（它直接写 stdout，管理后台看不到）。
// 5xx 记为 error，4xx 记为 warn。
func RequestLogger() gin.HandlerFunc {
	log := logger.Named("http")

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		fields := []logger.Field{
			logger.String("method", c.Request.Method),
			logger.String("path", c.Request.URL.Path),
			logger.Int("status", status),
			logger.Duration("latency", time.Since(start)),
			logger.String("ip", c.ClientIP()),
		}
		if q := c.Request.URL.RawQuery; q != "" {
			fields = append(fields, logger.String("query", q))
		}
		if errs := c.Errors.ByType(gin.ErrorTypePrivate).String(); errs != "" {
			fields = append(fields, logger.String("errors", errs))
		}

		msg := c.Request.Method + " " + c.Request.URL.Path
		switch {
		case status >= http.StatusInternalServerError:
			log.Error(msg, fields...)
		case status >= http.StatusBadRequest:
			log.Warn(msg, fields...)
		case isLogViewerRequest(c):
			// 日志页面开着实时刷新时每隔几秒请求一次，记成 info 会把真正的日志淹没
			log.Debug(msg, fields...)
		default:
			log.Info(msg, fields...)
		}
	}
}

func isLogViewerRequest(c *gin.Context) bool {
	return c.Request.Method == http.MethodGet && strings.HasPrefix(c.Request.URL.Path, "/admin/logs")
}

// Recovery 捕获 handler 中的 panic，记录调用栈后返回 500，替代 gin.Recovery()
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		logger.Named("http").Error("Panic recovered",
			logger.Any("panic", err),
			logger.String("method", c.Request.Method),
			logger.String("path", c.Request.URL.Path),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
