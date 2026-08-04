package domain

import "time"

// WebhookEvent 终态回调事件
type WebhookEvent struct {
	Event        string      `json:"event"` // task.finished
	TaskID       uint64      `json:"task_id"`
	BizID        string      `json:"biz_id"`
	BizScene     string      `json:"biz_scene"`
	Title        string      `json:"title"`
	Channel      ChannelType `json:"channel"`
	Status       TaskStatus  `json:"status"`
	TotalCount   int64       `json:"total_count"`
	SuccessCount int64       `json:"success_count"`
	FailCount    int64       `json:"fail_count"`
	SubTaskTotal int         `json:"sub_task_total"`
	SubTaskDone  int         `json:"sub_task_done"`
	FinishedAt   *time.Time  `json:"finished_at,omitempty"`
	Timestamp    time.Time   `json:"timestamp"`
}
