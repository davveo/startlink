package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RampUpStage 渐进放量阶梯：活动开始 AfterMin 分钟后，入队速率提升到 QPS。
// 大促首发时先小流量灰度，观察渠道回执正常再逐级放开，避免一把打满把厂商打挂。
type RampUpStage struct {
	AfterMin int `json:"after_min"`
	QPS      int `json:"qps"`
}

// maxRampUpStages 阶梯数量上限，防止构造超大 JSON 拖垮 worker
const maxRampUpStages = 20

// ParseRampUpJSON 解析放量阶梯；非法内容返回 nil（调用方回退固定 pace）
func ParseRampUpJSON(raw string) []RampUpStage {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var stages []RampUpStage
	if err := json.Unmarshal([]byte(raw), &stages); err != nil || len(stages) == 0 {
		return nil
	}
	return NormalizeRampUp(stages)
}

// NormalizeRampUp 按 after_min 升序排列并去重（同一时刻保留后者）
func NormalizeRampUp(stages []RampUpStage) []RampUpStage {
	if len(stages) == 0 {
		return nil
	}
	sort.SliceStable(stages, func(i, j int) bool { return stages[i].AfterMin < stages[j].AfterMin })
	out := make([]RampUpStage, 0, len(stages))
	for _, s := range stages {
		if n := len(out); n > 0 && out[n-1].AfterMin == s.AfterMin {
			out[n-1] = s
			continue
		}
		out = append(out, s)
	}
	return out
}

// ValidateRampUp 校验阶梯配置
func ValidateRampUp(stages []RampUpStage) error {
	if len(stages) == 0 {
		return nil
	}
	if len(stages) > maxRampUpStages {
		return fmt.Errorf("ramp_up 阶梯最多 %d 级", maxRampUpStages)
	}
	for i, s := range stages {
		if s.AfterMin < 0 {
			return fmt.Errorf("ramp_up[%d].after_min 不能为负", i)
		}
		if s.QPS <= 0 {
			return fmt.Errorf("ramp_up[%d].qps 必须大于 0", i)
		}
	}
	return nil
}

// ResolveRampUpQPS 按已运行时长解析当前入队速率上限。
// stages 为空时回退 paceQPS；两者都配置时取更保守的一方，
// 让 pace_qps 始终是活动的绝对上限。
func ResolveRampUpQPS(stages []RampUpStage, elapsed time.Duration, paceQPS int) int {
	if len(stages) == 0 {
		return paceQPS
	}
	elapsedMin := int(elapsed / time.Minute)
	if elapsedMin < 0 {
		elapsedMin = 0
	}
	qps := stages[0].QPS
	for _, s := range stages {
		if s.AfterMin > elapsedMin {
			break
		}
		qps = s.QPS
	}
	if paceQPS > 0 && paceQPS < qps {
		return paceQPS
	}
	return qps
}
