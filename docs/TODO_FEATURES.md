# Starlink 功能路线图（TODO）

更新日期：2026-08-09

**本文件只登记"尚未建设"的能力。** 已完成的可靠性整改、口径修正与工程债见 [TODO.md](TODO.md)，两者互补，不重复登记。

每项标注：**现状**（附代码证据路径，便于核实结论是否仍然成立）+ **要做什么**。优先级分四档：

| 档位 | 含义 |
|------|------|
| **P0** | 生产上线前的硬门槛，缺失会导致线上不可运维或数据无限膨胀 |
| **P1** | 平台化与运营效率，决定这套系统能否被多方接入、被运营独立使用 |
| **P2** | 增长与分析闭环，决定推送效果能否被度量和优化 |
| **P3** | 体验与长期工程质量 |

---

## 一、P0 · 生产上线必备

### 1. 可观测性：指标、追踪、日志关联

- [ ] **Prometheus 指标端点**
  - 现状：全库无 `prometheus` / `/metrics` / `pprof` 引用；仅有 `slog` 文本日志（`pkg/applog/applog.go`）
  - 要做：暴露 `/metrics`，核心指标至少覆盖——队列积压与 PEL 深度、DLQ 增量、各渠道发送成功率与 P99 时延、限流拒绝数、拆分耗时、聚合滞后、子任务认领速率
- [ ] **OpenTelemetry 链路追踪**
  - 现状：无 otel 依赖；API → Scheduler → Pusher 三进程调用链无法串联
  - 要做：接入 otel SDK，MQ 消息头透传 trace context，使一次活动投递可端到端追踪
- [x] **业务全链路 trace（混合粒度）**
  - 已做：创建活动生成 `trace_id`（`tr_*`）落 `main_tasks`；经 Splitter/Worker/`PushMessage`/Gateway/Callback/Aggregator 写入 `trace_events`
  - 粒度：活动/子任务节点全量；用户级仅失败/抑制/限流/延期/过期等异常（成功靠 `push_records` 下钻）
  - 查询：`GET /api/v1/traces`、`/traces/:trace_id`、`/trace-events`；运营台「全链路追踪」页；权限 `menu.traces` / `trace.view`
  - 热路径 fail-open（异步缓冲满则丢弃并打 warn）
- [x] **请求 ID 中间件**
  - 已做：`X-Request-Id`（`req_*`）中间件；创建活动响应另写 `X-Trace-Id`（活动级，不等于 request_id）
  - 待做：统一 slog 字段规范（`trace_id` / `biz_id` / `task_id` / `msg_id` / `service`）与 `applog.Options.Service`

### 2. 独立数据库迁移

- [ ] **`cmd/migrate` 独立迁移工具**
  - 现状：三个进程启动时均调用 `repo.AutoMigrate`（`internal/bootstrap/bootstrap.go`）；虽有 `GET_LOCK('starlink_schema_migrate')` 串行化（`internal/adapter/repo/task.go`），但仍无版本表、无回滚、无 DDL review
  - 要做：引入 goose / golang-migrate，业务进程启动只校验 schema 版本号，不执行 DDL

### 3. 健康探针补齐

- [ ] **Scheduler / Pusher 健康端点**
  - 现状：只有 api 有 `/healthz` + `/readyz`（`internal/handler/handler.go`、`internal/server/router.go`）；scheduler、pusher 无 HTTP server，`docker-compose.yml` 也未给这两个服务配 healthcheck
  - 要做：两进程各起轻量 HTTP 探针，上报 worker 心跳、消费滞后、最近一次成功处理时间
- [ ] **Readiness 深化**
  - 现状：`/readyz` 检查 MySQL / Redis / MQ（`redis_stream` 与 `memory` 驱动跳过 MQ 检查，见 `cmd/api/main.go`）
  - 要做：补充 Consumer Group 存在性、Audience Provider 可达性检查

### 4. CI 流水线

