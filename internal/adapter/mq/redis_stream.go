package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// RedisStreamOptions 消费/写入侧可靠性与容量参数（由配置填充）
type RedisStreamOptions struct {
	ClaimMinIdle time.Duration
	ClaimBatch   int64
	MaxDelivery  int64
	DLQSuffix    string

	MaxLen       int64         // 主队列上限；0=不限制
	DLQMaxLen    int64         // 死信上限；0=不限制
	MaxLenApprox bool          // MAXLEN ~
	TrimInterval time.Duration // 0=不定期 XTRIM
	AckXDel      bool          // ACK 后 XDEL
}

// OptionsFromConfig 将 yaml 配置转为驱动选项
func OptionsFromConfig(c config.RedisStreamMQConfig) RedisStreamOptions {
	maxLen := c.MaxLen
	if maxLen < 0 {
		maxLen = 0
	}
	dlqMax := c.DLQMaxLen
	switch {
	case dlqMax < 0:
		dlqMax = 0
	case dlqMax == 0:
		dlqMax = maxLen // 跟随主队列
	}

	approx := true
	if c.MaxLenApprox != nil {
		approx = *c.MaxLenApprox
	}
	ackXDel := true
	if c.AckXDel != nil {
		ackXDel = *c.AckXDel
	}

	trimSec := c.TrimIntervalSec
	if trimSec < 0 {
		trimSec = 0
	}

	opts := RedisStreamOptions{
		ClaimMinIdle: time.Duration(c.ClaimMinIdleMs) * time.Millisecond,
		ClaimBatch:   int64(c.ClaimBatch),
		MaxDelivery:  int64(c.MaxDelivery),
		DLQSuffix:    c.DLQSuffix,
		MaxLen:       maxLen,
		DLQMaxLen:    dlqMax,
		MaxLenApprox: approx,
		TrimInterval: time.Duration(trimSec) * time.Second,
		AckXDel:      ackXDel,
	}
	return normalizeOptions(opts)
}

func normalizeOptions(o RedisStreamOptions) RedisStreamOptions {
	if o.ClaimMinIdle <= 0 {
		o.ClaimMinIdle = 30 * time.Second
	}
	if o.ClaimBatch <= 0 {
		o.ClaimBatch = 16
	}
	if o.MaxDelivery <= 0 {
		o.MaxDelivery = 5
	}
	if o.DLQSuffix == "" {
		o.DLQSuffix = ":dlq"
	}
	return o
}

// DLQStreamName 死信 Stream 名
func DLQStreamName(stream, suffix string) string {
	if suffix == "" {
		suffix = ":dlq"
	}
	return stream + suffix
}

// ShouldDeadLetter 投递次数是否达到死信阈值（暂停/时窗/免打扰永不进 DLQ）
func ShouldDeadLetter(deliveryCount, maxDelivery int64, handlerErr error) bool {
	if handlerErr != nil && isDeferredRequeue(handlerErr) {
		return false
	}
	if maxDelivery <= 0 {
		return false
	}
	return deliveryCount >= maxDelivery
}

func isDeferredRequeue(err error) bool {
	return errors.Is(err, domain.ErrMainTaskPaused) ||
		errors.Is(err, domain.ErrOutsideSendWindow) ||
		errors.Is(err, domain.ErrQuietHours) ||
		errors.Is(err, domain.ErrChannelThrottled)
}

// RedisStream 基于 Redis Stream 的 MQ：Consumer Group + PEL 重投 + DLQ + 容量治理
type RedisStream struct {
	rdb      *redis.Client
	stream   string
	group    string
	opts     RedisStreamOptions
	lastTrim time.Time
}

func NewRedisStream(rdb *redis.Client, stream, group string, opts RedisStreamOptions) *RedisStream {
	return &RedisStream{
		rdb:    rdb,
		stream: stream,
		group:  group,
		opts:   normalizeOptions(opts),
	}
}

func (q *RedisStream) dlqStream() string {
	return DLQStreamName(q.stream, q.opts.DLQSuffix)
}

func (q *RedisStream) EnsureReady(ctx context.Context) error {
	err := q.rdb.XGroupCreateMkStream(ctx, q.stream, q.group, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create group: %w", err)
	}
	dlqGroup := q.group + "-dlq"
	err = q.rdb.XGroupCreateMkStream(ctx, q.dlqStream(), dlqGroup, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create dlq group: %w", err)
	}
	q.trimIfNeeded(ctx, true)
	return nil
}

func isBusyGroup(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "BUSYGROUP")
}

// EnsureGroup 兼容旧名
func (q *RedisStream) EnsureGroup(ctx context.Context) error {
	return q.EnsureReady(ctx)
}

