package redisx

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/port"
)

// suppressionAddChunk 单条 SADD 携带的成员数上限，避免一条命令过长
const suppressionAddChunk = 500

// SuppressionStore 把黑名单 / 退订变更同步到发送链路读的 Redis SET。
// key 必须与 audience.BlacklistFilter / audience.UnsubscribeFilter 完全一致，
// 否则名单写进 DB 也拦不住发送。
type SuppressionStore struct {
	rdb          *redis.Client
	blacklistKey string
	unsubPrefix  string
}

// NewSuppressionStore blacklistKey 取 cfg.Compliance.BlacklistKey，
// unsubPrefix 取 cfg.Compliance.UnsubscribeKeyPrefix（完整 key = prefix + channel）。
func NewSuppressionStore(rdb *redis.Client, blacklistKey, unsubPrefix string) *SuppressionStore {
	return &SuppressionStore{rdb: rdb, blacklistKey: blacklistKey, unsubPrefix: unsubPrefix}
}

var _ port.SuppressionStore = (*SuppressionStore)(nil)

// Enabled 未配置 Redis 或 key 时全部降级为 no-op
func (s *SuppressionStore) Enabled() bool {
	return s != nil && s.rdb != nil
}

func (s *SuppressionStore) AddBlacklist(ctx context.Context, userIDs []string) error {
	if !s.Enabled() || s.blacklistKey == "" || len(userIDs) == 0 {
		return nil
	}
	return s.addAll(ctx, s.blacklistKey, userIDs)
}

func (s *SuppressionStore) RemoveBlacklist(ctx context.Context, userID string) error {
	if !s.Enabled() || s.blacklistKey == "" || userID == "" {
		return nil
	}
	return s.rdb.SRem(ctx, s.blacklistKey, userID).Err()
}

func (s *SuppressionStore) AddUnsubscribe(ctx context.Context, channel string, userIDs []string) error {
	if !s.Enabled() || s.unsubPrefix == "" || channel == "" || len(userIDs) == 0 {
		return nil
	}
	return s.addAll(ctx, s.unsubPrefix+channel, userIDs)
}

func (s *SuppressionStore) RemoveUnsubscribe(ctx context.Context, channel, userID string) error {
	if !s.Enabled() || s.unsubPrefix == "" || channel == "" || userID == "" {
		return nil
	}
	return s.rdb.SRem(ctx, s.unsubPrefix+channel, userID).Err()
}

func (s *SuppressionStore) addAll(ctx context.Context, key string, userIDs []string) error {
	pipe := s.rdb.Pipeline()
	for start := 0; start < len(userIDs); start += suppressionAddChunk {
		end := start + suppressionAddChunk
		if end > len(userIDs) {
			end = len(userIDs)
		}
		members := make([]any, 0, end-start)
		for _, id := range userIDs[start:end] {
			if id == "" {
				continue
			}
			members = append(members, id)
		}
		if len(members) == 0 {
			continue
		}
		pipe.SAdd(ctx, key, members...)
	}
	_, err := pipe.Exec(ctx)
	return err
}
