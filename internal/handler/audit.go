package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/app/audit"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/response"
)

type AuditHandler struct {
	svc *audit.Service
}

func NewAuditHandler(svc *audit.Service) *AuditHandler {
	return &AuditHandler{svc: svc}
}

func (h *AuditHandler) List(c *gin.Context) {
	var q domain.ListAuditLogQuery
	_ = c.ShouldBindQuery(&q)
	if c.Query("success") == "1" || c.Query("success") == "true" {
		v := true
		q.Success = &v
	} else if c.Query("success") == "0" || c.Query("success") == "false" {
		v := false
		q.Success = &v
	}
	res, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
