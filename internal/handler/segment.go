package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/app/segment"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type SegmentHandler struct {
	svc *segment.Service
}

func NewSegmentHandler(svc *segment.Service) *SegmentHandler {
	return &SegmentHandler{svc: svc}
}

// List GET /api/v1/segments
func (h *SegmentHandler) List(c *gin.Context) {
	var q domain.ListSegmentQuery
	_ = c.ShouldBindQuery(&q)
	res, err := h.svc.ListSegments(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Get GET /api/v1/segments/:code
func (h *SegmentHandler) Get(c *gin.Context) {
	res, err := h.svc.GetSegment(c.Request.Context(), c.Param("code"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Create POST /api/v1/segments
func (h *SegmentHandler) Create(c *gin.Context) {
	var in domain.SegmentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	in.Operator = operatorOf(c, in.Operator)
	seg, err := h.svc.CreateSegment(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, seg)
}

// Update PUT /api/v1/segments/:code
func (h *SegmentHandler) Update(c *gin.Context) {
	var in domain.SegmentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	in.Operator = operatorOf(c, in.Operator)
	seg, err := h.svc.UpdateSegment(c.Request.Context(), c.Param("code"), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, seg)
}

// Delete DELETE /api/v1/segments/:code
func (h *SegmentHandler) Delete(c *gin.Context) {
	if err := h.svc.DeleteSegment(c.Request.Context(), c.Param("code")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// Refresh POST /api/v1/segments/:code/refresh
func (h *SegmentHandler) Refresh(c *gin.Context) {
	res, err := h.svc.RefreshSegment(c.Request.Context(), c.Param("code"), operatorOf(c, ""))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// ListSuppressions GET /api/v1/suppressions
func (h *SegmentHandler) ListSuppressions(c *gin.Context) {
	var q domain.ListSuppressionQuery
	_ = c.ShouldBindQuery(&q)
	res, err := h.svc.ListSuppressions(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// SuppressionStats GET /api/v1/suppressions/stats
func (h *SegmentHandler) SuppressionStats(c *gin.Context) {
	res, err := h.svc.SuppressionStats(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// AddSuppressions POST /api/v1/suppressions
func (h *SegmentHandler) AddSuppressions(c *gin.Context) {
	var in domain.SuppressionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	in.Operator = operatorOf(c, in.Operator)
	res, err := h.svc.AddSuppressions(c.Request.Context(), in)
	if err != nil {
		// 快路径同步失败时 DB 已落库，service 把已入库条数写进了错误文案，
		// 别让运营以为整批白导了又重来一遍
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// RemoveSuppression DELETE /api/v1/suppressions?kind=&user_id=&channel=
func (h *SegmentHandler) RemoveSuppression(c *gin.Context) {
	kind := domain.SuppressionKind(strings.TrimSpace(c.Query("kind")))
	res, err := h.svc.RemoveSuppression(c.Request.Context(), kind, c.Query("user_id"), c.Query("channel"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// operatorOf 优先取登录态用户名，鉴权关闭时回退请求体里的 operator
func operatorOf(c *gin.Context, fallback string) string {
	if u := strings.TrimSpace(auth.UsernameFromContext(c)); u != "" {
		return u
	}
	return strings.TrimSpace(fallback)
}
