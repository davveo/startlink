package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// InSendWindows 判断 now 是否落在任一时窗内；windows 空视为全天允许。
func InSendWindows(windows []SendWindow, now time.Time) bool {
	if len(windows) == 0 {
		return true
	}
	mins := now.Hour()*60 + now.Minute()
	for _, w := range windows {
		start, err1 := parseHHMM(w.Start)
		end, err2 := parseHHMM(w.End)
		if err1 != nil || err2 != nil {
			continue
		}
		if start == end {
			return true // 退化成全天
		}
		if start < end {
			if mins >= start && mins < end {
				return true
			}
			continue
		}
		// 跨午夜：如 22:00-08:00
		if mins >= start || mins < end {
			return true
		}
	}
	return false
}

func parseHHMM(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute")
	}
	return h*60 + m, nil
}

// InQuietHours 判断是否处于免打扰时段（可跨午夜）
func InQuietHours(start, end string, now time.Time) bool {
	if start == "" || end == "" {
		return false
	}
	s, err1 := parseHHMM(start)
	e, err2 := parseHHMM(end)
	if err1 != nil || err2 != nil {
		return false
	}
	mins := now.Hour()*60 + now.Minute()
	if s == e {
		return false
	}
	if s < e {
		return mins >= s && mins < e
	}
	return mins >= s || mins < e
}
