package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/starlink/push/internal/adapter/audience"
	"github.com/starlink/push/internal/app/trace"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

const (
	defaultMaxAudiencePages = 10000
	defaultMaxAudienceUsers = 5_000_000
	defaultMaxPageSize      = 2000
)

// Splitter 将主任务按用户分片拆成子任务（流式落库：边圈人边 CreateSubTasks）
type Splitter struct {
	tasks     port.TaskRepository
	push      port.PushRepository
	audience  AudienceResolver
	excluder  ExcludeResolver
	limiter   port.ChannelLimiter
	tracer    *trace.Recorder
	batchSize int
	maxPages  int
	maxUsers  int64
}

// AudienceResolver 抽象人群解析，避免 scheduler 直接依赖 adapter
type AudienceResolver interface {
	Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error)
}

// ExcludeResolver 解析排除名单人群段的成员集合
type ExcludeResolver interface {
	ResolveExcludeUserIDs(ctx context.Context, code string) (map[string]struct{}, error)
}

// SetExcludeResolver 注入排除名单解析器；未注入时活动上的 exclude_segment_code 会被拒绝执行，
// 而不是被静默忽略——静默忽略等于对整个排除名单误发。
func (s *Splitter) SetExcludeResolver(r ExcludeResolver) { s.excluder = r }

// SetTracer 注入全链路埋点（可选）
func (s *Splitter) SetTracer(t *trace.Recorder) { s.tracer = t }

func (s *Splitter) emit(ctx context.Context, main *domain.MainTask, subTaskID uint64, event, level, message string, detail map[string]any) {
	if s == nil || s.tracer == nil || main == nil || main.TraceID == "" {
		return
	}
	ev := trace.FromMain(main)
	ev.SubTaskID = subTaskID
	ev.Stage = domain.TraceStageSplit
	ev.Event = event
	ev.Level = level
	ev.Message = message
	ev.Detail = detail
	s.tracer.Emit(ctx, ev)
}

func NewSplitter(tasks port.TaskRepository, audience AudienceResolver, limiter port.ChannelLimiter, batchSize int, push ...port.PushRepository) *Splitter {
	if batchSize <= 0 {
		batchSize = 200
	}
	if batchSize > defaultMaxPageSize {
		batchSize = defaultMaxPageSize
	}
	var pushRepo port.PushRepository
	if len(push) > 0 {
		pushRepo = push[0]
	}
	return &Splitter{
		tasks:     tasks,
		push:      pushRepo,
		audience:  audience,
		limiter:   limiter,
		batchSize: batchSize,
		maxPages:  defaultMaxAudiencePages,
		maxUsers:  defaultMaxAudienceUsers,
	}
}

