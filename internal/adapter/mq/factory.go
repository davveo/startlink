package mq

import (
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/port"
)

// Driver 名称常量
const (
	DriverRedisStream = "redis_stream"
	DriverRocketMQ    = "rocketmq"
	DriverMemory      = "memory"
)

// Deps 驱动装配依赖（各驱动按需取用）
type Deps struct {
	Cfg   config.MQConfig
	Redis *redis.Client // redis_stream 需要；其他驱动可忽略
}

// Queues 一对高优/普通队列
type Queues struct {
	High   port.MessageQueue
	Normal port.MessageQueue
}

// Factory 驱动工厂：根据配置创建一对队列
type Factory func(deps Deps) (*Queues, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register 注册 MQ 驱动（各驱动 init 中调用，或业务侧自定义驱动）
func Register(name string, f Factory) {
	if name == "" || f == nil {
		panic("mq: invalid Register")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := factories[name]; ok {
		panic("mq: duplicate driver " + name)
	}
	factories[name] = f
}

// Drivers 已注册驱动列表
func Drivers() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	return out
}

// Open 按 mq.driver 创建 PriorityBroker（可插拔入口）
func Open(deps Deps) (port.PriorityBroker, error) {
	driver := deps.Cfg.Driver
	if driver == "" {
		driver = DriverRedisStream
	}
	mu.RLock()
	f, ok := factories[driver]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mq: unknown driver %q (registered: %v)", driver, Drivers())
	}
	qs, err := f(deps)
	if err != nil {
		return nil, fmt.Errorf("mq: open %s: %w", driver, err)
	}
	if qs == nil || qs.High == nil || qs.Normal == nil {
		return nil, fmt.Errorf("mq: driver %s returned nil queues", driver)
	}
	return NewPriorityRouter(driver, qs.High, qs.Normal), nil
}
