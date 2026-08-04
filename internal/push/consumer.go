package push

import (
	"context"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// Consumer 从 MQ 拉取消息并交给 Gateway 处理
type Consumer struct {
	mq          port.MessageQueue
	gateway     *Gateway
	consumerID  string
	concurrency int
}

func NewConsumer(mq port.MessageQueue, gateway *Gateway, consumerID string, concurrency int) *Consumer {
	if concurrency <= 0 {
		concurrency = 16
	}
	return &Consumer{
		mq:          mq,
		gateway:     gateway,
		consumerID:  consumerID,
		concurrency: concurrency,
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	return c.mq.Consume(ctx, c.consumerID, c.concurrency, func(ctx context.Context, msg domain.PushMessage) error {
		return c.gateway.Handle(ctx, msg)
	})
}
