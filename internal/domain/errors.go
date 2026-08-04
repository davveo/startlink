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
