package auth

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// Seed 顺序：权限目录 → 角色 → 用户；并一次性迁移旧 override。
func Seed(ctx context.Context, store port.AuthRepository, cfg config.AuthConfig, configPath string) error {
	if store == nil {
		return nil
	}
	if err := seedPermissionsIfEmpty(ctx, store); err != nil {
		return err
	}
	if err := seedRolesIfEmpty(ctx, store); err != nil {
		return err
	}
	if err := seedUsersIfEmpty(ctx, store, cfg); err != nil {
		return err
	}
	if configPath != "" {
		if err := migrateOverridesOnce(ctx, store, configPath); err != nil {
			slog.Warn("auth override migrate skipped", "err", err)
		}
	}
	return nil
}

func seedPermissionsIfEmpty(ctx context.Context, store port.AuthRepository) error {
	n, err := store.CountPermissions(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, meta := range BuiltinPermissionCatalog() {
		if err := store.CreatePermission(ctx, &domain.AuthPermission{
			Code:        meta.Code,
			Name:        meta.Name,
			GroupName:   meta.Group,
			Kind:        meta.Kind,
			Description: meta.Name,
			IsSystem:    true,
		}); err != nil {
			return err
		}
	}
	slog.Info("auth permissions seeded", "count", len(BuiltinPermissionCatalog()))
	return nil
}

func seedRolesIfEmpty(ctx context.Context, store port.AuthRepository) error {
	n, err := store.CountRoles(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	known, _ := store.ListAllPermissionCodes(ctx)
	for _, def := range DefaultRoleDefs() {
		role := &domain.AuthRole{
			Code:        def.Role,
			Name:        def.Name,
			Description: def.Description,
			IsSystem:    true,
		}
		if err := store.CreateRole(ctx, role); err != nil {
			return err
		}
		perms := SanitizePermissions(def.Permissions, known)
		if err := store.ReplaceRolePermissions(ctx, def.Role, perms); err != nil {
			return err
		}
	}
	slog.Info("auth roles seeded", "count", len(DefaultRoleDefs()))
	return nil
}

type seedAccount struct {
	Username string
	Password string
	Role     string
}

// seedMinPasswordLen 与 Manager.CreateUser 保持一致，避免 seed 绕过密码强度下限。
const seedMinPasswordLen = 10

func seedUsersIfEmpty(ctx context.Context, store port.AuthRepository, cfg config.AuthConfig) error {
	n, err := store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	byName := map[string]seedAccount{}
	if !cfg.Enabled {
		// 仅在鉴权关闭（单测/紧急排障）时给一组占位账号；启用鉴权时绝不内置任何默认口令
		for _, d := range []seedAccount{
			{Username: "admin", Password: "admin12345", Role: RoleAdmin},
			{Username: "operator", Password: "operator12345", Role: RoleOperator},
			{Username: "viewer", Password: "viewer12345", Role: RoleViewer},
		} {
			byName[d.Username] = d
		}
	}
	for _, u := range cfg.Users {
		name := NormalizeUsername(u.Username)
		if name == "" || u.Password == "" {
			continue
		}
		if !usernamePattern.MatchString(name) {
			return fmt.Errorf("auth.users[%s]: 用户名需 2–32 位字母数字._-", u.Username)
		}
		if len(u.Password) < seedMinPasswordLen {
			return fmt.Errorf("auth.users[%s]: 密码至少 %d 位", u.Username, seedMinPasswordLen)
		}
		byName[name] = seedAccount{
			Username: name,
			Password: u.Password,
			Role:     NormalizeRole(u.Role),
		}
	}
	if len(byName) == 0 {
		return fmt.Errorf("auth 已启用但 auth_users 为空且未配置 auth.users，无法创建首个管理员账号")
	}
	created := 0
	for _, u := range byName {
		hash, err := HashPassword(u.Password)
		if err != nil {
			return err
		}
		role := NormalizeRole(u.Role)
		if r, _ := store.GetRole(ctx, role); r == nil {
			role = RoleViewer
		}
		if err := store.CreateUser(ctx, &domain.AuthUser{
			Username:       u.Username,
			PasswordHash:   hash,
			SessionVersion: 1,
			Role:           role,
			Enabled:        true,
		}); err != nil {
			return err
		}
		created++
	}
	slog.Info("auth users seeded", "count", created)
	return nil
}

type authOverrideFile struct {
	Users    map[string]overrideUser `yaml:"users"`
	RoleDefs map[string]RoleDef      `yaml:"role_defs"`
}

type overrideUser struct {
	Password    string `yaml:"password,omitempty"`
	Role        string `yaml:"role,omitempty"`
	Enabled     *bool  `yaml:"enabled,omitempty"`
	DisplayName string `yaml:"display_name,omitempty"`
}

type legacyRoleOverrideFile struct {
	Roles map[string]string `yaml:"roles"`
}

func migrateOverridesOnce(ctx context.Context, store port.AuthRepository, configPath string) error {
	dir := dirOf(configPath)
	legacyPath := dir + "auth_roles.override.yaml"
	overridePath := dir + "auth_override.yaml"
	marker := dir + "auth_override.migrated"

	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	legacyData, hasLegacy := readOverrideFile(legacyPath)
	overrideData, hasOverride := readOverrideFile(overridePath)
	if !hasLegacy && !hasOverride {
		return nil
	}
	// 容器里 configs/ 常是只读卷：标记落不了盘就会每次启动重放 override，
	// 把管理员在界面上改过的角色权限/用户角色/启用状态静默回滚。此时宁可整段不迁移。
	if err := os.WriteFile(marker, []byte("migrated\n"), 0o644); err != nil {
		slog.Error("auth override migrate skipped: 迁移标记不可写，跳过以免每次重启回滚线上 RBAC 改动",
			"marker", marker, "err", err)
		return fmt.Errorf("write migrate marker %s: %w", marker, err)
	}

	migrated := false
	known, _ := store.ListAllPermissionCodes(ctx)

	if hasLegacy {
		var ov legacyRoleOverrideFile
		if yaml.Unmarshal(legacyData, &ov) == nil {
			for name, role := range ov.Roles {
				name = NormalizeUsername(name)
				role = NormalizeRole(role)
				if !validSeedUsername(name) {
					continue
				}
				u, err := store.GetUserByUsername(ctx, name)
				if err != nil || u == nil {
					continue
				}
				if r, _ := store.GetRole(ctx, role); r == nil {
					continue
				}
				_ = store.UpdateUser(ctx, name, map[string]any{"role": role})
				migrated = true
			}
		}
	}

	if hasOverride {
		var ov authOverrideFile
		if yaml.Unmarshal(overrideData, &ov) == nil {
			for code, def := range ov.RoleDefs {
				code = strings.TrimSpace(code)
				if code == "" {
					continue
				}
				perms := SanitizePermissions(def.Permissions, known)
				existing, err := store.GetRole(ctx, code)
				if err != nil {
					return err
				}
				if existing == nil {
					isSys := false
					if _, ok := DefaultRoleDefs()[code]; ok {
						isSys = true
					}
					name := def.Name
					if name == "" {
						name = code
					}
					if err := store.CreateRole(ctx, &domain.AuthRole{
						Code: code, Name: name, Description: def.Description, IsSystem: isSys,
					}); err != nil {
						return err
					}
				} else {
					fields := map[string]any{}
					if def.Name != "" {
						fields["name"] = def.Name
					}
					if def.Description != "" {
						fields["description"] = def.Description
					}
					if len(fields) > 0 {
						_ = store.UpdateRole(ctx, code, fields)
					}
				}
				if err := store.ReplaceRolePermissions(ctx, code, perms); err != nil {
					return err
				}
				migrated = true
			}
			for name, u := range ov.Users {
				name = NormalizeUsername(name)
				if !validSeedUsername(name) {
					continue
				}
				existing, err := store.GetUserByUsername(ctx, name)
				if err != nil {
					return err
				}
				role := NormalizeRole(u.Role)
				if role != "" {
					if r, _ := store.GetRole(ctx, role); r == nil {
						role = RoleViewer
					}
				}
				en := true
				if u.Enabled != nil {
					en = *u.Enabled
				}
				if existing == nil {
					if u.Password == "" {
						continue
					}
					plain := u.Password
					hash := plain
					if strings.HasPrefix(plain, "$2") {
						// 已是 hash，直接迁移。
					} else {
						h, err := HashPassword(plain)
						if err != nil {
							return err
						}
						hash = h
					}
					if role == "" {
						role = RoleViewer
					}
					if err := store.CreateUser(ctx, &domain.AuthUser{
						Username:       name,
						PasswordHash:   hash,
						SessionVersion: 1,
						DisplayName:    u.DisplayName,
						Role:           role,
						Enabled:        en,
					}); err != nil {
						return err
					}
					migrated = true
					continue
				}
				fields := map[string]any{}
				if u.Role != "" {
					fields["role"] = role
				}
				if u.Enabled != nil {
					fields["enabled"] = en
				}
				if u.DisplayName != "" {
					fields["display_name"] = u.DisplayName
				}
				if u.Password != "" {
					if strings.HasPrefix(u.Password, "$2") {
						fields["password_hash"] = u.Password
					} else {
						hash, err := HashPassword(u.Password)
						if err != nil {
							return err
						}
						fields["password_hash"] = hash
						fields["password_note"] = ""
						fields["session_version"] = gorm.Expr("session_version + 1")
					}
				}
				if len(fields) > 0 {
					_ = store.UpdateUser(ctx, name, fields)
					migrated = true
				}
			}
		}
	}

	slog.Info("auth override migrate finished", "marker", marker, "changed", migrated)
	return nil
}

func readOverrideFile(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// validSeedUsername 迁移/seed 路径同样过 usernamePattern：Session payload 以 "|" 分隔，
// 含 "|" 或超长的用户名建得出来却登不上。
func validSeedUsername(name string) bool {
	if !usernamePattern.MatchString(name) {
		slog.Warn("auth override skipped invalid username", "username", name)
		return false
	}
	return true
}

func dirOf(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[:i+1]
	}
	return ""
}
