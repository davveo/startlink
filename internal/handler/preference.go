package handler

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/app/preference"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type PreferenceHandler struct {
	svc *preference.Service
}

func NewPreferenceHandler(svc *preference.Service) *PreferenceHandler {
	return &PreferenceHandler{svc: svc}
}

func (h *PreferenceHandler) Get(c *gin.Context) {
	userID := strings.TrimSpace(c.Param("user_id"))
	if userID == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	view, err := h.svc.Get(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *PreferenceHandler) Upsert(c *gin.Context) {
	userID := strings.TrimSpace(c.Param("user_id"))
	if userID == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.PreferenceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	fillActor(c, &in)
	view, err := h.svc.Upsert(c.Request.Context(), userID, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *PreferenceHandler) List(c *gin.Context) {
	var q domain.ListPreferenceQuery
	_ = c.ShouldBindQuery(&q)
	if v, ok := parseBoolQuery(c.Query("marketing_opt_out")); ok {
		q.MarketingOptOut = &v
	}
	res, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *PreferenceHandler) Delete(c *gin.Context) {
	userID := strings.TrimSpace(c.Param("user_id"))
	if userID == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.PreferenceInput
	_ = c.ShouldBindJSON(&in)
	fillActor(c, &in)
	deleted, err := h.svc.Delete(c.Request.Context(), userID, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": deleted})
}

func (h *PreferenceHandler) ListConsent(c *gin.Context) {
	var q domain.ListConsentLogQuery
	_ = c.ShouldBindQuery(&q)
	if t, ok := parseTimeQuery(c.Query("since")); ok {
		q.Since = &t
	}
	if t, ok := parseTimeQuery(c.Query("until")); ok {
		q.Until = &t
	}
	res, err := h.svc.ListConsent(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// fillActor 操作人以登录态为准，请求体里的 operator 只是无登录态（内部 API）时的兜底
func fillActor(c *gin.Context, in *domain.PreferenceInput) {
	if operator := auth.UsernameFromContext(c); operator != "" {
		in.Operator = operator
	}
	if strings.TrimSpace(in.Source) == "" {
		in.Source = "console"
	}
}

func parseBoolQuery(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true, true
	case "0", "false":
		return false, true
	default:
		return false, false
	}
}

// parseTimeQuery 接受 RFC3339 与 datetime-local 控件产出的两种短格式
func parseTimeQuery(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
