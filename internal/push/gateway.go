package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/starlink/push/internal/adapter/channel"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

const mainStatusCacheTTL = 300 * time.Millisecond

type mainCacheEntry struct {
	status    domain.TaskStatus
	windows   []domain.SendWindow
	expiresAt time.Time
}

// Gateway 推送网关：渠道路由 + 模板渲染 + 限流 + 频控 + 重试 + 多渠道降级/并行 + 投递去重
type Gateway struct {
	channels   *channel.Registry
	cache      port.AggregatorCache
	pushRepo   port.PushRepository
	tasks      port.TaskRepository
	limiter    port.ChannelLimiter
	rateQPS    int
	maxRetry   int
	dedupTTL   int
	freq       config.FreqConfig
	tokenMu    sync.Mutex
	tokens     float64
	lastRefill time.Time

	mainMu    sync.Mutex
	mainCache map[uint64]mainCacheEntry
}

func NewGateway(
	channels *channel.Registry,
	cache port.AggregatorCache,
	pushRepo port.PushRepository,
	tasks port.TaskRepository,
	limiter port.ChannelLimiter,
	rateQPS, maxRetry, dedupTTLSec int,
	freq config.FreqConfig,
) *Gateway {
	if dedupTTLSec <= 0 {
		dedupTTLSec = 7 * 24 * 3600
	}
	if rateQPS <= 0 {
		rateQPS = 500
	}
	return &Gateway{
		channels:   channels,
		cache:      cache,
		pushRepo:   pushRepo,
		tasks:      tasks,
		limiter:    limiter,
		rateQPS:    rateQPS,
		maxRetry:   maxRetry,
		dedupTTL:   dedupTTLSec,
		freq:       freq,
		tokens:     float64(rateQPS),
		lastRefill: time.Now(),
		mainCache:  make(map[uint64]mainCacheEntry),
	}
}

func (g *Gateway) takeToken() bool {
	g.tokenMu.Lock()
	defer g.tokenMu.Unlock()
	now := time.Now()
	elapsed := now.Sub(g.lastRefill).Seconds()
	g.tokens += elapsed * float64(g.rateQPS)
	if g.tokens > float64(g.rateQPS) {
		g.tokens = float64(g.rateQPS)
	}
	g.lastRefill = now
	if g.tokens < 1 {
		return false
	}
	g.tokens--
	return true
}

// waitToken 渠道配额（优先）或遗留进程内全局桶。
func (g *Gateway) waitToken(ctx context.Context, ch domain.ChannelType, prio domain.Priority) error {
	if g.limiter != nil {
		if err := g.limiter.Wait(ctx, ch, prio); err != nil {
			if errors.Is(err, domain.ErrChannelThrottled) {
				return err
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return domain.ErrChannelThrottled
			}
			return err
		}
		return nil
	}
	for !g.takeToken() {
		select {
		case <-ctx.Done():
			return domain.ErrChannelThrottled
		case <-time.After(10 * time.Millisecond):
		}
	}
	return nil
}

// loadMainSnap 短 TTL 缓存主任务状态/时窗，显著减少热路径 GetMainTask。
func (g *Gateway) loadMainSnap(ctx context.Context, mainTaskID uint64, force bool) (domain.TaskStatus, []domain.SendWindow) {
	if g.tasks == nil {
		return "", nil
	}
	now := time.Now()
	if !force {
		g.mainMu.Lock()
		if e, ok := g.mainCache[mainTaskID]; ok && now.Before(e.expiresAt) {
			st, w := e.status, e.windows
			g.mainMu.Unlock()
			return st, w
		}
		g.mainMu.Unlock()
	}
	main, err := g.tasks.GetMainTask(ctx, mainTaskID)
	if err != nil || main == nil {
		return "", nil
	}
	entry := mainCacheEntry{
		status:    main.Status,
		windows:   main.SendWindows(),
		expiresAt: now.Add(mainStatusCacheTTL),
	}
	g.mainMu.Lock()
	g.mainCache[mainTaskID] = entry
	g.mainMu.Unlock()
	return entry.status, entry.windows
}