- [ ] **基础质量门禁**
  - 现状：无 `.github/` 与 `.gitlab-ci.yml`；`Makefile` 只有 build / tidy / docker 目标，无 test
  - 要做：`go test -race ./...` + `gofmt` + `golangci-lint` + `govulncheck` + 覆盖率报告；前端补 `tsc` 与 lint 门禁；固定 Go 工具链版本

### 5. 集成与端到端测试

- [ ] **真实依赖集成测试**
  - 现状：25 个 `*_test.go` 全部是单元/纯函数级，无 testcontainers、miniredis 或 sqlmock
  - 要做：用 testcontainers 起真实 MySQL + Redis，覆盖并发幂等、拆分租约、子任务认领、PEL/DLQ、回执事务、失败重推
- [ ] **端到端与故障注入**
  - 现状：无跨 api → scheduler → pusher 的链路测试
  - 要做：全链路用例 + 模拟进程退出、Redis/MySQL 瞬时不可用、渠道超时
- [ ] **基准与容量测试**
  - 现状：无 `testing.B`，无压测脚本
  - 要做：建立拆分吞吐、MQ 吞吐、聚合延迟、渠道限流的基准线，作为容量规划依据

### 6. 数据保留与归档

- [ ] **流水与日志的 TTL / 归档**
  - 现状：`push_records`、`push_receipts`、`audit_logs`、`notifications` 均只有写入与查询，无任何 purge / archive 逻辑（`internal/adapter/repo/*.go`）。表会无限增长
  - 要做：按表配置保留周期，定时归档到冷存或分区裁剪；`audit_logs` 需保留更久，单独策略
- [ ] **导出文件清理**
  - 现状：异步导出落本地 `data/exports/`（`cmd/api/main.go`），`export_jobs` 无过期删除
  - 要做：文件 TTL 清理 + 落对象存储（`internal/domain/task.go` 注释已提示）

### 7. Webhook 持久化 Outbox

- [ ] **终态事件 outbox**
  - 现状：内存异步 + 3 次指数退避，超限仅 `slog.Warn` 丢弃（`internal/adapter/webhook/client.go`）；进程重启即丢失在途回调
  - 要做：事件落 `webhook_outbox` 表，独立 worker 消费重试，支持死信与手工重放

### 8. DLQ / PEL 运维 API

- [ ] **死信运维接口**
  - 现状：只能用 redis-cli 手工排查（README「运维与排查」章节）
  - 要做：查询 DLQ 列表与错误详情、单条/批量重投、丢弃；所有人工操作写审计日志

---

## 二、P1 · 平台化

### 9. 多租户

- [ ] **租户数据模型**
  - 现状：`internal/domain/` 全目录无 `tenant_id` / `app_id` / `org_id`；`biz_id` 是全局唯一索引（`internal/domain/task.go`）
  - 要做：活动、模板、流水、配置增加 `tenant_id`；`biz_id` 改为租户内唯一；RBAC 增加租户维度；所有查询强制租户过滤

### 10. 密钥与凭据管理

- [ ] **多租户渠道凭据**
  - 现状：渠道地址与密钥来自全局配置（`internal/config/config.go`），`ChannelSender` 接口无鉴权上下文（`internal/port/port.go`）
  - 要做：per-tenant / per-app 凭据存储与注入，支持 API Key、OAuth、mTLS
- [ ] **Secret Manager 接入与轮换**
  - 现状：7 个 `STARLINK_*` 环境变量可覆盖（`internal/config/config.go`），但 `configs/config.docker.yaml` 仍内置明文默认口令，无 Vault / K8s Secrets 集成，无轮换方案
  - 要做：接入外部密钥源；补 session / callback / webhook 密钥的零停机轮换流程

### 11. 活动审批与版本

- [ ] **活动审批工作流**
  - 现状：模板有 `pending_review` / approve / reject（`internal/domain/template.go`），活动 `Publish` 直接 `draft → pending`（`internal/app/campaign/ops.go`），无审核态
  - 要做：活动增加审核状态与多级审批，大额人群或高风险场景强制走审批
