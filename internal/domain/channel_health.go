package domain

import "time"

// ChannelHealth 渠道健康快照（滑动窗口统计，非持久化）
type ChannelHealth struct {
	Channel   ChannelType `json:"channel"`
	WindowSec int         `json:"window_sec"`
	Total     int64       `json:"total"`
	Success   int64       `json:"success"`
	Failed    int64       `json:"failed"`
	Throttled int64       `json:"throttled"`
	// SuccessRate 成功率；样本不足时为 1（视作健康，避免冷启动误降级）
	SuccessRate float64 `json:"success_rate"`
	// Degraded 是否处于自动降级中：命中后该渠道在降级窗口内被跳过
	Degraded      bool       `json:"degraded"`
	DegradedUntil *time.Time `json:"degraded_until,omitempty"`
	Reason        string     `json:"reason,omitempty"`
}

// Healthy 是否可用于投放
func (h ChannelHealth) Healthy() bool { return !h.Degraded }

// ChannelSLARow 渠道 SLA 看板行（按 push_records 聚合）
type ChannelSLARow struct {
	Channel   ChannelType `json:"channel"`
	Provider  string      `json:"provider,omitempty"`
	Total     int64       `json:"total"`
	Sent      int64       `json:"sent"`
	Delivered int64       `json:"delivered"`
	Clicked   int64       `json:"clicked"`
	Failed    int64       `json:"failed"`
	// Suppressed 频控/退订/偏好等主动抑制，不计入渠道质量
	Suppressed int64 `json:"suppressed"`
	// SendSuccessRate 已提交给渠道且未失败的占比
	SendSuccessRate float64 `json:"send_success_rate"`
	// DeliveryRate 送达率 = delivered+clicked / sent+delivered+clicked
	DeliveryRate float64 `json:"delivery_rate"`
	ClickRate    float64 `json:"click_rate"`
	FailureRate  float64 `json:"failure_rate"`
	// AvgSendLatencyMs 创建到 sent_at 的平均耗时
	AvgSendLatencyMs float64 `json:"avg_send_latency_ms"`
}

// ComputeRates 由计数派生比率，保证前后端口径一致
func (r *ChannelSLARow) ComputeRates() {
	attempted := r.Sent + r.Delivered + r.Clicked + r.Failed
	if attempted > 0 {
		r.SendSuccessRate = float64(r.Sent+r.Delivered+r.Clicked) / float64(attempted)
		r.FailureRate = float64(r.Failed) / float64(attempted)
	}
	submitted := r.Sent + r.Delivered + r.Clicked
	if submitted > 0 {
		r.DeliveryRate = float64(r.Delivered+r.Clicked) / float64(submitted)
		r.ClickRate = float64(r.Clicked) / float64(submitted)
	}
}

// ChannelSLAQuery SLA 看板查询窗口
type ChannelSLAQuery struct {
	Since    *time.Time `form:"-"`
	Until    *time.Time `form:"-"`
	Channel  string     `form:"channel"`
	BizScene string     `form:"biz_scene"`
}