func (s *Splitter) Split(ctx context.Context, main *domain.MainTask, owner string) (err error) {
	s.emit(ctx, main, 0, domain.TraceEventSplitStarted, domain.TraceLevelInfo,
		fmt.Sprintf("开始拆分人群（主任务 #%d）", main.ID), nil)
	defer func() {
		if err != nil {
			s.emit(ctx, main, 0, domain.TraceEventSplitFailed, domain.TraceLevelError,
				fmt.Sprintf("主任务 #%d 拆分失败：%s", main.ID, err.Error()), map[string]any{
					"error": err.Error(),
				})
		}
	}()

	if main.Status == domain.TaskStatusCancelled {
		return fmt.Errorf("main task %d cancelled", main.ID)
	}
	// 过期活动圈人/建子任务/入队全是无效写入，入口直接短路
	if isExpired(main) {
		return fmt.Errorf("%w: main task %d expired at %s", domain.ErrCampaignExpired, main.ID, main.ExpireAt.Format(time.RFC3339))
	}

	var extra map[string]any
	if main.AudienceExtra != "" {
		_ = json.Unmarshal([]byte(main.AudienceExtra), &extra)
	}

	// 排除名单一次性解析：分页时逐页去查会放大上游压力，也无法保证各页看到同一份名单
	var excluded map[string]struct{}
	if code := strings.TrimSpace(main.ExcludeSegmentCode); code != "" {
		if s.excluder == nil {
			return fmt.Errorf("main task %d 配置了排除名单 %q，但调度器未注入排除解析器", main.ID, code)
		}
		var err error
		excluded, err = s.excluder.ResolveExcludeUserIDs(ctx, code)
		if err != nil {
			return fmt.Errorf("resolve exclude segment %q: %w", code, err)
		}
		slog.Info("exclude segment loaded", "main_task_id", main.ID, "segment", code, "size", len(excluded))
	}

	pageToken := ""
	shard := 0
	var total int64
	wroteAny := false
	pages := 0

	for {
		pages++
		if pages > s.maxPages {
			return fmt.Errorf("%w: max pages %d", domain.ErrAudienceLimitExceeded, s.maxPages)
		}

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

		pageSize := s.batchSize
		if pageSize > defaultMaxPageSize {
			pageSize = defaultMaxPageSize
		}
		page, err := s.audience.Resolve(ctx, domain.AudienceQuery{
			AudienceRef: main.AudienceRef,
			BizScene:    main.BizScene,
			Extra:       extra,
			PageToken:   pageToken,
			PageSize:    pageSize,
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
			extraMap := make(map[string]map[string]any)
			localeMap := make(map[string]string)
			tzMap := make(map[string]string)

			abPercent := -1
			abSalt := main.ExperimentSalt
			if extra != nil {
				switch v := extra["ab_sample_percent"].(type) {
				case float64:
					abPercent = int(v)
				case int:
					abPercent = v
				}
				if s, ok := extra["ab_sample_salt"].(string); ok && s != "" && abSalt == "" {
					abSalt = s
				}
			}

			var assigns []domain.ExperimentAssignment
			for _, u := range page.Users {
				if u.UserID == "" {
					continue
				}
				if _, skip := excluded[u.UserID]; skip {
					continue
				}
				userExtra := u.Extra
				if userExtra == nil {
					userExtra = map[string]any{}
				} else {
					// copy so we can annotate experiment group
					cp := make(map[string]any, len(userExtra)+2)
					for k, v := range userExtra {
						cp[k] = v
					}
					userExtra = cp
				}
				if main.ExperimentID != "" || main.ExperimentControlPercent > 0 {
					group := audience.ExperimentGroup(u.UserID, abSalt, main.ExperimentControlPercent)
					userExtra["experiment_id"] = main.ExperimentID
					userExtra["experiment_group"] = group
					assigns = append(assigns, domain.ExperimentAssignment{
						MainTaskID:   main.ID,
						UserID:       u.UserID,
						ExperimentID: main.ExperimentID,
						GroupName:    group,
					})
					if group == "control" {
						continue
					}
				} else if abPercent >= 0 && !audience.SampleByPercentWithSalt(u.UserID, abSalt, abPercent) {
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
				if len(userExtra) > 0 {
					extraMap[u.UserID] = userExtra
				}
				if u.Locale != "" {
					localeMap[u.UserID] = u.Locale
				}
				if u.Timezone != "" {
					tzMap[u.UserID] = u.Timezone
				}
			}
			if len(assigns) > 0 && s.push != nil {
				if err := s.push.CreateExperimentAssignments(ctx, assigns); err != nil {
					slog.Warn("save experiment assignments failed", "main_task_id", main.ID, "err", err)
				}
			}
			if len(ids) == 0 {
				if !page.HasMore {
					break
				}
				next, err := advancePageToken(pageToken, page)
				if err != nil {
					return err
				}
				pageToken = next
				continue
			}
			payload := map[string]any{"user_ids": ids}
			if len(varMap) > 0 {
				payload["vars"] = varMap
			}
			if len(chMap) > 0 {
				payload["channels"] = chMap
			}
			if len(extraMap) > 0 {
				// 用户级 Extra（手机号/邮箱/device token 等）随子任务落库；敏感字段建议上游加密或短期保留
				payload["extras"] = extraMap
			}
			if len(localeMap) > 0 {
				payload["locales"] = localeMap
			}
			if len(tzMap) > 0 {
				payload["timezones"] = tzMap
			}
			raw, _ := json.Marshal(payload)
			batch := []domain.SubTask{{
				MainTaskID: main.ID,
				ShardIndex: shard,
				UserIDs:    string(raw),
				TotalCount: len(ids),
				Status:     domain.TaskStatusPending,
			}}
			// 页首的续租在慢分页后可能已失效：写入与租约校验必须同事务，
			// 否则被抢租约的实例仍会插入已作废的分片，留下永久 pending 的孤儿。
			ok, err := s.tasks.CreateSubTasksWithLease(ctx, main.ID, owner, batch)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("lost split lease for main_task=%d before shard %d", main.ID, shard)
			}
			wroteAny = true
			total += int64(len(ids))
			if total > s.maxUsers {
				return fmt.Errorf("%w: max users %d", domain.ErrAudienceLimitExceeded, s.maxUsers)
			}
			subID := uint64(0)
			if len(batch) > 0 {
				subID = batch[0].ID
			}
			s.emit(ctx, main, subID, domain.TraceEventSplitShard, domain.TraceLevelInfo,
				fmt.Sprintf("主任务 #%d 分片 #%d 已创建子任务 #%d，本页 %d 人", main.ID, shard, subID, len(ids)),
				map[string]any{"shard": shard, "sub_task_id": subID, "users": len(ids), "total": total})
			shard++
			// 仅更新 total_count；sub_task_total 收尾再写，避免拆分中途被 Claim/聚合终态
			if err := s.tasks.PatchMainMeta(ctx, main.ID, total, 0); err != nil {
				slog.Warn("patch total during split", "id", main.ID, "err", err)
			}
		}
		if !page.HasMore {
			break
		}
		next, err := advancePageToken(pageToken, page)
		if err != nil {
			return err
		}
		pageToken = next
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

	// 非终态状态不做 CAS，恒返回 true，故无需处理版本冲突分支
	if _, err := s.tasks.UpdateMainTaskStats(ctx, main.ID, main.Version, 0, 0, 0, domain.TaskStatusRunning); err != nil {
		return err
	}
	// 收尾写入完整 meta；随后 ClearSplitLease，Claim 才放开
	if err := s.tasks.PatchMainMeta(ctx, main.ID, total, shard); err != nil {
		return err
	}
	_ = s.tasks.ClearSplitLease(ctx, main.ID)

	s.checkOverCapacity(ctx, main, total)
	s.emit(ctx, main, 0, domain.TraceEventSplitDone, domain.TraceLevelInfo,
		fmt.Sprintf("主任务 #%d 拆分完成：%d 人 / %d 分片", main.ID, total, shard),
		map[string]any{"total": total, "shards": shard})
	return nil
}

// advancePageToken 校验分页游标前进，防止 HasMore=true 且 token 空/重复导致死循环。
func advancePageToken(prev string, page *domain.AudiencePage) (string, error) {
	if page == nil {
		return "", domain.ErrAudiencePageStuck
	}
	next := page.NextPageToken
	if next == "" {
		return "", fmt.Errorf("%w: HasMore without NextPageToken", domain.ErrAudiencePageStuck)
	}
	if next == prev {
		return "", fmt.Errorf("%w: NextPageToken unchanged", domain.ErrAudiencePageStuck)
	}
	return next, nil
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
		if action != "pause" {
			continue // warn 模式下继续检查其余渠道
		}
		if ok, err := s.tasks.PauseMainTask(ctx, main.ID); err != nil {
			slog.Warn("auto-pause after over capacity failed", "id", main.ID, "err", err)
		} else if ok {
			slog.Info("main task paused due to channel quota", "id", main.ID, "channel", ch)
		}
		return
	}
}
