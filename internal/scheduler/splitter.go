package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/starlink/push/internal/adapter/audience"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

// Splitter 将主任务按用户分片拆成子任务（流式落库：边圈人边 CreateSubTasks）
type Splitter struct {
	tasks     port.TaskRepository
	audience  AudienceResolver
	limiter   port.ChannelLimiter
	batchSize int
}

// AudienceResolver 抽象人群解析，避免 scheduler 直接依赖 adapter
type AudienceResolver interface {
	Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error)
}

func NewSplitter(tasks port.TaskRepository, audience AudienceResolver, limiter port.ChannelLimiter, batchSize int) *Splitter {
	return &Splitter{tasks: tasks, audience: audience, limiter: limiter, batchSize: batchSize}
}

func (s *Splitter) Split(ctx context.Context, main *domain.MainTask, owner string) error {
	if main.Status == domain.TaskStatusCancelled {
		return fmt.Errorf("main task %d cancelled", main.ID)
	}

	var extra map[string]any
	if main.AudienceExtra != "" {
		_ = json.Unmarshal([]byte(main.AudienceExtra), &extra)
	}

	pageToken := ""
	shard := 0
	var total int64
	wroteAny := false

	for {
		cur, err := s.tasks.GetMainTask(ctx, main.ID)
		if err != nil {
			return err
		}
		if cur.Status == domain.TaskStatusCancelled {
			_, _ = s.tasks.CancelUnfinishedSubTasks(ctx, main.ID)
			return fmt.Errorf("main task %d cancelled during split", main.ID)
		}
		if cur.Status == domain.TaskStatusPaused {
			return fmt.Errorf("main task %d paused during split", main.ID)
		}
		if owner != "" {
			ok, err := s.tasks.RenewSplitLease(ctx, main.ID, owner)
			if err != nil {
				return fmt.Errorf("renew split lease: %w", err)
			}
			if !ok {
				return fmt.Errorf("lost split lease for main_task=%d", main.ID)
			}
		}

		page, err := s.audience.Resolve(ctx, domain.AudienceQuery{
			AudienceRef: main.AudienceRef,
			BizScene:    main.BizScene,
			Extra:       extra,
			PageToken:   pageToken,
			PageSize:    s.batchSize,
		})
		if err != nil {
			return fmt.Errorf("resolve audience: %w", err)
		}
		if len(page.Users) == 0 && !page.HasMore {
			break
		}
		if len(page.Users) > 0 {
			taskChs := main.ChannelList()
			ids := make([]string, 0, len(page.Users))
			varMap := make(map[string]map[string]string)
			chMap := make(map[string][]domain.ChannelType)

			abPercent := -1
			if extra != nil {
				switch v := extra["ab_sample_percent"].(type) {
				case float64:
					abPercent = int(v)
				case int:
					abPercent = v
				}
			}

			for _, u := range page.Users {
				if u.UserID == "" {
					continue
				}
				if abPercent >= 0 && !audience.SampleByPercent(u.UserID, abPercent) {
					continue
				}
				chs := domain.IntersectChannels(taskChs, u.Channels)
				if len(chs) == 0 {
					slog.Info("skip user: no reachable channel intersect", "user", u.UserID, "main_task_id", main.ID)
					continue
				}
				ids = append(ids, u.UserID)
				if len(u.Vars) > 0 {
					varMap[u.UserID] = u.Vars
				}
				if len(u.Channels) > 0 {
					chMap[u.UserID] = chs
				}
			}
			if len(ids) == 0 {
				if !page.HasMore {
					break
				}
				pageToken = page.NextPageToken
				continue
			}
			payload := map[string]any{"user_ids": ids}
			if len(varMap) > 0 {
				payload["vars"] = varMap
			}
			if len(chMap) > 0 {
				payload["channels"] = chMap
			}
			raw, _ := json.Marshal(payload)
			batch := []domain.SubTask{{
				MainTaskID: main.ID,
				ShardIndex: shard,
				UserIDs:    string(raw),
				TotalCount: len(ids),
				Status:     domain.TaskStatusPending,
			}}
			if err := s.tasks.CreateSubTasks(ctx, batch); err != nil {
				return err
			}
			wroteAny = true
			total += int64(len(ids))
			shard++
			// 仅更新 total_count；sub_task_total 收尾再写，避免拆分中途被 Claim/聚合终态
			if err := s.tasks.PatchMainMeta(ctx, main.ID, total, 0); err != nil {
				slog.Warn("patch total during split", "id", main.ID, "err", err)
			}
		}
		if !page.HasMore {
			break
		}
		pageToken = page.NextPageToken
	}

	if !wroteAny {
		return errcode.AudienceEmpty
	}

	cur, err := s.tasks.GetMainTask(ctx, main.ID)
	if err != nil {
		return err
	}
	if cur.Status == domain.TaskStatusCancelled {
		_, _ = s.tasks.CancelUnfinishedSubTasks(ctx, main.ID)
		return fmt.Errorf("main task %d cancelled after create subtasks", main.ID)
	}

	main.TotalCount = total
	main.SubTaskTotal = shard
	main.Status = domain.TaskStatusRunning
	now := time.Now()
	main.StartedAt = &now

	ok, err := s.tasks.UpdateMainTaskStats(ctx, main.ID, main.Version, 0, 0, 0, domain.TaskStatusRunning)
	if err != nil {
		return err
	}
	if !ok {
		cur, _ = s.tasks.GetMainTask(ctx, main.ID)
		if cur != nil && cur.Status == domain.TaskStatusCancelled {
			_, _ = s.tasks.CancelUnfinishedSubTasks(ctx, main.ID)
			return fmt.Errorf("main task %d cancelled", main.ID)
		}
		slog.Warn("main task version conflict after split", "id", main.ID)
	}
	// 收尾写入完整 meta；随后 ClearSplitLease，Claim 才放开
	if err := s.tasks.PatchMainMeta(ctx, main.ID, total, shard); err != nil {
		return err
	}
	_ = s.tasks.ClearSplitLease(ctx, main.ID)

	s.checkOverCapacity(ctx, main, total)
	return nil
}

