package domain

// Priority 推送优先级：事务通知走高优队列，营销促销走普通队列
type Priority string

const (
	PriorityHigh   Priority = "high"   // 事务通知（独立 Stream / Consumer Group）
	PriorityNormal Priority = "normal" // 营销促销（默认）
)

func (p Priority) Valid() bool {
	switch p {
	case "", PriorityHigh, PriorityNormal:
		return true
	default:
		return false
	}
}

func (p Priority) Normalize() Priority {
	if p == "" {
		return PriorityNormal
	}
	return p
}

func (p Priority) IsHigh() bool {
	return p.Normalize() == PriorityHigh
}

// ResolvePriority 显式 priority 优先；未传时按 highBizScenes 映射 biz_scene
func ResolvePriority(explicit Priority, bizScene string, highBizScenes []string) Priority {
	if explicit == PriorityHigh || explicit == PriorityNormal {
		return explicit
	}
	for _, s := range highBizScenes {
		if s != "" && s == bizScene {
			return PriorityHigh
		}
	}
	return PriorityNormal
}
