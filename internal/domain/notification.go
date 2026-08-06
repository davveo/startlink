package domain

import (
	"fmt"
	"time"
)

// NotificationLevel 通知级别
type NotificationLevel string

const (
	NotificationLevelInfo    NotificationLevel = "info"
	NotificationLevelSuccess NotificationLevel = "success"
	NotificationLevelWarning NotificationLevel = "warning"
	NotificationLevelError   NotificationLevel = "error"
)

// NotificationType 通知类型
type NotificationType string

const (
	NotificationTypeTaskFinished NotificationType = "task_finished"
)

// Notification 运营台站内通知
type Notification struct {
	ID            uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	Title         string            `gorm:"size:256;not null" json:"title"`
	Body          string            `gorm:"type:text" json:"body"`
	Level         NotificationLevel `gorm:"size:16;not null;index;default:info" json:"level"`
	Type          NotificationType  `gorm:"size:32;not null;index;default:task_finished" json:"type"`
	RelatedTaskID *uint64           `gorm:"index" json:"related_task_id,omitempty"`
	ReadAt        *time.Time        `gorm:"index" json:"read_at,omitempty"`
	CreatedBy     string            `gorm:"size:64" json:"created_by,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

func (Notification) TableName() string { return "notifications" }

// ListNotificationQuery 通知列表筛选
type ListNotificationQuery struct {
	UnreadOnly bool `form:"unread_only"`
	Page       int  `form:"page"`
	PageSize   int  `form:"page_size"`
}

// NewTaskTerminalNotification 根据主任务终态生成站内通知
func NewTaskTerminalNotification(task *MainTask, status TaskStatus) *Notification {
	if task == nil {
		return nil
	}
	st := status
	if st == "" {
		st = task.Status
	}
	title, body, level := taskTerminalCopy(task, st)
	tid := task.ID
	return &Notification{
		Title:         title,
		Body:          body,
		Level:         level,
		Type:          NotificationTypeTaskFinished,
		RelatedTaskID: &tid,
		CreatedBy:     task.CreatedBy,
	}
}

func taskTerminalCopy(task *MainTask, status TaskStatus) (title, body string, level NotificationLevel) {
	name := task.Title
	if name == "" {
		name = task.BizID
	}
	switch status {
	case TaskStatusSuccess:
		return "活动已成功完成",
			fmt.Sprintf("「%s」（#%d）已全部成功。用户成功 %d / 失败 %d。", name, task.ID, task.SuccessCount, task.FailCount),
			NotificationLevelSuccess
	case TaskStatusPartial:
		return "活动部分完成",
			fmt.Sprintf("「%s」（#%d）部分成功。用户成功 %d / 失败 %d。", name, task.ID, task.SuccessCount, task.FailCount),
			NotificationLevelWarning
	case TaskStatusFailed:
		return "活动失败结束",
			fmt.Sprintf("「%s」（#%d）已失败结束。用户成功 %d / 失败 %d。", name, task.ID, task.SuccessCount, task.FailCount),
			NotificationLevelError
	case TaskStatusCancelled:
		return "活动已取消",
			fmt.Sprintf("「%s」（#%d）已被取消。", name, task.ID),
			NotificationLevelInfo
	default:
		return "活动状态更新",
			fmt.Sprintf("「%s」（#%d）状态变为 %s。", name, task.ID, status),
			NotificationLevelInfo
	}
}
