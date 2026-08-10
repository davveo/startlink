package repo

import (
	"context"
	"errors"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gorm.io/gorm"
)

// SegmentRepo 可复用人群段持久化
type SegmentRepo struct {
	db *gorm.DB
}

func NewSegmentRepo(db *gorm.DB) *SegmentRepo {
	return &SegmentRepo{db: db}
}

var _ port.SegmentRepository = (*SegmentRepo)(nil)

func (r *SegmentRepo) Create(ctx context.Context, seg *domain.AudienceSegment) error {
	if seg == nil {
		return gorm.ErrInvalidValue
	}
	return r.db.WithContext(ctx).Create(seg).Error
}

func (r *SegmentRepo) Update(ctx context.Context, code string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Model(&domain.AudienceSegment{}).Where("code = ?", code).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetByCode 未命中返回 (nil, nil)，与 AuthRepo 一致；调用方按 nil 判断不存在。
func (r *SegmentRepo) GetByCode(ctx context.Context, code string) (*domain.AudienceSegment, error) {
	var seg domain.AudienceSegment
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&seg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &seg, nil
}

// Delete 不做引用校验，由 service 层先行 CountCampaignRefs。
func (r *SegmentRepo) Delete(ctx context.Context, code string) error {
	res := r.db.WithContext(ctx).Where("code = ?", code).Delete(&domain.AudienceSegment{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *SegmentRepo) List(ctx context.Context, q domain.ListSegmentQuery) ([]domain.AudienceSegment, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.AudienceSegment{})
	if q.Kind != "" {
		db = db.Where("kind = ?", q.Kind)
	}
	if q.BizScene != "" {
		db = db.Where("biz_scene = ?", q.BizScene)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("code LIKE ? OR name LIKE ?", like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.AudienceSegment
	err := db.Order("updated_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// CountCampaignRefs 统计把该 code 用作投放目标或排除名单的活动数
func (r *SegmentRepo) CountCampaignRefs(ctx context.Context, code string) (int64, error) {
	if strings.TrimSpace(code) == "" {
		return 0, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("segment_code = ? OR exclude_segment_code = ?", code, code).
		Count(&n).Error
	return n, err
}
