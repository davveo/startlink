# Starlink 功能待办清单（TODO）

基于当前代码与 README「已知限制」整理。按优先级推进；勾选表示已完成。

> 口径已落地：流水线终态按子任务入队；用户成功/失败与进度按 `push_records` 渠道结果（见 P1）。

---

## P0 · 可靠性（丢投 / 卡死）

- [x] **MQ PEL 自动重投**：`XAUTOCLAIM` / 读 pending、最大投递次数、死信队列（DLQ）；失败不 ACK 后可恢复消费
  - 实现：`internal/adapter/mq/redis_stream.go`；配置：`mq.redis_stream.*`
  - 暂停（`domain.ErrMainTaskPaused`）只留 PEL、不进 DLQ；达到 `max_delivery` 写入 `{topic}:dlq` 后 ACK
- [x] **Stream 容量治理**：`XADD MAXLEN` + 定期 `XTRIM`；ACK 后可选 `XDEL`
  - 配置：`maxlen` / `dlq_maxlen` / `maxlen_approx` / `trim_interval_sec` / `ack_xdel`
  - 默认主队列 10 万条、近似裁剪、60s 定期 trim、ACK 后删除条目；`-1` 可关闭对应能力
  - 注意：Redis `MAXLEN`/`XTRIM` 不感知 Consumer Group，上限过小可能裁掉仍在 PEL 的条目，请按积压水位留余量
- [x] **Pusher 真并发**：为每条消息起受限 goroutine，在 worker 内 ACK；让 `worker_concurrency` / `high_worker_concurrency` 真正生效
  - `RedisStream.Consume` / `MemoryQueue.Consume`：`batch`=并发度，worker 池内 `handleMessage`（含 ACK/PEL/DLQ）
  - `Consumer.Run` 将 concurrency 直接传给 `MQ.Consume`；渠道配额见 P3 `channel_quota`
- [x] **拆分卡单恢复**：主任务 `pending→running` 后崩溃无子任务时，lease/心跳 + 扫描重入拆分
  - 字段：`main_tasks.split_owner` / `split_lease_at`；配置：`scheduler.split_lease_sec`（默认 90）
  - `MarkMainTaskRunning` 写租约；`Splitter` 每页 `RenewSplitLease`；结束 `ClearSplitLease`
  - `ListStaleSplitMainTasks` + `ClaimStaleSplitMainTask`（无子任务且租约过期）由 `loopSplit` 每轮回收
- [x] **子任务完成幂等**：`UpdateSubTaskResult` 校验 `worker_id`/状态；`OnSubFinished` 按子任务去重，避免超时重认领导致 `done` 虚高
  - `UpdateSubTaskResult(id, workerID, …)`：仅 `worker_id` 匹配且 `running|retrying` 时可写终态；`updated=false` 则不聚合
  - `TryMarkSubFinished`：Redis SET `starlink:task:{id}:sub_finished`；`SetSubDone`（重推对齐）时清空该集合

---

## P1 · 状态与口径正确性

- [x] **活动成功口径定义并落地**
  - **流水线终态**（`success` / `partial` / `failed`）：仍按子任务入队结果聚合（Redis `GetSubDone`）
  - **用户成功/失败展示与主任务 `success_count`/`fail_count`**：以 `push_records` 渠道口径为准（任一渠道 `sent|delivered|clicked` 算成功用户；有失败且无成功渠道算失败用户）
  - 校准时机：聚合终态、回执处理、`realignCounters`（重推后）
- [x] **聚合计数可靠**：`UpdateMainTaskStats` 先无锁原子递增计数；仅终态切换 CAS `version`；冲突重试时增量置 0，不丢 `sub_done`
- [x] **暂停与拆分互斥**：`PatchMainMeta` 收口 `TaskRepository`，不写 `status`，排除 `paused`/`cancelled`/终态；`Splitter` 直接调用，禁止类型断言静默失败
- [x] **失败重推语义打磨**：仅 `failed`/`partial` 可重推；`ReopenMainTask` 不再接受 `running`；兜底不同步抹零 `fail`；`RetryResult.Status` 读库；重推前 `ClearDelivered` 清 dedup
- [x] **回执幂等状态机**：`PushStatus.CanTransitTo` 单向前进；同 `(record,event)` 回执去重；仅首次 `sent` 写 `sent_at`；失败保留/写入 `error_msg`，成功路径才清空
- [x] **进度与流水对齐**：`buildProgress` 有流水时以 `CountUserOutcomes` 为准
- [x] **回执错误分类**：流水不存在 → 404；其它 DB 错误原样返回（不再一律 404）

---

## P2 · 产品能力（接通已有 SPI / 字段）