func (q *RedisStream) xaddArgs(stream string, maxLen int64, values map[string]any) *redis.XAddArgs {
	args := &redis.XAddArgs{
		Stream: stream,
		Values: values,
	}
	if maxLen > 0 {
		args.MaxLen = maxLen
		args.Approx = q.opts.MaxLenApprox
	}
	return args
}

func (q *RedisStream) Publish(ctx context.Context, msgs []domain.PushMessage) error {
	pipe := q.rdb.Pipeline()
	for i := range msgs {
		raw, err := json.Marshal(msgs[i])
		if err != nil {
			return err
		}
		pipe.XAdd(ctx, q.xaddArgs(q.stream, q.opts.MaxLen, map[string]any{"payload": string(raw)}))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (q *RedisStream) Consume(ctx context.Context, consumerID string, batch int, handler func(ctx context.Context, msg domain.PushMessage) error) error {
	concurrency := batch
	if concurrency <= 0 {
		concurrency = 16
	}
	claimBatch := q.opts.ClaimBatch
	if claimBatch <= 0 {
		claimBatch = int64(concurrency)
	}

	// 受限 worker 池：每条消息独立 goroutine，在 worker 内完成 handler + ACK/PEL/DLQ
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()

	dispatch := func(xmsg redis.XMessage) bool {
		select {
		case <-ctx.Done():
			return false
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(m redis.XMessage) {
			defer wg.Done()
			defer func() { <-sem }()
			q.handleMessage(ctx, consumerID, m, handler)
		}(xmsg)
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		q.trimIfNeeded(ctx, false)

		// 1) 先认领空闲 PEL（同样走并发 worker）
		if !q.reclaimOnce(ctx, consumerID, claimBatch, dispatch) {
			return ctx.Err()
		}

		// 2) 再读新消息；Count≈并发度，背压由 sem 限制在途 handler 数
		streams, err := q.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    q.group,
			Consumer: consumerID,
			Streams:  []string{q.stream, ">"},
			Count:    int64(concurrency),
			Block:    time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("mq read failed", "driver", DriverRedisStream, "stream", q.stream, "err", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, xmsg := range stream.Messages {
				if !dispatch(xmsg) {
					return ctx.Err()
				}
			}
		}
	}
}

func (q *RedisStream) trimIfNeeded(ctx context.Context, force bool) {
	if q.opts.MaxLen <= 0 && q.opts.DLQMaxLen <= 0 {
		return
	}
	if !force {
		if q.opts.TrimInterval <= 0 {
			return
		}
		if !q.lastTrim.IsZero() && time.Since(q.lastTrim) < q.opts.TrimInterval {
			return
		}
	}
	q.lastTrim = time.Now()
	q.trimStream(ctx, q.stream, q.opts.MaxLen)
	q.trimStream(ctx, q.dlqStream(), q.opts.DLQMaxLen)
}

func (q *RedisStream) trimStream(ctx context.Context, stream string, maxLen int64) {
	if maxLen <= 0 || stream == "" {
		return
	}
	var err error
	var n int64
	if q.opts.MaxLenApprox {
		n, err = q.rdb.XTrimMaxLenApprox(ctx, stream, maxLen, 0).Result()
	} else {
		n, err = q.rdb.XTrimMaxLen(ctx, stream, maxLen).Result()
	}
	if err != nil {
		slog.Warn("mq xtrim failed", "stream", stream, "maxlen", maxLen, "err", err)
		return
	}
	if n > 0 {
		slog.Info("mq xtrim", "stream", stream, "trimmed", n, "maxlen", maxLen, "approx", q.opts.MaxLenApprox)
	}
}

// reclaimOnce 单轮 XAUTOCLAIM：空闲超过 ClaimMinIdle 的 pending 转交本 consumer 再处理。
// dispatch 返回 false 表示 ctx 已取消。
func (q *RedisStream) reclaimOnce(
	ctx context.Context,
	consumerID string,
	claimBatch int64,
	dispatch func(redis.XMessage) bool,
) bool {
	msgs, _, err := q.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   q.stream,
		Group:    q.group,
		Consumer: consumerID,
		MinIdle:  q.opts.ClaimMinIdle,
		Start:    "0-0",
		Count:    claimBatch,
	}).Result()
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		slog.Warn("mq autoclaim failed", "stream", q.stream, "group", q.group, "err", err)
		return true
	}
	for _, xmsg := range msgs {
		count, _ := q.deliveryCount(ctx, xmsg.ID)
		if ShouldDeadLetter(count, q.opts.MaxDelivery, nil) {
			payload, _ := xmsg.Values["payload"].(string)
			if err := q.moveToDLQ(ctx, consumerID, xmsg.ID, payload, count, fmt.Errorf("max delivery reached before retry")); err != nil {
				slog.Error("mq dlq write failed", "id", xmsg.ID, "err", err)
				continue
			}
			q.ack(ctx, xmsg.ID)
			slog.Warn("mq moved to dlq", "stream", q.stream, "id", xmsg.ID, "delivery", count, "dlq", q.dlqStream())
			continue
		}
		if !dispatch(xmsg) {
			return false
		}
	}
	return true
}

