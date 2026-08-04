package domain

import (
	"encoding/json"
	"time"
)

// MainTask 主任务：一次营销推送活动
type MainTask struct {
	ID            uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	BizID         string      `gorm:"size:64;uniqueIndex;not null" json:"biz_id"`
	BizScene      string      `gorm:"size:64;index;not null" json:"biz_scene"`
	Priority      Priority    `gorm:"size:16;not null;index;default:normal" json:"priority"` // high=事务通知 normal=营销
	Title         string      `gorm:"size:256;not null" json:"title"`
	Channel       ChannelType `gorm:"size:32;not null;index" json:"channel"`
	Channels      string      `gorm:"type:json" json:"channels,omitempty"`
	ChannelMode   ChannelMode `gorm:"size:16;not null;default:single" json:"channel_mode"`
	TemplateID    string      `gorm:"size:64" json:"template_id"`
	TemplateBody  string      `gorm:"type:text" json:"template_body"`
	AudienceRef   string      `gorm:"size:128;not null" json:"audience_ref"`
	AudienceExtra string      `gorm:"type:json" json:"audience_extra,omitempty"`
	Payload       string      `gorm:"type:json" json:"payload,omitempty"`
	TotalCount    int64       `gorm:"not null;default:0" json:"total_count"`
	SuccessCount  int64       `gorm:"not null;default:0" json:"success_count"`
	FailCount     int64       `gorm:"not null;default:0" json:"fail_count"`
	SubTaskTotal  int         `gorm:"not null;default:0" json:"sub_task_total"`
	SubTaskDone   int         `gorm:"not null;default:0" json:"sub_task_done"`
	Status        TaskStatus  `gorm:"size:32;not null;index;default:pending" json:"status"`
	Version       int64       `gorm:"not null;default:0" json:"version"`
	WebhookURL    string      `gorm:"size:512" json:"webhook_url,omitempty"`
	// SendWindowsJSON 分时投放窗 JSON，如 [{"start":"09:00","end":"21:00"}]
	SendWindowsJSON string `gorm:"type:json;column:send_windows" json:"send_windows,omitempty"`
	// PaceQPS 本活动入队速率上限；0 不限制
	PaceQPS int `gorm:"not null;default:0" json:"pace_qps,omitempty"`
	// SplitOwner / SplitLeaseAt：拆分租约，防止 pending→running 后崩溃导致永久卡单
	SplitOwner   string     `gorm:"size:64;index" json:"split_owner,omitempty"`
	SplitLeaseAt *time.Time `gorm:"index" json:"split_lease_at,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (MainTask) TableName() string { return "main_tasks" }

// SendWindows 解析分时窗
func (t *MainTask) SendWindows() []SendWindow {
	if t.SendWindowsJSON == "" || t.SendWindowsJSON == "null" {
		return nil
	}
	var list []SendWindow
	if err := json.Unmarshal([]byte(t.SendWindowsJSON), &list); err != nil {
		return nil
	}
	return list
}

// ChannelList 解析渠道链
func (t *MainTask) ChannelList() []ChannelType {
	if t.Channels == "" || t.Channels == "null" {
		if t.Channel != "" {
			return []ChannelType{t.Channel}
		}
		return nil
	}
	var list []ChannelType
	if err := json.Unmarshal([]byte(t.Channels), &list); err != nil || len(list) == 0 {
		if t.Channel != "" {
			return []ChannelType{t.Channel}
		}
		return nil
	}
	return list
}

// EffectiveChannelMode 有效渠道模式
func (t *MainTask) EffectiveChannelMode() ChannelMode {
	mode := t.ChannelMode.Normalize()
	chs := t.ChannelList()
	if len(chs) <= 1 {
		return ChannelModeSingle
	}
	if mode == ChannelModeSingle {
		return ChannelModeFallback
	}
	return mode
}

// SubTask 子任务：按用户分片，支持多 worker 并发认领
type SubTask struct {
	ID           uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MainTaskID   uint64     `gorm:"index;not null" json:"main_task_id"`
	ShardIndex   int        `gorm:"not null" json:"shard_index"`
	UserIDs      string     `gorm:"type:mediumtext;not null" json:"user_ids"` // JSON 数组
	TotalCount   int        `gorm:"not null;default:0" json:"total_count"`
	SuccessCount int        `gorm:"not null;default:0" json:"success_count"`
	FailCount    int        `gorm:"not null;default:0" json:"fail_count"`
	Status       TaskStatus `gorm:"size:32;not null;index;default:pending" json:"status"`
	RetryCount   int        `gorm:"not null;default:0" json:"retry_count"`
	WorkerID     string     `gorm:"size:64;index" json:"worker_id,omitempty"` // 认领者，便于水平扩展抢占
	ClaimedAt    *time.Time `json:"claimed_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	LastError    string     `gorm:"size:512" json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (SubTask) TableName() string { return "sub_tasks" }

// PushRecord 推送流水。唯一键 (main_task_id, user_id, channel) 防重复投递。
type PushRecord struct {
	ID         uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	MainTaskID uint64      `gorm:"uniqueIndex:uk_task_user_channel;not null" json:"main_task_id"`
	SubTaskID  uint64      `gorm:"index;not null" json:"sub_task_id"`
	UserID     string      `gorm:"size:64;uniqueIndex:uk_task_user_channel;not null" json:"user_id"`
	Channel    ChannelType `gorm:"size:32;uniqueIndex:uk_task_user_channel;not null" json:"channel"`
	Content    string      `gorm:"type:text" json:"content"`
	Status     PushStatus  `gorm:"size:32;not null;index;default:queued" json:"status"`
	ProviderID string      `gorm:"size:128;index" json:"provider_id,omitempty"` // 渠道侧消息 ID
	ErrorMsg   string      `gorm:"size:512" json:"error_msg,omitempty"`
	SentAt     *time.Time  `json:"sent_at,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

func (PushRecord) TableName() string { return "push_records" }

// PushReceipt 回执
type PushReceipt struct {
	ID           uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	PushRecordID uint64       `gorm:"index;not null" json:"push_record_id"`
	MainTaskID   uint64       `gorm:"index;not null" json:"main_task_id"`
	SubTaskID    uint64       `gorm:"index;not null" json:"sub_task_id"`
	UserID       string       `gorm:"size:64;index;not null" json:"user_id"`
	Channel      ChannelType  `gorm:"size:32;not null" json:"channel"`
	Event        ReceiptEvent `gorm:"size:32;not null;index" json:"event"`
	RawPayload   string       `gorm:"type:text" json:"raw_payload,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

func (PushReceipt) TableName() string { return "push_receipts" }
