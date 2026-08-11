# Starlink 功能待办清单（TODO）

更新日期：2026-08-06

基于代码与 README「已知限制」整理。按优先级推进；勾选表示已完成。

> **口径**：流水线终态按子任务入队；用户成功/失败与进度按 `push_records` 渠道结果（含 `suppressed` / `unreachable` 等抑制态分项）。

---

## 第一期（已完成）：可靠性与基础能力

### P0 · 可靠性（丢投 / 卡死）

- [x] **MQ PEL 自动重投**：`XAUTOCLAIM` / 读 pending、最大投递次数、死信队列（DLQ）；失败不 ACK 后可恢复消费
  - 实现：`internal/adapter/mq/redis_stream.go`；配置：`mq.redis_stream.*`
  - 暂停（`domain.ErrMainTaskPaused`）只留 PEL、不进 DLQ；达到 `max_delivery` 写入 `{topic}:dlq` 后 ACK
- [x] **Stream 容量治理**：`XADD MAXLEN` + 定期 `XTRIM`；ACK 后可选 `XDEL`
  - 配置：`maxlen` / `dlq_maxlen` / `maxlen_approx` / `trim_interval_sec` / `ack_xdel`
  - 默认主队列 10 万条、近似裁剪、60s 定期 trim、ACK 后删除条目；`-1` 可关闭对应能力
  - 注意：Redis `MAXLEN`/`XTRIM` 不感知 Consumer Group，上限过小可能裁掉仍在 PEL 的条目，请按积压水位留余量
- [x] **Pusher 真并发**：为每条消息起受限 goroutine，在 worker 内 ACK；让 `worker_concurrency` / `high_worker_concurrency` 真正生效
  - `RedisStream.Consume` / `MemoryQueue.Consume`：`batch`=并发度，worker 池内 `handleMessage`（含 ACK/PEL/DLQ）
  - `Consumer.Run` 将 concurrency 直接传给 `MQ.Consume`；渠道配额见 `channel_quota`
- [x] **拆分卡单恢复**：主任务 `pending→running` 后崩溃无子任务时，lease/心跳 + 扫描重入拆分
  - 字段：`main_tasks.split_owner` / `split_lease_at`；配置：`scheduler.split_lease_sec`（默认 90）
  - `MarkMainTaskRunning` 写租约；`Splitter` 每页 `RenewSplitLease`；结束 `ClearSplitLease`
  - `ListStaleSplitMainTasks` + `ClaimStaleSplitMainTask`（无子任务且租约过期）由 `loopSplit` 每轮回收
- [x] **子任务完成幂等**：`UpdateSubTaskResult` 校验 `worker_id`/状态；`OnSubFinished` 按子任务去重，避免超时重认领导致 `done` 虚高
  - `UpdateSubTaskResult(id, workerID, …)`：仅 `worker_id` 匹配且 `running|retrying` 时可写终态；`updated=false` 则不聚合
  - `TryMarkSubFinished`：Redis SET `starlink:task:{id}:sub_finished`；`SetSubDone`（重推对齐）时清空该集合

### P1 · 状态与口径正确性

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

### P2 · 产品能力（接通已有 SPI / 字段）

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

### P3 · 渠道配额（已完成）

- [x] **渠道配额 / 分布式限流**：`channel_quota` 按 channel×priority 分桶（Redis/进程内）；`ErrChannelThrottled` 留 PEL；Scheduler 反压 pace；enforce 准入拒创；429 自适应缩 QPS；拆分后超容量 warn/pause

### P4 · 性能与工程债

- [x] **主任务状态缓存**：Gateway `mainCache` TTL 300ms；单次 Handle 限流前后各刷新一次；doSend 仅 backoff 后强刷
- [x] **拆分并行**：`scheduler.split_concurrency`（默认 2）信号量并发 `runSplit`
- [x] **拆分流式落库**：分页 `CreateSubTasks`；拆分中 `Claim` 要求 `split_owner` 已清；卡单重拆先删半成品
- [x] **测试体系**：补 `NormalizeChannels` / `ResolvePriority` / `RenderTemplate` 单测（集成/E2E 仍可继续扩）
- [x] **错误处理规范化**：`sendParallel` 尊重 `waitToken`；Worker 用 `errors.Is`；空人群映射 `AudienceEmpty`
- [x] **工程清理**：`gofmt`；`go-sql-driver/mysql` 升为直接依赖；删除未用 `CreateRecords` / `CountSubTasksByStatus` / `MergeVars` / `Sanitize`
- [x] **Compose 构建解耦**：scheduler/pusher 各自 `build`，不再 `depends_on: api`

