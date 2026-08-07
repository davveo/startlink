package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
	"gorm.io/gorm"
)

const ctxUsernameKey = "auth_username"
const ctxRoleKey = "auth_role"
const ctxPermsKey = "auth_permissions"

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,31}$`)
var roleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// AccountView 运营台账号视图（不含密码明文）。
type AccountView struct {
	Username      string `json:"username"`
	DisplayName   string `json:"display_name,omitempty"`
	Role          string `json:"role"`
	Enabled       bool   `json:"enabled"`
	Source        string `json:"source"` // db
	PermissionCnt int    `json:"permission_count"`
}

// Manager Session Cookie + DB 用户/角色。
type Manager struct {
	enabled    bool
	secret     []byte
	cookieName string
	ttl        time.Duration
	secure     bool
	store      port.AuthRepository
}

func NewManager(cfg config.AuthConfig, store port.AuthRepository) *Manager {
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
		secure:     cfg.CookieSecure,
		store:      store,
	}
}

func (m *Manager) Enabled() bool { return m.enabled }

func (m *Manager) CookieName() string { return m.cookieName }

func (m *Manager) ctx() context.Context {
	return context.Background()
}

// NormalizeUsername 统一小写去空白：MySQL utf8mb4_*_ci 下 "ADMIN" 与 "admin" 是同一行，
// Go 侧若按大小写敏感比较，「不能禁用自己 / 不能自降权」这类自保护就能被大小写变体绕过。
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// timingDummyHash 用户不存在时也跑一次 bcrypt，抹平与"存在但密码错"的耗时差，避免用户名枚举。
var timingDummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("starlink-timing-equalizer")
	if err != nil {
		return ""
	}
	return h
})

// Authenticate 查库校验 bcrypt 密码；成功返回库中权威用户名（后续签发 Cookie / 审计一律用它）。
func (m *Manager) Authenticate(username, password string) (string, bool) {
	username = NormalizeUsername(username)
	if username == "" || m.store == nil {
		return "", false
	}
	u, err := m.store.GetUserByUsername(m.ctx(), username)
	if err != nil || u == nil || !u.Enabled {
		CheckPassword(timingDummyHash(), password)
		return "", false
	}
	if !CheckPassword(u.PasswordHash, password) {
		return "", false
	}
	return u.Username, true
}

func (m *Manager) RoleOf(username string) string {
	u, err := m.store.GetUserByUsername(m.ctx(), NormalizeUsername(username))
	if err != nil || u == nil {
		return RoleViewer
	}
	return NormalizeRole(u.Role)
}

func (m *Manager) PermissionsOf(username string) []string {
	return m.PermissionsForRole(m.RoleOf(username))
}

// PermissionsForRole 角色权限集合，fail-closed：
// 读库出错一律返回空集合（宁可 403 也不能放行），管理员把权限勾选清空后也必须真的是空集合，
// 只有"角色行本身不存在"（空库 / 未 seed）才回退内置默认。
func (m *Manager) PermissionsForRole(role string) []string {
	role = NormalizeRole(role)
	if m.store == nil {
		return PermissionsForRoleFallback(role)
	}
	perms, err := m.store.ListRolePermissions(m.ctx(), role)
	if err != nil {
		return nil
	}
	if len(perms) == 0 {
		existing, err := m.store.GetRole(m.ctx(), role)
		if err != nil || existing != nil {
			return nil
		}
		if def, ok := DefaultRoleDefs()[role]; ok {
			return append([]string(nil), def.Permissions...)
		}
		return viewerPermissions()
	}
	return SanitizePermissions(perms)
}

// PermissionsForRoleFallback 无 DB 时的内置回退。
func PermissionsForRoleFallback(role string) []string {
	if def, ok := DefaultRoleDefs()[NormalizeRole(role)]; ok {
		return append([]string(nil), def.Permissions...)
	}
	return viewerPermissions()
}

// UserInfo 登录/me 响应。
type UserInfo struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name,omitempty"`
	Role        string   `json:"role"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

func (m *Manager) InfoFor(username string) UserInfo {
	username = NormalizeUsername(username)
	role := RoleViewer
	display := ""
	if m.store != nil {
		if u, err := m.store.GetUserByUsername(m.ctx(), username); err == nil && u != nil {
			username = u.Username
			role = NormalizeRole(u.Role)
			display = u.DisplayName
		}
	}
	return UserInfo{
		Username:    username,
		DisplayName: display,
		Role:        role,
		Roles:       []string{role},
		Permissions: m.PermissionsForRole(role),
	}
}

func (m *Manager) ListAccounts() []AccountView {
	if m.store == nil {
		return nil
	}
	users, err := m.store.ListUsers(m.ctx())
	if err != nil {
		return nil
	}
	out := make([]AccountView, 0, len(users))
	for _, u := range users {
		role := NormalizeRole(u.Role)
		out = append(out, AccountView{
			Username:      u.Username,
			DisplayName:   u.DisplayName,
			Role:          role,
			Enabled:       u.Enabled,
			Source:        "db",
			PermissionCnt: len(m.PermissionsForRole(role)),
		})
	}
	return out
}

func (m *Manager) ListRoles() []RoleDef {
	if m.store == nil {
		defs := DefaultRoleDefs()
		out := make([]RoleDef, 0, len(defs))
		for _, d := range defs {
			out = append(out, d)
		}
		return out
	}
	roles, err := m.store.ListRoles(m.ctx())
	if err != nil {
		return nil
	}
	permMap, _ := m.store.ListAllRolePermissions(m.ctx())
	known, _ := m.store.ListAllPermissionCodes(m.ctx())
	out := make([]RoleDef, 0, len(roles))
	for _, r := range roles {
		// 角色行已存在时不回填内置默认，否则"清空全部权限"在界面上会显示成又勾满了
		perms := permMap[r.Code]
		out = append(out, RoleDef{
			Role:        r.Code,
			Name:        r.Name,
			Description: r.Description,
			Permissions: SanitizePermissions(perms, known),
			Builtin:     r.IsSystem,
		})
	}
	return out
}

func (m *Manager) ListPermissionsPage(q domain.ListPermissionQuery) ([]domain.AuthPermission, int64, error) {
	if m.store == nil {
		return nil, 0, errcode.Internal
	}
	return m.store.ListPermissions(m.ctx(), q)
}

func (m *Manager) ListPermissionMetas() []PermissionMeta {
	if m.store == nil {
		return BuiltinPermissionCatalog()
	}
	list, _, err := m.store.ListPermissions(m.ctx(), domain.ListPermissionQuery{Page: 1, PageSize: 500})
	if err != nil || len(list) == 0 {
		return BuiltinPermissionCatalog()
	}
	out := make([]PermissionMeta, 0, len(list))
	for _, p := range list {
		out = append(out, PermissionMeta{Code: p.Code, Name: p.Name, Group: p.GroupName, Kind: p.Kind})
	}
	return out
}

var permCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,63}$`)

