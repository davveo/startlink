package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/handler"
	"github.com/starlink/push/internal/port"
)

type Deps struct {
	Auth         *handler.AuthHandler
	RBAC         *handler.RBACHandler
	Sessions     *auth.Manager
	Campaign     *handler.CampaignHandler
	Callback     *handler.CallbackHandler
	Template     *handler.TemplateHandler
	Notification *handler.NotificationHandler
	Audit        *handler.AuditHandler
	Segment      *handler.SegmentHandler
	Preference   *handler.PreferenceHandler
	Trace        *handler.TraceHandler
	AuditRepo    port.AuditLogRepository
	Ready        gin.HandlerFunc
}

func New(cfg config.ServerConfig, deps Deps) *gin.Engine {
	if cfg.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	gin.EnableJsonDecoderDisallowUnknownFields()
	gin.EnableJsonDecoderUseNumber()
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), RequestIDMiddleware(), bodyLimitMiddleware(cfg.MaxBodyBytes), corsMiddleware(cfg.AllowedOrigins))
	if deps.AuditRepo != nil {
		r.Use(AuditMiddleware(deps.AuditRepo))
	}

	r.GET("/healthz", handler.Health)
	if deps.Ready != nil {
		r.GET("/readyz", deps.Ready)
	}

	api := r.Group("/api/v1")
	{
		// 公开：登录 / 当前用户 / 渠道回执
		api.POST("/auth/login", deps.Auth.Login)
		api.GET("/auth/me", deps.Auth.Me)
		api.POST("/callbacks/receipt", deps.Callback.Receive)

		protected := api.Group("")
		if deps.Sessions != nil && deps.Sessions.Enabled() {
			protected.Use(deps.Sessions.RequireAuth())
		}
		{
			protected.POST("/auth/logout", deps.Auth.Logout)

			protected.GET("/overview", deps.Campaign.Overview)

			// 活动：读接口任意登录用户；写操作按权限码
			protected.GET("/campaigns", deps.Campaign.List)
			protected.GET("/campaigns/biz/:biz_id", deps.Campaign.GetByBizID)
			protected.GET("/campaigns/:id/subtasks/:sub_id", deps.Campaign.GetSubTask)
			protected.GET("/campaigns/:id/subtasks", deps.Campaign.ListSubTasks)
			protected.GET("/campaigns/:id/funnel", deps.Campaign.Funnel)
			protected.GET("/campaigns/:id/failures", deps.Campaign.Failures)
			protected.GET("/campaigns/:id/experiment", deps.Campaign.Experiment)
			protected.GET("/campaigns/:id/records", deps.Campaign.ListRecords)
			protected.GET("/campaigns/:id", deps.Campaign.Get)
			protected.GET("/campaigns/:id/progress", deps.Campaign.Progress)
			protected.GET("/exports/:id", deps.Campaign.GetExport)
			protected.GET("/exports/:id/download", deps.Campaign.DownloadExport)
			protected.GET("/channels", deps.Campaign.ListChannels)

			perm := noopPerm
			if deps.Sessions != nil {
				perm = deps.Sessions.RequirePermission
			}
			protected.POST("/campaigns", perm(auth.PermCampaignCreate), deps.Campaign.Create)
			protected.POST("/campaigns/preflight", perm(auth.PermCampaignPreflight), deps.Campaign.Preflight)
			protected.POST("/campaigns/dry-run", perm(auth.PermCampaignDryRun), deps.Campaign.DryRun)
			protected.POST("/campaigns/batch/:action", perm(auth.PermCampaignBatch), deps.Campaign.Batch)
			protected.POST("/audiences/estimate", perm(auth.PermAudienceEstimate), deps.Campaign.EstimateAudience)
			protected.GET("/campaigns/:id/export", perm(auth.PermCampaignExport), deps.Campaign.ExportSync)
			protected.POST("/campaigns/:id/exports", perm(auth.PermCampaignExport), deps.Campaign.ExportAsync)
			protected.POST("/campaigns/:id/copy", perm(auth.PermCampaignCopy), deps.Campaign.Copy)
			protected.POST("/campaigns/:id/publish", perm(auth.PermCampaignPublish), deps.Campaign.Publish)
			protected.PUT("/campaigns/:id", perm(auth.PermCampaignUpdate), deps.Campaign.UpdateDraft)
			protected.POST("/campaigns/:id/cancel", perm(auth.PermCampaignCancel), deps.Campaign.Cancel)
			protected.POST("/campaigns/:id/pause", perm(auth.PermCampaignPause), deps.Campaign.Pause)
			protected.POST("/campaigns/:id/resume", perm(auth.PermCampaignResume), deps.Campaign.Resume)
			protected.POST("/campaigns/:id/retry", perm(auth.PermCampaignRetry), deps.Campaign.Retry)

			// 模板中心
			protected.GET("/templates", deps.Template.List)
			protected.GET("/templates/code/:code", deps.Template.GetByCode)
			protected.GET("/templates/:id", deps.Template.Get)
			protected.GET("/templates/:id/versions", deps.Template.ListVersions)
			protected.POST("/templates/preview", deps.Template.Preview)

			protected.POST("/templates", perm(auth.PermTemplateCreate), deps.Template.Create)
			protected.PUT("/templates/:id", perm(auth.PermTemplateEdit), deps.Template.Update)
			protected.DELETE("/templates/:id", perm(auth.PermTemplateDelete), deps.Template.Delete)
			protected.POST("/templates/:id/submit", perm(auth.PermTemplateSubmit), deps.Template.Submit)
			protected.POST("/templates/:id/approve", perm(auth.PermTemplateApprove), deps.Template.Approve)
			protected.POST("/templates/:id/reject", perm(auth.PermTemplateReject), deps.Template.Reject)
			protected.POST("/templates/:id/disable", perm(auth.PermTemplateDisable), deps.Template.Disable)
			protected.POST("/templates/:id/enable", perm(auth.PermTemplateEnable), deps.Template.Enable)
			protected.POST("/templates/:id/rollback", perm(auth.PermTemplateRollback), deps.Template.Rollback)

			// 消息通知中心
			if deps.Notification != nil {
				protected.GET("/notifications/stream", deps.Notification.Stream)
				protected.GET("/notifications", deps.Notification.List)
				protected.GET("/notifications/unread-count", deps.Notification.UnreadCount)
				protected.POST("/notifications/read-all", perm(auth.PermNotificationRead), deps.Notification.MarkAllRead)
				protected.POST("/notifications/:id/read", perm(auth.PermNotificationRead), deps.Notification.MarkRead)
			}

			// 人群资产：读接口任意登录用户，写操作按权限码
			if deps.Segment != nil {
				protected.GET("/segments", deps.Segment.List)
				protected.GET("/segments/:code", deps.Segment.Get)
				protected.POST("/segments", perm(auth.PermSegmentManage), deps.Segment.Create)
				protected.PUT("/segments/:code", perm(auth.PermSegmentManage), deps.Segment.Update)
				protected.DELETE("/segments/:code", perm(auth.PermSegmentManage), deps.Segment.Delete)
				protected.POST("/segments/:code/refresh", perm(auth.PermSegmentManage), deps.Segment.Refresh)

				protected.GET("/suppressions", deps.Segment.ListSuppressions)
				protected.GET("/suppressions/stats", deps.Segment.SuppressionStats)
				protected.POST("/suppressions", perm(auth.PermSuppressionManage), deps.Segment.AddSuppressions)
				protected.DELETE("/suppressions", perm(auth.PermSuppressionManage), deps.Segment.RemoveSuppression)
			}

			// 用户偏好中心：偏好涉及用户个人数据，读也要显式授权
			if deps.Preference != nil {
				protected.GET("/preferences", perm(auth.PermPreferenceView), deps.Preference.List)
				protected.GET("/preferences/:user_id", perm(auth.PermPreferenceView), deps.Preference.Get)
				protected.PUT("/preferences/:user_id", perm(auth.PermPreferenceManage), deps.Preference.Upsert)
				protected.DELETE("/preferences/:user_id", perm(auth.PermPreferenceManage), deps.Preference.Delete)
				protected.GET("/consent-logs", perm(auth.PermPreferenceView), deps.Preference.ListConsent)
			}

			// 全链路追踪：按 trace_id 查看活动消费路径与异常原因
			if deps.Trace != nil {
				protected.GET("/traces", perm(auth.PermTraceView), deps.Trace.List)
				protected.GET("/traces/:trace_id", perm(auth.PermTraceView), deps.Trace.Get)
				protected.GET("/trace-events", perm(auth.PermTraceView), deps.Trace.ListEvents)
			}

			// 审计日志（查询本身不写审计，因 GET 被中间件跳过）
			if deps.Audit != nil {
				protected.GET("/audit-logs", perm(auth.PermAuditView), deps.Audit.List)
			}

			// RBAC：角色 / 权限目录 / 用户（需 rbac.manage）
			if deps.RBAC != nil {
				protected.GET("/rbac/catalog", perm(auth.PermRBACManage), deps.RBAC.Catalog)
				protected.GET("/rbac/permissions", perm(auth.PermRBACManage), deps.RBAC.ListPermissions)
				protected.POST("/rbac/permissions", perm(auth.PermRBACManage), deps.RBAC.CreatePermission)
				protected.PUT("/rbac/permissions/:code", perm(auth.PermRBACManage), deps.RBAC.UpdatePermission)
				protected.GET("/rbac/roles", perm(auth.PermRBACManage), deps.RBAC.ListRoles)
				protected.POST("/rbac/roles", perm(auth.PermRBACManage), deps.RBAC.CreateRole)
				protected.PUT("/rbac/roles/:role", perm(auth.PermRBACManage), deps.RBAC.UpdateRole)
				protected.GET("/rbac/users", perm(auth.PermRBACManage), deps.RBAC.ListUsers)
				protected.POST("/rbac/users", perm(auth.PermRBACManage), deps.RBAC.CreateUser)
				protected.POST("/rbac/users/:username/reset-password", perm(auth.PermRBACManage), deps.RBAC.ResetPassword)
				protected.PUT("/rbac/users/:username/role", perm(auth.PermRBACManage), deps.RBAC.SetUserRole)
				protected.PUT("/rbac/users/:username", perm(auth.PermRBACManage), deps.RBAC.UpdateUser)
			}
		}
	}
	return r
}

// noopPerm 无 Session Manager 时占位（测试/特殊装配）。
func noopPerm(_ string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}

// corsMiddleware 便于本地 Vite 直连 API；Compose 下前端走 nginx 同源反代可不依赖 CORS。
func bodyLimitMiddleware(limit int64) gin.HandlerFunc {
	if limit <= 0 {
		limit = 2 << 20
	}
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = strings.TrimRight(strings.TrimSpace(origin), "/"); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin == "" {
			c.Next()
			return
		}
		// 浏览器对同源的写请求同样发送 Origin；此处放行，否则从 allowed_origins
		// 之外的地址（127.0.0.1、内网 IP、自定义域名）打开控制台会 403。
		if origin == requestOrigin(c.Request) {
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Starlink-Timestamp, X-Starlink-Nonce, X-Starlink-Signature")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// requestOrigin 还原本次请求自身的 origin，兼容 nginx 反代（X-Forwarded-Proto + Host）。
func requestOrigin(r *http.Request) string {
	if r == nil || r.Host == "" {
		return ""
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.ToLower(strings.TrimSpace(strings.Split(proto, ",")[0]))
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
