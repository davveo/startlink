package server

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/domain"
)

type ctxKey string

const (
	ctxRequestID ctxKey = "request_id"
	headerReqID         = "X-Request-Id"
	headerTrace         = "X-Trace-Id"
)

// RequestIDMiddleware 为每个 HTTP 请求生成/透传 request_id。
// 注意：request_id 是请求级；活动级 trace_id 在创建活动时生成并落库。
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := strings.TrimSpace(c.GetHeader(headerReqID))
		if rid == "" {
			rid = domain.NewRequestID()
		}
		c.Set(string(ctxRequestID), rid)
		c.Writer.Header().Set(headerReqID, rid)
		ctx := context.WithValue(c.Request.Context(), ctxRequestID, rid)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequestIDFromContext 取请求 ID
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// RequestIDFromGin 从 gin.Context 取
func RequestIDFromGin(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(string(ctxRequestID)); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return RequestIDFromContext(c.Request.Context())
}

// LogWithRequest 带 request_id 的 slog 便捷包装
func LogWithRequest(ctx context.Context, level slog.Level, msg string, args ...any) {
	rid := RequestIDFromContext(ctx)
	if rid != "" {
		args = append([]any{"request_id", rid}, args...)
	}
	slog.Log(ctx, level, msg, args...)
}