func (g *Gateway) InvalidateMainCache(mainTaskID uint64) {
	g.mainMu.Lock()
	delete(g.mainCache, mainTaskID)
	g.mainMu.Unlock()
}

func (g *Gateway) Handle(ctx context.Context, msg domain.PushMessage) error {
	st, windows := g.loadMainSnap(ctx, msg.MainTaskID, false)
	if st == domain.TaskStatusCancelled {
		slog.Info("skip push: main task cancelled", "main_task_id", msg.MainTaskID, "user", msg.UserID)
		_ = g.markCancelled(ctx, msg, msg.Channel, msg.Body)
		return nil
	}
	if st == domain.TaskStatusPaused {
		return domain.ErrMainTaskPaused
	}
	if len(windows) > 0 && !domain.InSendWindows(windows, time.Now()) {
		return domain.ErrOutsideSendWindow
	}

	if g.inQuietHours(msg.BizScene) {
		return domain.ErrQuietHours
	}

	content := RenderTemplate(msg.Body, msg.Vars)
	chs := msg.EffectiveChannels()
	mode := msg.EffectiveMode()
	if len(chs) == 0 {
		return fmt.Errorf("no channel configured")
	}

	switch mode {
	case domain.ChannelModeParallel:
		return g.sendParallel(ctx, msg, chs, content)
	case domain.ChannelModeFallback:
		return g.sendFallback(ctx, msg, chs, content)
	default:
		ok, err := g.sendOne(ctx, msg, chs[0], content, true)
		if ok {
			return nil
		}
		return err
	}
}