func (q *RedisStream) handleMessage(ctx context.Context, consumerID string, xmsg redis.XMessage, handler func(ctx context.Context, msg domain.PushMessage) error) {
	payload, _ := xmsg.Values["payload"].(string)
	var msg domain.PushMessage
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		slog.Error("mq unmarshal failed", "id", xmsg.ID, "err", err)
		_ = q.moveToDLQ(ctx, consumerID, xmsg.ID, payload, 0, err)
		q.ack(ctx, xmsg.ID)
		return
	}
	if msg.MsgID == "" {
		msg.MsgID = xmsg.ID
	}

	if err := handler(ctx, msg); err != nil {
		q.onHandlerFailure(ctx, consumerID, xmsg.ID, payload, err)
		return
	}
	q.ack(ctx, xmsg.ID)
}

func (q *RedisStream) onHandlerFailure(ctx context.Context, consumerID, id, payload string, handlerErr error) {
	if isDeferredRequeue(handlerErr) {
		slog.Info("mq defer reclaim", "stream", q.stream, "id", id, "err", handlerErr)
		return
	}

	count, err := q.deliveryCount(ctx, id)
	if err != nil {
		slog.Warn("mq delivery count failed, leave pending", "id", id, "err", err, "handler_err", handlerErr)
		return
	}
	if ShouldDeadLetter(count, q.opts.MaxDelivery, handlerErr) {
		if err := q.moveToDLQ(ctx, consumerID, id, payload, count, handlerErr); err != nil {
			slog.Error("mq dlq write failed, leave pending", "id", id, "err", err)
			return
		}
		q.ack(ctx, id)
		slog.Warn("mq moved to dlq after max delivery",
			"stream", q.stream, "id", id, "delivery", count, "max", q.opts.MaxDelivery, "err", handlerErr)
		return
	}
	slog.Warn("mq handler failed, will retry via PEL reclaim",
		"stream", q.stream, "id", id, "delivery", count, "max", q.opts.MaxDelivery, "err", handlerErr)
}

func (q *RedisStream) deliveryCount(ctx context.Context, id string) (int64, error) {
	list, err := q.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: q.stream,
		Group:  q.group,
		Start:  id,
		End:    id,
		Count:  1,
	}).Result()
	if err != nil {
		return 0, err
	}
	if len(list) == 0 {
		return 0, fmt.Errorf("pending entry not found: %s", id)
	}
	return list[0].RetryCount, nil
}

func (q *RedisStream) moveToDLQ(ctx context.Context, consumerID, sourceID, payload string, deliveryCount int64, cause error) error {
	errMsg := ""
	if cause != nil {
		errMsg = cause.Error()
	}
	return q.rdb.XAdd(ctx, q.xaddArgs(q.dlqStream(), q.opts.DLQMaxLen, map[string]any{
		"payload":        payload,
		"source_stream":  q.stream,
		"source_id":      sourceID,
		"group":          q.group,
		"consumer":       consumerID,
		"delivery_count": strconv.FormatInt(deliveryCount, 10),
		"error":          errMsg,
		"dead_at":        time.Now().UTC().Format(time.RFC3339),
	})).Err()
}

func (q *RedisStream) ack(ctx context.Context, id string) {
	if err := q.rdb.XAck(ctx, q.stream, q.group, id).Err(); err != nil {
		slog.Error("mq ack failed", "stream", q.stream, "id", id, "err", err)
		return
	}
	if !q.opts.AckXDel {
		return
	}
	if err := q.rdb.XDel(ctx, q.stream, id).Err(); err != nil {
		slog.Warn("mq xdel after ack failed", "stream", q.stream, "id", id, "err", err)
	}
}

func init() {
	Register(DriverRedisStream, func(deps Deps) (*Queues, error) {
		if deps.Redis == nil {
			return nil, fmt.Errorf("redis client required for driver %s", DriverRedisStream)
		}
		highTopic := deps.Cfg.High.TopicOrStream()
		normalTopic := deps.Cfg.Normal.TopicOrStream()
		if highTopic == "" || normalTopic == "" {
			return nil, fmt.Errorf("high/normal topic(stream) required")
		}
		if deps.Cfg.High.Group == "" || deps.Cfg.Normal.Group == "" {
			return nil, fmt.Errorf("high/normal group required")
		}
		opts := OptionsFromConfig(deps.Cfg.RedisStream)
		return &Queues{
			High:   NewRedisStream(deps.Redis, highTopic, deps.Cfg.High.Group, opts),
			Normal: NewRedisStream(deps.Redis, normalTopic, deps.Cfg.Normal.Group, opts),
		}, nil
	})
}

var _ port.MessageQueue = (*RedisStream)(nil)
