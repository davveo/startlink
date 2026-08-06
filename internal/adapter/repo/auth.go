package repo

import (
	"context"
	"errors"

	"github.com/starlink/push/internal/domain"
	"gorm.io/gorm"
)

type AuthRepo struct {
	db *gorm.DB
}

func NewAuthRepo(db *gorm.DB) *AuthRepo {
	return &AuthRepo{db: db}
}

func (r *AuthRepo) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.AuthUser{}).Count(&n).Error
	return n, err
}

func (r *AuthRepo) CountRoles(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.AuthRole{}).Count(&n).Error
	return n, err
}

func (r *AuthRepo) CountPermissions(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.AuthPermission{}).Count(&n).Error
	return n, err
}

func (r *AuthRepo) ListUsers(ctx context.Context) ([]domain.AuthUser, error) {
	var list []domain.AuthUser
	err := r.db.WithContext(ctx).Order("username ASC").Find(&list).Error
	return list, err
}

func (r *AuthRepo) GetUserByUsername(ctx context.Context, username string) (*domain.AuthUser, error) {
	var u domain.AuthUser
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *AuthRepo) CreateUser(ctx context.Context, u *domain.AuthUser) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *AuthRepo) UpdateUser(ctx context.Context, username string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Model(&domain.AuthUser{}).Where("username = ?", username).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AuthRepo) ListRoles(ctx context.Context) ([]domain.AuthRole, error) {
	var list []domain.AuthRole
	err := r.db.WithContext(ctx).Order("is_system DESC, code ASC").Find(&list).Error
	return list, err
}

func (r *AuthRepo) GetRole(ctx context.Context, code string) (*domain.AuthRole, error) {
	var role domain.AuthRole
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *AuthRepo) CreateRole(ctx context.Context, role *domain.AuthRole) error {
	return r.db.WithContext(ctx).Create(role).Error
}

func (r *AuthRepo) UpdateRole(ctx context.Context, code string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Model(&domain.AuthRole{}).Where("code = ?", code).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AuthRepo) ListRolePermissions(ctx context.Context, roleCode string) ([]string, error) {
	var rows []domain.AuthRolePermission
	if err := r.db.WithContext(ctx).Where("role_code = ?", roleCode).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.PermissionCode)
	}
	return out, nil
}

func (r *AuthRepo) ListAllRolePermissions(ctx context.Context) (map[string][]string, error) {
	var rows []domain.AuthRolePermission
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, row := range rows {
		out[row.RoleCode] = append(out[row.RoleCode], row.PermissionCode)
	}
	return out, nil
}

func (r *AuthRepo) ReplaceRolePermissions(ctx context.Context, roleCode string, perms []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_code = ?", roleCode).Delete(&domain.AuthRolePermission{}).Error; err != nil {
			return err
		}
		if len(perms) == 0 {
			return nil
		}
		rows := make([]domain.AuthRolePermission, 0, len(perms))
		for _, p := range perms {
			rows = append(rows, domain.AuthRolePermission{RoleCode: roleCode, PermissionCode: p})
		}
		return tx.Create(&rows).Error
	})
}

func (r *AuthRepo) ListPermissions(ctx context.Context, q domain.ListPermissionQuery) ([]domain.AuthPermission, int64, error) {
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	db := r.db.WithContext(ctx).Model(&domain.AuthPermission{})
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		db = db.Where("code LIKE ? OR name LIKE ? OR description LIKE ?", like, like, like)
	}
	if q.Group != "" {
		db = db.Where("group_name = ?", q.Group)
	}
	if q.Kind != "" {
		db = db.Where("kind = ?", q.Kind)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.AuthPermission
	err := db.Order("group_name ASC, code ASC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (r *AuthRepo) GetPermission(ctx context.Context, code string) (*domain.AuthPermission, error) {
	var p domain.AuthPermission
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *AuthRepo) CreatePermission(ctx context.Context, p *domain.AuthPermission) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *AuthRepo) UpdatePermission(ctx context.Context, code string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Model(&domain.AuthPermission{}).Where("code = ?", code).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AuthRepo) ListAllPermissionCodes(ctx context.Context) ([]string, error) {
	var codes []string
	err := r.db.WithContext(ctx).Model(&domain.AuthPermission{}).Order("code ASC").Pluck("code", &codes).Error
	return codes, err
}

func (r *AuthRepo) CountEnabledUsersWithPermission(ctx context.Context, perm string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Raw(`
SELECT COUNT(DISTINCT u.id) FROM auth_users u
INNER JOIN auth_role_permissions rp ON rp.role_code = u.role
WHERE u.enabled = 1 AND rp.permission_code = ?
`, perm).Scan(&n).Error
	return n, err
}
