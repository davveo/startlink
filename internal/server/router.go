package server

import (
	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/internal/handler"
)

type Deps struct {
	Auth     *handler.AuthHandler
	Sessions *auth.Manager
	Campaign *handler.CampaignHandler
	Callback *handler.CallbackHandler
	Template *handler.TemplateHandler
}

func New(mode string, deps Deps) *gin.Engine {
	if mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), corsMiddleware())

	r.GET("/healthz", handler.Health)

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

			protected.POST("/campaigns", deps.Campaign.Create)
			protected.GET("/campaigns", deps.Campaign.List)
			protected.POST("/campaigns/preflight", deps.Campaign.Preflight)
			protected.POST("/campaigns/dry-run", deps.Campaign.DryRun)
			protected.POST("/campaigns/batch/:action", deps.Campaign.Batch)
			protected.POST("/audiences/estimate", deps.Campaign.EstimateAudience)
			protected.GET("/campaigns/biz/:biz_id", deps.Campaign.GetByBizID)
			protected.GET("/campaigns/:id/subtasks/:sub_id", deps.Campaign.GetSubTask)
			protected.GET("/campaigns/:id/subtasks", deps.Campaign.ListSubTasks)
			protected.GET("/campaigns/:id/funnel", deps.Campaign.Funnel)
			protected.GET("/campaigns/:id/failures", deps.Campaign.Failures)
			protected.GET("/campaigns/:id/experiment", deps.Campaign.Experiment)
			protected.GET("/campaigns/:id/records", deps.Campaign.ListRecords)
			protected.GET("/campaigns/:id/export", deps.Campaign.ExportSync)
			protected.POST("/campaigns/:id/exports", deps.Campaign.ExportAsync)
			protected.POST("/campaigns/:id/copy", deps.Campaign.Copy)
			protected.POST("/campaigns/:id/publish", deps.Campaign.Publish)
			protected.PUT("/campaigns/:id", deps.Campaign.UpdateDraft)
			protected.GET("/campaigns/:id", deps.Campaign.Get)
			protected.GET("/campaigns/:id/progress", deps.Campaign.Progress)
			protected.POST("/campaigns/:id/cancel", deps.Campaign.Cancel)
			protected.POST("/campaigns/:id/pause", deps.Campaign.Pause)
			protected.POST("/campaigns/:id/resume", deps.Campaign.Resume)
			protected.POST("/campaigns/:id/retry", deps.Campaign.Retry)
			protected.GET("/exports/:id", deps.Campaign.GetExport)
			protected.GET("/exports/:id/download", deps.Campaign.DownloadExport)
			protected.GET("/channels", deps.Campaign.ListChannels)

			// 模板中心
			protected.POST("/templates", deps.Template.Create)
			protected.GET("/templates", deps.Template.List)
			protected.POST("/templates/preview", deps.Template.Preview)
			protected.GET("/templates/code/:code", deps.Template.GetByCode)
			protected.GET("/templates/:id", deps.Template.Get)
			protected.PUT("/templates/:id", deps.Template.Update)
			protected.DELETE("/templates/:id", deps.Template.Delete)
			protected.POST("/templates/:id/submit", deps.Template.Submit)
			protected.POST("/templates/:id/approve", deps.Template.Approve)
			protected.POST("/templates/:id/reject", deps.Template.Reject)
			protected.POST("/templates/:id/disable", deps.Template.Disable)
			protected.POST("/templates/:id/enable", deps.Template.Enable)
			protected.GET("/templates/:id/versions", deps.Template.ListVersions)
			protected.POST("/templates/:id/rollback", deps.Template.Rollback)
		}
	}
	return r
}

// corsMiddleware 便于本地 Vite 直连 API；Compose 下前端走 nginx 同源反代可不依赖 CORS。
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