---

## 第二期（已完成）：正确性补强

一期完成后增量审查发现的逻辑风险，已全部修复。

- [x] **补齐退订最终校验**
  - Gateway 发送前按 `user_id + channel` 终检；营销（normal）Redis 失败 fail-closed 留 PEL，事务（high）fail-open；已退订记 `suppressed`
- [x] **主任务状态查询失败时禁止继续发送**
  - `loadMainSnap` 返回 error；`ErrMainStatusUnavailable` 计入 deferred requeue，不进 DLQ
- [x] **回执幂等改为数据库原子约束**
  - `(push_record_id, event)` 唯一索引 `uk_receipt_record_event` + `ON CONFLICT DO NOTHING`
- [x] **回执状态更新与回执落库保持一致**
  - `PushRepo.ApplyReceipt` 同事务完成状态机校验、流水更新与回执插入；主任务计数在事务后校准
- [x] **消除 `provider_id` 回调歧义**
  - `PushRecord.Provider` + `(provider, channel, provider_id)` 唯一键；回执入参支持 `provider`/`channel`；`GetRecordByProviderRef`
- [x] **调整活动创建幂等检查顺序**
  - 基础校验后优先查 `biz_id`；同一 `biz_id` 请求摘要不同返回 `40901 Conflict`
- [x] **防止人群分页游标死循环**
  - `advancePageToken` 校验 token 前进；限制最大页数 / 最大用户数 / 单页大小
- [x] **保留用户级 `Extra`**
  - 子任务 payload 存 `extras`；Worker `MergeExtra`（用户覆盖活动）；敏感字段加密属后续治理
- [x] **限制 HTTP Provider/Sender 响应体大小**
  - `io.LimitReader` 1MiB；限制 AudiencePage 用户数、变量数量和字段长度
- [x] **细化被抑制消息状态**
  - 新增 `suppressed` / `unreachable` / `expired` / `quota_rejected`；频控/退订→`suppressed`，渠道未注册→`unreachable`；进度接口分项统计

---

## 待办 · API 与运营能力

- [x] 活动列表与筛选：分页列表，支持按场景、状态、渠道、优先级、创建时间、计划时间和负责人筛选
  - `GET /api/v1/campaigns`（biz_scene/status/channel/priority/created_by/keyword/created_*/scheduled_*/page）
  - 前端「任务」页；子任务：`GET /api/v1/campaigns/:id/subtasks`
- [x] 批量操作：批量暂停、恢复、取消、重推和结果导出
  - `POST /api/v1/campaigns/batch/:action`（pause/resume/cancel/retry）；前端多选批量；同步/异步 CSV 导出见下
- [x] 活动预检（preflight）：创建前返回预计人群量、过滤量、渠道可达量、预计耗时、容量风险和费用估算
  - `POST /api/v1/campaigns/preflight`；前端「活动」页预检按钮
- [x] 人群试算接口：只统计人群或解析少量样本，不创建主任务、不发送消息
  - `POST /api/v1/audiences/estimate`；前端「人群试算」
- [x] 测试发送与 dry-run：完成模板渲染和渠道配置校验，但不进入正式统计
  - `POST /api/v1/campaigns/dry-run`（`send=false` 仅渲染；`send=true` 写入 `is_test` 流水）
- [x] 活动草稿与复制：支持草稿、复制、开始前编辑和重新排期
  - 状态 `draft`；创建 `as_draft`；`PUT /campaigns/:id`、`POST .../publish`、`POST .../copy`
- [x] 投递漏斗：原始人群 → AB 抽样 → 黑名单/退订 → 不可达 → 入队 → 发送 → 送达 → 点击
  - `GET /api/v1/campaigns/:id/funnel`；前端「分析」页
- [x] 失败分析：按渠道、供应商错误码、是否可重试和时间段汇总
  - `GET /api/v1/campaigns/:id/failures`（按 channel/provider/error_msg 聚合计数；时间窗/可重试标签可后续增强）
