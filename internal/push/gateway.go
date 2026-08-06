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

// 包内哨兵：抑制态（频控/退订等已记流水，调用方视为“处理完成”）；主任务取消则停止发送链。
var (
	errSuppressed    = errors.New("suppressed")
	errMainCancelled = errors.New("main task cancelled")
)

type mainCacheEntry struct {
	status    domain.TaskStatus
	windows   []domain.SendWindow
	expiresAt time.Time
}

// Gateway 推送网关：渠道路由 + 模板渲染 + 限流 + 频控 + 退订终检 + 重试 + 多渠道降级/并行 + 投递去重
type Gateway struct {
	channels   *channel.Registry
	cache      port.AggregatorCache
	pushRepo   port.PushRepository
	tasks      port.TaskRepository
	unsub      port.UnsubscribeChecker
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
	unsub port.UnsubscribeChecker,
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
		unsub:      unsub,
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
// 数据库异常时返回 error，调用方必须 fail-closed（禁止继续发送）。
func (g *Gateway) loadMainSnap(ctx context.Context, mainTaskID uint64, force bool) (domain.TaskStatus, []domain.SendWindow, error) {
	if g.tasks == nil {
		return "", nil, nil
	}
	now := time.Now()
	if !force {
		g.mainMu.Lock()
		if e, ok := g.mainCache[mainTaskID]; ok && now.Before(e.expiresAt) {
			st, w := e.status, e.windows
			g.mainMu.Unlock()
			return st, w, nil
		}
		g.mainMu.Unlock()
	}
	main, err := g.tasks.GetMainTask(ctx, mainTaskID)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", domain.ErrMainStatusUnavailable, err)
	}
	if main == nil {
		return "", nil, domain.ErrMainStatusUnavailable
	}
	entry := mainCacheEntry{
		status:    main.Status,
		windows:   main.SendWindows(),
		expiresAt: now.Add(mainStatusCacheTTL),
	}
	g.mainMu.Lock()
	g.mainCache[mainTaskID] = entry
	g.mainMu.Unlock()
	return entry.status, entry.windows, nil
}

func (g *Gateway) InvalidateMainCache(mainTaskID uint64) {
	g.mainMu.Lock()
	delete(g.mainCache, mainTaskID)
	g.mainMu.Unlock()
}

func (g *Gateway) Handle(ctx context.Context, msg domain.PushMessage) error {
	st, windows, err := g.loadMainSnap(ctx, msg.MainTaskID, false)
	if err != nil {
		return err
	}
	if st == domain.TaskStatusCancelled {
		slog.Info("skip push: main task cancelled", "main_task_id", msg.MainTaskID, "user", msg.UserID)
		_ = g.markCancelled(ctx, msg, msg.Channel, msg.Body)
		return nil
	}
	if st == domain.TaskStatusPaused {
		return domain.ErrMainTaskPaused
	}

	if msg.ExpireAt != nil && !msg.ExpireAt.IsZero() && time.Now().After(*msg.ExpireAt) {
		return g.markExpired(ctx, msg)
	}

	now := g.nowInTZ(msg.Timezone)
	if len(windows) > 0 && !domain.InSendWindows(windows, now) {
		return domain.ErrOutsideSendWindow
	}

	if g.inQuietHours(msg.BizScene, now) {
		return domain.ErrQuietHours
	}

	chs := msg.ResolveSendChannels()
	mode := msg.EffectiveMode()
	if len(chs) == 0 {
		return fmt.Errorf("no channel configured")
	}

	switch mode {
	case domain.ChannelModeParallel:
		return g.sendParallel(ctx, msg, chs)
	case domain.ChannelModeAllSuccess:
		return g.sendAllSuccess(ctx, msg, chs)
	case domain.ChannelModeFallback, domain.ChannelModeCostPriority:
		return g.sendFallback(ctx, msg, chs)
	case domain.ChannelModeConditional:
		// 条件路由已解析为渠道链：单渠走 single，多渠默认降级
		if len(chs) == 1 {
			ok, err := g.sendOne(ctx, msg, chs[0], true)
			if ok {
				return nil
			}
			if errors.Is(err, errSuppressed) {
				return nil
			}
			return err
		}
		return g.sendFallback(ctx, msg, chs)
	default:
		ok, err := g.sendOne(ctx, msg, chs[0], true)
		if ok {
			return nil
		}
		if errors.Is(err, errSuppressed) {
			return nil
		}
		return err
	}
}

