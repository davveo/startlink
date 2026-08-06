package scheduler

import (
	"context"
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
	if subTaskID > 0 && a.cache != nil {
		first, err := a.cache.TryMarkSubFinished(ctx, mainTaskID, subTaskID)
		if err != nil {
			return err
		}
		if !first {
			slog.Info("aggregator skip duplicate sub finish", "main_task_id", mainTaskID, "sub_id", subTaskID)
			return nil
		}
	}

	done, err := a.cache.IncrSubDone(ctx, mainTaskID, success, fail)
	if err != nil {
		return err
	}

	main, err := a.tasks.GetMainTask(ctx, mainTaskID)
	if err != nil {
		return err
	}
	if main.Status.IsTerminal() {
		return nil
	}

	// 仅累加 sub_task_done；用户 success/fail 由 push_records 校准，避免与入队增量互相覆盖
	_, err = a.tasks.UpdateMainTaskStats(ctx, main.ID, main.Version, 0, 0, 1, main.Status)
	if err != nil {
		return err
	}

	// 暂停中：不推进终态
	if main.Status == domain.TaskStatusPaused {
		return nil
	}

	// 拆分未收尾（sub_task_total 尚未写入）时不推进终态
	if main.SubTaskTotal <= 0 || int(done) < main.SubTaskTotal {
		return nil
	}

	succTotal, failTotal, _, err := a.cache.GetSubDone(ctx, mainTaskID)
	if err != nil {
		return err
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
