# 异步 Pusher 层 · 精细化代码解析

本文面向源码精读，拆解 `cmd/pusher` 如何消费 Scheduler 写入的 MQ，完成「模板渲染 → 去重占位 → 渠道发送 → ACK」。配套见 [`创建活动主流程.md`](创建活动主流程.md)、[`Scheduler层代码解析.md`](Scheduler层代码解析.md)。

---

## 1. 在系统中的位置

```text
Scheduler：子任务 → MQ.Publish([]PushMessage)  【按 priority 进 high / normal】
        │
        ▼
┌──────────────── cmd/pusher ─────────────────────────┐
│  -queue=all|high|normal                             │
│  Consumer.Run → MQ.Consume → Gateway.Handle         │
│    ├─ 主任务门禁（cancelled / paused）              │
│    ├─ 渠道配额限流（channel × priority）            │
│    ├─ RenderTemplate + single/fallback/parallel     │
│    └─ sendOne：Redis 去重 → ClaimDelivery → 渠道 SPI│
└─────────────────────────────────────────────────────┘
        │
        ▼
  push_records 流水 + Redis dedup 标记
  （送达/点击回执由 API callback 另路写入，不在本进程）
```

**Pusher 不管圈人、拆片、主任务终态聚合**；它只对单条 `PushMessage` 做渠道投递。Scheduler 侧的「子任务成功」= 入队成功；真正「用户是否发出去」以 `push_records` + Redis dedup 为准。

---

## 2. 进程启动与队列模式

入口：[`cmd/pusher/main.go`](../cmd/pusher/main.go)

| 步骤 | 代码 | 说明 |
|------|------|------|
| 1 | `bootstrap.NewInfra` | Channels / AggCache / PushRepo / Tasks / MQ |
| 2 | `MQ.EnsureReady` + `Redis.Ping` | Consumer Group / 去重依赖 |
| 3 | `push.NewGateway(..., limiter, ...)` | **全进程共享 Gateway**；配额由 `ChannelLimiter`（Redis/本地）提供 |
| 4 | 按 `-queue` 启 1～2 个 `Consumer` | high / normal 可分实例部署 |

### 2.1 `-queue` 开关

| 值 | 消费队列 | 并发配置 |
|----|----------|----------|
| `all`（默认） | high + normal 各起一个 Consumer | `high_worker_concurrency` / `worker_concurrency` |
| `high` | 仅 high | `high_worker_concurrency` |
| `normal` | 仅 normal | `worker_concurrency` |

任一 Consumer 非 cancel 错误 → `cancel()` 拉停整进程。

### 2.2 配置（`pusher`）

| 配置 | 用途 | 典型值 |
|------|------|--------|
| `worker_concurrency` | normal 队列 **并发 handler** 数（兼 XREADGROUP Count） | 16 |
| `high_worker_concurrency` | high 队列 **并发 handler** 数 | 32 |
| `rate_limit_qps` | `channel_quota` 关闭时的遗留全局桶；开启时作 `global_qps` 缺省 | 500 |
| `channel_quota.*` | 按渠道×优先级配额、反压、准入、429 自适应 | 见配置 |
| `max_retry` | 默认额外重试次数（总尝试 = max_retry+1）；可被 `channels.*.max_retry` 覆盖 | 3 |
| `retry_backoff` | 默认退避：`exponential` / `linear` / `fixed` | exponential |
| `retry_base_ms` / `retry_max_ms` | 退避基数与上限 | 50 / 5000 |
| `timeout_sec` | 默认单次 Send 超时；可被 `channels.*.timeout_sec` 覆盖 | 10 |
| `channels.*.max_retry` 等 | 按渠道覆盖重试次数、退避、超时 | 可选 |
| `dedup_ttl_sec` | Redis 成功标记 TTL | 604800（7 天） |

> `channel_quota.distributed=true` 时全集群共享 Redis 桶；`high_reserve_ratio>0` 时 high/normal 分桶。关闭配额时多实例总 QPS ≈ `rate_limit_qps × 实例数`。

---

## 3. 协程模型：`Consumer.Run`

文件：[`internal/push/gateway.go`](../internal/push/gateway.go) 末尾

```go
return c.mq.Consume(ctx, c.consumerID, c.concurrency, c.gateway.Handle)
```

`concurrency`（`worker_concurrency` / `high_worker_concurrency`）传给 MQ 驱动的 `batch` 参数。

