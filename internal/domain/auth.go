package domain

import "time"

// AuthUser 运营台账号（密码存 bcrypt hash；password_note 为可查看的初始/重置副本）
type AuthUser struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	PasswordNote string    `gorm:"size:128" json:"-"` // 仅管理员经专用接口可读；列表不返回
	DisplayName  string    `gorm:"size:128" json:"display_name,omitempty"`
	Role         string    `gorm:"size:64;index;not null" json:"role"` // auth_roles.code
	Enabled      bool      `gorm:"not null;default:true;index" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (AuthUser) TableName() string { return "auth_users" }

// AuthRole 运营台角色
type AuthRole struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        string    `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Description string    `gorm:"size:512" json:"description,omitempty"`
	IsSystem    bool      `gorm:"not null;default:false;index" json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AuthRole) TableName() string { return "auth_roles" }

// AuthRolePermission 角色 ↔ 权限码
type AuthRolePermission struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	RoleCode       string `gorm:"size:64;uniqueIndex:uk_role_perm;not null;index" json:"role_code"`
	PermissionCode string `gorm:"size:64;uniqueIndex:uk_role_perm;not null" json:"permission_code"`
}

func (AuthRolePermission) TableName() string { return "auth_role_permissions" }

// AuthPermission 可注册的权限目录（菜单/按钮）
type AuthPermission struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        string    `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	GroupName   string    `gorm:"size:64;index;column:group_name" json:"group"`
	Kind        string    `gorm:"size:16;index;not null" json:"kind"` // menu | action
	Description string    `gorm:"size:512" json:"description,omitempty"`
	IsSystem    bool      `gorm:"not null;default:false;index" json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AuthPermission) TableName() string { return "auth_permissions" }

// ListPermissionQuery 权限列表筛选
type ListPermissionQuery struct {
	Keyword  string `form:"keyword"`
	Group    string `form:"group"`
	Kind     string `form:"kind"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}
