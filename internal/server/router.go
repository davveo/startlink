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
	r.Use(gin.Recovery(), gin.Logger())

	r.GET("/healthz", handler.Health)

	api := r.Group("/api/v1")
	{
		api.POST("/campaigns", deps.Campaign.Create)
		api.GET("/campaigns/biz/:biz_id", deps.Campaign.GetByBizID)
		api.GET("/campaigns/:id", deps.Campaign.Get)
		api.GET("/campaigns/:id/progress", deps.Campaign.Progress)
		api.POST("/campaigns/:id/cancel", deps.Campaign.Cancel)
		api.POST("/campaigns/:id/pause", deps.Campaign.Pause)
		api.POST("/campaigns/:id/resume", deps.Campaign.Resume)
		api.POST("/campaigns/:id/retry", deps.Campaign.Retry)
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