- [ ] **活动版本历史**
  - 现状：模板有 `template_versions` 快照与 rollback，活动只有当前草稿可 `UpdateDraft`，无版本表
  - 要做：草稿变更留痕、版本对比、回滚
- [ ] **已发布活动的受限编辑**
  - 现状：发布后只能 pause / cancel / retry，改不了受众与模板
  - 要做：定义可安全热改的字段子集（如 `pace_qps`、`expire_at`），支持运行中调整

### 12. 服务韧性与防护

- [ ] **API 层限流与登录防爆破**
  - 现状：`internal/server/router.go` 无 throttle 中间件；`internal/auth/session.go` 无失败锁定
  - 要做：接口级限流 + 登录失败计数锁定 + 验证码
- [ ] **熔断与重试预算**
  - 现状：MQ `max_delivery`、Gateway `max_retry`、Webhook 3 次各自独立，无全局预算；渠道故障只有线性重试，无熔断
  - 要做：上游调用（Audience / Channel / Webhook）加熔断与统一重试预算
- [ ] **按渠道重试策略**
  - 现状：全局 `maxRetry` 应用于所有渠道（`cmd/pusher/main.go`、`internal/push/gateway.go`）
  - 要做：重试次数、退避曲线、超时按渠道独立配置

### 13. API 文档

- [ ] **OpenAPI / Swagger**
  - 现状：仅 README 手写接口说明，无机器可读契约
  - 要做：生成 OpenAPI 规范，供接入方与契约测试使用

---

## 三、P1–P2 · 运营与投放能力

> **进度（2026-08-10）：15 人群资产化、16 用户偏好中心已端到端可用；14 / 17 / 18 仍只有底座。**
>
> 已交付并验证（`go build`、`go vet`、`go test ./internal/...` 全绿，前端 `npm run build` 通过，
> 本地 docker 栈跑通 `scripts/smoke_segments_preferences.sh` 40/40 与 `scripts/smoke_delivery_optout.sh` 11/11）：
>
> - **人群段 / 排除名单 / 黑名单退订**：repo + service + API + 前端两个页面；创建活动时 `segment_code` 展开为 `audience_ref`，`exclude_segment_code` 在拆分阶段剔除成员（冒烟验证：6 人人群排除 2 人后只产生 4 条流水）
> - **用户偏好中心**：偏好 CRUD + 差量同意审计 + 带 TTL/容量上限/负缓存的 Resolver；Gateway 发送前终检营销总开关、主题、渠道与用户级免打扰（冒烟验证：退订用户 `suppressed`，原因写入 `error_msg`，其余 7 人正常 `sent`）
> - **跨活动营销频次上限**：`freq.marketing_limit` / `marketing_window_sec`，只约束 normal 优先级，默认 0 不启用
>
> 尚未打通的底座（模型、接口、字段已就位，缺 repo/service/API/前端与引擎集成）：
>
> - 领域模型：`CampaignSchedule` / `CampaignScheduleRun`（`internal/domain/campaign_schedule.go`）、`RampUpStage`（`internal/domain/rampup.go`）、`ChannelHealth` / `ChannelSLARow`（`internal/domain/channel_health.go`）
> - 自研 5 段 cron 解析器 `internal/domain/cron.go`（离线环境不引入新依赖，含单测）
> - `MainTask.RampUpJSON` / `ScheduleID`；`ChannelContent` 与 `SendRequest` 的 `provider_template_id` / `provider_sign_name`
> - `ScheduleRepository`、`ChannelHealthTracker` 接口；`PushRepo.AggregateChannelSLA` 已实现但无 API
>
> 下方标记含义：`[x]` = 端到端可用，`[~]` = 仅有底座，`[ ]` = 未动工。

### 14. 周期性活动

- [~] **Cron / 重复投放**
  - 现状：`MainTask.ScheduledAt` 是单次绝对时间，`ListPendingMainTasks` 只比较 `scheduled_at <= now`（`internal/adapter/repo/task.go`）
  - 已有：`CampaignSchedule` / `CampaignScheduleRun` 模型（`(schedule_id, planned_at)` 唯一键保证多实例只派生一次）、cron 解析与 `ComputeNext`、`ScheduleRepository` 接口
  - 待做：repo 实现、service、CRUD API、调度器派生循环、前端页面（暂停周期 / 跳过单次 / 执行历史）