func (g *Gateway) inQuietHours(bizScene string) bool {
	qh := g.freq.QuietHours
	if !g.freq.Enabled || !qh.Enabled {
		return false
	}
	if len(qh.Scenes) > 0 {
		ok := false
		scene := strings.ToLower(bizScene)
		for _, s := range qh.Scenes {
			if strings.ToLower(s) == scene {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return domain.InQuietHours(qh.Start, qh.End, time.Now())
}

func (g *Gateway) allowFreq(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType) (bool, string) {
	if !g.freq.Enabled || g.cache == nil {
		return true, ""
	}
	checks := []struct {
		key string
		lim int
		win int
		why string
	}{
		{fmt.Sprintf("user:%s", msg.UserID), g.freq.UserLimit, g.freq.UserWindowSec, "user_freq"},
		{fmt.Sprintf("user:%s:ch:%s", msg.UserID, ch), g.freq.UserChannelLimit, g.freq.UserChannelWindowSec, "user_channel_freq"},
		{fmt.Sprintf("scene:%s", msg.BizScene), g.freq.SceneLimit, g.freq.SceneWindowSec, "scene_freq"},
	}
	for _, c := range checks {
		if c.lim <= 0 || c.win <= 0 {
			continue
		}
		ok, err := g.cache.Allow(ctx, c.key, c.lim, c.win)
		if err != nil {
			slog.Warn("freq allow error", "key", c.key, "err", err)
			continue
		}
		if !ok {
			return false, c.why
		}
	}
	return true, ""
}

func (g *Gateway) markCancelled(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType, content string) error {
	if g.pushRepo == nil {
		return nil
	}
	id, dup, inFlight, err := g.pushRepo.ClaimDelivery(ctx, &domain.PushRecord{
		MainTaskID: msg.MainTaskID,
		SubTaskID:  msg.SubTaskID,
		UserID:     msg.UserID,
		Channel:    ch,
		Content:    content,
	})
	if err != nil {
		return err
	}
	if dup || inFlight {
		return nil
	}
	return g.pushRepo.UpdateRecordStatus(ctx, id, domain.PushStatusCancelled, "", "main task cancelled")
}

func (g *Gateway) checkMainGate(ctx context.Context, mainTaskID uint64, force bool) error {
	st, _ := g.loadMainSnap(ctx, mainTaskID, force)
	if st == domain.TaskStatusCancelled {
		return errMainCancelled
	}
	if st == domain.TaskStatusPaused {
		return domain.ErrMainTaskPaused
	}
	return nil
}

var errMainCancelled = errors.New("main task cancelled")

// sendFallback 按配置顺序依次降级：前一渠道失败才尝试下一渠道，成功即停
func (g *Gateway) sendFallback(ctx context.Context, msg domain.PushMessage, chs []domain.ChannelType, content string) error {
	var lastErr error
	for i, ch := range chs {
		if err := g.checkMainGate(ctx, msg.MainTaskID, i > 0); err != nil {
			if errors.Is(err, errMainCancelled) {
				_ = g.markCancelled(ctx, msg, ch, content)
				return nil
			}
			return err
		}

		ok, err := g.sendOne(ctx, msg, ch, content, true)
		if ok {
			if i > 0 {
				slog.Info("channel fallback success", "user", msg.UserID, "channel", ch, "tried", i+1)
			}
			return nil
		}
		if errors.Is(err, domain.ErrMainTaskPaused) ||
			errors.Is(err, domain.ErrOutsideSendWindow) ||
			errors.Is(err, domain.ErrQuietHours) ||
			errors.Is(err, domain.ErrChannelThrottled) {
			return err
		}
		if err == nil {
			return nil
		}
		lastErr = err
		slog.Warn("channel failed, try next", "user", msg.UserID, "channel", ch, "err", err, "next_index", i+1)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("all channels failed")
}

// sendParallel 同内容多渠道并行；任一成功即用户成功，全部失败才失败
func (g *Gateway) sendParallel(ctx context.Context, msg domain.PushMessage, chs []domain.ChannelType, content string) error {
	type outcome struct {
		ch  domain.ChannelType
		ok  bool
		err error
	}
	ch := make(chan outcome, len(chs))
	var wg sync.WaitGroup
	for _, c := range chs {
		wg.Add(1)
		go func(chType domain.ChannelType) {
			defer wg.Done()
			ok, err := g.sendOne(ctx, msg, chType, content, true)
			ch <- outcome{ch: chType, ok: ok, err: err}
		}(c)
	}
	wg.Wait()
	close(ch)

	anyOK := false
	var lastErr error
	for o := range ch {
		if o.ok {
			anyOK = true
			continue
		}
		if o.err != nil {
			if errors.Is(o.err, domain.ErrChannelThrottled) ||
				errors.Is(o.err, context.Canceled) ||
				errors.Is(o.err, context.DeadlineExceeded) {
				return o.err
			}
			lastErr = o.err
		}
	}
	if anyOK {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("all parallel channels failed")
}

// sendOne 发送单一渠道；save=true 时写流水并做用户+活动+渠道去重。返回 (是否成功, error)
func (g *Gateway) sendOne(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType, content string, save bool) (bool, error) {
	if save {
		if g.cache != nil {
			if ok, err := g.cache.HasDelivered(ctx, msg.MainTaskID, msg.UserID, ch); err == nil && ok {
				slog.Info("dedup skip: redis", "main_task_id", msg.MainTaskID, "user", msg.UserID, "channel", ch)
				return true, nil
			}
		}

		recordID, dup, inFlight, err := g.pushRepo.ClaimDelivery(ctx, &domain.PushRecord{
			MainTaskID: msg.MainTaskID,
			SubTaskID:  msg.SubTaskID,
			UserID:     msg.UserID,
			Channel:    ch,
			Content:    content,
		})
		if err != nil {
			return false, err
		}
		if dup {
			slog.Info("dedup skip: already delivered", "main_task_id", msg.MainTaskID, "user", msg.UserID, "channel", ch)
			if g.cache != nil {
				_ = g.cache.MarkDelivered(ctx, msg.MainTaskID, msg.UserID, ch, g.dedupTTL)
			}
			return true, nil
		}
		if inFlight {
			return false, fmt.Errorf("delivery in progress")
		}

		if ok, why := g.allowFreq(ctx, msg, ch); !ok {
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusFailed, "", why)
			slog.Info("freq denied", "user", msg.UserID, "channel", ch, "reason", why)
			return false, nil
		}

		// 去重/频控通过后再扣渠道令牌；超时留 PEL，占位改回 queued
		if err := g.waitToken(ctx, ch, msg.Priority); err != nil {
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusQueued, "", "channel throttled")
			return false, err
		}
		// 限流等待后强刷主任务状态
		if err := g.checkMainGate(ctx, msg.MainTaskID, true); err != nil {
			if errors.Is(err, errMainCancelled) {
				_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusCancelled, "", "main task cancelled")
				return false, nil
			}
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusQueued, "", err.Error())
			return false, err
		}

		return g.doSend(ctx, msg, ch, content, true, recordID)
	}
	if ok, _ := g.allowFreq(ctx, msg, ch); !ok {
		return false, nil
	}
	if err := g.waitToken(ctx, ch, msg.Priority); err != nil {
		return false, err
	}
	return g.doSend(ctx, msg, ch, content, false, 0)
}

func (g *Gateway) doSend(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType, content string, save bool, recordID uint64) (bool, error) {
	sender, err := g.channels.Get(ch)
	if err != nil {
		if save && recordID > 0 {
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusFailed, "", err.Error())
		}
		return false, err
	}

	req := domain.SendRequest{
		MsgID:   fmt.Sprintf("%s-%s", msg.MsgID, ch),
		UserID:  msg.UserID,
		Channel: ch,
		Title:   msg.Title,
		Content: content,
		Vars:    msg.Vars,
		Extra:   msg.Extra,
	}

	var result *domain.SendResult
	var lastErr error
	for attempt := 0; attempt <= g.maxRetry; attempt++ {
		// 仅首次与每次 backoff 后刷新状态，避免每 attempt 打库
		st, _ := g.loadMainSnap(ctx, msg.MainTaskID, attempt > 0)
		if st == domain.TaskStatusCancelled {
			if save && recordID > 0 {
				_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusCancelled, "", "main task cancelled")
			}
			return false, nil
		}
		if st == domain.TaskStatusPaused {
			if save && recordID > 0 {
				_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusQueued, "", "main task paused")
			}
			return false, domain.ErrMainTaskPaused
		}
		result, lastErr = sender.Send(ctx, req)
		if result != nil && result.Throttled && g.limiter != nil {
			g.limiter.ObserveVendorThrottle(ctx, ch)
		}
		if lastErr == nil && result != nil && result.Success {
			break
		}
		if result != nil && !result.Retryable && lastErr == nil {
			break
		}
		backoff := time.Duration(1<<attempt) * 50 * time.Millisecond
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(backoff):
		}
	}

	status := domain.PushStatusSent
	providerID := ""
	errMsg := ""
	ok := true
	if lastErr != nil || result == nil || !result.Success {
		ok = false
		status = domain.PushStatusFailed
		if lastErr != nil {
			errMsg = lastErr.Error()
		} else if result != nil {
			errMsg = result.ErrorMsg
		} else {
			errMsg = "unknown send failure"
		}
		slog.Warn("push failed", "user", msg.UserID, "channel", ch, "err", errMsg)
	} else {
		providerID = result.ProviderID
	}

	if save && recordID > 0 {
		if err := g.pushRepo.UpdateRecordStatus(ctx, recordID, status, providerID, errMsg); err != nil {
			return false, err
		}
	}
	if ok && g.cache != nil {
		_ = g.cache.MarkDelivered(ctx, msg.MainTaskID, msg.UserID, ch, g.dedupTTL)
	}
	if !ok {
		return false, fmt.Errorf("send failed [%s]: %s", ch, errMsg)
	}
	return true, nil
}
