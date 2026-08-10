package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// MaxSuppressionBulk 单次批量写入上限；再大就该走离线导入，避免一条请求锁表太久
	MaxSuppressionBulk = 5000
	// suppressionInsertBatch 单条 INSERT 的行数
	suppressionInsertBatch = 500
	// suppressionIterBatch IterAll 每批载入行数
	suppressionIterBatch = 1000
)

// SuppressionRepo 黑名单 / 退订名单的权威副本
type SuppressionRepo struct {
	db *gorm.DB
}

func NewSuppressionRepo(db *gorm.DB) *SuppressionRepo {
	return &SuppressionRepo{db: db}
}

var _ port.SuppressionRepository = (*SuppressionRepo)(nil)

// BulkAdd 幂等批量写入：命中唯一键 (kind,user_id,channel) 的行直接跳过，
// 返回值是真实新增行数，供上层区分「本次真加了多少」与「本来就在名单里」。
func (r *SuppressionRepo) BulkAdd(ctx context.Context, entries []domain.SuppressionEntry) (int64, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	if len(entries) > MaxSuppressionBulk {
		return 0, fmt.Errorf("suppression bulk size %d exceeds limit %d", len(entries), MaxSuppressionBulk)
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "kind"}, {Name: "user_id"}, {Name: "channel"}},
		DoNothing: true,
	}).CreateInBatches(entries, suppressionInsertBatch)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *SuppressionRepo) Remove(ctx context.Context, kind domain.SuppressionKind, userID, channel string) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("kind = ? AND user_id = ? AND channel = ?", kind, userID, channel).
		Delete(&domain.SuppressionEntry{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *SuppressionRepo) List(ctx context.Context, q domain.ListSuppressionQuery) ([]domain.SuppressionEntry, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.SuppressionEntry{})
	if q.Kind != "" {
		db = db.Where("kind = ?", q.Kind)
	}
	if uid := strings.TrimSpace(q.UserID); uid != "" {
		db = db.Where("user_id = ?", uid)
	}
	if ch := strings.TrimSpace(q.Channel); ch != "" {
		db = db.Where("channel = ?", ch)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("user_id LIKE ? OR reason LIKE ? OR operator LIKE ?", like, like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.SuppressionEntry
	err := db.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *SuppressionRepo) CountByKind(ctx context.Context) (map[domain.SuppressionKind]int64, error) {
	type row struct {
		Kind domain.SuppressionKind
		N    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&domain.SuppressionEntry{}).
		Select("kind, COUNT(*) AS n").Group("kind").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[domain.SuppressionKind]int64, len(rows))
	for _, x := range rows {
		out[x.Kind] = x.N
	}
	return out, nil
}

// IterAll 分批遍历全表（Redis 快路径重建），避免一次性把整张名单读进内存。
func (r *SuppressionRepo) IterAll(ctx context.Context, fn func(domain.SuppressionEntry) error) error {
	if fn == nil {
		return nil
	}
	var batch []domain.SuppressionEntry
	res := r.db.WithContext(ctx).Model(&domain.SuppressionEntry{}).Order("id ASC").
		FindInBatches(&batch, suppressionIterBatch, func(tx *gorm.DB, _ int) error {
			for i := range batch {
				if err := fn(batch[i]); err != nil {
					return err
				}
			}
			return nil
		})
	return res.Error
}
