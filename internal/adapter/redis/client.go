package redisx

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

type Client struct {
	rdb *redis.Client
}

func New(addr, password string, db int) *Client {
	return &Client{rdb: redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})}
}

func (c *Client) RDB() *redis.Client { return c.rdb }

func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Aggregator 子任务终态计数 + 简易频控
type Aggregator struct {
	rdb *redis.Client
}

func NewAggregator(c *Client) *Aggregator {
	return &Aggregator{rdb: c.rdb}
}

func keyStats(mainTaskID uint64) string {
	return fmt.Sprintf("starlink:task:%d:stats", mainTaskID)
}

func keySubFinished(mainTaskID uint64) string {
	return fmt.Sprintf("starlink:task:%d:sub_finished", mainTaskID)
}

func (a *Aggregator) IncrSubDone(ctx context.Context, mainTaskID uint64, success, fail int64) (int64, error) {
	key := keyStats(mainTaskID)
	pipe := a.rdb.Pipeline()
	pipe.HIncrBy(ctx, key, "success", success)
	pipe.HIncrBy(ctx, key, "fail", fail)
	doneCmd := pipe.HIncrBy(ctx, key, "done", 1)
	pipe.Expire(ctx, key, 7*24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return doneCmd.Val(), nil
}

func (a *Aggregator) GetSubDone(ctx context.Context, mainTaskID uint64) (success, fail, done int64, err error) {
	m, err := a.rdb.HGetAll(ctx, keyStats(mainTaskID)).Result()
	if err != nil {
		return 0, 0, 0, err
	}
	success, _ = strconv.ParseInt(m["success"], 10, 64)
	fail, _ = strconv.ParseInt(m["fail"], 10, 64)
	done, _ = strconv.ParseInt(m["done"], 10, 64)
	return
}

func (a *Aggregator) SetSubDone(ctx context.Context, mainTaskID uint64, success, fail, done int64) error {
	statsKey := keyStats(mainTaskID)
	finKey := keySubFinished(mainTaskID)
	pipe := a.rdb.Pipeline()
	pipe.HSet(ctx, statsKey, "success", success, "fail", fail, "done", done)
	pipe.Expire(ctx, statsKey, 7*24*time.Hour)
	pipe.Del(ctx, finKey) // 重推/对齐后允许子任务再次计入完成
	_, err := pipe.Exec(ctx)
	return err
}

// TryMarkSubFinished 用 Redis SET 记录已完成子任务；首次 SADD 返回 true
func (a *Aggregator) TryMarkSubFinished(ctx context.Context, mainTaskID, subTaskID uint64) (bool, error) {
	key := keySubFinished(mainTaskID)
	n, err := a.rdb.SAdd(ctx, key, strconv.FormatUint(subTaskID, 10)).Result()
	if err != nil {
		return false, err
	}
	if n > 0 {
		_ = a.rdb.Expire(ctx, key, 7*24*time.Hour).Err()
	}
	return n == 1, nil
}

// UnmarkSubFinished 回滚幂等标记，供聚合中途失败时重放
func (a *Aggregator) UnmarkSubFinished(ctx context.Context, mainTaskID, subTaskID uint64) error {
	return a.rdb.SRem(ctx, keySubFinished(mainTaskID), strconv.FormatUint(subTaskID, 10)).Err()
}

func (a *Aggregator) Allow(ctx context.Context, key string, limit int, windowSec int) (bool, error) {
	return a.AllowAll(ctx, []port.FrequencyLimit{{Key: key, Limit: limit, WindowSec: windowSec}})
}

var allowAllScript = redis.NewScript(`
for i, key in ipairs(KEYS) do
  local current = tonumber(redis.call('GET', key) or '0')
  local limit = tonumber(ARGV[(i - 1) * 2 + 1])
  if current >= limit then
    return 0
  end
end
for i, key in ipairs(KEYS) do
  local value = redis.call('INCR', key)
  if value == 1 then
    redis.call('PEXPIRE', key, tonumber(ARGV[(i - 1) * 2 + 2]))
  end
end
return 1
`)

func (a *Aggregator) AllowAll(ctx context.Context, limits []port.FrequencyLimit) (bool, error) {
	keys := make([]string, 0, len(limits))
	args := make([]any, 0, len(limits)*2)
	for _, item := range limits {
		if item.Limit <= 0 || item.WindowSec <= 0 {
			continue
		}
		keys = append(keys, "starlink:freq:"+item.Key)
		args = append(args, item.Limit, int64(item.WindowSec)*int64(time.Second/time.Millisecond))
	}
	if len(keys) == 0 {
		return true, nil
	}
	n, err := allowAllScript.Run(ctx, a.rdb, keys, args...).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func dedupKey(mainTaskID uint64, userID string, channel domain.ChannelType) string {
	return fmt.Sprintf("starlink:dedup:%d:%s:%s", mainTaskID, userID, channel)
}

func (a *Aggregator) HasDelivered(ctx context.Context, mainTaskID uint64, userID string, channel domain.ChannelType) (bool, error) {
	n, err := a.rdb.Exists(ctx, dedupKey(mainTaskID, userID, channel)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (a *Aggregator) MarkDelivered(ctx context.Context, mainTaskID uint64, userID string, channel domain.ChannelType, ttlSec int) error {
	if ttlSec <= 0 {
		ttlSec = 7 * 24 * 3600
	}
	return a.rdb.Set(ctx, dedupKey(mainTaskID, userID, channel), "1", time.Duration(ttlSec)*time.Second).Err()
}

func (a *Aggregator) ClearDelivered(ctx context.Context, mainTaskID uint64, userID string, channel domain.ChannelType) error {
	return a.rdb.Del(ctx, dedupKey(mainTaskID, userID, channel)).Err()
}