### 15. 人群资产化

- [x] **可复用人群段（Segment）**
  - 已交付：`internal/adapter/repo/segment.go`、`internal/app/segment/service.go`、`internal/handler/segment.go`、`web/src/pages/SegmentsPage.tsx`
  - CRUD + 成员数刷新（翻页统计有 50 页 / 20 万人上限，触顶标注为估算下界）；创建活动传 `segment_code` 时服务端展开为 `audience_ref` 与 `audience_extra`，活动自带参数优先
  - 人群段被活动引用时禁止删除，接口返回引用数
- [x] **排除名单**
  - 已交付：`Splitter.SetExcludeResolver` + `segment.Service.ResolveExcludeUserIDs`，拆分阶段一次性解析后逐用户剔除
  - 排除段停用或解析超限时**整单失败**而非静默跳过——少剔一部分等于对这批人误发
  - 未注入解析器却配了 `exclude_segment_code` 的活动会直接报错，不会静默忽略
- [x] **黑名单与退订管理面**
  - 已交付：`internal/adapter/repo/suppression.go`、`internal/adapter/redis/suppression.go`、`web/src/pages/SuppressionPage.tsx`
  - DB 为权威副本、Redis SET 为发送链路快路径，写入顺序 DB → Redis；Redis 同步失败不回滚 DB 并在返回值中说明
  - 批量导入幂等（`ON CONFLICT DO NOTHING`，返回真实新增数），支持前端导出 CSV
  - 遗留：`RebuildSuppressionCache`（从 DB 重建 Redis）已实现但未挂运维接口

### 16. 用户偏好中心

- [x] **偏好中心 API**
  - 已交付：`internal/adapter/repo/preference.go`、`internal/app/preference/{resolver,service}.go`、`internal/handler/preference.go`、`web/src/pages/PreferencesPage.tsx`
  - Resolver 带 TTL、容量上限与负缓存；`Invalidate` 只清本进程，跨进程靠 TTL 收敛，pusher 侧建议 15~30s
  - 遗留：当前只有需要后台登录的管理接口，**面向终端用户的 H5 退订页还需要一套按签名 token 鉴权的匿名端点**
- [x] **按主题/品类订阅**
  - `topic` 贯穿 `CreateCampaignInput` → `MainTask` → `PushMessage`，未显式指定时回退 `biz_scene`
  - Gateway 在 `checkUnsubscribed` 中调用 `UserPreference.Blocks()` 终检
- [x] **用户级免打扰**
  - `Gateway.inUserQuietHours` 与全局 `quiet_hours` 取并集：用户设置只会更严，不能放宽平台策略
  - 时区优先用户自带，其次活动时区；high 优先级不受约束
- [x] **跨活动营销频次上限**
  - `freq.marketing_limit` / `freq.marketing_window_sec`，键为 `mkt:<user_id>`，只统计 normal 优先级
  - 默认 0（不启用），避免升级后突然开始拦截存量投放
  - 遗留：活动互斥规则未做
- [x] **同意与偏好变更审计**
  - `ConsentLog` 差量记录：营销开关、每个渠道与主题的增删、免打扰与期望时段变更各一条，`scope` 形如 `channel:sms` / `topic:promotion`
  - 未变化的字段不产生记录（冒烟已验证重复提交不新增）
  - 遗留：时区变更不记 consent（非同意语义）

### 17. 投放策略进阶

- [~] **智能发送时间（STO）**
  - 已有：`UserPreference.PreferredHour` 字段
  - 待做：Gateway 按期望时段改派，以及基于历史打开率反推 preferred_hour 的离线任务（**主体逻辑未动工**）