| 层 | 行为 |
|----|------|
| `Consumer.Run` | 把并发度交给驱动；不再在外层套无效信号量 |
| `RedisStream.Consume` | 大小为 concurrency 的 **worker 池**：读循环 `dispatch` → goroutine 内 `handleMessage`（含 ACK/PEL/DLQ）；`sem` 背压 |
| `MemoryQueue.Consume` | 同样 worker 池；失败简单重新入队 |
| `RocketMQ` | 在途 handler 上限；吞吐还取决于 transport 是否并发回调 |

并行来源：

1. **单队列内**最多 `*_worker_concurrency` 条消息同时 `Handle`
2. 多个 pusher 实例（Consumer Group 分片）
3. 同实例 high + normal 两条 Consume
4. `sendParallel` 内部多渠道 goroutine

注意：配额在 **ChannelLimiter** 上按渠道分桶；主任务状态有 **300ms 进程内短缓存**。

---

## 4. MQ 消费契约（默认 Redis Stream）

文件：[`internal/adapter/mq/redis_stream.go`](../internal/adapter/mq/redis_stream.go)

```text
sem = chan(size=concurrency)   // worker 池
loop:
  trimIfNeeded
  // ① PEL：XAUTOCLAIM → 达 max_delivery 则 DLQ+ACK；否则 dispatch(worker)
  // ② XREADGROUP > Count=concurrency
  for each message:
    acquire sem (背压)
    go worker:
      handleMessage → handler
      成功 → XACK (+可选 XDEL)
      paused → 不 ACK
      失败 → PEL / 满次数 DLQ+ACK
      release sem
ctx cancel → wait in-flight workers
```

| handler 返回 | 效果 |
|--------------|------|
| `nil` | ACK，消息不再投递 |
| `domain.ErrMainTaskPaused` / `ErrQuietHours` / `ErrOutsideSendWindow` | **不 ACK**；PEL 等待后重投；**永不进 DLQ** |
| 其它 error | **不 ACK**；空闲 `claim_min_idle_ms` 后 `XAUTOCLAIM` 再投；满 `max_delivery` → `{topic}:dlq` 并 ACK |

配置（`mq.redis_stream`）：

| 项 | 默认 | 作用 |
|----|------|------|
| `claim_min_idle_ms` | 30000 | PEL 可认领空闲时间 |
| `claim_batch` | 16 | 每轮 XAUTOCLAIM 条数 |
| `max_delivery` | 5 | 满则进 DLQ |
| `dlq_suffix` | `:dlq` | 死信名=`topic`+suffix |
| `maxlen` | 100000 | 主队列 `XADD MAXLEN` / 定期 `XTRIM`；`-1` 关闭 |
| `dlq_maxlen` | 0（跟随） | 死信上限；`-1` 关闭 |
| `maxlen_approx` | true | 近似裁剪 `~` |
| `trim_interval_sec` | 60 | 消费侧定期 trim；`-1` 关闭 |
| `ack_xdel` | true | ACK 后 `XDEL` 释放条目 |

容量路径：Publish/`moveToDLQ` 带 `MAXLEN`；Consume/`EnsureReady` 按间隔 `XTRIM`；成功 ACK 后可选删除条目。Redis 裁剪**不感知** Consumer Group，上限勿小于峰值 PEL。

反序列化失败会写入 DLQ 后 ACK（毒丸丢弃，避免死循环）。

RocketMQ / Memory 驱动未实现同等 PEL/DLQ；Memory 失败时简单重新入队。

---

## 5. `Gateway.Handle` —— 单条消息总控

> 2026-08-11 补记：限流前还有 **偏好/退订/用户免打扰/营销频次** 终检；渠道链由 `msg.ResolveSendChannels()` 决定（含 conditional / cost_priority）；`doSend` 使用按渠道 `RetryPolicy`；业务异常可写 `trace_events`（成功靠 `push_records` 下钻）。

```go
func (g *Gateway) Handle(ctx context.Context, msg domain.PushMessage) error {
	// ① 主任务门禁（cancelled / paused）
	// ② 偏好 / Redis 退订 / quiet hours / marketing freq → suppressed + ACK
	// ③ 频控 Allow；waitToken(channel, priority)
	// ④ RenderTemplate + ResolveSendChannels
	// ⑤ switch mode → sendParallel / sendFallback / sendOne（按渠道重试）
}
```

### 5.1 主任务门禁

