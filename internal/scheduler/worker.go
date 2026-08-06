package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// Worker 调度 Worker：认领子任务 → 投递 MQ → 上报聚合
// 多实例部署即可水平扩展
type Worker struct {
	id               string
	tasks            port.TaskRepository
	mq               port.MessagePublisher
	agg              *Aggregator
	splitter         *Splitter
	limiter          port.ChannelLimiter
	concurrency      int
	splitConcurrency int
	pollInterval     time.Duration
	claimTimeoutSec  int
	splitLeaseSec    int
	splitSem         chan struct{}
}

func NewWorker(
	tasks port.TaskRepository,
	mq port.MessagePublisher,
	agg *Aggregator,
	splitter *Splitter,
	limiter port.ChannelLimiter,
	concurrency int,
	pollIntervalMs int,
	claimTimeoutSec int,
	splitLeaseSec int,
	splitConcurrency int,
) *Worker {
	if concurrency <= 0 {
		concurrency = 8
	}
	if splitLeaseSec <= 0 {
		splitLeaseSec = 90
	}
	if splitConcurrency <= 0 {
		splitConcurrency = 2
	}
	return &Worker{
		id:               "scheduler-" + uuid.NewString()[:8],
		tasks:            tasks,
		mq:               mq,
		agg:              agg,
		splitter:         splitter,
		limiter:          limiter,
		concurrency:      concurrency,
		splitConcurrency: splitConcurrency,
		pollInterval:     time.Duration(pollIntervalMs) * time.Millisecond,
		claimTimeoutSec:  claimTimeoutSec,
		splitLeaseSec:    splitLeaseSec,
		splitSem:         make(chan struct{}, splitConcurrency),
	}
}

func (w *Worker) ID() string { return w.id }

func (w *Worker) Run(ctx context.Context) error {
	slog.Info("scheduler worker started",
		"id", w.id,
		"concurrency", w.concurrency,
		"split_concurrency", w.splitConcurrency,
		"split_lease_sec", w.splitLeaseSec,
	)

	go w.loopSplit(ctx)

	var wg sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w.loopClaim(ctx, idx)
		}(i)
	}
	wg.Wait()
	return ctx.Err()
}

func (w *Worker) loopSplit(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.splitPending(ctx)
			w.recoverStaleSplits(ctx)
		}
	}
}

func (w *Worker) spawnSplit(ctx context.Context, mainID uint64) {
	select {
	case <-ctx.Done():
		return
	case w.splitSem <- struct{}{}:
	}
	go func() {
		defer func() { <-w.splitSem }()
		w.runSplit(ctx, mainID)
	}()
}

func (w *Worker) splitPending(ctx context.Context) {
	list, err := w.tasks.ListPendingMainTasks(ctx, 10)
	if err != nil {
		slog.Error("list pending main tasks", "err", err)
		return
	}
	for i := range list {
		main := list[i]
		claimed, err := w.tasks.MarkMainTaskRunning(ctx, main.ID, w.id)
		if err != nil {
			slog.Warn("mark running failed", "id", main.ID, "err", err)
			continue
		}
		if !claimed {
			continue
		}
		w.spawnSplit(ctx, main.ID)
	}
}

func (w *Worker) recoverStaleSplits(ctx context.Context) {
	stale, err := w.tasks.ListStaleSplitMainTasks(ctx, w.splitLeaseSec, 10)
	if err != nil {
		slog.Error("list stale split tasks", "err", err)
		return
	}
	for i := range stale {
		id := stale[i].ID
		ok, err := w.tasks.ClaimStaleSplitMainTask(ctx, id, w.id, w.splitLeaseSec)
		if err != nil {
			slog.Warn("claim stale split failed", "id", id, "err", err)
			continue
		}
		if !ok {
			continue
		}
		slog.Warn("reclaim stale split main task", "id", id, "worker", w.id)
		w.spawnSplit(ctx, id)
	}
}

