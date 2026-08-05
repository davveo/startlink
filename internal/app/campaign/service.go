package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/starlink/push/internal/adapter/webhook"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
	"gorm.io/gorm"
)

// Service 应用层通用活动服务
type Service struct {
	tasks          port.TaskRepository
	pushRepo       port.PushRepository
	cache          port.AggregatorCache
	notifier       *webhook.Client
	templates      port.TemplateRepository
	limiter        port.ChannelLimiter
	batchSize      int
	highBizScenes  []string
	defaultChannel domain.ChannelType
}

type Deps struct {
	Tasks          port.TaskRepository
	PushRepo       port.PushRepository
	Cache          port.AggregatorCache
	Notifier       *webhook.Client
	Templates      port.TemplateRepository
	Limiter        port.ChannelLimiter
	BatchSize      int
	HighBizScenes  []string
	DefaultChannel domain.ChannelType
}

func NewService(deps Deps) *Service {
	bs := deps.BatchSize
	if bs <= 0 {
		bs = 200
	}
	return &Service{
		tasks:          deps.Tasks,
		pushRepo:       deps.PushRepo,
		cache:          deps.Cache,
		notifier:       deps.Notifier,
		templates:      deps.Templates,
		limiter:        deps.Limiter,
		batchSize:      bs,
		highBizScenes:  deps.HighBizScenes,
		defaultChannel: deps.DefaultChannel,
	}
}

type CreateResult struct {
	TaskID uint64            `json:"task_id"`
	BizID  string            `json:"biz_id"`
	Status domain.TaskStatus `json:"status"`
}

type CancelResult struct {
	TaskID          uint64            `json:"task_id"`
	Status          domain.TaskStatus `json:"status"`
	AlreadyTerminal bool              `json:"already_terminal,omitempty"`
	CancelledSubs   int64             `json:"cancelled_subs"`
}

type PauseResult struct {
	TaskID uint64            `json:"task_id"`
	Status domain.TaskStatus `json:"status"`
}

type ResumeResult struct {
	TaskID uint64            `json:"task_id"`
	Status domain.TaskStatus `json:"status"`
}

type RetryResult struct {
	TaskID         uint64            `json:"task_id"`
	Status         domain.TaskStatus `json:"status"`
	ResetSubs      int64             `json:"reset_subs"`
	NewSubs        int               `json:"new_subs"`
	RetryUserCount int               `json:"retry_user_count"`
}