| 状态 | 返回 | 副作用 |
|------|------|--------|
| `cancelled` | `nil`（会 ACK） | `markCancelled`：Claim 占位后标 `cancelled` |
| `paused` | `error`（`ErrMainTaskPaused`，不 ACK） | 无写库；PEL 等待恢复后 `XAUTOCLAIM` 重投 |
| 查库失败 / 空 | 继续发送 | `taskStatus` 返回 `""`，不拦截 |

渠道令牌等待后强刷状态，避免排队期间任务被取消/暂停。

### 5.2 渠道配额限流（`channel_quota`）

- 主闸：`ChannelLimiter.Wait(channel, priority)`（Redis Lua 令牌桶或进程内）
- 可选全局保护闸 `global_qps`
- `high_reserve_ratio>0` → high/normal 分桶；否则共享渠道桶
- 超时 → `domain.ErrChannelThrottled`，与静默/时窗一样**不进 DLQ**
- 厂商 HTTP 429 → `SendResult.Throttled` → 自适应缩有效 QPS
- `enabled=false` 回退进程内 `rate_limit_qps`
- 去重/频控通过后、真正 `doSend` 前才扣令牌；fallback/parallel **每个实际尝试的渠道各扣一次**

### 5.3 模板渲染

[`internal/push/template.go`](../internal/push/template.go)：

- 模式：`{{var_name}}`（允许空格）
- 缺变量 → 替换成 **空串**（不是保留占位符）
- Body 来自 MQ 里主任务快照，**不回查模板表**

### 5.4 渠道路由决策

[`domain.PushMessage.EffectiveChannels/Mode`](../internal/domain/message.go)：

| 条件 | 实际 mode |
|------|-----------|
| 渠道数 ≤ 1 | 强制 `single` |
| 多渠道且 mode=`single`/空 | 升为 `fallback` |
| 显式 `fallback` / `parallel` | 按配置 |

然后：

```text
parallel  → sendParallel
fallback  → sendFallback
default   → sendOne(chs[0])
```

---

## 6. 发送策略（`channel_mode`）

除经典 **single / fallback / parallel** 外，活动还可指定：

| mode | 行为 |
|------|------|
| `all_success` | 多渠道均需成功（细节见多渠道策略精读） |
| `conditional` | 按 `channel_routes` 规则匹配渠道链 |
| `cost_priority` | 按 `channel_costs` 成本排序后发送 |

实现入口：`domain.ResolveSendChannels` → Gateway 再走 fallback/parallel/single 执行器。

### 6.1 `sendFallback` / `sendParallel` 要点（原 §6）

### 6.1 `single`：只打第一条渠道

`sendOne(..., chs[0], save=true)`；成功 `nil`，失败把 error 抛给 Consume（不 ACK）。

### 6.2 `fallback`：顺序降级

```text
for i, ch in chs:
  门禁 cancelled → markCancelled 该渠道并 return nil
  门禁 paused → return error
  i>0 → 再 waitToken
  sendOne(ch)
  ok → return nil（成功即停）
  err==nil（取消/去重路径）→ return nil
  否则记 lastErr，试下一渠道
全部失败 → return lastErr
```

降级粒度是 **渠道**，不是用户批次。每个渠道独立去重键 `(main, user, channel)`。

### 6.3 `parallel`：多渠并行

每个渠道一个 goroutine：`waitToken` + `sendOne`。

| 结果 | Handle 返回 |
|------|-------------|
| 任一 `ok` | `nil`（用户维度成功） |
| 全部失败 | `lastErr` 或 `all parallel channels failed` |

注意：并行时 **不会**因其中一个 paused 立刻取消其他 goroutine；各 goroutine 自己查状态。任一成功即整体 ACK，失败渠道可能已写 `push_records=failed`。

---

## 7. 核心：`sendOne` + `doSend`（去重与投递）

### 7.1 两层去重

```text
① Redis HasDelivered(main, user, channel)
     命中 → return (true, nil)   // 视为成功，Consume 会 ACK
② DB ClaimDelivery 唯一索引 (main_task_id, user_id, channel)
     duplicate（已 sent/delivered/clicked）→ MarkDelivered 回填 Redis → (true, nil)
     inFlight（2 分钟内仍 sending）→ (false, "delivery in progress")  // 不 ACK
     抢占成功 → doSend(recordID)
```

Redis key：`starlink:dedup:{mainTaskID}:{userID}:{channel}`，TTL=`dedup_ttl_sec`。

### 7.2 `ClaimDelivery` 状态机（DB）

文件：[`internal/adapter/repo/task.go`](../internal/adapter/repo/task.go)

