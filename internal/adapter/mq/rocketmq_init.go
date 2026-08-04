package mq

import "github.com/starlink/push/internal/config"

// TryInitRocketTransport 尝试注入 Apache RocketMQ Transport。
// 默认构建为空操作；使用 -tags rocketmq 编译时由 rocketmq_apache.go 在 init 中挂接实现。
var rocketInitFn = func(cfg config.RocketMQConfig) {}

func TryInitRocketTransport(cfg config.RocketMQConfig) {
	rocketInitFn(cfg)
}
