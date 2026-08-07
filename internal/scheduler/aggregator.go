package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// Aggregator 状态聚合器：监听子任务终态，原子累加计数并 CAS 推进主任务终态
type Aggregator struct {
	tasks    port.TaskRepository
	push     port.PushRepository
	cache    port.AggregatorCache
	notifier port.Notifier
	inbox    port.NotificationRepository
}

func NewAggregator(tasks port.TaskRepository, cache port.AggregatorCache, notifier port.Notifier, push port.PushRepository, inbox port.NotificationRepository) *Aggregator {
	return &Aggregator{tasks: tasks, push: push, cache: cache, notifier: notifier, inbox: inbox}
}

// OnSubFinished 子任务完成后聚合；按 subTaskID 幂等，重复调用不会虚高 done
func (a *Aggregator) OnSubFinished(ctx context.Context, mainTaskID, subTaskID uint64, success, fail int64) error {
	if a.cache == nil {
		return fmt.Errorf("aggregator cache is required")
	}

	// 先读主任务再打标记：GetMainTask 无副作用，失败时不必回滚
	main, err := a.tasks.GetMainTask(ctx, mainTaskID)
	if err != nil {
		return err
	}
	if main == nil {
		return fmt.Errorf("main task %d not found", mainTaskID)
	}

	marked := false
	if subTaskID > 0 {
		first, err := a.cache.TryMarkSubFinished(ctx, mainTaskID, subTaskID)
		if err != nil {
			return err
		}
		if !first {
			slog.Info("aggregator skip duplicate sub finish", "main_task_id", mainTaskID, "sub_id", subTaskID)
			return nil
		}
		marked = true
	}
	if main.Status.IsTerminal() {
		return nil
	}

	// DB 的 sub_task_done 才是权威计数，先落库再动 Redis：
	// 落库失败必须回滚幂等标记，否则该子任务的聚合永久丢失（调用方无重放路径）。
	// 仅累加 sub_task_done；用户 success/fail 由 push_records 校准，避免与入队增量互相覆盖
	if _, err := a.tasks.UpdateMainTaskStats(ctx, main.ID, main.Version, 0, 0, 1, main.Status); err != nil {
		a.rollbackSubFinished(ctx, mainTaskID, subTaskID, marked)
		return err
	}

	done := int64(main.SubTaskDone) + 1
	// Redis 计数只是加速通道，出错不回滚标记（DB 已递增，回滚会导致重复累加）
	if redisDone, err := a.cache.IncrSubDone(ctx, mainTaskID, success, fail); err != nil {
		slog.Warn("aggregator incr redis counter failed, fallback to db counter",
			"main_task_id", mainTaskID, "sub_id", subTaskID, "err", err)
	} else if redisDone > done {
		done = redisDone
	}

	// 暂停中：不推进终态
	if main.Status == domain.TaskStatusPaused {
		return nil
	}
	return a.finalizeIfDone(ctx, main, done)
}

func (a *Aggregator) rollbackSubFinished(ctx context.Context, mainTaskID, subTaskID uint64, marked bool) {
	if !marked {
		return
	}
	if err := a.cache.UnmarkSubFinished(ctx, mainTaskID, subTaskID); err != nil {
		slog.Error("aggregator rollback sub finish mark failed",
			"main_task_id", mainTaskID, "sub_id", subTaskID, "err", err)
	}
}

// FinalizeStale 终态补推：Redis 计数丢失（重启/逐出/超 TTL）时主任务会永久停在 running，
// 由 reaper 按 DB 的 sub_task_done 重新判定。CAS 保证与正常聚合路径不会重复推进。
func (a *Aggregator) FinalizeStale(ctx context.Context, mainTaskID uint64) error {
	if a.cache == nil {
		return fmt.Errorf("aggregator cache is required")
	}
	main, err := a.tasks.GetMainTask(ctx, mainTaskID)
	if err != nil {
		return err
	}
	if main == nil || main.Status != domain.TaskStatusRunning {
		return nil
	}
	return a.finalizeIfDone(ctx, main, int64(main.SubTaskDone))
}