```text
INSERT status=sending
  ├─ 成功 → 拿到新 id，去发
  └─ 唯一键冲突 → 读已有行
        ├─ DeliveredOK → duplicate
        ├─ sending 且 UpdatedAt < 2min → inFlight
        └─ failed/cancelled/queued/陈旧 sending
              → UPDATE 抢回 sending；RowsAffected=0 再判 duplicate/inFlight
```

设计意图：

- **防双发**：同用户同活动同渠道最多一条「成功」语义流水
- **允许失败重推 / 暂停恢复**：`failed`/`queued`/`cancelled` 可被重新 Claim
- **防并发双 worker**：新鲜 `sending` 返回 inFlight，调用方失败 → 不 ACK（依赖后续重投）

### 7.3 `doSend`：渠道调用与重试

```text
policy = retries.For(channel)   // 默认 + channels.* 覆盖
channels.Get(ch) → ChannelSender.Send
for attempt = 0..policy.MaxRetry:
  cancelled → 流水 cancelled，return (false, nil)  // Handle 视为「无 error」→ ACK
  paused    → 流水改回 queued，return (false, "paused") → 不 ACK
  Send(WithTimeout(policy.Timeout))
  成功 → break
  !Retryable（且 lastErr==nil）→ break 不再重试
  else policy.BackoffDelay(attempt)  // exponential/linear/fixed，封顶 retry_max_ms
写流水 sent/failed；成功则 MarkDelivered
```

`SendRequest.MsgID` = `{msg.MsgID}-{channel}`，便于渠道侧幂等。

内置渠道：[`internal/adapter/channel/registry.go`](../internal/adapter/channel/registry.go) 的 `stubSender`（2ms sleep + 假 ProviderID）。真实 APNs/短信等通过 `Register` 替换。

### 7.4 `Handle` 返回值 → ACK 对照表（精读必看）

| 场景 | sendOne/Handle | Consume |
|------|----------------|---------|
| 渠道发送成功 | `nil` | ACK |
| Redis/DB 去重跳过 | `nil` | ACK |
| 主任务 cancelled（门禁或发送中） | `nil` | ACK（不再发） |
| 主任务 paused | `ErrMainTaskPaused` | **不 ACK**（PEL 重投；不进 DLQ） |
| 渠道失败（含 fallback 全挂） | `error` | **不 ACK** → PEL → 满次数进 DLQ |
| delivery inFlight | `error` | **不 ACK** → 同上 |
| 无渠道 / 渠道配额超时 | `error`（含 `ErrChannelThrottled`） | **不 ACK** → PEL；配额超时不进 DLQ |

---

## 8. 流水状态与上下游语义差

### 8.1 `PushStatus`

| 状态 | 含义 |
|------|------|
| `sending` | Claim 占位中 |
| `sent` | 渠道 API 返回成功（非用户已读） |
| `delivered` / `clicked` | 一般由 **callback** 回执更新 |
| `failed` | 发送失败 |
| `queued` | 暂停时释放占位，允许恢复后重 Claim |
| `cancelled` | 主任务取消导致跳过 |

### 8.2 与 Scheduler 计数的口径拆分（P1）

| 层级 | 「成功」含义 |
|------|----------------|
| Scheduler 子任务 / 流水线终态 | **入队 MQ 成功/失败** → 主任务 `success`/`partial`/`failed` |
| 主任务 `success_count`/`fail_count`、进度 API | **`push_records` 渠道口径**（回执/终态后校准） |
| Pusher `push_records` + Redis dedup | **渠道调用成功**（`sent`） |
| callback `delivered`/`clicked` | 厂商回执；状态机单向前进，幂等 |

排查「用户没收到」应查 `push_records`；看活动是否「拆完并入队完」看主任务状态与 `sub_task_done`。

回执：`UpdateRecordStatus` 经 `CanTransitTo`；仅首次 `sent` 写 `sent_at`；失败可带 `error_msg`；DB 错与流水不存在分码返回。

---

## 9. 取消 / 暂停 / 恢复在 Pusher 的表现

```text
Cancel：
  Handle 门禁 → markCancelled → ACK（消息丢弃式跳过）
  发送中途 cancel → 流水 cancelled，return (false,nil) → ACK

Pause：
  Handle / doSend → ErrMainTaskPaused → 不 ACK
  若已 Claim：流水改 queued，释放占位
  Redis Stream：留在 PEL，空闲后 XAUTOCLAIM；暂停错误不进 DLQ

Resume：
  主任务回 running 后，下一轮 claim 即可再次 Handle
```

