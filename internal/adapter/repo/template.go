package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/starlink/push/internal/domain"
	"gorm.io/gorm"
)

type TemplateRepo struct {
	db *gorm.DB
}

func NewTemplateRepo(db *gorm.DB) *TemplateRepo {
	return &TemplateRepo{db: db}
}

func (r *TemplateRepo) Create(ctx context.Context, tpl *domain.Template) error {
	if tpl.Code == "" {
		// 占位唯一 code，避免并发空串撞唯一索引；创建后再规范为 tpl_{id}
		tpl.Code = "tmp_" + uuid.NewString()
	}
	tpl.SyncJSONColumns()
	if err := r.db.WithContext(ctx).Create(tpl).Error; err != nil {
		return err
	}
	tpl.HydrateJSON()
	return nil
}

func (r *TemplateRepo) Update(ctx context.Context, tpl *domain.Template) error {
	ok, err := r.UpdateCAS(ctx, tpl, tpl.Version)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("template version conflict")
	}
	return nil
}

func (r *TemplateRepo) UpdateCAS(ctx context.Context, tpl *domain.Template, expectedVersion int64) (bool, error) {
	tpl.SyncJSONColumns()
	updates := map[string]any{
		"code":               tpl.Code,
		"name":               tpl.Name,
		"body":               tpl.Body,
		"contents":           tpl.ContentsJSON,
		"var_schema":         tpl.VarSchemaJSON,
		"missing_var_policy": tpl.MissingVarPolicy,
		"default_locale":     tpl.DefaultLocale,
		"locales":            tpl.LocalesJSON,
		"biz_scene":          tpl.BizScene,
		"channel_hint":       tpl.ChannelHint,
		"status":             tpl.Status,
		"revision":           tpl.Revision,
		"reject_reason":      tpl.RejectReason,
		"updated_by":         tpl.UpdatedBy,
		"reviewed_by":        tpl.ReviewedBy,
		"reviewed_at":        tpl.ReviewedAt,
		"version":            expectedVersion + 1,
	}
	res := r.db.WithContext(ctx).Model(&domain.Template{}).
		Where("id = ? AND version = ?", tpl.ID, expectedVersion).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected > 0 {
		tpl.Version = expectedVersion + 1
		tpl.HydrateJSON()
		return true, nil
	}
	return false, nil
}

func (r *TemplateRepo) GetByID(ctx context.Context, id uint64) (*domain.Template, error) {
	var t domain.Template
	if err := r.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, err
	}
	t.HydrateJSON()
	return &t, nil
}

func (r *TemplateRepo) GetByCode(ctx context.Context, code string) (*domain.Template, error) {
	var t domain.Template
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&t).Error; err != nil {
		return nil, err
	}
	t.HydrateJSON()
	return &t, nil
}

func (r *TemplateRepo) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&domain.Template{}, id).Error
}

func (r *TemplateRepo) List(ctx context.Context, q domain.ListTemplateQuery) ([]domain.Template, int64, error) {
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	tx := r.db.WithContext(ctx).Model(&domain.Template{})
	if q.BizScene != "" {
		tx = tx.Where("biz_scene = ?", q.BizScene)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		tx = tx.Where("code LIKE ? OR name LIKE ? OR body LIKE ?", like, like, like)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []domain.Template
	err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	for i := range list {
		list[i].HydrateJSON()
	}
	return list, total, err
}

func (r *TemplateRepo) CreateVersion(ctx context.Context, ver *domain.TemplateVersion) error {
	if ver.ContentsJSON == nil {
		ver.ContentsJSON = domain.MarshalJSONColumn(ver.Contents, false)
	}
	if ver.VarSchemaJSON == nil {
		ver.VarSchemaJSON = domain.MarshalJSONColumn(ver.VarSchema, true)
	}
	if ver.LocalesJSON == nil {
		ver.LocalesJSON = domain.MarshalJSONColumn(ver.Locales, false)
	}
	return r.db.WithContext(ctx).Create(ver).Error
}

func (r *TemplateRepo) ListVersions(ctx context.Context, templateID uint64, limit int) ([]domain.TemplateVersion, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var list []domain.TemplateVersion
	err := r.db.WithContext(ctx).Where("template_id = ?", templateID).
		Order("revision DESC").Limit(limit).Find(&list).Error
	for i := range list {
		list[i].HydrateJSON()
	}
	return list, err
}

func (r *TemplateRepo) GetVersion(ctx context.Context, templateID uint64, revision int64) (*domain.TemplateVersion, error) {
	var v domain.TemplateVersion
	if err := r.db.WithContext(ctx).Where("template_id = ? AND revision = ?", templateID, revision).First(&v).Error; err != nil {
		return nil, err
	}
	v.HydrateJSON()
	return &v, nil
}

// EnsureTemplateCode 创建后若仍是临时 code 则回填 tpl_{id}
func EnsureTemplateCode(tpl *domain.Template) {
	if tpl.ID > 0 && (tpl.Code == "" || strings.HasPrefix(tpl.Code, "tmp_")) {
		tpl.Code = fmt.Sprintf("tpl_%d", tpl.ID)
	}
}
