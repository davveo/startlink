package auth

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gopkg.in/yaml.v3"
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

func seedUsersIfEmpty(ctx context.Context, store port.AuthRepository, cfg config.AuthConfig) error {
	n, err := store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// 默认内置账号；YAML 可覆盖同名密码/角色
	defaults := []seedAccount{
		{Username: "admin", Password: "admin123", Role: RoleAdmin},
		{Username: "operator", Password: "operator123", Role: RoleOperator},
		{Username: "viewer", Password: "viewer123", Role: RoleViewer},
		{Username: "demo", Password: "demo123", Role: RoleViewer},
	}
	byName := map[string]seedAccount{}
	for _, d := range defaults {
		byName[d.Username] = d
	}
	for _, u := range cfg.Users {
		name := strings.TrimSpace(u.Username)
		if name == "" || u.Password == "" {
			continue
		}
		byName[name] = seedAccount{
			Username: name,
			Password: u.Password,
			Role:     NormalizeRole(u.Role),
		}
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
			Username:     u.Username,
			PasswordHash: hash,
			PasswordNote: u.Password, // 初始可查看副本
			Role:         role,
			Enabled:      true,
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

	migrated := false
	known, _ := store.ListAllPermissionCodes(ctx)

	if data, err := os.ReadFile(legacyPath); err == nil {
		var ov legacyRoleOverrideFile
		if yaml.Unmarshal(data, &ov) == nil {
			for name, role := range ov.Roles {
				name = strings.TrimSpace(name)
				role = NormalizeRole(role)
				if name == "" {
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

	if data, err := os.ReadFile(overridePath); err == nil {
		var ov authOverrideFile
		if yaml.Unmarshal(data, &ov) == nil {
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
				name = strings.TrimSpace(name)
				if name == "" {
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
					note := ""
					if strings.HasPrefix(plain, "$2") {
						// 已是 hash，无可查看副本
					} else {
						h, err := HashPassword(plain)
						if err != nil {
							return err
						}
						hash = h
						note = plain
					}
					if role == "" {
						role = RoleViewer
					}
					if err := store.CreateUser(ctx, &domain.AuthUser{
						Username:     name,
						PasswordHash: hash,
						PasswordNote: note,
						DisplayName:  u.DisplayName,
						Role:         role,
						Enabled:      en,
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
						fields["password_note"] = u.Password
					}
				}
				if len(fields) > 0 {
					_ = store.UpdateUser(ctx, name, fields)
					migrated = true
				}
			}
		}
	}

	if migrated {
		_ = os.WriteFile(marker, []byte("migrated\n"), 0o644)
		slog.Info("auth override migrated into mysql", "marker", marker)
	}
	return nil
}

func dirOf(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[:i+1]
	}
	return ""
}