- [~] **渐进式放量**
  - 已有：`RampUpStage` 模型、`ResolveRampUpQPS`（与 `pace_qps` 取更保守值）、`MainTask.RampUpJSON`
  - 待做：`internal/scheduler/worker.go` 的 pace 循环改用阶梯速率；按成功率自动刹车
- [ ] **多步 Drip 旅程**
  - 现状：域模型只有 `MainTask` / `SubTask`，一次活动 = 一次拆分 + 一次投递
  - 待做：步骤序列与触发链（如「未点击则 3 天后再发一次」）（**尚未动工，工作量最大，建议独立排期**）
- [ ] **全链路 Dry-run 仿真**
  - 现状：Dry-run 针对单个 `user_id`（`internal/app/campaign/ops.go`），无法模拟整活动的频控与时窗命中
  - 待做：全量仿真并输出报告（**尚未动工**）

### 18. 渠道生产化

- [ ] **真实渠道 SDK 接入**
  - 现状：6 种渠道默认全部是 `stubSender`（`internal/adapter/channel/registry.go`），生产投递依赖外部 HTTP 适配层
  - 待做：至少接入一家真实厂商作为参考实现（**需要厂商账号与密钥，无法在本地闭环验证**）
- [~] **渠道健康度与自动降级**
  - 已有：`ChannelHealth` 类型与 `ChannelHealthTracker` 接口（约定 tracker 不可用时 fail-open）
  - 待做：Redis 滑动窗口实现、Gateway 选渠道前查询并跳过降级渠道、人工解除降级 API
- [~] **厂商模板 ID 一等支持**
  - 已有：`ChannelContent.ProviderTemplateID` / `ProviderSignName`、`SendRequest` 同名字段、`ResolveProviderTemplate()`
  - 待做：Gateway 发送时透传、创建活动时校验强监管渠道必须带模板号
- [~] **渠道 SLA 看板**
  - 已有：`ChannelSLARow` 与 `PushRepo.AggregateChannelSLA`（抑制类状态不计入成功率分母）
  - 待做：查询 API + 前端看板；配额水位需与 `ChannelLimiter.Utilization` 合并展示

---

## 四、P2 · 增长与分析

### 19. 实验能力完善

- [ ] **多臂实验**
  - 现状：只有 `control` / `treatment` 二分（`internal/adapter/audience/registry.go`）
  - 要做：支持 N 个变体与任意流量配比
- [ ] **变体级差异化内容**
  - 现状：活动绑定单一模板快照（`internal/app/campaign/service.go`），实验只控制"发或不发"
  - 要做：变体可绑定不同模板、标题、渠道与发送时间
- [ ] **实验独立实体**
  - 现状：实验参数直接挂在 `MainTask` 上（`internal/domain/task.go`），无 `experiments` 表
  - 要做：跨活动的实验资源与生命周期管理
- [ ] **显著性检验与自动决胜**
  - 现状：`AggregateExperiment` 只算成功率计数（`internal/adapter/repo/task.go`）
  - 要做：p-value、置信区间、样本量估算、自动选出胜出变体
- [ ] **运行中流量再分配**
  - 现状：`control_percent` 创建时固定，运行中不可调
  - 要做：灰度过程中动态调整变体流量
- [ ] **全局 Holdout**
  - 现状：抽样盐值是 per-campaign，无跨活动对照组
  - 要做：全局 holdout 组，度量推送整体增量价值

### 20. 转化闭环

- [ ] **转化事件与目标**
  - 现状：漏斗止于 `clicked`（`internal/domain/status.go`），回执只有 delivered / clicked / failed（`internal/app/callback/service.go`），无 conversion 模型
  - 要做：业务方回传转化事件，活动可配置目标与归因窗口，输出 ROI
- [ ] **链接包装与主动点击追踪**
  - 现状：点击完全依赖渠道回执上报，`SendRequest` 无 tracking id，无短链模块
  - 要做：短链服务 + UTM 注入，平台自主采集点击

### 21. 分析与数据流通

- [ ] **跨活动分析**
  - 现状：`OverviewView` 只有状态计数与最近 8 条活动（`internal/app/campaign/overview.go`）
  - 要做：cohort、留存、渠道对比、场景对比报表
