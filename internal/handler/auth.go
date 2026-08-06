package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type AuthHandler struct {
	sessions *auth.Manager
}

func NewAuthHandler(sessions *auth.Manager) *AuthHandler {
	return &AuthHandler{sessions: sessions}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if !h.sessions.Authenticate(req.Username, req.Password) {
		response.Fail(c, errcode.Unauthorized)
		return
	}
	c.Set("audit_login_user", req.Username)
	h.sessions.IssueCookie(c, req.Username)
	response.OK(c, gin.H{"username": req.Username})
}

// Logout POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	h.sessions.ClearCookie(c)
	response.OK(c, gin.H{"ok": true})
}

// Me GET /api/v1/auth/me — 未登录返回 40101；公开路由，不经 RequireAuth。
func (h *AuthHandler) Me(c *gin.Context) {
	if !h.sessions.Enabled() {
		response.OK(c, gin.H{"username": "anonymous", "auth_disabled": true})
		return
	}
	username, err := h.sessions.Username(c)
	if err != nil {
		response.Fail(c, errcode.Unauthorized)
		return
	}
	response.OK(c, gin.H{"username": username})
}
