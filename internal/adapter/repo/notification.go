package repo

import (
	"context"
	"time"

	"github.com/starlink/push/internal/domain"
	"gorm.io/gorm"
)

type NotificationRepo struct {
	db *gorm.DB
}

func NewNotificationRepo(db *gorm.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

func (r *NotificationRepo) Create(ctx context.Context, n *domain.Notification) error {
	return r.db.WithContext(ctx).Create(n).Error
}

func (r *NotificationRepo) List(ctx context.Context, q domain.ListNotificationQuery) ([]domain.Notification, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.Notification{})
	if q.UnreadOnly {
		db = db.Where("read_at IS NULL")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []domain.Notification
	err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (r *NotificationRepo) CountUnread(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.Notification{}).Where("read_at IS NULL").Count(&n).Error
	return n, err
}

func (r *NotificationRepo) MarkRead(ctx context.Context, id uint64) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("id = ? AND read_at IS NULL", id).
		Update("read_at", now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *NotificationRepo) MarkAllRead(ctx context.Context) (int64, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("read_at IS NULL").
		Update("read_at", now)
	return res.RowsAffected, res.Error
}
