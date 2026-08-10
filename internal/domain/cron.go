package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule 标准 5 段 cron：分 时 日 月 周。
// 自实现而非引入依赖：周期活动只需要基础语法（* , - /），
// 且项目要在无外网的超算环境离线构建，少一个 module 少一分风险。
type CronSchedule struct {
	expr    string
	minute  [60]bool
	hour    [24]bool
	dom     [32]bool // 1-31
	month   [13]bool // 1-12
	dow     [7]bool  // 0=周日
	domStar bool
	dowStar bool
}

// Expr 原始表达式
func (c *CronSchedule) Expr() string {
	if c == nil {
		return ""
	}
	return c.expr
}

type cronFieldSpec struct {
	name string
	min  int
	max  int
}

var cronFields = []cronFieldSpec{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 7}, // 7 与 0 同为周日
}

// ParseCron 解析 5 段 cron 表达式
func ParseCron(expr string) (*CronSchedule, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron 表达式需要 5 段（分 时 日 月 周），当前 %d 段", len(fields))
	}
	c := &CronSchedule{expr: strings.Join(fields, " ")}
	sets := make([][]bool, len(fields))
	for i, spec := range cronFields {
		set, err := parseCronField(fields[i], spec)
		if err != nil {
			return nil, err
		}
		sets[i] = set
	}
	c.domStar = isCronWildcard(fields[2])
	c.dowStar = isCronWildcard(fields[4])

	for v := 0; v <= 59; v++ {
		c.minute[v] = sets[0][v]
	}
	for v := 0; v <= 23; v++ {
		c.hour[v] = sets[1][v]
	}
	for v := 1; v <= 31; v++ {
		c.dom[v] = sets[2][v]
	}
	for v := 1; v <= 12; v++ {
		c.month[v] = sets[3][v]
	}
	for v := 0; v <= 7; v++ {
		if sets[4][v] {
			c.dow[v%7] = true
		}
	}
	return c, nil
}

// isCronWildcard 判断字段是否等价于「不限制」，用于 cron 的日/周或语义
func isCronWildcard(field string) bool {
	field = strings.TrimSpace(field)
	return field == "*" || field == "?"
}

func parseCronField(field string, spec cronFieldSpec) ([]bool, error) {
	set := make([]bool, spec.max+1)
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("cron %s 段存在空项", spec.name)
		}
		step := 1
		if idx := strings.Index(part, "/"); idx >= 0 {
			stepStr := part[idx+1:]
			part = part[:idx]
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cron %s 段步长非法: %s", spec.name, stepStr)
			}
			step = n
		}
		lo, hi := spec.min, spec.max
		switch {
		case isCronWildcard(part):
			// 保持全域
		case strings.Contains(part, "-"):
			bounds := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("cron %s 段区间非法: %s", spec.name, part)
			}
			lo, hi = a, b
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("cron %s 段取值非法: %s", spec.name, part)
			}
			lo, hi = n, n
		}
		if lo < spec.min || hi > spec.max || lo > hi {
			return nil, fmt.Errorf("cron %s 段超出范围 [%d,%d]: %s", spec.name, spec.min, spec.max, field)
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return set, nil
}

// matchDay 日/周匹配。cron 惯例：日与周都被限制时取「或」，否则取被限制的那个。
func (c *CronSchedule) matchDay(t time.Time) bool {
	if !c.month[int(t.Month())] {
		return false
	}
	domOK := c.dom[t.Day()]
	dowOK := c.dow[int(t.Weekday())]
	switch {
	case c.domStar && c.dowStar:
		return true
	case c.domStar:
		return dowOK
	case c.dowStar:
		return domOK
	default:
		return domOK || dowOK
	}
}

// maxCronLookaheadDays 找不到下次触发时的放弃阈值（如 2/31 这类永不成立的表达式）
const maxCronLookaheadDays = 366 * 4

// Next 返回 after 之后的下一个触发时刻（不含 after 本身）；无解返回零值。
func (c *CronSchedule) Next(after time.Time) time.Time {
	if c == nil {
		return time.Time{}
	}
	loc := after.Location()
	// 从下一分钟整点开始搜索
	t := after.Truncate(time.Minute).Add(time.Minute)
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)

	for day := 0; day <= maxCronLookaheadDays; day++ {
		if !c.matchDay(t) {
			// 整天不匹配就跳到次日零点，避免逐分钟空转
			next := t.AddDate(0, 0, 1)
			t = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
			continue
		}
		for {
			if c.hour[t.Hour()] && c.minute[t.Minute()] {
				return t
			}
			nt := t.Add(time.Minute)
			if nt.Day() != t.Day() || nt.Month() != t.Month() || nt.Year() != t.Year() {
				t = time.Date(nt.Year(), nt.Month(), nt.Day(), 0, 0, 0, 0, loc)
				break
			}
			t = nt
		}
	}
	return time.Time{}
}

// LoadLocation 解析 IANA 时区；空或非法回退本地时区
func LoadLocation(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local
	}
	return loc
}