func (g *Gateway) nowInTZ(tz string) time.Time {
	now := time.Now()
	if strings.TrimSpace(tz) == "" {
		return now
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return now
	}
	return now.In(loc)
}

func (g *Gateway) inQuietHours(bizScene string, now time.Time) bool {
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
	return domain.InQuietHours(qh.Start, qh.End, now)
}

func (g *Gateway) markExpired(ctx context.Context, msg domain.PushMessage) error {
	chs := msg.EffectiveChannels()
	if len(chs) == 0 {
		chs = []domain.ChannelType{msg.Channel}
	}
	for _, ch := range chs {
		if ch == "" {
			continue
		}
		title, body, _, _ := g.resolveContent(msg, ch)
		_ = title
		if g.pushRepo == nil {
			continue
		}
		id, dup, inFlight, err := g.pushRepo.ClaimDelivery(ctx, g.newRecord(msg, ch, body))
		if err != nil {
			slog.Warn("expire claim failed", "user", msg.UserID, "channel", ch, "err", err)
			continue
		}
		if dup || inFlight {
			continue
		}
		_ = g.pushRepo.UpdateRecordStatus(ctx, id, domain.PushStatusExpired, "", "campaign expired")
	}
	slog.Info("skip push: expired", "main_task_id", msg.MainTaskID, "user", msg.UserID)
	return nil // ACK 成功路径，不留 PEL
}

