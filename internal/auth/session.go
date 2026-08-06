package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

const ctxUsernameKey = "auth_username"
const ctxRoleKey = "auth_role"
const ctxPermsKey = "auth_permissions"

// Manager 签发/校验 HMAC 签名的 Session Cookie，并校验配置文件账号。
type Manager struct {
	enabled    bool
	secret     []byte
	cookieName string
	ttl        time.Duration
	users      map[string]string // username -> password
	roles      map[string]string // username -> role
}

func NewManager(cfg config.AuthConfig) *Manager {
	users := make(map[string]string, len(cfg.Users))
	roles := make(map[string]string, len(cfg.Users))
	for _, u := range cfg.Users {
		if u.Username == "" {
			continue
		}
		users[u.Username] = u.Password
		roles[u.Username] = NormalizeRole(u.Role)
	}
	ttlHours := cfg.TTLHours
	if ttlHours <= 0 {
		ttlHours = 24
	}
	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = "starlink_session"
	}
	return &Manager{
		enabled:    cfg.Enabled,
		secret:     []byte(cfg.SessionSecret),
		cookieName: cookieName,
		ttl:        time.Duration(ttlHours) * time.Hour,
		users:      users,
		roles:      roles,
	}
}

func (m *Manager) Enabled() bool { return m.enabled }

func (m *Manager) CookieName() string { return m.cookieName }

// Authenticate 明文比对配置账号（内部运营台）。
func (m *Manager) Authenticate(username, password string) bool {
	if username == "" {
		return false
	}
	expected, ok := m.users[username]
	return ok && expected == password
}

// RoleOf 返回用户角色；未知用户返回 viewer。
func (m *Manager) RoleOf(username string) string {
	if role, ok := m.roles[username]; ok {
		return role
	}
	return RoleViewer
}

// PermissionsOf 返回用户权限列表。
func (m *Manager) PermissionsOf(username string) []string {
	return PermissionsForRole(m.RoleOf(username))
}

// UserInfo 登录/me 响应字段。
type UserInfo struct {
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

func (m *Manager) InfoFor(username string) UserInfo {
	role := m.RoleOf(username)
	return UserInfo{
		Username:    username,
		Role:        role,
		Roles:       []string{role},
		Permissions: m.PermissionsOf(username),
	}
}

// IssueCookie 写入 HttpOnly Session Cookie（载荷 username|exp + HMAC）。
func (m *Manager) IssueCookie(c *gin.Context, username string) {
	exp := time.Now().Add(m.ttl).Unix()
	token := m.sign(username, exp)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // 本地 HTTP；生产经 HTTPS 反代时可改为 true
	})
}

// ClearCookie 清除 Session Cookie。
func (m *Manager) ClearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Username 从 Cookie 解析当前用户；无效返回 error。
func (m *Manager) Username(c *gin.Context) (string, error) {
	cookie, err := c.Request.Cookie(m.cookieName)
	if err != nil || cookie.Value == "" {
		return "", errcode.Unauthorized
	}
	username, err := m.verify(cookie.Value)
	if err != nil {
		return "", errcode.Unauthorized
	}
	return username, nil
}

// UsernameFromContext 读取 RequireAuth 写入的用户名。
func UsernameFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxUsernameKey)
	s, _ := v.(string)
	return s
}

// RoleFromContext 读取 RequireAuth 写入的角色。
func RoleFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxRoleKey)
	s, _ := v.(string)
	return s
}

// PermissionsFromContext 读取 RequireAuth 写入的权限。
func PermissionsFromContext(c *gin.Context) []string {
	v, _ := c.Get(ctxPermsKey)
	s, _ := v.([]string)
	return s
}

// RequireAuth Gin 中间件：校验 Cookie；auth.enabled=false 时直接放行。
func (m *Manager) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.enabled {
			c.Set(ctxUsernameKey, "anonymous")
			c.Set(ctxRoleKey, RoleAdmin)
			c.Set(ctxPermsKey, AllPermissions())
			c.Next()
			return
		}
		username, err := m.Username(c)
		if err != nil {
			response.Fail(c, errcode.Unauthorized)
			c.Abort()
			return
		}
		role := m.RoleOf(username)
		c.Set(ctxUsernameKey, username)
		c.Set(ctxRoleKey, role)
		c.Set(ctxPermsKey, PermissionsForRole(role))
		c.Next()
	}
}

// RequirePermission 校验当前用户是否具备指定权限码；auth 关闭时放行。
func (m *Manager) RequirePermission(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.enabled {
			c.Next()
			return
		}
		perms, _ := c.Get(ctxPermsKey)
		list, _ := perms.([]string)
		if list == nil {
			username := UsernameFromContext(c)
			list = m.PermissionsOf(username)
		}
		if !HasPermission(list, perm) {
			response.Fail(c, errcode.Forbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (m *Manager) sign(username string, exp int64) string {
	payload := fmt.Sprintf("%s|%d", username, exp)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func (m *Manager) verify(token string) (string, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("bad token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	payload := string(raw)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write(raw)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", fmt.Errorf("bad signature")
	}
	sep := strings.LastIndex(payload, "|")
	if sep <= 0 {
		return "", fmt.Errorf("bad payload")
	}
	username := payload[:sep]
	exp, err := strconv.ParseInt(payload[sep+1:], 10, 64)
	if err != nil {
		return "", err
	}
	if time.Now().Unix() > exp {
		return "", fmt.Errorf("expired")
	}
	if _, ok := m.users[username]; !ok {
		return "", fmt.Errorf("unknown user")
	}
	return username, nil
}