// checkOverCapacity 拆分后按渠道配额估算是否超容量（三期）
func (s *Splitter) checkOverCapacity(ctx context.Context, main *domain.MainTask, total int64) {
	if s.limiter == nil || !s.limiter.Enabled() || total <= 0 {
		return
	}
	chs := main.ChannelList()
	if len(chs) == 0 && main.Channel != "" {
		chs = []domain.ChannelType{main.Channel}
	}
	prio := main.Priority.Normalize()
	action := "warn"
	if qh, ok := s.limiter.(interface{ OverCapacityAction() string }); ok {
		if a := qh.OverCapacityAction(); a != "" {
			action = a
		}
	}
	for _, ch := range chs {
		if s.limiter.AdmissionMode(ch) != "enforce" {
			continue
		}
		minutes := s.limiter.TargetFinishMinutes(ch)
		if minutes <= 0 {
			minutes = 60
		}
		demand := float64(total) / (float64(minutes) * 60)
		avail := s.limiter.AvailableQPS(ch, prio)
		if avail <= 0 || demand <= avail {
			continue
		}
		slog.Warn("split over channel capacity",
			"main_task_id", main.ID,
			"biz_id", main.BizID,
			"channel", ch,
			"total", total,
			"demand_qps", demand,
			"available_qps", avail,
			"action", action,
			"stage", "split_over_capacity",
		)
		if action == "pause" {
			if ok, err := s.tasks.PauseMainTask(ctx, main.ID); err != nil {
				slog.Warn("auto-pause after over capacity failed", "id", main.ID, "err", err)
			} else if ok {
				slog.Info("main task paused due to channel quota", "id", main.ID, "channel", ch)
			}
		}
		return
	}
}