- [x] 结果明细与异步导出：提供用户级流水查询，大结果集导出到对象存储
  - `GET .../records`；同步 `GET .../export`；异步 `POST .../exports` + `GET /exports/:id[/download]`（当前落本地 `data/exports`，可换对象存储）
- [x] 运营概览：活动状态分布、近 24h 发送量/成功率、最近活动列表
  - `GET /api/v1/overview`；前端「概览」页
- [x] 消息通知中心：任务终态（成功/部分成功/失败/取消）写站内通知；列表/未读数/已读
  - 表 `notifications`；`GET /api/v1/notifications`、`/unread-count`；`POST .../:id/read`、`/read-all`
  - Aggregator 终态 + Cancel + 拆分失败写入；前端侧栏角标与「通知」页
  - SSE：`GET /api/v1/notifications/stream`（session 鉴权）；Create/已读经 Redis pub/sub → 进程内 Hub fan-out；前端 EventSource + 重连拉未读；弱化 2min 兜底轮询
- [x] 审计日志：写操作中间件记录（非 GET）；表 `audit_logs`；`GET /api/v1/audit-logs`；侧栏「审计日志」页
- [ ] DLQ / PEL 运维 API：查询、查看错误、单条/批量重投和丢弃；人工操作写审计日志（替代纯 redis-cli）

---

## 待办 · 模板与投放能力

- [x] 多渠道模板内容：同一模板为 App Push、短信、邮件等分别维护标题、正文和扩展字段
  - `Template.contents` JSON；活动快照 `template_contents`；Gateway 按渠道取内容，缺省回退 `body`+活动 `title`
- [x] 模板变量 Schema：声明必填变量、类型、默认值、示例值和敏感等级，创建活动时提前校验
  - `var_schema`；Create / Preflight / DryRun / Preview 校验
- [x] 模板预览与测试渲染：明确缺失变量策略（报错、保留或默认值）
  - `missing_var_policy`=`error|keep|default|empty`；`RenderTemplateWithPolicy`；`POST /templates/preview`
- [x] 模板版本历史与回滚：保存变更版本、审核记录、差异和回滚入口
  - 表 `template_versions`（`revision`）；更新/审核落快照；`GET .../versions`、`POST .../rollback`→draft
- [x] 多语言与地区版本：按用户 locale 选择模板，支持默认语言回退
  - `default_locale` + `locales`；Resolve：user → default → root body
- [x] 用户时区投放：静默时段和发送窗口按用户时区执行
  - `TargetUser/PushMessage.Timezone`；有 TZ 用用户时区，否则服务器本地
- [x] 实验平台化：增加 `experiment_id`、对照组、分层比例和结果指标；抽样哈希加入实验或活动盐值并持久化分组
  - MainTask 实验字段；盐值哈希；`ExperimentAssignment` 落库；流水带 `experiment_group`
  - `GET /api/v1/campaigns/:id/experiment` 聚合看板；前端「分析」页实验指标表
- [x] 活动过期时间：增加 `expire_at`，超时队列消息标记为 `expired`，不再调用渠道
  - Gateway 过期写 `PushStatusExpired` 并 ACK（不返 Err）；`ClaimDelivery` 走 `newRecord`
- [x] 发送策略扩展：支持所有渠道均成功、条件路由、成本优先和最大降级次数
  - `channel_mode=all_success|conditional|cost_priority`；`max_fallback`
  - `channel_routes` / `channel_costs` 落库；Gateway `ResolveSendChannels`；前端创建页 JSON 编辑

---

## 待办 · 多租户、安全与平台化

- [x] **API 鉴权授权**：运营台登录门禁（Session Cookie）+ MySQL RBAC；YAML 仅作库空 seed
- [ ] **多租户数据模型**：活动、模板、流水和配置增加 `tenant_id/app_id`；`biz_id` 改为租户内唯一
- [~] **RBAC 与审批策略**：角色/权限/用户三分页；MySQL 表 `auth_users` / `auth_roles` / `auth_role_permissions`；多级审批仍待办
  - 路由：`/settings/roles`、`/settings/permissions`、`/settings/users`
  - API：`/api/v1/rbac/roles|permissions|users`（写操作需 `rbac.manage`）
  - 密码 bcrypt；权限码目录仍以 `internal/auth/rbac.go` 为准
