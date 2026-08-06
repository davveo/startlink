package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/app/notify"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type NotificationHandler struct {
	svc *notify.Service
}

func NewNotificationHandler(svc *notify.Service) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

func (h *NotificationHandler) List(c *gin.Context) {
	var q domain.ListNotificationQuery
	_ = c.ShouldBindQuery(&q)
	if c.Query("unread_only") == "1" {
		q.UnreadOnly = true
	}
	res, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	n, err := h.svc.UnreadCount(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"count": n})
}

func (h *NotificationHandler) MarkRead(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	n, err := h.svc.MarkAllRead(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"updated": n})
}

// Stream GET /api/v1/notifications/stream — SSE 实时推送
func (h *NotificationHandler) Stream(c *gin.Context) {
	hub := h.svc.Hub()
	if hub == nil {
		response.Fail(c, errcode.Internal)
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	// 连接时先推一次当前未读数
	if n, err := h.svc.UnreadCount(c.Request.Context()); err == nil {
		writeSSE(c.Writer, "unread", notify.Event{Type: "unread", UnreadCount: n})
		c.Writer.Flush()
	}

	events, cancel := hub.Subscribe()
	defer cancel()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-c.Request.Context().Done():
			return false
		case evt, ok := <-events:
			if !ok {
				return false
			}
			name := evt.Type
			if name == "" {
				name = "notification"
			}
			writeSSE(w, name, evt)
			return true
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			return true
		}
	})
}

func writeSSE(w io.Writer, event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
