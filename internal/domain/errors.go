package domain

import "errors"

// ErrMainTaskPaused 主任务暂停：MQ 应留在 PEL 等待重投，且不计入死信。
var ErrMainTaskPaused = errors.New("main task paused")

// ErrOutsideSendWindow 不在投放时窗：留 PEL 稍后重投，不进 DLQ。
var ErrOutsideSendWindow = errors.New("outside send window")

// ErrQuietHours 免打扰时段：留 PEL 稍后重投，不进 DLQ。
var ErrQuietHours = errors.New("quiet hours")

// ErrChannelThrottled 渠道配额等待超时：留 PEL 稍后重投，不进 DLQ，不记业务失败。
var ErrChannelThrottled = errors.New("channel throttled")

// ErrMainStatusUnavailable 主任务状态查询失败：fail-closed，留 PEL 待恢复后再发。
var ErrMainStatusUnavailable = errors.New("main task status unavailable")

// ErrUnsubscribed 用户已退订该渠道（Gateway 终检）。
var ErrUnsubscribed = errors.New("user unsubscribed")

// ErrAudiencePageStuck 人群分页游标未前进或 HasMore 无 token，防止死循环。
var ErrAudiencePageStuck = errors.New("audience page cursor stuck")

// ErrAudienceLimitExceeded 超过最大页数或最大用户数保护上限。
var ErrAudienceLimitExceeded = errors.New("audience resolve limit exceeded")
