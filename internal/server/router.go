package server

import (
	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/handler"
)

type Deps struct {
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
		api.POST("/campaigns", deps.Campaign.Create)
		api.GET("/campaigns", deps.Campaign.List)
		api.POST("/campaigns/preflight", deps.Campaign.Preflight)
		api.POST("/campaigns/dry-run", deps.Campaign.DryRun)
		api.POST("/campaigns/batch/:action", deps.Campaign.Batch)
		api.POST("/audiences/estimate", deps.Campaign.EstimateAudience)
		api.GET("/campaigns/biz/:biz_id", deps.Campaign.GetByBizID)
		api.GET("/campaigns/:id/subtasks/:sub_id", deps.Campaign.GetSubTask)
		api.GET("/campaigns/:id/subtasks", deps.Campaign.ListSubTasks)
		api.GET("/campaigns/:id/funnel", deps.Campaign.Funnel)
		api.GET("/campaigns/:id/failures", deps.Campaign.Failures)
		api.GET("/campaigns/:id/records", deps.Campaign.ListRecords)
		api.GET("/campaigns/:id/export", deps.Campaign.ExportSync)
		api.POST("/campaigns/:id/exports", deps.Campaign.ExportAsync)
		api.POST("/campaigns/:id/copy", deps.Campaign.Copy)
		api.POST("/campaigns/:id/publish", deps.Campaign.Publish)
		api.PUT("/campaigns/:id", deps.Campaign.UpdateDraft)
		api.GET("/campaigns/:id", deps.Campaign.Get)
		api.GET("/campaigns/:id/progress", deps.Campaign.Progress)
		api.POST("/campaigns/:id/cancel", deps.Campaign.Cancel)
		api.POST("/campaigns/:id/pause", deps.Campaign.Pause)
		api.POST("/campaigns/:id/resume", deps.Campaign.Resume)
		api.POST("/campaigns/:id/retry", deps.Campaign.Retry)
		api.GET("/exports/:id", deps.Campaign.GetExport)
		api.GET("/exports/:id/download", deps.Campaign.DownloadExport)
		api.GET("/channels", deps.Campaign.ListChannels)

		api.POST("/callbacks/receipt", deps.Callback.Receive)

		// 模板中心
		api.POST("/templates", deps.Template.Create)
		api.GET("/templates", deps.Template.List)
		api.GET("/templates/code/:code", deps.Template.GetByCode)
		api.GET("/templates/:id", deps.Template.Get)
		api.PUT("/templates/:id", deps.Template.Update)
		api.DELETE("/templates/:id", deps.Template.Delete)
		api.POST("/templates/:id/submit", deps.Template.Submit)
		api.POST("/templates/:id/approve", deps.Template.Approve)
		api.POST("/templates/:id/reject", deps.Template.Reject)
		api.POST("/templates/:id/disable", deps.Template.Disable)
		api.POST("/templates/:id/enable", deps.Template.Enable)
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