func (m *Manager) CreatePermission(code, name, group, kind, description string) error {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if !permCodePattern.MatchString(code) || name == "" {
		return errcode.New(40001, "权限码需小写字母开头（字母数字._），名称必填")
	}
	if kind != "menu" && kind != "action" {
		kind = "action"
	}
	if group == "" {
		group = "自定义"
	}
	if m.store == nil {
		return errcode.Internal
	}
	if existing, _ := m.store.GetPermission(m.ctx(), code); existing != nil {
		return errcode.New(40901, "权限码已存在")
	}
	return m.store.CreatePermission(m.ctx(), &domain.AuthPermission{
		Code: code, Name: name, GroupName: group, Kind: kind, Description: description, IsSystem: false,
	})
}

func (m *Manager) UpdatePermission(code, name, group, kind, description string) error {
	code = strings.TrimSpace(code)
	if m.store == nil {
		return errcode.Internal
	}
	existing, err := m.store.GetPermission(m.ctx(), code)
	if err != nil {
		return err
	}
	if existing == nil {
		return errcode.NotFound
	}
	fields := map[string]any{}
	if name != "" {
		fields["name"] = name
	}
	if group != "" {
		fields["group_name"] = group
	}
	if kind == "menu" || kind == "action" {
		fields["kind"] = kind
	}
	fields["description"] = description
	return m.store.UpdatePermission(m.ctx(), code, fields)
}

func (m *Manager) CreateUser(username, password, role, displayName string) error {
	username = NormalizeUsername(username)
	password = strings.TrimSpace(password)
	role = NormalizeRole(role)
	if !usernamePattern.MatchString(username) || len(password) < 10 {
		return errcode.New(40001, "用户名需 2–32 位字母数字._-，密码至少 10 位")
	}
	if m.store == nil {
		return errcode.Internal
	}
	if existing, _ := m.store.GetRole(m.ctx(), role); existing == nil {
		return errcode.New(40001, "角色不存在")
	}
	if u, _ := m.store.GetUserByUsername(m.ctx(), username); u != nil {
		return errcode.New(40901, "用户已存在")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return errcode.Internal
	}
	return m.store.CreateUser(m.ctx(), &domain.AuthUser{
		Username:       username,
		PasswordHash:   hash,
		SessionVersion: 1,
		DisplayName:    strings.TrimSpace(displayName),
		Role:           role,
		Enabled:        true,
	})
}