func (w *Worker) runSplit(ctx context.Context, mainID uint64) {
	fresh, err := w.tasks.GetMainTask(ctx, mainID)
	if err != nil {
		return
	}
	if fresh.Status == domain.TaskStatusCancelled {
		slog.Info("skip split: main task cancelled", "id", mainID)
		_, _ = w.tasks.CancelUnfinishedSubTasks(ctx, mainID)
		_ = w.tasks.ClearSplitLease(ctx, mainID)
		return
	}
	if fresh.Status == domain.TaskStatusPaused {
		slog.Info("skip split: main task paused", "id", mainID)
		_ = w.tasks.ClearSplitLease(ctx, mainID)
		return
	}
	// 卡单重拆：清掉半成品子任务，避免重复分片
	if n, err := w.tasks.DeleteSubTasksByMainTask(ctx, mainID); err != nil {
		slog.Warn("cleanup subtasks before split", "id", mainID, "err", err)
	} else if n > 0 {
		slog.Info("cleared partial subtasks before split", "id", mainID, "deleted", n)
	}
	if err := w.splitter.Split(ctx, fresh, w.id); err != nil {
		slog.Error("split failed", "id", mainID, "err", err)
		cur, _ := w.tasks.GetMainTask(ctx, mainID)
		if cur != nil && cur.Status == domain.TaskStatusCancelled {
			_, _ = w.tasks.CancelUnfinishedSubTasks(ctx, mainID)
			_ = w.tasks.ClearSplitLease(ctx, mainID)
			return
		}
		if cur != nil && cur.SplitOwner != "" && cur.SplitOwner != w.id {
			slog.Warn("split aborted: lease lost", "id", mainID, "owner", cur.SplitOwner)
			return
		}
		if cur != nil && cur.Status == domain.TaskStatusPaused {
			_ = w.tasks.ClearSplitLease(ctx, mainID)
			return
		}
		_, _ = w.tasks.UpdateMainTaskStats(ctx, fresh.ID, fresh.Version, 0, 0, 0, domain.TaskStatusFailed)
		_ = w.tasks.ClearSplitLease(ctx, mainID)
		return
	}
	slog.Info("main task split done", "id", mainID)
}

func (w *Worker) loopClaim(ctx context.Context, idx int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		st, err := w.tasks.ClaimSubTask(ctx, fmt.Sprintf("%s-%d", w.id, idx), w.claimTimeoutSec)
		if err != nil {
			slog.Error("claim subtask", "err", err)
			time.Sleep(w.pollInterval)
			continue
		}
		if st == nil {
			time.Sleep(w.pollInterval)
			continue
		}
		err = w.processSubTask(ctx, st)
		if errors.Is(err, errMainCancelled) || errors.Is(err, errMainPaused) {
			continue
		}
		if err != nil {
			slog.Error("process subtask", "sub_id", st.ID, "err", err)
			updated, uerr := w.tasks.UpdateSubTaskResult(ctx, st.ID, st.WorkerID, 0, st.TotalCount, domain.TaskStatusFailed, err.Error())
			if uerr != nil {
				slog.Error("update fail result", "err", uerr)
				continue
			}
			if !updated {
				slog.Info("skip fail aggregate: lost claim", "sub_id", st.ID, "worker", st.WorkerID)
				continue
			}
			_ = w.agg.OnSubFinished(ctx, st.MainTaskID, st.ID, 0, int64(st.TotalCount))
		}
	}
}

type subPayload struct {
	UserIDs   []string                        `json:"user_ids"`
	Vars      map[string]map[string]string    `json:"vars"`
	Channels  map[string][]domain.ChannelType `json:"channels"`
	Extras    map[string]map[string]any       `json:"extras"`
	Locales   map[string]string               `json:"locales"`
	Timezones map[string]string               `json:"timezones"`
}

var (
	errMainCancelled = errors.New("main task cancelled")
	errMainPaused    = errors.New("main task paused")
)

