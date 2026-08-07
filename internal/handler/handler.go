package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/adapter/channel"
	"github.com/starlink/push/internal/app/callback"
	"github.com/starlink/push/internal/app/campaign"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type CampaignHandler struct {
	svc      *campaign.Service
	channels *channel.Registry
}

func NewCampaignHandler(svc *campaign.Service, channels *channel.Registry) *CampaignHandler {
	return &CampaignHandler{svc: svc, channels: channels}
}

func (h *CampaignHandler) Create(c *gin.Context) {
	var in domain.CreateCampaignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) List(c *gin.Context) {
	var q domain.ListCampaignQuery
	_ = c.ShouldBindQuery(&q)
	res, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) ListSubTasks(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var q domain.ListSubTaskQuery
	_ = c.ShouldBindQuery(&q)
	res, err := h.svc.ListSubTasks(c.Request.Context(), id, q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) GetSubTask(c *gin.Context) {
	mainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	subID, err := strconv.ParseUint(c.Param("sub_id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.GetSubTask(c.Request.Context(), mainID, subID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	view, err := h.svc.GetProgress(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *CampaignHandler) GetByBizID(c *gin.Context) {
	bizID := c.Param("biz_id")
	view, err := h.svc.GetProgressByBizID(c.Request.Context(), bizID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *CampaignHandler) Progress(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	view, err := h.svc.GetProgress(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

func (h *CampaignHandler) Cancel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Cancel(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Pause(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Pause(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Resume(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Resume(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Retry(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Retry(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) ListChannels(c *gin.Context) {
	response.OK(c, gin.H{"channels": h.channels.List()})
}

func (h *CampaignHandler) Overview(c *gin.Context) {
	view, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

type CallbackHandler struct {
	svc      *callback.Service
	verifier *callback.Verifier
}

func NewCallbackHandler(svc *callback.Service, verifier *callback.Verifier) *CallbackHandler {
	return &CallbackHandler{svc: svc, verifier: verifier}
}

func (h *CallbackHandler) Receive(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.verifier.Verify(
		c.Request.Context(),
		c.GetHeader("X-Starlink-Timestamp"),
		c.GetHeader("X-Starlink-Nonce"),
		c.GetHeader("X-Starlink-Signature"),
		raw,
	); err != nil {
		response.Fail(c, err)
		return
	}
	var in callback.ReceiptInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || in.ProviderID == "" || in.Event == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.svc.Handle(c.Request.Context(), in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"accepted": true})
}

func Health(c *gin.Context) {
	response.OK(c, gin.H{"status": "up"})
}

func Readiness(checks ...func(context.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		for _, check := range checks {
			if check != nil {
				if err := check(ctx); err != nil {
					c.JSON(http.StatusServiceUnavailable, response.Body{
						Code: errcode.Internal.Code, Message: "service not ready",
					})
					return
				}
			}
		}
		response.OK(c, gin.H{"status": "ready"})
	}
}
