package domain

import (
	"testing"
	"time"
)

func mustCron(t *testing.T, expr string) *CronSchedule {
	t.Helper()
	c, err := ParseCron(expr)
	if err != nil {
		t.Fatalf("ParseCron(%q) 意外失败: %v", expr, err)
	}
	return c
}

func TestParseCronRejectsBadInput(t *testing.T) {
	cases := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * * 13 *",
		"*/0 * * * *",
		"5-1 * * * *",
		"a * * * *",
	}
	for _, expr := range cases {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q) 应当报错，实际通过", expr)
		}
	}
}

func TestCronNext(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name string
		expr string
		from time.Time
		want time.Time
	}{
		{
			name: "每分钟",
			expr: "* * * * *",
			from: time.Date(2026, 8, 10, 9, 30, 20, 0, loc),
			want: time.Date(2026, 8, 10, 9, 31, 0, 0, loc),
		},
		{
			name: "每天 09:00",
			expr: "0 9 * * *",
			from: time.Date(2026, 8, 10, 9, 0, 0, 0, loc),
			want: time.Date(2026, 8, 11, 9, 0, 0, 0, loc),
		},
		{
			name: "每 15 分钟",
			expr: "*/15 * * * *",
			from: time.Date(2026, 8, 10, 9, 7, 0, 0, loc),
			want: time.Date(2026, 8, 10, 9, 15, 0, 0, loc),
		},
		{
			name: "跨月",
			expr: "0 0 1 * *",
			from: time.Date(2026, 8, 10, 0, 0, 0, 0, loc),
			want: time.Date(2026, 9, 1, 0, 0, 0, 0, loc),
		},
		{
			name: "仅工作日",
			expr: "30 8 * * 1-5",
			from: time.Date(2026, 8, 8, 12, 0, 0, 0, loc), // 周六
			want: time.Date(2026, 8, 10, 8, 30, 0, 0, loc),
		},
		{
			name: "周日用 0 表示",
			expr: "0 10 * * 0",
			from: time.Date(2026, 8, 10, 0, 0, 0, 0, loc), // 周一
			want: time.Date(2026, 8, 16, 10, 0, 0, 0, loc),
		},
		{
			name: "列表取值",
			expr: "0 9,18 * * *",
			from: time.Date(2026, 8, 10, 10, 0, 0, 0, loc),
			want: time.Date(2026, 8, 10, 18, 0, 0, 0, loc),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustCron(t, tc.expr).Next(tc.from)
			if !got.Equal(tc.want) {
				t.Fatalf("Next() = %s，期望 %s", got, tc.want)
			}
		})
	}
}

// 日与周同时被限制时取「或」，这是 crontab 的既有惯例，容易实现成「与」。
func TestCronNextDayOrWeekday(t *testing.T) {
	c := mustCron(t, "0 0 13 * 5") // 每月 13 号，或每周五
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	got := c.Next(from)
	want := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) // 13 号恰为周四，靠 dom 命中
	if !got.Equal(want) {
		t.Fatalf("Next() = %s，期望 %s", got, want)
	}
}

// 2 月 30 日永不成立，必须收敛返回零值而不是死循环。
func TestCronNextImpossibleExpr(t *testing.T) {
	c := mustCron(t, "0 0 30 2 *")
	if got := c.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); !got.IsZero() {
		t.Fatalf("不可能的表达式应返回零值，实际 %s", got)
	}
}

func TestCronNextPreservesLocation(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("时区库不可用: %v", err)
	}
	c := mustCron(t, "0 9 * * *")
	got := c.Next(time.Date(2026, 8, 10, 10, 0, 0, 0, sh))
	want := time.Date(2026, 8, 11, 9, 0, 0, 0, sh)
	if !got.Equal(want) {
		t.Fatalf("Next() = %s，期望 %s", got, want)
	}
}

func TestScheduleComputeNextRespectsEndAt(t *testing.T) {
	end := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	s := &CampaignSchedule{CronExpr: "0 * * * *", Timezone: "UTC", EndAt: &end}
	next, err := s.ComputeNext(time.Date(2026, 8, 10, 11, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ComputeNext 失败: %v", err)
	}
	if !next.Equal(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("期望命中 12:00，实际 %s", next)
	}
	// 越过 end_at 后应返回零值表示不再触发
	next, err = s.ComputeNext(time.Date(2026, 8, 10, 12, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ComputeNext 失败: %v", err)
	}
	if !next.IsZero() {
		t.Fatalf("超过 end_at 应返回零值，实际 %s", next)
	}
}

func TestScheduleComputeNextRespectsMaxRuns(t *testing.T) {
	s := &CampaignSchedule{CronExpr: "0 * * * *", Timezone: "UTC", MaxRuns: 3, RunCount: 3}
	next, err := s.ComputeNext(time.Date(2026, 8, 10, 11, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ComputeNext 失败: %v", err)
	}
	if !next.IsZero() {
		t.Fatalf("达到 max_runs 应返回零值，实际 %s", next)
	}
}

func TestScheduleBizIDStableForSamePlannedTime(t *testing.T) {
	s := &CampaignSchedule{Code: "weekly-promo", Timezone: "UTC"}
	planned := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	if a, b := s.BizIDFor(planned), s.BizIDFor(planned); a != b {
		t.Fatalf("同一触发时刻 biz_id 应稳定: %s vs %s", a, b)
	}
	if got, want := s.BizIDFor(planned), "weekly-promo-20260810T0900"; got != want {
		t.Fatalf("BizIDFor = %s，期望 %s", got, want)
	}
}