---

## 10. 水平扩展怎么扩

| 手段 | 效果 |
|------|------|
| 多启 `cmd/pusher`（同 Group） | Stream / RocketMQ 按 Consumer Group 分片 |
| `-queue=high` 与 `-queue=normal` 分池 | 事务与营销隔离资源 |
| 调高 `*_worker_concurrency` | 增大单队列并发 handler 数（及 XREADGROUP Count） |
| 调 `channel_quota.channels.*.qps` | 按渠道打厂商上限 |
| 调 `rate_limit_qps` / `global_qps` | 全局保护闸 |

DB 去重唯一索引 + Redis 标记保证多实例不会把同一 `(活动,用户,渠道)` 重复标成功（inFlight 窗口内可能短暂互斥失败）。

---

## 11. 建议断点 / 日志阅读顺序

| 顺序 | 位置 | 看什么 |
|------|------|--------|
| 1 | `cmd/pusher/main.go` | `-queue`、两个 Consumer、共享 Gateway |
| 2 | `mq/redis_stream.go` Consume | XAUTOCLAIM → 新消息；ACK / PEL / DLQ |
| 3 | `Consumer.Run` + `RedisStream.Consume` | concurrency → worker 池；worker 内 ACK |
| 4 | `Gateway.Handle` | 门禁、限流、mode 分支 |
| 5 | `sendFallback` / `sendParallel` | 多渠道语义 |
| 6 | `sendOne` → `ClaimDelivery` | 去重三分支 |
| 7 | `doSend` | 重试、paused→queued、MarkDelivered |
| 8 | `channel/registry.go` | SPI 替换点 |

本地可：制造渠道失败观察 PEL 下降与 DLQ 增长；打 paused 确认消息留 PEL 且不进 DLQ；同一 MsgID 重投看是否 `dedup skip`。

---

## 12. 已知坑（精读对齐）

| 坑 | 说明 |
|----|------|
| 失败不 ACK | 留 PEL；`claim_min_idle_ms` 后自动 `XAUTOCLAIM` 重投 |
| 满 `max_delivery` | 写入 `{topic}:dlq` 并 ACK（暂停错误除外） |
| Stream 容量 | `maxlen` + 定期 `XTRIM` + `ack_xdel`；裁剪不感知 PEL，勿设过小 |
| 并发与限流 | handler 可并行；配额按 channel×priority；`-queue=all` 时仍共享同一 Limiter（分桶隔离） |
| parallel 部分失败 | 用户仍算成功；失败渠道留 failed 流水 |
| cancelled 返回 nil | 会 ACK，MQ 侧不再重试（符合「别再发」） |
| Scheduler 成功 ≠ 渠道成功 | 两套口径，排障要分清 |

---

## 13. 文件索引

| 文件 | 职责 |
|------|------|
| [`cmd/pusher/main.go`](../cmd/pusher/main.go) | 进程入口、双队列 Consumer |
| [`internal/push/gateway.go`](../internal/push/gateway.go) | Gateway + Consumer + 限流/策略/发送 |
| [`internal/push/template.go`](../internal/push/template.go) | `{{var}}` 渲染 |
| [`internal/adapter/mq/redis_stream.go`](../internal/adapter/mq/redis_stream.go) | 并发消费、PEL、DLQ、MAXLEN/XTRIM、ACK+XDEL |
| [`internal/adapter/mq/priority_router.go`](../internal/adapter/mq/priority_router.go) | high/normal 分流（Pusher 用 High()/Normal()） |
| [`internal/adapter/repo/task.go`](../internal/adapter/repo/task.go) | `ClaimDelivery` / `UpdateRecordStatus` |
| [`internal/adapter/redis/client.go`](../internal/adapter/redis/client.go) | `HasDelivered` / `MarkDelivered` |
| [`internal/adapter/channel/registry.go`](../internal/adapter/channel/registry.go) | 渠道 SPI + stub |
| [`internal/domain/message.go`](../internal/domain/message.go) | `PushMessage` / `Effective*` |
| [`internal/domain/status.go`](../internal/domain/status.go) | `PushStatus` / `ChannelMode` |
| [`internal/app/callback/service.go`](../internal/app/callback/service.go) | 送达/点击回执（API 进程） |

---

*读完本文后，建议用一条真实 `PushMessage` 跟完：Consume → Handle → ClaimDelivery → stub Send → XACK，并对照 Scheduler 文档理解「入队成功」与「渠道成功」的分界。*
