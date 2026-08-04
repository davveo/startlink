package port

import (
	"context"

	"github.com/starlink/push/internal/domain"
)

// AudienceProvider 人群圈选 SPI。
// 业务方实现此接口即可快速对接，无需改动调度/推送核心。
// 支持按 BizScene 注册多个 Provider。
type AudienceProvider interface {
	// Name 提供者名称，用于注册与日志
	Name() string
	// Supports 是否支持该业务场景
	Supports(bizScene string) bool
	// Resolve 分页拉取目标人群（已含黑名单/退订等过滤，或由 Filter 二次过滤）
	Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error)
}

// AudienceFilter 特殊群体过滤（黑名单 / 退订 / 免打扰 / 合规）
type AudienceFilter interface {
	Filter(ctx context.Context, bizScene string, users []domain.TargetUser) ([]domain.TargetUser, error)
}

// ChannelSender 推送渠道 SPI。新增渠道只需实现并注册。
type ChannelSender interface {
	Channel() domain.ChannelType
	Send(ctx context.Context, req domain.SendRequest) (*domain.SendResult, error)
}

// MessageQueue 单队列抽象（一个 topic/stream + consumer group）。
// 业务只依赖本接口；具体驱动（Redis Stream / RocketMQ / Memory…）通过 mq.Register 插拔。
type MessageQueue interface {
	// Publish 批量投递；实现方应尽量原子或尽力而为
	Publish(ctx context.Context, msgs []domain.PushMessage) error
	// Consume 阻塞消费；batch 同时表示预取批量与最大并发 handler 数。
	// 实现方应在受限 worker 内调用 handler，并在该 worker 内完成 ACK / 失败重试（勿在同步读循环里串行处理）。
	Consume(ctx context.Context, consumerID string, batch int, handler func(ctx context.Context, msg domain.PushMessage) error) error
	// EnsureReady 初始化 topic / consumer group 等
	EnsureReady(ctx context.Context) error
}

// MessagePublisher 仅投递能力（Scheduler 只需 Publish）
type MessagePublisher interface {
	Publish(ctx context.Context, msgs []domain.PushMessage) error
}

// PriorityBroker 双优先级队列门面：Publish 按消息 Priority 分流；High/Normal 供 Pusher 独立消费。
type PriorityBroker interface {
	MessagePublisher
	Driver() string
	High() MessageQueue
	Normal() MessageQueue
	EnsureReady(ctx context.Context) error
	Close() error
}

