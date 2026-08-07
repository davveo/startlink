package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/auth"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

// RBACHandler 角色 / 权限 / 用户管理（MySQL）。
type RBACHandler struct {
	sessions *auth.Manager
}

func NewRBACHandler(sessions *auth.Manager) *RBACHandler {
	return &RBACHandler{sessions: sessions}
}

func (h *RBACHandler) persistNote() gin.H {
	return gin.H{
		"mode": "mysql",
		"note": "用户/角色/权限存 MySQL。密码只保存 bcrypt 哈希；YAML 仅用于库空 seed。",
	}
}

func findRole(items []auth.RoleDef, code string) any {
	for _, r := range items {
		if r.Role == code {
			return r
		}
	}
	return nil
}

func (h *RBACHandler) Catalog(c *gin.Context) {
	response.OK(c, gin.H{
		"roles":       h.sessions.ListRoles(),
		"permissions": h.sessions.ListPermissionMetas(),
		"persistence": h.persistNote(),
	})
}

// ListPermissions GET /api/v1/rbac/permissions?page=&page_size=&keyword=&group=&kind=
func (h *RBACHandler) ListPermissions(c *gin.Context) {
	var q domain.ListPermissionQuery
	_ = c.ShouldBindQuery(&q)
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	items, total, err := h.sessions.ListPermissionsPage(q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items, "total": total, "page": q.Page, "page_size": q.PageSize})
}

type upsertPermRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

// CreatePermission POST /api/v1/rbac/permissions
func (h *RBACHandler) CreatePermission(c *gin.Context) {
	var req upsertPermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.sessions.CreatePermission(req.Code, req.Name, req.Group, req.Kind, req.Description); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true, "code": strings.TrimSpace(req.Code), "persisted": true})
}

// UpdatePermission PUT /api/v1/rbac/permissions/:code
func (h *RBACHandler) UpdatePermission(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	var req upsertPermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.sessions.UpdatePermission(code, req.Name, req.Group, req.Kind, req.Description); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true, "code": code, "persisted": true})
}

func (h *RBACHandler) ListRoles(c *gin.Context) {
	response.OK(c, gin.H{
		"items":       h.sessions.ListRoles(),
		"persistence": h.persistNote(),
	})
}

type upsertRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Role        string   `json:"role"`
}

func (h *RBACHandler) CreateRole(c *gin.Context) {
	var req upsertRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Role) == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	code := strings.TrimSpace(req.Role)
	if err := h.sessions.UpsertRole(code, req.Name, req.Description, req.Permissions, true); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"role": findRole(h.sessions.ListRoles(), code), "persisted": true})
}

func (h *RBACHandler) UpdateRole(c *gin.Context) {
	roleID := strings.TrimSpace(c.Param("role"))
	var req upsertRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.sessions.UpsertRole(roleID, req.Name, req.Description, req.Permissions, false); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"role": findRole(h.sessions.ListRoles(), roleID), "persisted": true})
}

func (h *RBACHandler) ListUsers(c *gin.Context) {
	response.OK(c, gin.H{"items": h.sessions.ListAccounts()})
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
}

func (h *RBACHandler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.sessions.CreateUser(req.Username, req.Password, req.Role, req.DisplayName); err != nil {
		response.Fail(c, err)
		return
	}
	info := h.sessions.InfoFor(strings.TrimSpace(req.Username))
	response.OK(c, gin.H{
		"username":     info.Username,
		"display_name": info.DisplayName,
		"role":         info.Role,
		"roles":        info.Roles,
		"permissions":  info.Permissions,
		"persisted":    true,
	})
}

type updateUserRequest struct {
	Role        *string `json:"role"`
	Password    *string `json:"password"`
	Enabled     *bool   `json:"enabled"`
	DisplayName *string `json:"display_name"`
}

func (h *RBACHandler) UpdateUser(c *gin.Context) {
	username := strings.TrimSpace(c.Param("username"))
	if username == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	actor := auth.UsernameFromContext(c)
	if err := h.sessions.UpdateUser(actor, username, req.Role, req.Password, req.Enabled, req.DisplayName); err != nil {
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
		"persisted":    true,
	})
}

type setRoleRequest struct {
	Role string `json:"role"`
}

func (h *RBACHandler) SetUserRole(c *gin.Context) {
	username := strings.TrimSpace(c.Param("username"))
	if username == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var req setRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Role == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	actor := auth.UsernameFromContext(c)
	if err := h.sessions.SetUserRole(actor, username, req.Role); err != nil {
		response.Fail(c, err)
		return
	}
	info := h.sessions.InfoFor(username)
	response.OK(c, gin.H{
		"username": info.Username, "role": info.Role, "roles": info.Roles,
		"permissions": info.Permissions, "persisted": true,
	})
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

// ResetPassword POST /api/v1/rbac/users/:username/reset-password
func (h *RBACHandler) ResetPassword(c *gin.Context) {
	username := strings.TrimSpace(c.Param("username"))
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Password) == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.sessions.ResetPassword(username, req.Password); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true, "username": username, "persisted": true})
}