func (g *Gateway) resolveContent(msg domain.PushMessage, ch domain.ChannelType) (title, body string, extra map[string]any, err error) {
	title, body, extra = domain.ResolveChannelContent(ch, msg.Title, msg.Body, msg.Contents)
	policy := msg.MissingVarPolicy.Normalize()
	defaults := map[string]string{}
	if msg.Extra != nil {
		if raw, ok := msg.Extra["var_defaults"].(map[string]any); ok {
			for k, v := range raw {
				defaults[k] = fmt.Sprint(v)
			}
		}
	}
	rt, err1 := RenderTemplateWithPolicy(title, msg.Vars, policy, defaults)
	rb, err2 := RenderTemplateWithPolicy(body, msg.Vars, policy, defaults)
	if err1 != nil {
		return rt, rb, extra, err1
	}
	if err2 != nil {
		return rt, rb, extra, err2
	}
	return rt, rb, extra, nil
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

// checkUnsubscribed 发送前按 user+channel 终检。
// 已退订 → suppressed；Redis 不可用时营销消息 fail-closed（返回 error 留 PEL），事务消息 fail-open。
func (g *Gateway) checkUnsubscribed(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType) error {
	if g.unsub == nil {
		return nil
	}
	ok, err := g.unsub.IsUnsubscribed(ctx, msg.UserID, ch)
	if err != nil {
		if msg.Priority.Normalize() == domain.PriorityHigh {
			slog.Warn("unsubscribe check failed, fail-open for high priority",
				"user", msg.UserID, "channel", ch, "err", err)
			return nil
		}
		return fmt.Errorf("unsubscribe check: %w", err)
	}
	if ok {
		return domain.ErrUnsubscribed
	}
	return nil
}

func (g *Gateway) markCancelled(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType, content string) error {
	if g.pushRepo == nil {
		return nil
	}
	id, dup, inFlight, err := g.pushRepo.ClaimDelivery(ctx, g.newRecord(msg, ch, content))
	if err != nil {
		return err
	}
	if dup || inFlight {
		return nil
	}
	return g.pushRepo.UpdateRecordStatus(ctx, id, domain.PushStatusCancelled, "", "main task cancelled")
}

func (g *Gateway) checkMainGate(ctx context.Context, mainTaskID uint64, force bool) error {
	st, _, err := g.loadMainSnap(ctx, mainTaskID, force)
	if err != nil {
		return err
	}
	if st == domain.TaskStatusCancelled {
		return errMainCancelled
	}
	if st == domain.TaskStatusPaused {
		return domain.ErrMainTaskPaused
	}
	return nil
}

func (g *Gateway) newRecord(msg domain.PushMessage, ch domain.ChannelType, content string) *domain.PushRecord {
	return &domain.PushRecord{
		MainTaskID:      msg.MainTaskID,
		SubTaskID:       msg.SubTaskID,
		UserID:          msg.UserID,
		Channel:         ch,
		Provider:        string(ch),
		Content:         content,
		ExperimentGroup: experimentGroupFromExtra(msg.Extra),
	}
}

func experimentGroupFromExtra(extra map[string]any) string {
	if extra == nil {
		return ""
	}
	if g, ok := extra["experiment_group"].(string); ok {
		return g
	}
	return ""
}

// sendFallback 按配置顺序依次降级：前一渠道失败才尝试下一渠道，成功即停
func (g *Gateway) sendFallback(ctx context.Context, msg domain.PushMessage, chs []domain.ChannelType) error {
	if msg.MaxFallback > 0 && msg.MaxFallback+1 < len(chs) {
		chs = chs[:msg.MaxFallback+1]
	}
	var lastErr error
	anySuppressed := false
	for i, ch := range chs {
		if err := g.checkMainGate(ctx, msg.MainTaskID, i > 0); err != nil {
			if errors.Is(err, errMainCancelled) {
				_, body, _, _ := g.resolveContent(msg, ch)
				_ = g.markCancelled(ctx, msg, ch, body)
				return nil
			}
			return err
		}

		ok, err := g.sendOne(ctx, msg, ch, true)
		if ok {
			if i > 0 {
				slog.Info("channel fallback success", "user", msg.UserID, "channel", ch, "tried", i+1)
			}
			return nil
		}
		if errors.Is(err, errSuppressed) {
			anySuppressed = true
			continue
		}
		if errors.Is(err, domain.ErrMainTaskPaused) ||
			errors.Is(err, domain.ErrOutsideSendWindow) ||
			errors.Is(err, domain.ErrQuietHours) ||
			errors.Is(err, domain.ErrChannelThrottled) ||
			errors.Is(err, domain.ErrMainStatusUnavailable) {
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
	if anySuppressed {
		return nil
	}
	return fmt.Errorf("all channels failed")
}

// sendParallel 同内容多渠道并行；任一成功即用户成功，全部失败才失败
func (g *Gateway) sendParallel(ctx context.Context, msg domain.PushMessage, chs []domain.ChannelType) error {
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
			ok, err := g.sendOne(ctx, msg, chType, true)
			ch <- outcome{ch: chType, ok: ok, err: err}
		}(c)
	}
	wg.Wait()
	close(ch)

	anyOK := false
	anySuppressed := false
	var lastErr error
	for o := range ch {
		if o.ok {
			anyOK = true
			continue
		}
		if errors.Is(o.err, errSuppressed) {
			anySuppressed = true
			continue
		}
		if o.err != nil {
			if errors.Is(o.err, domain.ErrChannelThrottled) ||
				errors.Is(o.err, domain.ErrMainStatusUnavailable) ||
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
	if anySuppressed {
		return nil
	}
	return fmt.Errorf("all parallel channels failed")
}

// sendAllSuccess 并行发送，全部渠道成功才算成功
func (g *Gateway) sendAllSuccess(ctx context.Context, msg domain.PushMessage, chs []domain.ChannelType) error {
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
			ok, err := g.sendOne(ctx, msg, chType, true)
			ch <- outcome{ch: chType, ok: ok, err: err}
		}(c)
	}
	wg.Wait()
	close(ch)

	var lastErr error
	for o := range ch {
		if o.ok {
			continue
		}
		if errors.Is(o.err, errSuppressed) {
			return fmt.Errorf("all_success: channel %s suppressed", o.ch)
		}
		if o.err != nil {
			if errors.Is(o.err, domain.ErrChannelThrottled) ||
				errors.Is(o.err, domain.ErrMainStatusUnavailable) ||
				errors.Is(o.err, context.Canceled) ||
				errors.Is(o.err, context.DeadlineExceeded) {
				return o.err
			}
			lastErr = o.err
			continue
		}
		lastErr = fmt.Errorf("all_success: channel %s failed", o.ch)
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

// sendOne 发送单一渠道；save=true 时写流水并做用户+活动+渠道去重。返回 (是否成功, error)
func (g *Gateway) sendOne(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType, save bool) (bool, error) {
	title, content, chExtra, renderErr := g.resolveContent(msg, ch)
	sendExtra := domain.MergeExtra(msg.Extra, chExtra)

	if renderErr != nil && msg.MissingVarPolicy.Normalize() == domain.MissingVarError {
		if save && g.pushRepo != nil {
			recordID, dup, inFlight, err := g.pushRepo.ClaimDelivery(ctx, g.newRecord(msg, ch, content))
			if err == nil && !dup && !inFlight {
				_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusFailed, "", renderErr.Error())
			}
		}
		return false, renderErr
	}

	if save {
		if g.cache != nil {
			if ok, err := g.cache.HasDelivered(ctx, msg.MainTaskID, msg.UserID, ch); err == nil && ok {
				slog.Info("dedup skip: redis", "main_task_id", msg.MainTaskID, "user", msg.UserID, "channel", ch)
				return true, nil
			}
		}

		recordID, dup, inFlight, err := g.pushRepo.ClaimDelivery(ctx, g.newRecord(msg, ch, content))
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

		if err := g.checkUnsubscribed(ctx, msg, ch); err != nil {
			if errors.Is(err, domain.ErrUnsubscribed) {
				_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusSuppressed, "", "unsubscribed")
				slog.Info("unsubscribe denied", "user", msg.UserID, "channel", ch)
				return false, errSuppressed
			}
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusQueued, "", err.Error())
			return false, err
		}

		if ok, why := g.allowFreq(ctx, msg, ch); !ok {
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusSuppressed, "", why)
			slog.Info("freq denied", "user", msg.UserID, "channel", ch, "reason", why)
			return false, errSuppressed
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

		return g.doSend(ctx, msg, ch, title, content, sendExtra, true, recordID)
	}
	if err := g.checkUnsubscribed(ctx, msg, ch); err != nil {
		if errors.Is(err, domain.ErrUnsubscribed) {
			return false, errSuppressed
		}
		return false, err
	}
	if ok, _ := g.allowFreq(ctx, msg, ch); !ok {
		return false, errSuppressed
	}
	if err := g.waitToken(ctx, ch, msg.Priority); err != nil {
		return false, err
	}
	return g.doSend(ctx, msg, ch, title, content, sendExtra, false, 0)
}