- [x] **回执接口鉴权与验签**：HMAC 签名 + 时间窗 + nonce 防重放（`internal/app/callback/verifier.go`；配置 `callback.*`）
- [~] **密钥与凭据管理**：已支持 7 个 `STARLINK_*` 环境变量覆盖敏感配置；per-tenant 凭据、Secret Manager 接入与轮换仍待办
- [~] **Webhook 安全与可靠**：主机白名单（防 SSRF）、签名、并发限制、内存重试已实现（`internal/adapter/webhook/client.go`）；持久化 outbox 仍待办
- [x] **服务端 HTTP 防护**：ReadHeader/Read/Write/Idle 超时、`max_body_bytes` 请求体上限、严格 JSON 解码（`cmd/api/main.go`、`internal/server/router.go`）
- [ ] **上游调用韧性**：Audience、Channel、Webhook 增加熔断、重试预算、连接池和请求指标
- [ ] **独立 DB 迁移**：提供 `cmd/migrate`；三进程不再并发 `AutoMigrate`，业务进程只检查 schema 版本
- [~] **Readiness / Liveness 分离**：api 已有 `/readyz`（检查 MySQL/Redis/MQ）；scheduler、pusher 仍无探针
- [ ] **可观测性**：Prometheus（队列积压、PEL、发送成功率、限流拒绝、拆分耗时等）+ OpenTelemetry；结构化日志关联 `trace_id/biz_id/task_id/msg_id`
- [x] **审计日志**：写操作中间件记录操作者、来源 IP、请求摘要（`internal/server/audit_mw.go`）；租户维度待多租户模型落地后补

---

## 待办 · 数据治理与工程质量

- [ ] 数据保留与归档：为流水、回执、DLQ 和任务配置 TTL/归档周期
- [ ] 隐私保护：用户标识和联系方式脱敏或加密；提供按用户查询与删除能力（含用户级 Extra 敏感字段）
- [ ] 数据库索引评审：按活动列表、流水、回执和归档查询补复合索引并执行 `EXPLAIN`
- [ ] 集成测试：使用真实 MySQL/Redis 验证并发幂等、租约、PEL、DLQ、回执事务和重推
- [ ] 端到端与故障注入：覆盖完整链路，并模拟进程退出、Redis/MySQL 短暂故障
- [ ] 竞态与模糊测试：CI 增加 `go test -race ./...` 和模板、JSON、频控、时窗 fuzz test
- [ ] 基准与容量测试：建立拆分、MQ、数据库、聚合和渠道限流基准
- [ ] 固定 Go 工具链：项目要求 Go 1.22+；在 CI、开发容器或版本管理文件中固定版本

---

## 建议迭代顺序

1. ~~第一期 P0～P4（可靠性、口径、SPI、配额、工程债）~~ ✅
2. ~~第二期正确性补强（退订终检、状态 fail-closed、回执事务/唯一键、provider 消歧、幂等顺序、分页保护、Extra、HTTP 限制、抑制态）~~ ✅
3. API 与运营：活动列表、预检、测试发送、投递漏斗、DLQ 运维 API
4. 安全与平台：~~登录门禁 + 简易 RBAC~~（租户/审批仍待）、回执验签、Webhook outbox、独立迁移、Readiness、指标
5. ~~模板与投放：多渠道模板、版本历史、多语言、用户时区、实验平台~~ ✅
6. 多租户、数据归档、隐私治理、集成/E2E/容量测试

---

## 相关文档

| 文档 | 说明 |
|------|------|
| [TODO_FEATURES.md](TODO_FEATURES.md) | **尚未建设的功能路线图**（本文件只记已完成整改与工程债） |
| [user-guide/用户使用手册.md](user-guide/用户使用手册.md) | **运营台用户使用手册**（含界面截图） |
| [README.md](../README.md) | 已知限制原文与修复建议 |
| [创建活动主流程.md](创建活动主流程.md) | 创建链路 |
| [Scheduler层代码解析.md](Scheduler层代码解析.md) | 调度层 |
| [Pusher层代码解析.md](Pusher层代码解析.md) | 推送层 |
| [优先级队列精读.md](优先级队列精读.md) | high/normal |
| [多渠道策略精读.md](多渠道策略精读.md) | single/fallback/parallel |

---

*新增能力请同步在本文件追加勾选框；完成项打勾并在 PR 中引用对应条目。本文件已合并原根目录 `TODO_V2.md`。*
