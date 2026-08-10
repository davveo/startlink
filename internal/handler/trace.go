package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/app/trace"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type TraceHandler struct {
	svc *trace.Service
}

func NewTraceHandler(svc *trace.Service) *TraceHandler {
	return &TraceHandler{svc: svc}
}

// List GET /api/v1/traces — 链路摘要列表
func (h *TraceHandler) List(c *gin.Context) {
	var q domain.ListTraceQuery
	_ = c.ShouldBindQuery(&q)
	if id := strings.TrimSpace(c.Query("task_id")); id != "" && q.MainTaskID == 0 {
		if n, err := strconv.ParseUint(id, 10, 64); err == nil {
			q.MainTaskID = n
		}
	}
	res, err := h.svc.ListSummaries(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Get GET /api/v1/traces/:trace_id — 单条链路时间线（分页；支持 level/stage/anomaly_only）
func (h *TraceHandler) Get(c *gin.Context) {
	var q domain.ListTraceQuery
	_ = c.ShouldBindQuery(&q)
	q.TraceID = c.Param("trace_id")
	if strings.EqualFold(c.Query("anomaly_only"), "1") || strings.EqualFold(c.Query("anomaly_only"), "true") {
		q.AnomalyOnly = true
	}
	res, err := h.svc.GetTimeline(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("X-Trace-Id", res.TraceID)
	response.OK(c, res)
}

// ListEvents GET /api/v1/trace-events — 扁平事件检索（按 user/level 等）
func (h *TraceHandler) ListEvents(c *gin.Context) {
	var q domain.ListTraceQuery
	_ = c.ShouldBindQuery(&q)
	if strings.EqualFold(c.Query("anomaly_only"), "1") || strings.EqualFold(c.Query("anomaly_only"), "true") {
		q.AnomalyOnly = true
	}
	if strings.TrimSpace(q.TraceID) == "" && strings.TrimSpace(q.BizID) == "" && q.MainTaskID == 0 && strings.TrimSpace(q.UserID) == "" {
		response.Fail(c, errcode.New(40001, "至少提供 trace_id / biz_id / main_task_id / user_id 之一"))
		return
	}
	items, total, err := h.svc.ListEvents(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	page, size := q.Page, q.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	response.OK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
