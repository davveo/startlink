package notify

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// Bus 包装 NotificationRepository：Create/已读后经 Redis 广播，API 侧再 fan-out 到 SSE。
// 实现 port.NotificationRepository，可直接注入 campaign / aggregator。
type Bus struct {
	repo port.NotificationRepository
	hub  *Hub
	rdb  *redis.Client
}

func NewBus(repo port.NotificationRepository, hub *Hub, rdb *redis.Client) *Bus {
	return &Bus{repo: repo, hub: hub, rdb: rdb}
}

func (b *Bus) Create(ctx context.Context, n *domain.Notification) error {
	if err := b.repo.Create(ctx, n); err != nil {
		return err
	}
	b.publish(ctx, "notification", n)
	return nil
}

func (b *Bus) List(ctx context.Context, q domain.ListNotificationQuery) ([]domain.Notification, int64, error) {
	return b.repo.List(ctx, q)
}

func (b *Bus) CountUnread(ctx context.Context) (int64, error) {
	return b.repo.CountUnread(ctx)
}

func (b *Bus) MarkRead(ctx context.Context, id uint64) (bool, error) {
	ok, err := b.repo.MarkRead(ctx, id)
	if err != nil {
		return false, err
	}
	if ok {
		b.publish(ctx, "unread", nil)
	}
	return ok, nil
}

func (b *Bus) MarkAllRead(ctx context.Context) (int64, error) {
	n, err := b.repo.MarkAllRead(ctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		b.publish(ctx, "unread", nil)
	}
	return n, nil
}

func (b *Bus) publish(ctx context.Context, typ string, n *domain.Notification) {
	unread, err := b.repo.CountUnread(ctx)
	if err != nil {
		slog.Warn("notify bus count unread failed", "err", err)
	}
	evt := Event{Type: typ, UnreadCount: unread}
	if n != nil {
		cp := *n
		evt.Notification = &cp
	}
	// 优先 Redis（跨 api/scheduler）；无 Redis 时仅本地 hub
	if b.rdb != nil {
		payload, err := EncodeEvent(evt)
		if err != nil {
			slog.Warn("notify bus encode failed", "err", err)
			return
		}
		if err := b.rdb.Publish(ctx, RedisChannel, payload).Err(); err != nil {
			slog.Warn("notify bus redis publish failed", "err", err)
			// 降级本地
			if b.hub != nil {
				b.hub.Broadcast(evt)
			}
		}
		return
	}
	if b.hub != nil {
		b.hub.Broadcast(evt)
	}
}

// ListenRedis 订阅 Redis 频道并转发到本地 Hub（仅 API 进程调用）
func (b *Bus) ListenRedis(ctx context.Context) {
	if b == nil || b.rdb == nil || b.hub == nil {
		return
	}
	sub := b.rdb.Subscribe(ctx, RedisChannel)
	ch := sub.Channel()
	go func() {
		defer func() { _ = sub.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				evt, err := DecodeEvent([]byte(msg.Payload))
				if err != nil {
					slog.Warn("notify bus decode failed", "err", err)
					continue
				}
				b.hub.Broadcast(evt)
			}
		}
	}()
}
