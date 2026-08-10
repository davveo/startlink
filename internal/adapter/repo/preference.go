package repo

import (
	"context"
	"errors"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// preferenceBatchSize GetMany 单批 IN 条件上限，避免超长 SQL 打爆 max_allowed_packet
const preferenceBatchSize = 500

type PreferenceRepo struct {
	db *gorm.DB
}

var _ port.PreferenceRepository = (*PreferenceRepo)(nil)

func NewPreferenceRepo(db *gorm.DB) *PreferenceRepo {
	return &PreferenceRepo{db: db}
}

// Get 未配置偏好返回 (nil, nil)：调用方按「无偏好 = 不拦截」处理，
// 把 ErrRecordNotFound 抛给发送链路会让整条消息 fail-closed。
func (r *PreferenceRepo) Get(ctx context.Context, userID string) (*domain.UserPreference, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}
	var p domain.UserPreference
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetMany 批量读取；无偏好的 user_id 不出现在返回 map 中
func (r *PreferenceRepo) GetMany(ctx context.Context, userIDs []string) (map[string]*domain.UserPreference, error) {
	out := make(map[string]*domain.UserPreference, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	uniq := make([]string, 0, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	for start := 0; start < len(uniq); start += preferenceBatchSize {
		end := start + preferenceBatchSize
		if end > len(uniq) {
			end = len(uniq)
		}
		var list []domain.UserPreference
		if err := r.db.WithContext(ctx).
			Where("user_id IN ?", uniq[start:end]).
			Find(&list).Error; err != nil {
			return nil, err
		}
		for i := range list {
			p := list[i]
			out[p.UserID] = &p
		}
	}
	return out, nil
}

// Upsert 按 user_id 唯一键写入；JSON 列空值必须落 "[]"，MySQL 拒绝空串（Error 3140）
func (r *PreferenceRepo) Upsert(ctx context.Context, pref *domain.UserPreference) error {
	if pref == nil {
		return nil
	}
	pref.UserID = strings.TrimSpace(pref.UserID)
	if pref.UserID == "" {
		return gorm.ErrInvalidValue
	}
	if domain.JSONColumnValue(pref.OptOutChannelsJSON, "") == "" {
		pref.OptOutChannelsJSON = domain.MarshalJSONColumn([]string{}, true)
	}
	if domain.JSONColumnValue(pref.OptOutTopicsJSON, "") == "" {
		pref.OptOutTopicsJSON = domain.MarshalJSONColumn([]string{}, true)
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"timezone",
			"quiet_start",
			"quiet_end",
			"preferred_hour",
			"opt_out_channels",
			"opt_out_topics",
			"marketing_opt_out",
			"updated_by",
			"updated_at",
		}),
	}).Create(pref).Error
}

func (r *PreferenceRepo) List(ctx context.Context, q domain.ListPreferenceQuery) ([]domain.UserPreference, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.UserPreference{})
	if uid := strings.TrimSpace(q.UserID); uid != "" {
		db = db.Where("user_id = ?", uid)
	}
	if q.MarketingOptOut != nil {
		db = db.Where("marketing_opt_out = ?", *q.MarketingOptOut)
	}
	// JSON_CONTAINS 走元素级匹配，LIKE '%sms%' 会把 "sms_intl" 之类的值也命中
	if ch := strings.ToLower(strings.TrimSpace(q.Channel)); ch != "" {
		db = db.Where("JSON_CONTAINS(opt_out_channels, JSON_QUOTE(?))", ch)
	}
	if topic := strings.TrimSpace(q.Topic); topic != "" {
		// 主题保留用户输入的大小写，匹配时两侧同时降级为小写
		db = db.Where(`(JSON_CONTAINS(opt_out_topics, JSON_QUOTE(?))
			OR JSON_CONTAINS(LOWER(opt_out_topics), JSON_QUOTE(?)))`,
			topic, strings.ToLower(topic))
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.UserPreference
	err := db.Order("updated_at DESC, id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error
	return list, total, err
}

func (r *PreferenceRepo) Delete(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	res := r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&domain.UserPreference{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *PreferenceRepo) AppendConsent(ctx context.Context, logs []domain.ConsentLog) error {
	if len(logs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(logs, 200).Error
}

func (r *PreferenceRepo) ListConsent(ctx context.Context, q domain.ListConsentLogQuery) ([]domain.ConsentLog, int64, error) {
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

	db := r.db.WithContext(ctx).Model(&domain.ConsentLog{})
	if uid := strings.TrimSpace(q.UserID); uid != "" {
		db = db.Where("user_id = ?", uid)
	}
	if action := strings.TrimSpace(q.Action); action != "" {
		db = db.Where("action = ?", action)
	}
	if scope := strings.TrimSpace(q.Scope); scope != "" {
		// scope 形如 channel:sms，允许按前缀（channel / topic）筛选
		if strings.HasSuffix(scope, ":") {
			db = db.Where("scope LIKE ?", scope+"%")
		} else {
			db = db.Where("scope = ?", scope)
		}
	}
	if q.Since != nil {
		db = db.Where("created_at >= ?", *q.Since)
	}
	if q.Until != nil {
		db = db.Where("created_at < ?", *q.Until)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.ConsentLog
	err := db.Order("id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error
	return list, total, err
}
