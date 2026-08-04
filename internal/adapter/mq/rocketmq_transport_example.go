package mq

// RocketMQ Apache Transport 接入说明（默认构建不含官方 SDK）。
//
// 启用步骤：
//
//  1. go get github.com/apache/rocketmq-client-go/v2
//  2. go build -tags rocketmq ./cmd/scheduler ./cmd/pusher
//  3. 配置：
//
//     mq:
//       driver: rocketmq
//       high:   { topic: starlink-push-high, group: pushers-high }
//       normal: { topic: starlink-push-normal, group: pushers-normal }
//       rocketmq:
//         name_servers: ["127.0.0.1:9876"]
//         access_key: ""
//         secret_key: ""
//         namespace: ""
//         retry: 2
//
// bootstrap 在 driver=rocketmq 时调用 TryInitRocketTransport；
// 带 -tags rocketmq 时注入 ApacheRocketTransport，否则 Open 仍会提示未设置 Transport。
//
// 注意：RocketMQ 路径尚无 Redis Stream 同等的 PEL/DLQ 语义；失败依赖 SDK 重试。