- [ ] **时序指标**
  - 现状：漏斗是静态计数（`internal/port/port.go`）
  - 要做：按小时/渠道的发送量与 CTR 曲线
- [ ] **数仓同步**
  - 现状：导出只写本地 CSV（`internal/app/campaign/ops.go`）
  - 要做：流水与事件同步到 Kafka / 对象存储 / 数仓，支撑离线分析

---

## 五、P3 · 体验与工程质量

- [ ] **富媒体与交互组件建模**：`ChannelContent` 目前是纯文本 title / body + 自由 extra，缺按钮、Deep Link、图片的结构化 schema（`internal/domain/template_content.go`）
- [ ] **模板版本 diff 与引用分析**：已有 `ListVersions`，缺版本对比 API；停用模板前无法查询被哪些活动引用（`internal/app/template/service.go`）
- [ ] **隐私合规**：用户标识与联系方式脱敏或加密；按用户查询与删除（GDPR 式）能力；用户级 `Extra` 敏感字段治理
- [ ] **数据库索引评审**：按活动列表、流水、回执、归档查询补复合索引并逐条 `EXPLAIN`
- [ ] **K8s / Helm 部署清单**：当前仅 docker-compose，无 manifests、资源 limits、HPA、PDB、preStop hook
- [ ] **Scheduler 优雅退出**：`internal/scheduler/worker.go` 在 ctx 取消时直接退出循环，未 drain 在途子任务
- [ ] **前端 E2E**：`web/` 无 Playwright / Cypress，无前端 CI
- [ ] **备份与灾备**：无 MySQL / Redis 备份脚本，无 RPO / RTO 定义与恢复演练
- [ ] **memory / rocketmq 驱动可靠性对齐**：两个驱动未实现与 `redis_stream` 同等的 PEL / DLQ 语义

---

## 建议推进顺序

1. **先让系统可运维**：CI 门禁 → Prometheus 指标 + 请求 ID → scheduler/pusher 探针 → 独立迁移工具
2. **再让数据不失控**：流水与日志保留策略 → 导出落对象存储 → Webhook outbox → DLQ 运维 API
3. **然后平台化**：多租户模型 → 凭据管理 → 活动审批与版本
4. ~~**接着补运营效率**：人群段与排除名单 → 偏好中心~~ —— 已完成（见第三节 15、16）；剩余的周期性活动（14）、渐进放量与仿真（17）、渠道生产化（18）建议按此序推进
5. **最后做增长闭环**：多臂实验 → 转化追踪 → 跨活动分析与数仓同步

集成测试建议不要排在最后，而是伴随第 1、2 步同步补齐——多租户与审批这类改动会大范围触碰现有查询，没有集成测试兜底风险很高。

---

## 已确认完成（勿重复建设）

以下能力在排查中确认已实现，`TODO.md` 中的旧标记已同步更新：

| 能力 | 实现位置 |
|------|----------|
| 回执 HMAC 验签 + 时间窗 + nonce 防重放 | `internal/app/callback/verifier.go` |
| Webhook 主机白名单、签名、并发限制 | `internal/adapter/webhook/client.go` |
| 服务端 HTTP 超时与请求体上限 | `cmd/api/main.go`、`internal/server/router.go` |
| Readiness 探针（MySQL / Redis / MQ） | `internal/handler/handler.go`、`internal/server/router.go` |
| 审计日志（操作者、IP、请求摘要） | `internal/server/audit_mw.go`、`internal/adapter/repo/audit.go` |
| MySQL RBAC（角色/权限/用户） | `internal/auth/`、`internal/handler/rbac.go` |
| 渠道分布式配额与 429 自适应 | `internal/adapter/redis/quota.go` |
| Redis Stream PEL / DLQ / XTRIM | `internal/adapter/mq/redis_stream.go` |

---

*新增能力请在本文件追加条目；完成后打勾并在 PR 中引用。缺陷修复与口径整改记入 [TODO.md](TODO.md)。*