func (m *Manager) UpdateUser(actor, username string, role *string, password *string, enabled *bool, displayName *string) error {
	username = NormalizeUsername(username)
	actor = NormalizeUsername(actor)
	if m.store == nil {
		return errcode.Internal
	}
	u, err := m.store.GetUserByUsername(m.ctx(), username)
	if err != nil {
		return err
	}
	if u == nil {
		return errcode.NotFound
	}
	// 以库中的用户名为准：请求里的大小写变体在 _ci 排序下同样命中这一行
	username = u.Username
	isSelf := strings.EqualFold(actor, u.Username)
	fields := map[string]any{}
	if role != nil {
		r := NormalizeRole(*role)
		if existing, _ := m.store.GetRole(m.ctx(), r); existing == nil {
			return errcode.New(40001, "角色不存在")
		}
		prevRBAC := HasPermission(m.PermissionsForRole(u.Role), PermRBACManage)
		nextRBAC := HasPermission(m.PermissionsForRole(r), PermRBACManage)
		if prevRBAC && !nextRBAC {
			n, _ := m.store.CountEnabledUsersWithPermission(m.ctx(), PermRBACManage)
			if n <= 1 {
				return errcode.New(40001, "不能移除最后一个具备权限管理能力的管理员")
			}
			if isSelf {
				return errcode.New(40001, "不能取消自己的权限管理角色，以免锁死")
			}
		}
		fields["role"] = r
		fields["session_version"] = gorm.Expr("session_version + 1")
	}
	if password != nil {
		p := strings.TrimSpace(*password)
		if p != "" {
			if len(p) < 10 {
				return errcode.New(40001, "密码至少 10 位")
			}
			hash, err := HashPassword(p)
			if err != nil {
				return errcode.Internal
			}
			fields["password_hash"] = hash
			fields["password_note"] = ""
			fields["session_version"] = gorm.Expr("session_version + 1")
		}
	}
	if enabled != nil {
		if isSelf && !*enabled {
			return errcode.New(40001, "不能禁用自己的账号")
		}
		if HasPermission(m.PermissionsForRole(u.Role), PermRBACManage) && !*enabled {
			n, _ := m.store.CountEnabledUsersWithPermission(m.ctx(), PermRBACManage)
			if n <= 1 {
				return errcode.New(40001, "不能禁用最后一个管理员")
			}
		}
		fields["enabled"] = *enabled
		fields["session_version"] = gorm.Expr("session_version + 1")
	}
	if displayName != nil {
		fields["display_name"] = strings.TrimSpace(*displayName)
	}
	if len(fields) == 0 {
		return nil
	}
	if err := m.store.UpdateUser(m.ctx(), username, fields); err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.NotFound
		}
		return err
	}
	return nil
}

func (m *Manager) SetUserRole(actor, username, role string) error {
	r := role
	return m.UpdateUser(actor, username, &r, nil, nil, nil)
}

// ResetPassword 重置密码并使现有 Session 全部失效。
func (m *Manager) ResetPassword(username, newPassword string) error {
	username = NormalizeUsername(username)
	newPassword = strings.TrimSpace(newPassword)
	if len(newPassword) < 10 {
		return errcode.New(40001, "密码至少 10 位")
	}
	if m.store == nil {
		return errcode.Internal
	}
	u, err := m.store.GetUserByUsername(m.ctx(), username)
	if err != nil {
		return err
	}
	if u == nil {
		return errcode.NotFound
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return errcode.Internal
	}
	return m.store.UpdateUser(m.ctx(), u.Username, map[string]any{
		"password_hash":   hash,
		"password_note":   "",
		"session_version": gorm.Expr("session_version + 1"),
	})
}

func (m *Manager) UpsertRole(roleID, name, description string, permissions []string, create bool) error {
	roleID = strings.TrimSpace(roleID)
	if !roleIDPattern.MatchString(roleID) {
		return errcode.New(40001, "角色 ID 需小写字母开头，2–32 位字母数字下划线")
	}
	if m.store == nil {
		return errcode.Internal
	}
	known, _ := m.store.ListAllPermissionCodes(m.ctx())
	perms := SanitizePermissions(permissions, known)
	if roleID == RoleAdmin {
		if !HasPermission(perms, PermRBACManage) {
			perms = append(perms, PermRBACManage)
		}
		if !HasPermission(perms, PermMenuSettings) {
			perms = append(perms, PermMenuSettings)
		}
		perms = SanitizePermissions(perms, known)
	}
	existing, err := m.store.GetRole(m.ctx(), roleID)
	if err != nil {
		return err
	}
	if create {
		if existing != nil {
			return errcode.New(40901, "角色已存在")
		}
		isSys := false
		if _, ok := DefaultRoleDefs()[roleID]; ok {
			isSys = true
		}
		if name == "" {
			name = roleID
		}
		if err := m.store.CreateRole(m.ctx(), &domain.AuthRole{
			Code: roleID, Name: name, Description: description, IsSystem: isSys,
		}); err != nil {
			return err
		}
	} else {
		if existing == nil {
			return errcode.NotFound
		}
		fields := map[string]any{}
		if name != "" {
			fields["name"] = name
		}
		fields["description"] = description
		if err := m.store.UpdateRole(m.ctx(), roleID, fields); err != nil {
			return err
		}
		if err := m.guardLastRBACManageRole(roleID, perms); err != nil {
			return err
		}
	}
	return m.store.ReplaceRolePermissions(m.ctx(), roleID, perms)
}