// TaskRepository 主/子任务持久化
type TaskRepository interface {
	CreateMainTask(ctx context.Context, task *domain.MainTask) error
	GetMainTask(ctx context.Context, id uint64) (*domain.MainTask, error)
	GetMainTaskByBizID(ctx context.Context, bizID string) (*domain.MainTask, error)
	UpdateMainTaskStats(ctx context.Context, id uint64, version int64, successDelta, failDelta int64, subDoneDelta int, status domain.TaskStatus) (bool, error)
	// PatchMainMeta 写入拆分后的 total_count / sub_task_total；不改 status，且不覆盖 paused/cancelled/终态
	PatchMainMeta(ctx context.Context, id uint64, total int64, subTotal int) error
	// SyncMainUserCounts 仅校准主任务用户成功/失败数（渠道口径）
	SyncMainUserCounts(ctx context.Context, id uint64, success, fail int64) error
	// MarkMainTaskRunning 原子抢占 pending 主任务并写入拆分租约，返回是否抢占成功
	MarkMainTaskRunning(ctx context.Context, id uint64, workerID string) (bool, error)
	// RenewSplitLease 拆分过程中续约；必须仍由 workerID 持有租约
	RenewSplitLease(ctx context.Context, id uint64, workerID string) (bool, error)
	// ClearSplitLease 拆分结束（成功/失败）后清理租约字段
	ClearSplitLease(ctx context.Context, id uint64) error
	// ListStaleSplitMainTasks 列出 running、无子任务、租约过期的卡单主任务
	ListStaleSplitMainTasks(ctx context.Context, leaseTimeoutSec int, limit int) ([]domain.MainTask, error)
	// ClaimStaleSplitMainTask 抢占卡单拆分权（校验无子任务 + 租约过期）
	ClaimStaleSplitMainTask(ctx context.Context, id uint64, workerID string, leaseTimeoutSec int) (bool, error)
	// CancelMainTask 将可取消状态的主任务置为 cancelled，返回是否更新成功
	CancelMainTask(ctx context.Context, id uint64) (bool, error)
	// PauseMainTask 暂停主任务（pending/running/retrying → paused）
	PauseMainTask(ctx context.Context, id uint64) (bool, error)
	// ResumeMainTask 恢复暂停任务；hasSubTasks=true → running，否则 → pending
	ResumeMainTask(ctx context.Context, id uint64, hasSubTasks bool) (bool, error)
	// ReopenMainTask 将 failed/partial 重新打开为 running，供失败重推（不含 running）
	ReopenMainTask(ctx context.Context, id uint64, addSubTasks int) (bool, error)
	ListPendingMainTasks(ctx context.Context, limit int) ([]domain.MainTask, error)

	CreateSubTasks(ctx context.Context, tasks []domain.SubTask) error
	// DeleteSubTasksByMainTask 删除主任务下全部子任务（卡单重拆前清理半成品）
	DeleteSubTasksByMainTask(ctx context.Context, mainTaskID uint64) (int64, error)
	// ClaimSubTask 原子认领一个待执行/超时子任务（水平扩展核心）；已取消主任务下的子任务不会被认领
	ClaimSubTask(ctx context.Context, workerID string, claimTimeoutSec int) (*domain.SubTask, error)
	// CancelUnfinishedSubTasks 批量取消未完成子任务（pending/running/retrying）
	CancelUnfinishedSubTasks(ctx context.Context, mainTaskID uint64) (int64, error)
	// ReleaseSubTask 将执行中的子任务释放回 pending（暂停场景）
	ReleaseSubTask(ctx context.Context, id uint64) error
	// ResetFailedSubTasks 将失败子任务重置为 pending，返回影响行数
	ResetFailedSubTasks(ctx context.Context, mainTaskID uint64) (int64, error)
	// MaxShardIndex 当前最大分片号，无子任务返回 -1
	MaxShardIndex(ctx context.Context, mainTaskID uint64) (int, error)
	// ListSubTasksByStatus 按状态列出子任务（失败重推解析用户）
	ListSubTasksByStatus(ctx context.Context, mainTaskID uint64, status domain.TaskStatus) ([]domain.SubTask, error)
	// SyncMainCounters 对齐主任务计数（重推/重置后）
	SyncMainCounters(ctx context.Context, id uint64, success, fail int64, subDone, subTotal int) error
	// UpdateSubTaskResult 仅当仍由 workerID 认领且处于 running/retrying 时写入终态；updated=false 表示丢认领或已终态
	UpdateSubTaskResult(ctx context.Context, id uint64, workerID string, success, fail int, status domain.TaskStatus, lastErr string) (updated bool, err error)
	// SummarizeSubTasks 按状态汇总子任务数与用户数，用于进度查询
	SummarizeSubTasks(ctx context.Context, mainTaskID uint64) ([]domain.SubTaskStatusSummary, error)
}

// UserPushOutcomes 按 push_records 汇总的用户级渠道口径
type UserPushOutcomes struct {
	SuccessUsers int64 // 任一渠道 sent/delivered/clicked 的去重用户数
	FailUsers    int64 // 有失败且无任何渠道成功的去重用户数
	HasRecords   bool  // 是否已有流水
}