func (w *Worker) processSubTask(ctx context.Context, st *domain.SubTask) error {
	main, err := w.tasks.GetMainTask(ctx, st.MainTaskID)
	if err != nil {
		return err
	}
	if main.Status == domain.TaskStatusCancelled {
		_, _ = w.tasks.UpdateSubTaskResult(ctx, st.ID, st.WorkerID, 0, 0, domain.TaskStatusCancelled, "main task cancelled")
		slog.Info("skip subtask: main cancelled", "sub_id", st.ID, "main_id", st.MainTaskID)
		return errMainCancelled
	}
	if main.Status == domain.TaskStatusPaused {
		_ = w.tasks.ReleaseSubTask(ctx, st.ID)
		slog.Info("release subtask: main paused", "sub_id", st.ID, "main_id", st.MainTaskID)
		return errMainPaused
	}

	var payload subPayload
	if err := json.Unmarshal([]byte(st.UserIDs), &payload); err != nil {
		return fmt.Errorf("parse user_ids: %w", err)
	}

	var campaignExtra map[string]any
	if main.Payload != "" && main.Payload != "null" {
		_ = json.Unmarshal([]byte(main.Payload), &campaignExtra)
	}

	msgs := make([]domain.PushMessage, 0, len(payload.UserIDs))
	now := time.Now()
	taskChs := main.ChannelList()
	chMode := main.EffectiveChannelMode()
	prio := main.Priority.Normalize()
	rootContents := main.ContentsMap()
	rootLocales := main.LocalesMap()
	for _, uid := range payload.UserIDs {
		chs := taskChs
		if userChs, ok := payload.Channels[uid]; ok && len(userChs) > 0 {
			chs = userChs
		}
		if len(chs) == 0 {
			continue
		}
		mode := chMode
		if len(chs) <= 1 && mode != domain.ChannelModeConditional && mode != domain.ChannelModeCostPriority {
			mode = domain.ChannelModeSingle
		}
		vars := payload.Vars[uid]
		// 覆盖顺序：活动 Payload 为底，用户 Extra 覆盖同名键（手机号/token 等以用户为准）
		merged := domain.MergeExtra(campaignExtra, payload.Extras[uid])
		locale := ""
		if payload.Locales != nil {
			locale = payload.Locales[uid]
		}
		tz := ""
		if payload.Timezones != nil {
			tz = payload.Timezones[uid]
		}
		body, contents := domain.ResolveLocaleContent(main.TemplateBody, rootContents, main.DefaultLocale, rootLocales, locale)
		msgs = append(msgs, domain.PushMessage{
			MsgID:            fmt.Sprintf("%d-%d-%s", st.MainTaskID, st.ID, uid),
			MainTaskID:       st.MainTaskID,
			SubTaskID:        st.ID,
			UserID:           uid,
			Channel:          chs[0],
			Channels:         chs,
			ChannelMode:      mode,
			TemplateID:       main.TemplateID,
			Title:            main.Title,
			Body:             body,
			Contents:         contents,
			Vars:             vars,
			Extra:            merged,
			BizScene:         main.BizScene,
			Priority:         prio,
			Locale:           locale,
			Timezone:         tz,
			MissingVarPolicy: main.MissingVarPolicy,
			ExpireAt:         main.ExpireAt,
			MaxFallback:      main.MaxFallback,
			ChannelRoutes:    main.ChannelRoutes(),
			ChannelCosts:     main.ChannelCosts(),
			CreatedAt:        now,
		})
	}

	main, err = w.tasks.GetMainTask(ctx, st.MainTaskID)
	if err != nil {
		return err
	}
	if main.Status == domain.TaskStatusCancelled {
		_, _ = w.tasks.UpdateSubTaskResult(ctx, st.ID, st.WorkerID, 0, 0, domain.TaskStatusCancelled, "main task cancelled")
		return errMainCancelled
	}
	if main.Status == domain.TaskStatusPaused {
		_ = w.tasks.ReleaseSubTask(ctx, st.ID)
		return errMainPaused
	}

	if err := w.publishMsgs(ctx, main, msgs); err != nil {
		return err
	}

	updated, err := w.tasks.UpdateSubTaskResult(ctx, st.ID, st.WorkerID, len(msgs), 0, domain.TaskStatusSuccess, "")
	if err != nil {
		return err
	}
	if !updated {
		slog.Info("skip success aggregate: lost claim", "sub_id", st.ID, "worker", st.WorkerID)
		return nil
	}
	return w.agg.OnSubFinished(ctx, st.MainTaskID, st.ID, int64(len(msgs)), 0)
}

// publishMsgs 入队；渠道高压时按 backpressure 拉长 pace（二期反压）
func (w *Worker) publishMsgs(ctx context.Context, main *domain.MainTask, msgs []domain.PushMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	pace := main.PaceQPS
	ch := msgs[0].Channel
	if len(msgs[0].Channels) > 0 {
		ch = msgs[0].Channels[0]
	}
	prio := main.Priority.Normalize()

	if w.limiter != nil && w.limiter.Enabled() {
		util, err := w.limiter.Utilization(ctx, ch, prio)
		if err == nil {
			if qh, ok := w.limiter.(quotaBackpressure); ok {
				bp := qh.Backpressure()
				if bp.Enabled && qh.SustainedHigh(ch, prio, util) {
					if pace <= 0 {
						pace = bp.DefaultPaceWhenThrottled
					} else {
						pace = int(float64(pace) * bp.SlowdownFactor)
						if pace < 1 {
							pace = 1
						}
					}
					slog.Info("enqueue slowdown",
						"main_task_id", main.ID,
						"channel", ch,
						"priority", prio,
						"util", util,
						"pace_qps", pace,
						"stage", "enqueue_slowdown",
					)
				}
			}
		}
	}

	if pace > 0 {
		interval := time.Second / time.Duration(pace)
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		for i := range msgs {
			if i > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(interval):
				}
			}
			if err := w.mq.Publish(ctx, msgs[i:i+1]); err != nil {
				return fmt.Errorf("publish mq: %w", err)
			}
		}
		return nil
	}
	if err := w.mq.Publish(ctx, msgs); err != nil {
		return fmt.Errorf("publish mq: %w", err)
	}
	return nil
}

type quotaBackpressure interface {
	Backpressure() config.QuotaBackpressureConfig
	SustainedHigh(channel domain.ChannelType, priority domain.Priority, util float64) bool
}
