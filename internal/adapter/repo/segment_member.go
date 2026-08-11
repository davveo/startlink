package repo

import (
	"context"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SegmentMemberRepo 静态人群成员持久化
type SegmentMemberRepo struct {
	db *gorm.DB
}

func NewSegmentMemberRepo(db *gorm.DB) *SegmentMemberRepo {
	return &SegmentMemberRepo{db: db}
}

var _ port.SegmentMemberRepository = (*SegmentMemberRepo)(nil)

func (r *SegmentMemberRepo) BulkUpsert(ctx context.Context, members []domain.AudienceSegmentMember) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}
	// 分批写入，避免单次 SQL 过大
	const batch = 500
	var inserted int64
	for i := 0; i < len(members); i += batch {
		end := i + batch
		if end > len(members) {
			end = len(members)
		}
		chunk := members[i:end]
		res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "segment_code"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"phone", "email", "vars", "updated_at",
			}),
		}).Create(&chunk)
		if res.Error != nil {
			return inserted, res.Error
		}
		// MySQL ON CONFLICT 更新也会计入 RowsAffected；用「写入前 COUNT」不够准。
		// 这里把 RowsAffected 当作「触及行数」，服务层另用 Count 刷新 member_count。
		inserted += res.RowsAffected
	}
	return inserted, nil
}

func (r *SegmentMemberRepo) DeleteBySegment(ctx context.Context, segmentCode string) error {
	segmentCode = strings.TrimSpace(segmentCode)
	if segmentCode == "" {
		return gorm.ErrInvalidValue
	}
	return r.db.WithContext(ctx).
		Where("segment_code = ?", segmentCode).
		Delete(&domain.AudienceSegmentMember{}).Error
}

func (r *SegmentMemberRepo) List(ctx context.Context, segmentCode string, q domain.ListSegmentMemberQuery) ([]domain.AudienceSegmentMember, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.AudienceSegmentMember{}).
		Where("segment_code = ?", segmentCode)
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("user_id LIKE ? OR phone LIKE ? OR email LIKE ?", like, like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.AudienceSegmentMember
	err := db.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *SegmentMemberRepo) Count(ctx context.Context, segmentCode string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.AudienceSegmentMember{}).
		Where("segment_code = ?", segmentCode).Count(&n).Error
	return n, err
}

func (r *SegmentMemberRepo) ListPage(ctx context.Context, segmentCode string, offset, limit int) ([]domain.AudienceSegmentMember, error) {
	if limit <= 0 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var list []domain.AudienceSegmentMember
	err := r.db.WithContext(ctx).
		Where("segment_code = ?", segmentCode).
		Order("id ASC").
		Offset(offset).Limit(limit).
		Find(&list).Error
	return list, err
}
