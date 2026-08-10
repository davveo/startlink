package domain

import (
	"testing"
	"time"
)

func TestResolveRampUpQPS(t *testing.T) {
	stages := []RampUpStage{
		{AfterMin: 0, QPS: 50},
		{AfterMin: 10, QPS: 200},
		{AfterMin: 30, QPS: 1000},
	}
	cases := []struct {
		name    string
		elapsed time.Duration
		pace    int
		want    int
	}{
		{"起步阶段", 0, 0, 50},
		{"未到下一级", 9 * time.Minute, 0, 50},
		{"刚好进入第二级", 10 * time.Minute, 0, 200},
		{"最后一级之后保持", 5 * time.Hour, 0, 1000},
		{"pace_qps 更小时作为绝对上限", 5 * time.Hour, 100, 100},
		{"pace_qps 更大时不放大阶梯", 0, 5000, 50},
		{"负数耗时按 0 处理", -time.Minute, 0, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveRampUpQPS(stages, tc.elapsed, tc.pace); got != tc.want {
				t.Fatalf("ResolveRampUpQPS = %d，期望 %d", got, tc.want)
			}
		})
	}
}

func TestResolveRampUpQPSFallsBackToPace(t *testing.T) {
	if got := ResolveRampUpQPS(nil, time.Hour, 300); got != 300 {
		t.Fatalf("无阶梯时应回退 pace_qps，实际 %d", got)
	}
	if got := ResolveRampUpQPS(nil, time.Hour, 0); got != 0 {
		t.Fatalf("无阶梯且无 pace 时应返回 0（不限速），实际 %d", got)
	}
}

func TestNormalizeRampUpSortsAndDedups(t *testing.T) {
	got := NormalizeRampUp([]RampUpStage{
		{AfterMin: 30, QPS: 1000},
		{AfterMin: 0, QPS: 10},
		{AfterMin: 0, QPS: 50}, // 同一时刻保留后者
	})
	want := []RampUpStage{{AfterMin: 0, QPS: 50}, {AfterMin: 30, QPS: 1000}}
	if len(got) != len(want) {
		t.Fatalf("归一化后长度 = %d，期望 %d：%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %+v，期望 %+v", i, got[i], want[i])
		}
	}
}

func TestValidateRampUp(t *testing.T) {
	if err := ValidateRampUp(nil); err != nil {
		t.Fatalf("空阶梯应合法: %v", err)
	}
	if err := ValidateRampUp([]RampUpStage{{AfterMin: -1, QPS: 10}}); err == nil {
		t.Error("after_min 为负应报错")
	}
	if err := ValidateRampUp([]RampUpStage{{AfterMin: 0, QPS: 0}}); err == nil {
		t.Error("qps 为 0 应报错")
	}
	tooMany := make([]RampUpStage, maxRampUpStages+1)
	for i := range tooMany {
		tooMany[i] = RampUpStage{AfterMin: i, QPS: 1}
	}
	if err := ValidateRampUp(tooMany); err == nil {
		t.Error("超过阶梯上限应报错")
	}
}

func TestParseRampUpJSON(t *testing.T) {
	got := ParseRampUpJSON(`[{"after_min":10,"qps":200},{"after_min":0,"qps":50}]`)
	if len(got) != 2 || got[0].AfterMin != 0 || got[0].QPS != 50 {
		t.Fatalf("解析结果未按 after_min 排序: %+v", got)
	}
	for _, raw := range []string{"", "null", "[]", "not json", `{"a":1}`} {
		if s := ParseRampUpJSON(raw); s != nil {
			t.Errorf("ParseRampUpJSON(%q) 应返回 nil，实际 %+v", raw, s)
		}
	}
}
