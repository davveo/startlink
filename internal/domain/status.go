package domain

// TaskStatus 主/子任务状态机
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"   // 待执行
	TaskStatusRunning   TaskStatus = "running"   // 执行中
	TaskStatusPaused    TaskStatus = "paused"    // 已暂停（可恢复）
	TaskStatusSuccess   TaskStatus = "success"   // 全部成功
	TaskStatusPartial   TaskStatus = "partial"   // 部分失败
	TaskStatusFailed    TaskStatus = "failed"    // 全部失败
	TaskStatusCancelled TaskStatus = "cancelled" // 已取消
	TaskStatusRetrying  TaskStatus = "retrying"  // 重试中
)

func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskStatusSuccess, TaskStatusPartial, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func (s TaskStatus) IsCancellable() bool {
	switch s {
	case TaskStatusPending, TaskStatusRunning, TaskStatusPaused, TaskStatusRetrying:
		return true
	default:
		return false
	}
}

func (s TaskStatus) IsPausable() bool {
	switch s {
	case TaskStatusPending, TaskStatusRunning, TaskStatusRetrying:
		return true
	default:
		return false
	}
}

func (s TaskStatus) IsResumable() bool {
	return s == TaskStatusPaused
}

func (s TaskStatus) IsRetryable() bool {
	switch s {
	case TaskStatusFailed, TaskStatusPartial:
		return true
	default:
		return false
	}
}

// PushStatus 单条推送流水状态
type PushStatus string

const (
	PushStatusQueued    PushStatus = "queued"
	PushStatusSending   PushStatus = "sending"
	PushStatusSent      PushStatus = "sent"
	PushStatusDelivered PushStatus = "delivered"
	PushStatusClicked   PushStatus = "clicked"
	PushStatusFailed    PushStatus = "failed"
	PushStatusCancelled PushStatus = "cancelled" // 因主任务取消而跳过
)

// DeliveredOK 是否已成功投递（去重终态）
func (s PushStatus) DeliveredOK() bool {
	switch s {
	case PushStatusSent, PushStatusDelivered, PushStatusClicked:
		return true
	default:
		return false
	}
}

// Reclaimable 失败/取消/排队态可被失败重推或 MQ 重试重新占位
func (s PushStatus) Reclaimable() bool {
	switch s {
	case PushStatusFailed, PushStatusCancelled, PushStatusQueued:
		return true
	default:
		return false
	}
}

// CanTransitTo 流水状态单向前进（含幂等同态与占位回收）。
// queued → sending → sent → delivered → clicked；失败/取消可回到 sending/queued 以重投。
func (from PushStatus) CanTransitTo(to PushStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case PushStatusQueued:
		return to == PushStatusSending || to == PushStatusFailed || to == PushStatusCancelled
	case PushStatusSending:
		return to == PushStatusSent || to == PushStatusFailed || to == PushStatusCancelled || to == PushStatusQueued
	case PushStatusSent:
		return to == PushStatusDelivered || to == PushStatusClicked || to == PushStatusFailed
	case PushStatusDelivered:
		return to == PushStatusClicked || to == PushStatusFailed
	case PushStatusClicked:
		return false
	case PushStatusFailed, PushStatusCancelled:
		return to == PushStatusSending || to == PushStatusQueued
	default:
		return false
	}
}

// ChannelType 推送渠道
type ChannelType string

const (
	ChannelAppPush  ChannelType = "app_push" // APNs / FCM
	ChannelSMS      ChannelType = "sms"
	ChannelEmail    ChannelType = "email"
	ChannelInbox    ChannelType = "inbox"    // 站内信 / IM
	ChannelWecom    ChannelType = "wecom"    // 企业微信
	ChannelDingtalk ChannelType = "dingtalk" // 钉钉
)

func (c ChannelType) Valid() bool {
	switch c {
	case ChannelAppPush, ChannelSMS, ChannelEmail, ChannelInbox, ChannelWecom, ChannelDingtalk:
		return true
	default:
		return false
	}
}

// ChannelMode 多渠道推送策略
type ChannelMode string

const (
	ChannelModeSingle   ChannelMode = "single"   // 单渠道（默认）
	ChannelModeFallback ChannelMode = "fallback" // 按配置顺序依次降级，成功即停
	ChannelModeParallel ChannelMode = "parallel" // 同内容多渠道并行，任一成功即算成功
)

func (m ChannelMode) Valid() bool {
	switch m {
	case "", ChannelModeSingle, ChannelModeFallback, ChannelModeParallel:
		return true
	default:
		return false
	}
}

func (m ChannelMode) Normalize() ChannelMode {
	if m == "" {
		return ChannelModeSingle
	}
	return m
}

// ReceiptEvent 回执事件类型
type ReceiptEvent string

const (
	ReceiptDelivered ReceiptEvent = "delivered"
	ReceiptClicked   ReceiptEvent = "clicked"
	ReceiptFailed    ReceiptEvent = "failed"
)
