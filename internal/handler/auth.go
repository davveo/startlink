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
	// 先记录待审计的用户名：失败登录也要能看出被尝试的是哪个账号，否则爆破在审计里全是 anonymous。
	c.Set("audit_login_user", req.Username)
	username, ok := h.sessions.Authenticate(req.Username, req.Password)
	if !ok {
		response.Fail(c, errcode.Unauthorized)
		return
	}
	// 以库中的用户名为准签发，避免大小写差异让同一账号在审计里分裂成两个身份。
	c.Set("audit_login_user", username)
	if err := h.sessions.IssueCookie(c, username); err != nil {
		response.Fail(c, err)
		return
	}
	info := h.sessions.InfoFor(username)
	response.OK(c, gin.H{
		"username":     info.Username,
		"display_name": info.DisplayName,
		"role":         info.Role,
		"roles":        info.Roles,
		"permissions":  info.Permissions,
	})
}

// Logout POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	h.sessions.ClearCookie(c)
	response.OK(c, gin.H{"ok": true})
}

// Me GET /api/v1/auth/me — 未登录返回 40101；公开路由，不经 RequireAuth。
func (h *AuthHandler) Me(c *gin.Context) {
	if !h.sessions.Enabled() {
		response.OK(c, gin.H{
			"username":      "anonymous",
			"auth_disabled": true,
			"role":          auth.RoleAdmin,
			"roles":         []string{auth.RoleAdmin},
			"permissions":   auth.AllPermissions(),
		})
		return
	}
	username, err := h.sessions.Username(c)
	if err != nil {
		response.Fail(c, errcode.Unauthorized)
		return
	}
	info := h.sessions.InfoFor(username)
	response.OK(c, gin.H{
		"username":     info.Username,
		"display_name": info.DisplayName,
		"role":         info.Role,
		"roles":        info.Roles,
		"permissions":  info.Permissions,
	})
}