// ProgressView 主任务进度视图
type ProgressView struct {
	TaskID      uint64               `json:"task_id"`
	BizID       string               `json:"biz_id"`
	BizScene    string               `json:"biz_scene"`
	Title       string               `json:"title"`
	Channel     domain.ChannelType   `json:"channel"`
	Channels    []domain.ChannelType `json:"channels,omitempty"`
	ChannelMode domain.ChannelMode   `json:"channel_mode"`
	Priority    domain.Priority      `json:"priority"`
	Status      domain.TaskStatus    `json:"status"`

	TotalUsers         int64 `json:"total_users"`
	SuccessUsers       int64 `json:"success_users"`
	FailUsers          int64 `json:"fail_users"`
	SuppressedUsers    int64 `json:"suppressed_users"`
	UnreachableUsers   int64 `json:"unreachable_users"`
	ExpiredUsers       int64 `json:"expired_users"`
	QuotaRejectedUsers int64 `json:"quota_rejected_users"`
	CancelledUsers     int64 `json:"cancelled_users"`
	InProgressUsers    int64 `json:"in_progress_users"`

	SubTaskTotal  int `json:"sub_task_total"`
	SubTaskDone   int `json:"sub_task_done"`
	SubPending    int `json:"sub_pending"`
	SubRunning    int `json:"sub_running"`
	SubSuccess    int `json:"sub_success"`
	SubFailed     int `json:"sub_failed"`
	SubCancelled  int `json:"sub_cancelled"`
	SubInProgress int `json:"sub_in_progress"`

	ProgressPercent float64 `json:"progress_percent"`
	ProgressText    string  `json:"progress_text"`
	Finished        bool    `json:"finished"`

	WebhookURL  string     `json:"webhook_url,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (s *Service) Create(ctx context.Context, in domain.CreateCampaignInput) (*CreateResult, error) {
	in.ApplyDefaultChannel(s.defaultChannel)
	primary, chList, mode, err := in.NormalizeChannels()
	if err != nil {
		return nil, errcode.InvalidParam
	}
	if in.BizID == "" || in.BizScene == "" || in.AudienceRef == "" {
		return nil, errcode.InvalidParam
	}
	if in.TemplateID == "" {
		return nil, errcode.InvalidParam
	}
	if !in.Priority.Valid() {
		return nil, errcode.InvalidParam
	}
	if s.templates == nil {
		return nil, errcode.Internal
	}

	// 基础校验后优先查幂等：模板后续停用不影响同一 biz_id 重试
	exist, err := s.tasks.GetMainTaskByBizID(ctx, in.BizID)
	if err == nil && exist != nil {
		if !campaignRequestMatches(exist, in, primary, chList, mode) {
			return nil, errcode.Conflict
		}
		return &CreateResult{TaskID: exist.ID, BizID: exist.BizID, Status: exist.Status}, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	tpl, err := s.templates.GetByCode(ctx, in.TemplateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	if !tpl.Status.Usable() {
		return nil, errcode.TemplateNotUsable
	}

	prio := domain.ResolvePriority(in.Priority, in.BizScene, s.highBizScenes)

	if err := s.checkQuotaAdmission(ctx, in, chList, prio); err != nil {
		return nil, err
	}

	extra, _ := json.Marshal(in.AudienceExtra)
	payload, _ := json.Marshal(in.Payload)
	chsJSON, _ := json.Marshal(chList)
	windowsJSON, _ := json.Marshal(in.SendWindows)

	task := &domain.MainTask{
		BizID:           in.BizID,
		BizScene:        in.BizScene,
		Priority:        prio,
		Title:           in.Title,
		Channel:         primary,
		Channels:        string(chsJSON),
		ChannelMode:     mode,
		TemplateID:      tpl.Code,
		TemplateBody:    tpl.Body, // 快照，后续改模板不影响已创建活动
		AudienceRef:     in.AudienceRef,
		AudienceExtra:   string(extra),
		Payload:         string(payload),
		WebhookURL:      in.WebhookURL,
		SendWindowsJSON: string(windowsJSON),
		PaceQPS:         in.PaceQPS,
		Status:          domain.TaskStatusPending,
		ScheduledAt:     in.ScheduledAt,
	}
	if err := s.tasks.CreateMainTask(ctx, task); err != nil {
		// 并发创建同一 biz_id：回查已有任务（唯一索引兜底）
		if exist2, e2 := s.tasks.GetMainTaskByBizID(ctx, in.BizID); e2 == nil && exist2 != nil {
			if !campaignRequestMatches(exist2, in, primary, chList, mode) {
				return nil, errcode.Conflict
			}
			return &CreateResult{TaskID: exist2.ID, BizID: exist2.BizID, Status: exist2.Status}, nil
		}
		return nil, err
	}
	return &CreateResult{TaskID: task.ID, BizID: task.BizID, Status: task.Status}, nil
}

// campaignRequestMatches 同一 biz_id 的幂等重试须请求摘要一致，否则 Conflict。
func campaignRequestMatches(exist *domain.MainTask, in domain.CreateCampaignInput, primary domain.ChannelType, chList []domain.ChannelType, mode domain.ChannelMode) bool {
	if exist.BizScene != in.BizScene || exist.Title != in.Title ||
		exist.TemplateID != in.TemplateID || exist.AudienceRef != in.AudienceRef {
		return false
	}
	if exist.Channel != primary || !channelListsEqual(exist.ChannelList(), chList) {
		return false
	}
	wantMode := mode.Normalize()
	if len(chList) <= 1 {
		wantMode = domain.ChannelModeSingle
	} else if wantMode == domain.ChannelModeSingle {
		wantMode = domain.ChannelModeFallback
	}
	return exist.EffectiveChannelMode() == wantMode
}

func channelListsEqual(a, b []domain.ChannelType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkQuotaAdmission 对 admission=enforce 的渠道做创建准入（三期）
func (s *Service) checkQuotaAdmission(ctx context.Context, in domain.CreateCampaignInput, chs []domain.ChannelType, prio domain.Priority) error {
	if s.limiter == nil || !s.limiter.Enabled() {
		return nil
	}
	policy := strings.ToLower(strings.TrimSpace(in.QuotaPolicy))
	if policy == "" {
		policy = "reject" // enforce 渠道默认拒绝超额
	}
	if policy == "queue" {
		return nil
	}

	hint := audienceSizeHint(in.AudienceExtra)
	if hint <= 0 {
		return nil // 无总量提示时跳过硬拒，留给拆分后告警
	}

	for _, ch := range chs {
		if s.limiter.AdmissionMode(ch) != "enforce" {
			continue
		}
		minutes := in.ExpectedFinishMinutes
		if minutes <= 0 {
			minutes = s.limiter.TargetFinishMinutes(ch)
		}
		if minutes <= 0 {
			minutes = 60
		}
		demand := float64(hint) / (float64(minutes) * 60)
		avail := s.limiter.AvailableQPS(ch, prio)
		if avail <= 0 {
			continue
		}
		if demand > avail {
			slog.Warn("campaign admission rejected",
				"biz_id", in.BizID,
				"channel", ch,
				"demand_qps", demand,
				"available_qps", avail,
				"audience_hint", hint,
				"stage", "admission_reject",
			)
			_ = ctx
			return errcode.QuotaExceeded
		}
	}
	return nil
}

func audienceSizeHint(extra map[string]any) int64 {
	if extra == nil {
		return 0
	}
	for _, k := range []string{"total_hint", "audience_size", "total"} {
		v, ok := extra[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int:
			return int64(n)
		case int64:
			return n
		case json.Number:
			i, _ := n.Int64()
			return i
		}
	}
	return 0
}

func (s *Service) Get(ctx context.Context, id uint64) (*domain.MainTask, error) {
	task, err := s.tasks.GetMainTask(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	return task, nil
}

func (s *Service) GetByBizID(ctx context.Context, bizID string) (*domain.MainTask, error) {
	task, err := s.tasks.GetMainTaskByBizID(ctx, bizID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	return task, nil
}

func (s *Service) GetProgress(ctx context.Context, id uint64) (*ProgressView, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildProgress(ctx, task)
}

func (s *Service) GetProgressByBizID(ctx context.Context, bizID string) (*ProgressView, error) {
	task, err := s.GetByBizID(ctx, bizID)
	if err != nil {
		return nil, err
	}
	return s.buildProgress(ctx, task)
}

func (s *Service) buildProgress(ctx context.Context, task *domain.MainTask) (*ProgressView, error) {
	summaries, err := s.tasks.SummarizeSubTasks(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	view := &ProgressView{
		TaskID:       task.ID,
		BizID:        task.BizID,
		BizScene:     task.BizScene,
		Title:        task.Title,
		Channel:      task.Channel,
		Channels:     task.ChannelList(),
		ChannelMode:  task.EffectiveChannelMode(),
		Priority:     task.Priority.Normalize(),
		Status:       task.Status,
		TotalUsers:   task.TotalCount,
		SubTaskTotal: task.SubTaskTotal,
		SubTaskDone:  task.SubTaskDone,
		WebhookURL:   task.WebhookURL,
		ScheduledAt:  task.ScheduledAt,
		StartedAt:    task.StartedAt,
		FinishedAt:   task.FinishedAt,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
		Finished:     task.Status.IsTerminal(),
	}

	var (
		successUsers, failUsers, cancelledUsers, inProgressUsers                 int64
		subPending, subRunning, subSuccess, subFailed, subCancelled, subRetrying int
	)

	for _, sm := range summaries {
		switch sm.Status {
		case domain.TaskStatusPending:
			subPending = sm.SubCount
			inProgressUsers += sm.UserTotal
		case domain.TaskStatusRunning:
			subRunning = sm.SubCount
			inProgressUsers += sm.UserTotal
		case domain.TaskStatusRetrying:
			subRetrying = sm.SubCount
			inProgressUsers += sm.UserTotal
		case domain.TaskStatusSuccess:
			subSuccess = sm.SubCount
			successUsers += sm.UserSuccess
		case domain.TaskStatusFailed:
			subFailed = sm.SubCount
			failUsers += sm.UserFail
		case domain.TaskStatusPartial:
			successUsers += sm.UserSuccess
			failUsers += sm.UserFail
		case domain.TaskStatusCancelled:
			subCancelled = sm.SubCount
			cancelledUsers += sm.UserTotal
		}
	}

	// 用户成功/失败/抑制以 push_records 渠道口径为准
	var suppressedUsers, unreachableUsers, expiredUsers, quotaRejectedUsers int64
	if s.pushRepo != nil {
		oc, err := s.pushRepo.CountUserOutcomes(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		if oc.HasRecords {
			successUsers = oc.SuccessUsers
			failUsers = oc.FailUsers
			suppressedUsers = oc.SuppressedUsers
			unreachableUsers = oc.UnreachableUsers
			expiredUsers = oc.ExpiredUsers
			quotaRejectedUsers = oc.QuotaRejectedUsers
		}
	} else {
		if successUsers == 0 && task.SuccessCount > 0 {
			successUsers = task.SuccessCount
		}
		if failUsers == 0 && task.FailCount > 0 {
			failUsers = task.FailCount
		}
	}
	settledExtra := suppressedUsers + unreachableUsers + expiredUsers + quotaRejectedUsers
	if view.TotalUsers == 0 {
		view.TotalUsers = successUsers + failUsers + cancelledUsers + inProgressUsers + settledExtra
	}

	if task.TotalCount > 0 {
		view.TotalUsers = task.TotalCount
		finished := successUsers + failUsers + cancelledUsers + settledExtra
		if finished > task.TotalCount {
			finished = task.TotalCount
		}
		inProgressUsers = task.TotalCount - finished
		if inProgressUsers < 0 {
			inProgressUsers = 0
		}
		if task.Status.IsTerminal() {
			inProgressUsers = 0
			if cancelledUsers == 0 && task.Status == domain.TaskStatusCancelled {
				cancelledUsers = task.TotalCount - successUsers - failUsers - settledExtra
				if cancelledUsers < 0 {
					cancelledUsers = 0
				}
			}
		}
	}

	view.SuccessUsers = successUsers
	view.FailUsers = failUsers
	view.SuppressedUsers = suppressedUsers
	view.UnreachableUsers = unreachableUsers
	view.ExpiredUsers = expiredUsers
	view.QuotaRejectedUsers = quotaRejectedUsers
	view.CancelledUsers = cancelledUsers
	view.InProgressUsers = inProgressUsers
	view.SubPending = subPending
	view.SubRunning = subRunning
	view.SubSuccess = subSuccess
	view.SubFailed = subFailed
	view.SubCancelled = subCancelled
	view.SubInProgress = subPending + subRunning + subRetrying

	view.ProgressPercent, view.ProgressText = calcProgress(
		task.Status,
		view.TotalUsers,
		successUsers+failUsers+cancelledUsers+settledExtra,
		task.SubTaskTotal,
		task.SubTaskDone,
	)
	return view, nil
}

func calcProgress(status domain.TaskStatus, totalUsers, finishedUsers int64, subTotal, subDone int) (float64, string) {
	if status.IsTerminal() {
		return 100, "100.00%"
	}
	if (status == domain.TaskStatusPending || status == domain.TaskStatusPaused) && totalUsers == 0 && subTotal == 0 {
		return 0, "0.00%"
	}

	var pct float64
	switch {
	case totalUsers > 0:
		pct = float64(finishedUsers) / float64(totalUsers) * 100
	case subTotal > 0:
		pct = float64(subDone) / float64(subTotal) * 100
	default:
		pct = 0
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	pct = math.Round(pct*100) / 100
	return pct, fmt.Sprintf("%.2f%%", pct)
}

func (s *Service) Cancel(ctx context.Context, id uint64) (*CancelResult, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.Status.IsTerminal() {
		return &CancelResult{
			TaskID:          task.ID,
			Status:          task.Status,
			AlreadyTerminal: true,
		}, nil
	}

	ok, err := s.tasks.CancelMainTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		fresh, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return &CancelResult{
			TaskID:          fresh.ID,
			Status:          fresh.Status,
			AlreadyTerminal: fresh.Status.IsTerminal(),
		}, nil
	}

	n, err := s.tasks.CancelUnfinishedSubTasks(ctx, task.ID)
	if err != nil {
		slog.Error("cancel unfinished subtasks failed", "main_task_id", task.ID, "err", err)
		return nil, err
	}
	slog.Info("campaign cancelled", "main_task_id", task.ID, "cancelled_subs", n)

	fresh, _ := s.Get(ctx, id)
	if fresh != nil {
		s.fireWebhook(fresh)
	}
	return &CancelResult{
		TaskID:        task.ID,
		Status:        domain.TaskStatusCancelled,
		CancelledSubs: n,
	}, nil
}

// Pause 暂停任务：停止认领与新投递，已入队消息在 Pusher 侧暂缓
func (s *Service) Pause(ctx context.Context, id uint64) (*PauseResult, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.Status.IsPausable() {
		return nil, errcode.InvalidState
	}
	ok, err := s.tasks.PauseMainTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errcode.InvalidState
	}
	slog.Info("campaign paused", "main_task_id", task.ID)
	return &PauseResult{TaskID: task.ID, Status: domain.TaskStatusPaused}, nil
}

// Resume 恢复暂停任务
func (s *Service) Resume(ctx context.Context, id uint64) (*ResumeResult, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.Status.IsResumable() {
		return nil, errcode.InvalidState
	}
	hasSubs := task.SubTaskTotal > 0
	ok, err := s.tasks.ResumeMainTask(ctx, task.ID, hasSubs)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errcode.InvalidState
	}
	status := domain.TaskStatusPending
	if hasSubs {
		status = domain.TaskStatusRunning
	}
	slog.Info("campaign resumed", "main_task_id", task.ID, "status", status)
	return &ResumeResult{TaskID: task.ID, Status: status}, nil
}

// Retry 失败重推：重置失败子任务 + 为渠道失败用户新建子任务
func (s *Service) Retry(ctx context.Context, id uint64) (*RetryResult, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !task.Status.IsRetryable() {
		return nil, errcode.InvalidState
	}

	failedSubs, err := s.tasks.ListSubTasksByStatus(ctx, task.ID, domain.TaskStatusFailed)
	if err != nil {
		return nil, err
	}
	inFailedSubs := map[string]struct{}{}
	for _, st := range failedSubs {
		for _, uid := range parseSubUserIDs(st.UserIDs) {
			inFailedSubs[uid] = struct{}{}
		}
	}

	var pushFailed []string
	if s.pushRepo != nil {
		pushFailed, err = s.pushRepo.ListFailedUserIDs(ctx, task.ID)
		if err != nil {
			return nil, err
		}
	}

	extraUsers := make([]string, 0)
	for _, uid := range pushFailed {
		if _, ok := inFailedSubs[uid]; !ok {
			extraUsers = append(extraUsers, uid)
		}
	}

	resetN, err := s.tasks.ResetFailedSubTasks(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	newSubs := 0
	if len(extraUsers) > 0 {
		shards, err := s.buildRetryShards(ctx, task.ID, extraUsers)
		if err != nil {
			return nil, err
		}
		if err := s.tasks.CreateSubTasks(ctx, shards); err != nil {
			return nil, err
		}
		newSubs = len(shards)
	}

	retryUsers := len(inFailedSubs) + len(extraUsers)
	if resetN == 0 && newSubs == 0 {
		return nil, errcode.NothingToRetry
	}

	// 清除 dedup，允许失败用户强制重发
	if s.cache != nil {
		chs := task.ChannelList()
		if len(chs) == 0 && task.Channel != "" {
			chs = []domain.ChannelType{task.Channel}
		}
		for uid := range inFailedSubs {
			for _, ch := range chs {
				_ = s.cache.ClearDelivered(ctx, task.ID, uid, ch)
			}
		}
		for _, uid := range extraUsers {
			for _, ch := range chs {
				_ = s.cache.ClearDelivered(ctx, task.ID, uid, ch)
			}
		}
	}

	ok, err := s.tasks.ReopenMainTask(ctx, task.ID, newSubs)
	if err != nil {
		return nil, err
	}
	if !ok {
		// 不抹零 fail；仅同步子任务总量等
		if err := s.tasks.SyncMainCounters(ctx, task.ID, task.SuccessCount, task.FailCount, task.SubTaskDone, task.SubTaskTotal+newSubs); err != nil {
			return nil, err
		}
	}

	if err := s.realignCounters(ctx, task.ID); err != nil {
		slog.Warn("realign counters after retry", "id", task.ID, "err", err)
	}

	fresh, _ := s.Get(ctx, id)
	status := domain.TaskStatusRunning
	if fresh != nil {
		status = fresh.Status
	}

	slog.Info("campaign retry", "main_task_id", task.ID, "reset_subs", resetN, "new_subs", newSubs, "users", retryUsers)
	return &RetryResult{
		TaskID:         task.ID,
		Status:         status,
		ResetSubs:      resetN,
		NewSubs:        newSubs,
		RetryUserCount: retryUsers,
	}, nil
}

func (s *Service) buildRetryShards(ctx context.Context, mainTaskID uint64, userIDs []string) ([]domain.SubTask, error) {
	maxIdx, err := s.tasks.MaxShardIndex(ctx, mainTaskID)
	if err != nil {
		return nil, err
	}
	shard := maxIdx + 1
	var out []domain.SubTask
	for i := 0; i < len(userIDs); i += s.batchSize {
		end := i + s.batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		batch := userIDs[i:end]
		raw, _ := json.Marshal(map[string]any{"user_ids": batch})
		out = append(out, domain.SubTask{
			MainTaskID: mainTaskID,
			ShardIndex: shard,
			UserIDs:    string(raw),
			TotalCount: len(batch),
			Status:     domain.TaskStatusPending,
		})
		shard++
	}
	return out, nil
}

func parseSubUserIDs(raw string) []string {
	var payload struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return payload.UserIDs
}

func (s *Service) realignCounters(ctx context.Context, mainTaskID uint64) error {
	summaries, err := s.tasks.SummarizeSubTasks(ctx, mainTaskID)
	if err != nil {
		return err
	}
	var pipeSucc, pipeFail int64
	var done, total int
	for _, sm := range summaries {
		total += sm.SubCount
		switch sm.Status {
		case domain.TaskStatusSuccess:
			done += sm.SubCount
			pipeSucc += sm.UserSuccess
		case domain.TaskStatusFailed:
			done += sm.SubCount
			pipeFail += sm.UserFail
		case domain.TaskStatusCancelled, domain.TaskStatusPartial:
			done += sm.SubCount
			pipeSucc += sm.UserSuccess
			pipeFail += sm.UserFail
		}
	}
	userSucc, userFail := pipeSucc, pipeFail
	if s.pushRepo != nil {
		oc, err := s.pushRepo.CountUserOutcomes(ctx, mainTaskID)
		if err != nil {
			return err
		}
		if oc.HasRecords {
			userSucc = oc.SuccessUsers
			userFail = oc.FailUsers
		}
	}
	if err := s.tasks.SyncMainCounters(ctx, mainTaskID, userSucc, userFail, done, total); err != nil {
		return err
	}
	if s.cache != nil {
		// Redis 聚合仍按流水线（入队）口径，供后续子任务终态判定
		return s.cache.SetSubDone(ctx, mainTaskID, pipeSucc, pipeFail, int64(done))
	}
	return nil
}

func (s *Service) fireWebhook(task *domain.MainTask) {
	if s.notifier == nil || !task.Status.IsTerminal() {
		return
	}
	url := s.notifier.ResolveURL(task.WebhookURL)
	if url == "" {
		return
	}
	event := domain.WebhookEvent{
		Event:        "task.finished",
		TaskID:       task.ID,
		BizID:        task.BizID,
		BizScene:     task.BizScene,
		Title:        task.Title,
		Channel:      task.Channel,
		Status:       task.Status,
		TotalCount:   task.TotalCount,
		SuccessCount: task.SuccessCount,
		FailCount:    task.FailCount,
		SubTaskTotal: task.SubTaskTotal,
		SubTaskDone:  task.SubTaskDone,
		FinishedAt:   task.FinishedAt,
		Timestamp:    time.Now(),
	}
	s.notifier.NotifyAsync(url, event)
}

// NotifyFinished 供调度聚合器调用
func (s *Service) NotifyFinished(task *domain.MainTask) {
	s.fireWebhook(task)
}