- [x] **真实人群 Provider**：`audience.http` HTTP 圈人优先注册；Demo 仅支持 `demo_scenes`（默认 `demo`/`dev`），不再兜底所有 `biz_scene`
- [x] **真实渠道 Sender**：`pusher.channels.*.mode=http` 覆盖 stub；HTTP 状态映射 `Retryable`（5xx/429 可重试，4xx 不可）；内置 stub 支持 `Extra.force_fail`
- [x] **用户可达渠道**：Splitter `IntersectChannels(任务链, TargetUser.Channels)`；子任务 payload 落 `channels`；Worker 按用户写入 `PushMessage`
- [x] **频控 / 免打扰**：`freq.*` 配置；Gateway 调 `Allow`（用户/用户+渠道/场景）；`quiet_hours` 返回 `ErrQuietHours` 留 PEL
- [x] **Title / Payload 透传**：Worker → `PushMessage.Title/Extra` → `SendRequest`
- [x] **`campaign.default_channel` 生效**：创建活动未指定渠道时回填
- [x] **黑名单 / 退订 AudienceFilter**：Redis SET `compliance.blacklist_key` / `unsubscribe_key_prefix{channel}`；保留 `_block` 演示 Filter
- [x] **模板并发与乐观锁**：空 `code` 用 `tmp_{uuid}` 占位再改 `tpl_{id}`；`version` + `UpdateCAS`
- [x] **定时 / 分段投放增强**：`send_windows` / `pace_qps`；窗外 `ErrOutsideSendWindow` 留 PEL；`audience_extra.ab_sample_percent` 抽样
- [x] **扩展渠道类型**：`wecom` / `dingtalk`（Valid + stub Register）
- [x] **RocketMQ 生产 Transport**：`-tags rocketmq` 启用官方客户端；`TryInitRocketTransport` 由 bootstrap 注入

---

## P3 · 安全、运维与可观测

- [ ] **API 鉴权授权**：创建/取消/暂停/重推/模板审核需认证；按租户或业务线授权
- [ ] **回执接口鉴权与验签**：防伪造送达/点击
- [ ] **Webhook 安全与可靠**：URL 白名单（防 SSRF）、签名、失败重试、outbox 持久化
- [x] **渠道配额 / 分布式限流**：`channel_quota` 按 channel×priority 分桶（Redis/进程内）；`ErrChannelThrottled` 留 PEL；Scheduler 反压 pace；enforce 准入拒创；429 自适应缩 QPS；拆分后超容量 warn/pause
- [ ] **独立 DB 迁移任务**：三进程不再并发 `AutoMigrate`；启动只做 schema 校验
- [ ] **Readiness / 健康检查**：探测 MySQL、Redis、MQ，而非空 `/healthz`
- [ ] **Prometheus 指标**：队列积压、PEL、发送成功率、限流拒绝、拆分耗时等
- [ ] **链路追踪**：API → Scheduler → MQ → Pusher → 渠道，统一 trace/biz_id
- [ ] **审计日志**：活动与模板关键操作落审计
- [ ] **运维查询 API**：PEL/卡单查询、手工修复入口（替代纯 redis-cli）

---

## P4 · 性能与工程债

- [x] **主任务状态缓存**：Gateway `mainCache` TTL 300ms；单次 Handle 限流前后各刷新一次；doSend 仅 backoff 后强刷
- [x] **拆分并行**：`scheduler.split_concurrency`（默认 2）信号量并发 `runSplit`
- [x] **拆分流式落库**：分页 `CreateSubTasks`；拆分中 `Claim` 要求 `split_owner` 已清；卡单重拆先删半成品
- [x] **测试体系**：补 `NormalizeChannels` / `ResolvePriority` / `RenderTemplate` 单测（集成/E2E 仍可继续扩）
- [x] **错误处理规范化**：`sendParallel` 尊重 `waitToken`；Worker 用 `errors.Is`；空人群映射 `AudienceEmpty`
- [x] **工程清理**：`gofmt`；`go-sql-driver/mysql` 升为直接依赖；删除未用 `CreateRecords` / `CountSubTasksByStatus` / `MergeVars` / `Sanitize`
- [x] **Compose 构建解耦**：scheduler/pusher 各自 `build`，不再 `depends_on: api`

---

## 建议迭代顺序

1. P0：~~PEL 重投 + DLQ~~、~~Stream 容量治理~~、~~Pusher 真并发~~、~~拆分卡单恢复~~（已完成）  
2. P0：（P3）~~限流隔离 / 分布式限流~~（已完成：`channel_quota`）  
3. P1：~~口径 / 聚合 CAS / `PatchMainMeta` / 重推 / 回执状态机 / 进度对齐~~（已完成）  
4. P2：~~人群/渠道 SPI、可达渠道、频控、Title/Payload、default_channel、合规 Filter、模板 CAS、分时窗/AB、企微钉钉、RocketMQ Transport~~（已完成）  
6. P4：~~主任务状态缓存 / 拆分并行+流式落库 / 单测 / 错误规范化 / 工程清理 / Compose 解耦~~（已完成）  
7. P3：鉴权、Webhook、指标、独立迁移（可与运维项并行）  

---

## 相关文档

| 文档 | 说明 |
|------|------|
| [README.md](../README.md) | 已知限制原文与修复建议 |
| [创建活动主流程.md](创建活动主流程.md) | 创建链路 |
| [Scheduler层代码解析.md](Scheduler层代码解析.md) | 调度层 |
| [Pusher层代码解析.md](Pusher层代码解析.md) | 推送层 |
| [优先级队列精读.md](优先级队列精读.md) | high/normal |
| [多渠道策略精读.md](多渠道策略精读.md) | single/fallback/parallel |

---

*新增能力请同步在本文件追加勾选框；完成项打勾并在 PR 中引用对应条目。*