// PushRepository 推送流水与回执
type PushRepository interface {
	UpdateRecordStatus(ctx context.Context, id uint64, status domain.PushStatus, providerID, errMsg string) error
	GetRecordByProviderID(ctx context.Context, providerID string) (*domain.PushRecord, error)
	CreateReceipt(ctx context.Context, receipt *domain.PushReceipt) error
	// ListFailedUserIDs 查询主任务下「无任何渠道成功」且存在失败流水的用户
	ListFailedUserIDs(ctx context.Context, mainTaskID uint64) ([]string, error)
	// CountUserOutcomes 用户级成功/失败口径（以渠道结果为准）
	CountUserOutcomes(ctx context.Context, mainTaskID uint64) (UserPushOutcomes, error)
	// ClaimDelivery 按 (main_task, user, channel) 占位；duplicate=true 表示已成功投递应跳过
	// inFlight=true 表示另一 worker 正在发送，调用方宜稍后重试
	ClaimDelivery(ctx context.Context, rec *domain.PushRecord) (id uint64, duplicate, inFlight bool, err error)
}

// AggregatorCache 子任务终态计数 / 频控 / 投递去重（Redis）
type AggregatorCache interface {
	IncrSubDone(ctx context.Context, mainTaskID uint64, success, fail int64) (done int64, err error)
	GetSubDone(ctx context.Context, mainTaskID uint64) (success, fail, done int64, err error)
	// SetSubDone 重推时重置/对齐计数（同时清空子任务完成去重集合）
	SetSubDone(ctx context.Context, mainTaskID uint64, success, fail, done int64) error
	// TryMarkSubFinished 按子任务幂等标记完成；first=true 表示首次，应累加聚合计数
	TryMarkSubFinished(ctx context.Context, mainTaskID, subTaskID uint64) (first bool, err error)
	// Allow 频控：返回是否允许推送
	Allow(ctx context.Context, key string, limit int, windowSec int) (bool, error)
	// HasDelivered 用户+活动+渠道是否已成功投递（快路径）
	HasDelivered(ctx context.Context, mainTaskID uint64, userID string, channel domain.ChannelType) (bool, error)
	// MarkDelivered 标记成功投递，TTL 覆盖活动生命周期
	MarkDelivered(ctx context.Context, mainTaskID uint64, userID string, channel domain.ChannelType, ttlSec int) error
	// ClearDelivered 失败重推前清除成功标记（一般失败路径不写 mark，此接口供显式重推）
	ClearDelivered(ctx context.Context, mainTaskID uint64, userID string, channel domain.ChannelType) error
}

// TemplateRepository 模板中心
type TemplateRepository interface {
	Create(ctx context.Context, tpl *domain.Template) error
	Update(ctx context.Context, tpl *domain.Template) error
	// UpdateCAS 乐观锁更新：WHERE id=? AND version=?，成功则 version+1
	UpdateCAS(ctx context.Context, tpl *domain.Template, expectedVersion int64) (bool, error)
	GetByID(ctx context.Context, id uint64) (*domain.Template, error)
	GetByCode(ctx context.Context, code string) (*domain.Template, error)
	Delete(ctx context.Context, id uint64) error
	List(ctx context.Context, q domain.ListTemplateQuery) ([]domain.Template, int64, error)
}

// Notifier 终态通知（Webhook）
type Notifier interface {
	NotifyTaskFinished(ctx context.Context, url string, event domain.WebhookEvent) error
}

// ChannelLimiter 渠道级配额限流（按 channel × priority 分桶；可选全局保护闸）。
// Wait 超时应返回 domain.ErrChannelThrottled（留 PEL，不记业务失败）。
type ChannelLimiter interface {
	Enabled() bool
	Wait(ctx context.Context, channel domain.ChannelType, priority domain.Priority) error
	// Utilization 约 0~1；越高表示越接近打满
	Utilization(ctx context.Context, channel domain.ChannelType, priority domain.Priority) (float64, error)
	// AvailableQPS 当前有效可用 QPS（含 high 预留与自适应收缩）
	AvailableQPS(channel domain.ChannelType, priority domain.Priority) float64
	// ObserveVendorThrottle 厂商限流反馈（如 429）
	ObserveVendorThrottle(ctx context.Context, channel domain.ChannelType)
	// AdmissionMode soft | enforce；未配置渠道返回 soft
	AdmissionMode(channel domain.ChannelType) string
	TargetFinishMinutes(channel domain.ChannelType) int
}
