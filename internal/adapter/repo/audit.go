package repo

import (
	"context"
	"time"

	"github.com/starlink/push/internal/domain"
	"gorm.io/gorm"
)

type AuditRepo struct {
	db *gorm.DB
}

func NewAuditRepo(db *gorm.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

func (r *AuditRepo) Create(ctx context.Context, log *domain.AuditLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *AuditRepo) List(ctx context.Context, q domain.ListAuditLogQuery) ([]domain.AuditLog, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.AuditLog{})
	if q.Operator != "" {
		db = db.Where("operator = ?", q.Operator)
	}
	if q.Action != "" {
		db = db.Where("action LIKE ?", q.Action+"%")
	}
	if q.Success != nil {
		db = db.Where("success = ?", *q.Success)
	}
	if q.Since != "" {
		if t, err := time.Parse(time.RFC3339, q.Since); err == nil {
			db = db.Where("created_at >= ?", t)
		}
	}
	if q.Until != "" {
		if t, err := time.Parse(time.RFC3339, q.Until); err == nil {
			db = db.Where("created_at <= ?", t)
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []domain.AuditLog
	err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}