func (g *Gateway) doSend(ctx context.Context, msg domain.PushMessage, ch domain.ChannelType, title, content string, extra map[string]any, save bool, recordID uint64) (bool, error) {
	sender, err := g.channels.Get(ch)
	if err != nil {
		if save && recordID > 0 {
			_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusUnreachable, "", err.Error())
		}
		return false, errSuppressed
	}

	req := domain.SendRequest{
		MsgID:   fmt.Sprintf("%s-%s", msg.MsgID, ch),
		UserID:  msg.UserID,
		Channel: ch,
		Title:   title,
		Content: content,
		Vars:    msg.Vars,
		Extra:   extra,
	}

	var result *domain.SendResult
	var lastErr error
	for attempt := 0; attempt <= g.maxRetry; attempt++ {
		// 仅首次与每次 backoff 后刷新状态，避免每 attempt 打库
		st, _, snapErr := g.loadMainSnap(ctx, msg.MainTaskID, attempt > 0)
		if snapErr != nil {
			if save && recordID > 0 {
				_ = g.pushRepo.UpdateRecordStatus(ctx, recordID, domain.PushStatusQueued, "", snapErr.Error())
			}
			return false, snapErr
		}
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
	provider := string(ch)
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
		if result.Provider != "" {
			provider = result.Provider
		}
	}

	if save && recordID > 0 {
		if err := g.pushRepo.UpdateRecordDelivery(ctx, recordID, status, provider, providerID, errMsg); err != nil {
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
