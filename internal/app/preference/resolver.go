package preference

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

const (
	// DefaultResolverTTL 偏好变更容忍的最大生效延迟
	DefaultResolverTTL = 60 * time.Second
	// DefaultResolverMaxEntries 条目上限；超限先清过期项，仍超限再随机淘汰
	DefaultResolverMaxEntries = 50000
)

type resolverEntry struct {
	// pref 为 nil 表示负缓存：该用户确实没有偏好记录。
	// 不缓存 nil 的话，绝大多数没配过偏好的用户每条消息都要打一次 DB。
	pref      *domain.UserPreference
	expiresAt time.Time
}

// Resolver 发送链路的偏好读取器：带 TTL 与容量上限的进程内缓存。
type Resolver struct {
	repo       port.PreferenceRepository
	ttl        time.Duration
	maxEntries int

	mu      sync.RWMutex
	entries map[string]resolverEntry
}

var _ port.PreferenceResolver = (*Resolver)(nil)

func NewResolver(repo port.PreferenceRepository, ttl time.Duration, maxEntries int) *Resolver {
	if ttl <= 0 {
		ttl = DefaultResolverTTL
	}
	if maxEntries <= 0 {
		maxEntries = DefaultResolverMaxEntries
	}
	return &Resolver{
		repo:       repo,
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[string]resolverEntry),
	}
}

// Resolve 返回用户偏好；无记录返回 (nil, nil)。
// DB 异常时返回 error，由调用方按渠道优先级决定 fail-open / fail-closed。
func (r *Resolver) Resolve(ctx context.Context, userID string) (*domain.UserPreference, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || r.repo == nil {
		return nil, nil
	}
	now := time.Now()

	r.mu.RLock()
	e, ok := r.entries[userID]
	r.mu.RUnlock()
	if ok && now.Before(e.expiresAt) {
		return e.pref, nil
	}

	pref, err := r.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.entries[userID] = resolverEntry{pref: pref, expiresAt: now.Add(r.ttl)}
	r.evictLocked(now)
	r.mu.Unlock()
	return pref, nil
}

func (r *Resolver) Invalidate(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	r.mu.Lock()
	delete(r.entries, userID)
	r.mu.Unlock()
}

// Len 当前缓存条目数（含未清理的过期项），供监控与测试使用
func (r *Resolver) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// evictLocked 过期条目不会自己消失，长跑进程里 map 只增不减。
// 超过上限先清过期项；仍超限则随机淘汰到 90% 水位，避免每次写入都触发全表扫描。
func (r *Resolver) evictLocked(now time.Time) {
	if len(r.entries) <= r.maxEntries {
		return
	}
	for k, e := range r.entries {
		if !now.Before(e.expiresAt) {
			delete(r.entries, k)
		}
	}
	if len(r.entries) <= r.maxEntries {
		return
	}
	target := r.maxEntries * 9 / 10
	for k := range r.entries {
		if len(r.entries) <= target {
			break
		}
		delete(r.entries, k)
	}
}
