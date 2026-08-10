package segment

import (
	"context"
	"fmt"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
)

// SuppressionListResult 抑制名单列表响应
type SuppressionListResult struct {
	Items    []domain.SuppressionEntry `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

// SuppressionStatsResult 顶部统计卡
type SuppressionStatsResult struct {
	Blacklist   int64 `json:"blacklist"`
	Unsubscribe int64 `json:"unsubscribe"`
	Total       int64 `json:"total"`
}

// AddSuppressionResult 批量加入结果。
// Redis 同步失败时 DB 已经落库（DB 是权威副本），这里如实回传条数与失败原因，
// 让运营知道「名单已存，但热路径还没生效，需要重建缓存」。
type AddSuppressionResult struct {
	// Submitted 去重后提交的条数
	Submitted int `json:"submitted"`
	// Added 真实新增条数（已存在的不计）
	Added int64 `json:"added"`
	// Skipped 已在名单中的条数
	Skipped int64 `json:"skipped"`
	// Synced Redis 快路径是否同步成功
	Synced    bool   `json:"synced"`
	SyncError string `json:"sync_error,omitempty"`
}

// RemoveSuppressionResult 移除结果
type RemoveSuppressionResult struct {
	Removed   bool   `json:"removed"`
	Synced    bool   `json:"synced"`
	SyncError string `json:"sync_error,omitempty"`
}

func (s *Service) ListSuppressions(ctx context.Context, q domain.ListSuppressionQuery) (*SuppressionListResult, error) {
	if q.Kind != "" && !q.Kind.Valid() {
		return nil, errcode.New(40001, "kind 只能是 blacklist 或 unsubscribe")
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	list, total, err := s.suppression.List(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.SuppressionEntry{}
	}
	return &SuppressionListResult{Items: list, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

func (s *Service) SuppressionStats(ctx context.Context) (*SuppressionStatsResult, error) {
	counts, err := s.suppression.CountByKind(ctx)
	if err != nil {
		return nil, err
	}
	out := &SuppressionStatsResult{
		Blacklist:   counts[domain.SuppressionBlacklist],
		Unsubscribe: counts[domain.SuppressionUnsubscribe],
	}
	out.Total = out.Blacklist + out.Unsubscribe
	return out, nil
}

// AddSuppressions 先写 DB 再同步 Redis。Redis 失败返回错误但不回滚 DB，
// 并把已入库条数带在结果里；调用方据此提示运营重建快路径缓存即可，不必重复导入。
func (s *Service) AddSuppressions(ctx context.Context, in domain.SuppressionInput) (*AddSuppressionResult, error) {
	if !in.Kind.Valid() {
		return nil, errcode.New(40001, "kind 只能是 blacklist 或 unsubscribe")
	}
	channel := in.NormalizeChannel()
	if in.Kind == domain.SuppressionUnsubscribe {
		if channel == "" {
			return nil, errcode.New(40001, "退订必须指定 channel")
		}
		if !domain.ChannelType(channel).Valid() {
			return nil, errcode.New(40001, fmt.Sprintf("不支持的渠道：%s", channel))
		}
	}

	userIDs, err := normalizeUserIDs(in.UserIDs)
	if err != nil {
		return nil, err
	}

	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "console"
	}
	entries := make([]domain.SuppressionEntry, 0, len(userIDs))
	for _, uid := range userIDs {
		entries = append(entries, domain.SuppressionEntry{
			Kind:     in.Kind,
			UserID:   uid,
			Channel:  channel,
			Reason:   strings.TrimSpace(in.Reason),
			Source:   source,
			Operator: in.Operator,
		})
	}

	added, err := s.suppression.BulkAdd(ctx, entries)
	if err != nil {
		return nil, err
	}
	out := &AddSuppressionResult{
		Submitted: len(entries),
		Added:     added,
		Skipped:   int64(len(entries)) - added,
		Synced:    true,
	}
	if out.Skipped < 0 {
		out.Skipped = 0
	}

	if s.store != nil {
		var syncErr error
		if in.Kind == domain.SuppressionBlacklist {
			syncErr = s.store.AddBlacklist(ctx, userIDs)
		} else {
			syncErr = s.store.AddUnsubscribe(ctx, channel, userIDs)
		}
		if syncErr != nil {
			out.Synced = false
			out.SyncError = truncate(syncErr.Error(), 200)
			return out, errcode.New(50002, fmt.Sprintf(
				"已入库 %d 条，但 Redis 快路径同步失败：%s", out.Added, out.SyncError))
		}
	}
	return out, nil
}

// RemoveSuppression 先删 DB 再清 Redis；同上，Redis 失败不回滚 DB。
func (s *Service) RemoveSuppression(ctx context.Context, kind domain.SuppressionKind, userID, channel string) (*RemoveSuppressionResult, error) {
	if !kind.Valid() {
		return nil, errcode.New(40001, "kind 只能是 blacklist 或 unsubscribe")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errcode.New(40001, "user_id 不能为空")
	}
	channel = strings.TrimSpace(channel)
	if kind == domain.SuppressionBlacklist {
		channel = domain.SuppressionAllChannels
	} else if channel == "" {
		return nil, errcode.New(40001, "退订必须指定 channel")
	}

	removed, err := s.suppression.Remove(ctx, kind, userID, channel)
	if err != nil {
		return nil, err
	}
	out := &RemoveSuppressionResult{Removed: removed, Synced: true}
	if s.store != nil {
		var syncErr error
		if kind == domain.SuppressionBlacklist {
			syncErr = s.store.RemoveBlacklist(ctx, userID)
		} else {
			syncErr = s.store.RemoveUnsubscribe(ctx, channel, userID)
		}
		if syncErr != nil {
			out.Synced = false
			out.SyncError = truncate(syncErr.Error(), 200)
			return out, errcode.New(50002, fmt.Sprintf(
				"已从库中移除，但 Redis 快路径同步失败：%s", out.SyncError))
		}
	}
	return out, nil
}

// RebuildSuppressionCache 用 DB 权威副本重建 Redis 快路径。
// Redis 数据丢失或同步失败后靠它补齐；目前未挂路由，由运维/定时任务调用。
func (s *Service) RebuildSuppressionCache(ctx context.Context) (int64, error) {
	if s.store == nil {
		return 0, nil
	}
	var n int64
	err := s.suppression.IterAll(ctx, func(e domain.SuppressionEntry) error {
		var err error
		if e.Kind == domain.SuppressionBlacklist {
			err = s.store.AddBlacklist(ctx, []string{e.UserID})
		} else {
			err = s.store.AddUnsubscribe(ctx, e.Channel, []string{e.UserID})
		}
		if err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

// normalizeUserIDs 去空、去重（保序）、限长与批量上限
func normalizeUserIDs(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if len(id) > maxUserIDLen {
			return nil, errcode.New(40001, fmt.Sprintf("user_id 长度不能超过 %d：%s", maxUserIDLen, truncate(id, 80)))
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, errcode.New(40001, "user_ids 不能为空")
	}
	if len(out) > maxSuppressionBatch {
		return nil, errcode.New(40001, fmt.Sprintf("单次最多提交 %d 个 user_id，当前 %d", maxSuppressionBatch, len(out)))
	}
	return out, nil
}