// guardLastRBACManageRole 从角色上摘掉 rbac.manage 前，确认系统里还留有别的管理员。
// UpdateUser 侧已有同类保护，但改角色权限是另一条能把权限管理彻底锁死的路径：
// 若 rbac.manage 只挂在这个自定义角色上，清空后无人能再进权限管理，也无法自救。
func (m *Manager) guardLastRBACManageRole(roleID string, next []string) error {
	current, err := m.store.ListRolePermissions(m.ctx(), roleID)
	if err != nil {
		return err
	}
	if !HasPermission(current, PermRBACManage) || HasPermission(next, PermRBACManage) {
		return nil
	}
	total, err := m.store.CountEnabledUsersWithPermission(m.ctx(), PermRBACManage)
	if err != nil {
		return err
	}
	users, err := m.store.ListUsers(m.ctx())
	if err != nil {
		return err
	}
	var inRole int64
	for _, u := range users {
		if u.Enabled && NormalizeRole(u.Role) == roleID {
			inRole++
		}
	}
	if total-inRole <= 0 {
		return errcode.New(40001, "不能移除最后一个具备权限管理能力的角色权限")
	}
	return nil
}

func (m *Manager) IssueCookie(c *gin.Context, username string) error {
	u, err := m.store.GetUserByUsername(c.Request.Context(), NormalizeUsername(username))
	if err != nil || u == nil || !u.Enabled {
		return errcode.Unauthorized
	}
	version := u.SessionVersion
	if version == 0 {
		version = 1
	}
	exp := time.Now().Add(m.ttl).Unix()
	// 签库中的用户名，避免同一个人因大小写差异在审计里裂成两个身份
	token := m.sign(u.Username, version, exp)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	})
	return nil
}

func (m *Manager) ClearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	})
}

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

func UsernameFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxUsernameKey)
	s, _ := v.(string)
	return s
}

func RoleFromContext(c *gin.Context) string {
	v, _ := c.Get(ctxRoleKey)
	s, _ := v.(string)
	return s
}

func PermissionsFromContext(c *gin.Context) []string {
	v, _ := c.Get(ctxPermsKey)
	s, _ := v.([]string)
	return s
}

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
		info := m.InfoFor(username)
		c.Set(ctxUsernameKey, username)
		c.Set(ctxRoleKey, info.Role)
		c.Set(ctxPermsKey, info.Permissions)
		c.Next()
	}
}

func (m *Manager) RequirePermission(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.enabled {
			c.Next()
			return
		}
		// 空集合是合法结果（角色被清空授权），只有上下文里压根没放过权限才回源查库
		perms, ok := c.Get(ctxPermsKey)
		list, _ := perms.([]string)
		if !ok {
			list = m.PermissionsOf(UsernameFromContext(c))
		}
		if !HasPermission(list, perm) {
			response.Fail(c, errcode.Forbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (m *Manager) sign(username string, version uint64, exp int64) string {
	payload := fmt.Sprintf("%s|%d|%d", username, version, exp)
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
	// 末两段固定是 version|exp，用户名取其前的全部内容，含 "|" 的用户名也能正常登入
	expSep := strings.LastIndexByte(payload, '|')
	if expSep <= 0 {
		return "", fmt.Errorf("bad payload")
	}
	verSep := strings.LastIndexByte(payload[:expSep], '|')
	if verSep <= 0 {
		return "", fmt.Errorf("bad payload")
	}
	username := payload[:verSep]
	version, err := strconv.ParseUint(payload[verSep+1:expSep], 10, 64)
	if err != nil {
		return "", err
	}
	exp, err := strconv.ParseInt(payload[expSep+1:], 10, 64)
	if err != nil {
		return "", err
	}
	if time.Now().Unix() > exp {
		return "", fmt.Errorf("expired")
	}
	if m.store == nil {
		return "", fmt.Errorf("auth store unavailable")
	}
	u, err := m.store.GetUserByUsername(m.ctx(), NormalizeUsername(username))
	if err != nil || u == nil || !u.Enabled {
		return "", fmt.Errorf("unknown or disabled user")
	}
	currentVersion := u.SessionVersion
	if currentVersion == 0 {
		currentVersion = 1
	}
	if currentVersion != version {
		return "", fmt.Errorf("session revoked")
	}
	return u.Username, nil
}
