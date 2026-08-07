package server

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// AuditMiddleware 对非 GET/HEAD/OPTIONS 的 API 写操作记一条审计（跳过 healthz / 静态）
func AuditMiddleware(repo port.AuditLogRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		switch method {
		case "GET", "HEAD", "OPTIONS":
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == "/healthz" || strings.HasPrefix(path, "/web-healthz") {
			c.Next()
			return
		}
		// 仅审计 /api/v1
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}
		// 渠道回执是机器流量，量级与发送量同级，记审计会撑爆表并按请求打满 goroutine。
		if strings.HasPrefix(path, "/api/v1/callbacks/") {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		operator := auth.UsernameFromContext(c)
		if operator == "" {
			// 登录成功后 cookie 已签发，尝试从 body 旁路（middleware 后无法读 body）；用查询参数兜底
			if path == "/api/v1/auth/login" {
				operator = c.GetString("audit_login_user")
			}
		}
		if operator == "" {
			operator = "anonymous"
		}

		status := c.Writer.Status()
		success := status > 0 && status < 400
		action, resType, resID := inferAuditAction(method, c.FullPath(), path)

		entry := &domain.AuditLog{
			Operator:     operator,
			Action:       action,
			ResourceType: resType,
			ResourceID:   resID,
			Method:       method,
			Path:         path,
			IP:           c.ClientIP(),
			Detail:       "",
			Success:      success,
			CreatedAt:    start,
		}
		if !success {
			entry.Detail = `{"status":` + strconv.Itoa(status) + `}`
		}

		go func(log *domain.AuditLog) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := repo.Create(ctx, log); err != nil {
				slog.Warn("audit log write failed", "action", log.Action, "err", err)
			}
		}(entry)
	}
}

func inferAuditAction(method, fullPath, rawPath string) (action, resType, resID string) {
	p := fullPath
	if p == "" {
		p = rawPath
	}
	switch {
	case p == "/api/v1/auth/login":
		return "auth.login", "auth", ""
	case p == "/api/v1/auth/logout":
		return "auth.logout", "auth", ""
	case method == "POST" && p == "/api/v1/campaigns":
		return "campaign.create", "campaign", ""
	case method == "PUT" && strings.HasPrefix(p, "/api/v1/campaigns/:id"):
		return "campaign.update", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.HasSuffix(p, "/publish"):
		return "campaign.publish", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.HasSuffix(p, "/cancel"):
		return "campaign.cancel", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.HasSuffix(p, "/pause"):
		return "campaign.pause", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.HasSuffix(p, "/resume"):
		return "campaign.resume", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.HasSuffix(p, "/retry"):
		return "campaign.retry", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.HasSuffix(p, "/copy"):
		return "campaign.copy", "campaign", paramID(rawPath, "/api/v1/campaigns/")
	case strings.Contains(p, "/campaigns/batch/"):
		return "campaign.batch", "campaign", ""
	case p == "/api/v1/campaigns/preflight":
		return "campaign.preflight", "campaign", ""
	case p == "/api/v1/campaigns/dry-run":
		return "campaign.dry_run", "campaign", ""
	case p == "/api/v1/audiences/estimate":
		return "audience.estimate", "audience", ""
	case strings.HasSuffix(p, "/exports") && method == "POST":
		return "export.create", "export", paramID(rawPath, "/api/v1/campaigns/")
	case method == "POST" && p == "/api/v1/templates":
		return "template.create", "template", ""
	case method == "PUT" && strings.HasPrefix(p, "/api/v1/templates/:id"):
		return "template.update", "template", paramID(rawPath, "/api/v1/templates/")
	case method == "DELETE" && strings.HasPrefix(p, "/api/v1/templates/:id"):
		return "template.delete", "template", paramID(rawPath, "/api/v1/templates/")
	case strings.HasSuffix(p, "/submit"):
		return "template.submit", "template", paramID(rawPath, "/api/v1/templates/")
	case strings.HasSuffix(p, "/approve"):
		return "template.approve", "template", paramID(rawPath, "/api/v1/templates/")
	case strings.HasSuffix(p, "/reject"):
		return "template.reject", "template", paramID(rawPath, "/api/v1/templates/")
	case strings.HasSuffix(p, "/disable"):
		return "template.disable", "template", paramID(rawPath, "/api/v1/templates/")
	case strings.HasSuffix(p, "/enable"):
		return "template.enable", "template", paramID(rawPath, "/api/v1/templates/")
	case strings.HasSuffix(p, "/rollback"):
		return "template.rollback", "template", paramID(rawPath, "/api/v1/templates/")
	case p == "/api/v1/templates/preview":
		return "template.preview", "template", ""
	case p == "/api/v1/notifications/read-all":
		return "notification.read_all", "notification", ""
	case strings.HasSuffix(p, "/read") && strings.Contains(p, "/notifications/"):
		return "notification.read", "notification", paramID(rawPath, "/api/v1/notifications/")
	case strings.Contains(p, "/rbac/users/") && strings.HasSuffix(p, "/role"):
		return "rbac.set_role", "rbac", paramID(rawPath, "/api/v1/rbac/users/")
	case method == "POST" && p == "/api/v1/rbac/users":
		return "rbac.user_create", "rbac", ""
	case method == "PUT" && strings.HasPrefix(p, "/api/v1/rbac/users/:username"):
		return "rbac.user_update", "rbac", paramID(rawPath, "/api/v1/rbac/users/")
	case strings.HasSuffix(p, "/reset-password") && strings.Contains(p, "/rbac/users/"):
		return "rbac.reset_password", "rbac", paramID(rawPath, "/api/v1/rbac/users/")
	case method == "POST" && p == "/api/v1/rbac/roles":
		return "rbac.role_create", "rbac", ""
	case method == "PUT" && strings.HasPrefix(p, "/api/v1/rbac/roles/:role"):
		return "rbac.role_update", "rbac", paramID(rawPath, "/api/v1/rbac/roles/")
	case method == "POST" && p == "/api/v1/rbac/permissions":
		return "rbac.perm_create", "rbac", ""
	case method == "PUT" && strings.HasPrefix(p, "/api/v1/rbac/permissions/:code"):
		return "rbac.perm_update", "rbac", paramID(rawPath, "/api/v1/rbac/permissions/")
	default:
		return strings.ToLower(method) + " " + p, "", ""
	}
}

func paramID(rawPath, prefix string) string {
	rest := strings.TrimPrefix(rawPath, prefix)
	if rest == rawPath {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}