// finalizeIfDone 全部子任务完成时 CAS 推进主任务终态
func (a *Aggregator) finalizeIfDone(ctx context.Context, main *domain.MainTask, done int64) error {
	// 拆分未收尾（sub_task_total 尚未写入）时不推进终态
	if main.SubTaskTotal <= 0 || done < int64(main.SubTaskTotal) {
		return nil
	}
	mainTaskID := main.ID

	succTotal, failTotal, _, err := a.cache.GetSubDone(ctx, mainTaskID)
	if err != nil {
		return err
	}
	if succTotal == 0 && failTotal == 0 {
		// Redis 计数已丢失（重启/逐出/超 TTL），退回 DB 计数判定，避免一律判成功
		succTotal, failTotal = main.SuccessCount, main.FailCount
	}
	// 流水线终态仍按子任务入队成功/失败判定
	final := domain.TaskStatusSuccess
	switch {
	case failTotal == 0:
		final = domain.TaskStatusSuccess
	case succTotal == 0:
		final = domain.TaskStatusFailed
	default:
		final = domain.TaskStatusPartial
	}

	ok, err := a.tasks.UpdateMainTaskStats(ctx, main.ID, main.Version, 0, 0, 0, final)
	if err != nil {
		return err
	}
	if !ok {
		main, err = a.tasks.GetMainTask(ctx, mainTaskID)
		if err != nil {
			return err
		}
		if main.Status.IsTerminal() || main.Status == domain.TaskStatusPaused {
			_ = a.refreshUserCounts(ctx, mainTaskID)
			return nil
		}
		ok, err = a.tasks.UpdateMainTaskStats(ctx, main.ID, main.Version, 0, 0, 0, final)
		if err != nil {
			return err
		}
		if !ok {
			slog.Warn("aggregator cas conflict on finish", "main_task_id", mainTaskID)
			return nil
		}
	}
	_ = a.refreshUserCounts(ctx, mainTaskID)
	slog.Info("main task aggregated", "id", mainTaskID, "status", final, "pipeline_success", succTotal, "pipeline_fail", failTotal)
	a.notifyFinished(mainTaskID, final)
	return nil
}

// refreshUserCounts 用 push_records 渠道口径校准主任务 success/fail（展示与 Webhook）
func (a *Aggregator) refreshUserCounts(ctx context.Context, mainTaskID uint64) error {
	if a.push == nil {
		return nil
	}
	oc, err := a.push.CountUserOutcomes(ctx, mainTaskID)
	if err != nil {
		slog.Warn("refresh user counts", "main_task_id", mainTaskID, "err", err)
		return err
	}
	if !oc.HasRecords {
		return nil
	}
	return a.tasks.SyncMainUserCounts(ctx, mainTaskID, oc.SuccessUsers, oc.FailUsers)
}

func (a *Aggregator) notifyFinished(mainTaskID uint64, status domain.TaskStatus) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		main, err := a.tasks.GetMainTask(ctx, mainTaskID)
		if err != nil {
			return
		}
		a.emitInbox(ctx, main, status)
		if a.notifier == nil {
			return
		}
		event := domain.WebhookEvent{
			Event:        "task.finished",
			TaskID:       main.ID,
			BizID:        main.BizID,
			BizScene:     main.BizScene,
			Title:        main.Title,
			Channel:      main.Channel,
			Status:       status,
			TotalCount:   main.TotalCount,
			SuccessCount: main.SuccessCount,
			FailCount:    main.FailCount,
			SubTaskTotal: main.SubTaskTotal,
			SubTaskDone:  main.SubTaskDone,
			FinishedAt:   main.FinishedAt,
			Timestamp:    time.Now(),
		}
		if err := a.notifier.NotifyTaskFinished(ctx, main.WebhookURL, event); err != nil {
			slog.Warn("webhook notify failed", "task_id", mainTaskID, "err", err)
		}
	}()
}

// EmitTaskTerminal 主任务进入终态时写站内通知（供拆分失败等路径复用）
func (a *Aggregator) EmitTaskTerminal(ctx context.Context, mainTaskID uint64, status domain.TaskStatus) {
	if a == nil {
		return
	}
	main, err := a.tasks.GetMainTask(ctx, mainTaskID)
	if err != nil {
		return
	}
	a.emitInbox(ctx, main, status)
}

func (a *Aggregator) emitInbox(ctx context.Context, main *domain.MainTask, status domain.TaskStatus) {
	if a.inbox == nil || main == nil {
		return
	}
	n := domain.NewTaskTerminalNotification(main, status)
	if n == nil {
		return
	}
	if err := a.inbox.Create(ctx, n); err != nil {
		slog.Warn("inbox notification failed", "task_id", main.ID, "status", status, "err", err)
	}
}
