package domain

import "time"

// SplitLeaseExpired 拆分租约是否已过期（nil 租约视为过期，可供回收）
func SplitLeaseExpired(leaseAt *time.Time, leaseTimeoutSec int, now time.Time) bool {
	if leaseTimeoutSec <= 0 {
		leaseTimeoutSec = 90
	}
	if leaseAt == nil {
		return true
	}
	deadline := now.Add(-time.Duration(leaseTimeoutSec) * time.Second)
	return leaseAt.Before(deadline)
}
