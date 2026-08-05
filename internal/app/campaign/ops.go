package campaign

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/starlink/push/internal/adapter/audience"
	"github.com/starlink/push/internal/adapter/channel"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/internal/push"
	"github.com/starlink/push/pkg/errcode"
	"gorm.io/gorm"
)

// AudienceResolver 人群试算 / 预检
type AudienceResolver interface {
	Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error)
}

type BatchItemResult struct {
	ID      uint64 `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type BatchResult struct {
	Action  string            `json:"action"`
	Total   int               `json:"total"`
	Success int               `json:"success"`
	Failed  int               `json:"failed"`
	Items   []BatchItemResult `json:"items"`
}

type AudienceEstimateResult struct {
	RawCount       int64               `json:"raw_count"`
	AfterFilter    int64               `json:"after_filter"`
	AfterAB        int64               `json:"after_ab"`
	ReachableCount int64               `json:"reachable_count"`
	SkippedNoChan  int64               `json:"skipped_no_channel"`
	PagesScanned   int                 `json:"pages_scanned"`
	HasMore        bool                `json:"has_more"`
	TotalHint      int64               `json:"total_hint,omitempty"`
	Sample         []domain.TargetUser `json:"sample,omitempty"`
	ABPercent      int                 `json:"ab_percent,omitempty"`
}

type PreflightResult struct {
	Estimate         AudienceEstimateResult `json:"estimate"`
	Priority         domain.Priority        `json:"priority"`
	Channels         []domain.ChannelType   `json:"channels"`
	ChannelMode      domain.ChannelMode     `json:"channel_mode"`
	TemplateOK       bool                   `json:"template_ok"`
	TemplateCode     string                 `json:"template_code,omitempty"`
	EstimatedSeconds float64                `json:"estimated_seconds,omitempty"`
	CapacityRisk     []string               `json:"capacity_risk,omitempty"`
	CostHint         string                 `json:"cost_hint,omitempty"`
	Warnings         []string               `json:"warnings,omitempty"`
}

type DryRunResult struct {
	RenderedTitle   string               `json:"rendered_title,omitempty"`
	RenderedContent string               `json:"rendered_content"`
	MissingVars     []string             `json:"missing_vars,omitempty"`
	Channels        []domain.ChannelType `json:"channels,omitempty"`
	ChannelMode     domain.ChannelMode   `json:"channel_mode,omitempty"`
	Sent            bool                 `json:"sent"`
	SendResults     []domain.SendResult  `json:"send_results,omitempty"`
	TestRecordIDs   []uint64             `json:"test_record_ids,omitempty"`
}

type FunnelView struct {
	TaskID                 uint64                `json:"task_id"`
	AudienceRawCount       int64                 `json:"audience_raw_count"`
	AudienceFilteredCount  int64                 `json:"audience_filtered_count"`
	AudienceReachableCount int64                 `json:"audience_reachable_count"`
	EnqueuedUsers          int64                 `json:"enqueued_users"`
	Pipeline               port.FunnelCounts     `json:"pipeline"`
	UserOutcomes           port.UserPushOutcomes `json:"user_outcomes"`
}

type FailureAnalysisResult struct {
	TaskID uint64               `json:"task_id"`
	Items  []port.FailureAggRow `json:"items"`
}

type RecordListResult struct {
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Items    []domain.PushRecord `json:"items"`
}

func (s *Service) SetAudience(a AudienceResolver)  { s.audience = a }
func (s *Service) SetChannels(c *channel.Registry) { s.channels = c }
func (s *Service) SetExportDir(dir string) {
	if dir == "" {
		dir = "data/exports"
	}
	s.exportDir = dir
}

// ---- 批量操作 ----

func (s *Service) BatchAction(ctx context.Context, action string, ids []uint64) (*BatchResult, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	out := &BatchResult{Action: action, Total: len(ids), Items: make([]BatchItemResult, 0, len(ids))}
	for _, id := range ids {
		item := BatchItemResult{ID: id}
		var err error
		switch action {
		case "pause":
			_, err = s.Pause(ctx, id)
		case "resume":
			_, err = s.Resume(ctx, id)
		case "cancel":
			_, err = s.Cancel(ctx, id)
		case "retry":
			_, err = s.Retry(ctx, id)
		default:
			return nil, errcode.InvalidParam
		}
		if err != nil {
			item.OK = false
			item.Message = err.Error()
			out.Failed++
		} else {
			item.OK = true
			out.Success++
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// ---- 人群试算 / 预检 ----

func (s *Service) EstimateAudience(ctx context.Context, in domain.AudienceEstimateInput) (*AudienceEstimateResult, error) {
	if s.audience == nil {
		return nil, errcode.Internal
	}
	if in.BizScene == "" || in.AudienceRef == "" {
		return nil, errcode.InvalidParam
	}
	maxPages := in.MaxPages
	if maxPages <= 0 {
		maxPages = 5
	}
	if maxPages > 50 {
		maxPages = 50
	}
	sampleLimit := in.SampleLimit
	if sampleLimit <= 0 {
		sampleLimit = 20
	}
	if sampleLimit > 100 {
		sampleLimit = 100
	}

	taskChs := in.Channels
	if len(taskChs) == 0 && in.Channel != "" {
		taskChs = []domain.ChannelType{in.Channel}
	}

	abPercent := -1
	if in.AudienceExtra != nil {
		switch v := in.AudienceExtra["ab_sample_percent"].(type) {
		case float64:
			abPercent = int(v)
		case int:
			abPercent = v
		case json.Number:
			n, _ := v.Int64()
			abPercent = int(n)
		}
	}

	res := &AudienceEstimateResult{ABPercent: abPercent, Sample: make([]domain.TargetUser, 0, sampleLimit)}
	token := ""
	pageSize := s.batchSize
	if pageSize <= 0 {
		pageSize = 200
	}

	for page := 0; page < maxPages; page++ {
		pg, err := s.audience.Resolve(ctx, domain.AudienceQuery{
			AudienceRef: in.AudienceRef,
			BizScene:    in.BizScene,
			Extra:       in.AudienceExtra,
			PageToken:   token,
			PageSize:    pageSize,
		})
		if err != nil {
			return nil, err
		}
		res.PagesScanned++
		if pg.TotalHint > 0 {
			res.TotalHint = pg.TotalHint
		}
		raw := int64(len(pg.Users))
		res.RawCount += raw
		res.AfterFilter += raw // Filter 已在 Registry 内完成

		for _, u := range pg.Users {
			keep := true
			if abPercent >= 0 && !audience.SampleByPercent(u.UserID, abPercent) {
				keep = false
			} else {
				res.AfterAB++
			}
			if !keep {
				continue
			}
			chs := domain.IntersectChannels(taskChs, u.Channels)
			if len(taskChs) > 0 && len(chs) == 0 {
				res.SkippedNoChan++
				continue
			}
			res.ReachableCount++
			if len(res.Sample) < sampleLimit {
				res.Sample = append(res.Sample, u)
			}
		}

		res.HasMore = pg.HasMore
		if !pg.HasMore || pg.NextPageToken == "" || pg.NextPageToken == token {
			res.HasMore = false
			break
		}
		token = pg.NextPageToken
	}
	return res, nil
}

func (s *Service) Preflight(ctx context.Context, in domain.CreateCampaignInput) (*PreflightResult, error) {
	in.ApplyDefaultChannel(s.defaultChannel)
	primary, chList, mode, err := in.NormalizeChannels()
	if err != nil {
		return nil, errcode.InvalidParam
	}
	_ = primary
	out := &PreflightResult{
		Channels:     chList,
		ChannelMode:  mode,
		Priority:     domain.ResolvePriority(in.Priority, in.BizScene, s.highBizScenes),
		Warnings:     []string{},
		CapacityRisk: []string{},
	}

	if s.templates != nil && in.TemplateID != "" {
		tpl, err := s.templates.GetByCode(ctx, in.TemplateID)
		if err == nil && tpl.Status.Usable() {
			out.TemplateOK = true
			out.TemplateCode = tpl.Code
		} else {
			out.Warnings = append(out.Warnings, "template missing or not usable")
		}
	} else {
		out.Warnings = append(out.Warnings, "template_id empty")
	}

	est, err := s.EstimateAudience(ctx, domain.AudienceEstimateInput{
		BizScene:      in.BizScene,
		AudienceRef:   in.AudienceRef,
		AudienceExtra: in.AudienceExtra,
		Channels:      chList,
		MaxPages:      5,
		SampleLimit:   10,
	})
	if err != nil {
		out.Warnings = append(out.Warnings, "audience estimate failed: "+err.Error())
	} else {
		out.Estimate = *est
	}

	hint := out.Estimate.ReachableCount
	if hint <= 0 {
		hint = audienceSizeHint(in.AudienceExtra)
	}
	if s.limiter != nil && s.limiter.Enabled() && hint > 0 {
		var minAvail float64 = -1
		for _, ch := range chList {
			avail := s.limiter.AvailableQPS(ch, out.Priority)
			if avail <= 0 {
				continue
			}
			if minAvail < 0 || avail < minAvail {
				minAvail = avail
			}
			minutes := in.ExpectedFinishMinutes
			if minutes <= 0 {
				minutes = s.limiter.TargetFinishMinutes(ch)
			}
			if minutes <= 0 {
				minutes = 60
			}
			demand := float64(hint) / (float64(minutes) * 60)
			if demand > avail && s.limiter.AdmissionMode(ch) == "enforce" {
				out.CapacityRisk = append(out.CapacityRisk,
					fmt.Sprintf("%s demand_qps=%.2f > available=%.2f", ch, demand, avail))
			}
		}
		if minAvail > 0 {
			out.EstimatedSeconds = float64(hint) / minAvail
		}
	}
	// 粗略费用提示（演示）
	out.CostHint = fmt.Sprintf("approx %d reachable users × channels=%d", hint, len(chList))
	return out, nil
}

// ---- Dry-run / 测试发送 ----

func (s *Service) DryRun(ctx context.Context, in domain.DryRunInput) (*DryRunResult, error) {
	if s.templates == nil || in.TemplateID == "" {
		return nil, errcode.InvalidParam
	}
	tpl, err := s.templates.GetByCode(ctx, in.TemplateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	vars := in.Vars
	if vars == nil {
		vars = map[string]string{}
	}
	missing := missingTemplateVars(tpl.Body, vars)
	content := push.RenderTemplate(tpl.Body, vars)
	title := push.RenderTemplate(in.Title, vars)

	chs := in.Channels
	if len(chs) == 0 && in.Channel != "" {
		chs = []domain.ChannelType{in.Channel}
	}
	mode := in.ChannelMode.Normalize()
	if len(chs) <= 1 {
		mode = domain.ChannelModeSingle
	}

	out := &DryRunResult{
		RenderedTitle:   title,
		RenderedContent: content,
		MissingVars:     missing,
		Channels:        chs,
		ChannelMode:     mode,
	}
	if !in.Send {
		return out, nil
	}
	if s.channels == nil || len(chs) == 0 {
		return nil, errcode.InvalidParam
	}
	userID := in.UserID
	if userID == "" {
		userID = "test_user"
	}
	for _, ch := range chs {
		sender, err := s.channels.Get(ch)
		if err != nil || sender == nil {
			out.SendResults = append(out.SendResults, domain.SendResult{
				Success: false, ErrorMsg: "channel not registered: " + string(ch),
			})
			continue
		}
		sr, err := sender.Send(ctx, domain.SendRequest{
			MsgID:   fmt.Sprintf("dryrun-%d-%s", time.Now().UnixNano(), ch),
			UserID:  userID,
			Channel: ch,
			Title:   title,
			Content: content,
			Vars:    vars,
			Extra:   map[string]any{"dry_run": true},
		})
		if err != nil {
			out.SendResults = append(out.SendResults, domain.SendResult{Success: false, ErrorMsg: err.Error()})
			continue
		}
		out.SendResults = append(out.SendResults, *sr)
		st := domain.PushStatusSent
		if !sr.Success {
			st = domain.PushStatusFailed
		}
		rec := &domain.PushRecord{
			MainTaskID: 0,
			SubTaskID:  0,
			UserID:     userID,
			Channel:    ch,
			Content:    content,
			Status:     st,
			Provider:   sr.Provider,
			ErrorMsg:   sr.ErrorMsg,
			IsTest:     true,
		}
		if sr.ProviderID != "" {
			pid := sr.ProviderID
			rec.ProviderID = &pid
		}
		if err := s.pushRepo.CreateTestRecord(ctx, rec); err == nil {
			out.TestRecordIDs = append(out.TestRecordIDs, rec.ID)
		}
	}
	out.Sent = true
	return out, nil
}

func missingTemplateVars(body string, vars map[string]string) []string {
	re := push.VarPattern()
	seen := map[string]struct{}{}
	var missing []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		k := m[1]
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		if _, ok := vars[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

// ---- 草稿 / 复制 / 发布 / 更新 ----

func (s *Service) Publish(ctx context.Context, id uint64) (*CreateResult, error) {
	task, err := s.tasks.GetMainTask(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	if task.Status != domain.TaskStatusDraft {
		return nil, errcode.InvalidParam
	}
	if err := s.tasks.UpdateMainTaskFields(ctx, id, map[string]any{
		"status": domain.TaskStatusPending,
	}); err != nil {
		return nil, err
	}
	return &CreateResult{TaskID: id, BizID: task.BizID, Status: domain.TaskStatusPending}, nil
}

func (s *Service) UpdateDraft(ctx context.Context, id uint64, in domain.CreateCampaignInput) (*CreateResult, error) {
	task, err := s.tasks.GetMainTask(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	if task.Status != domain.TaskStatusDraft {
		return nil, errcode.InvalidParam
	}
	in.ApplyDefaultChannel(s.defaultChannel)
	primary, chList, mode, err := in.NormalizeChannels()
	if err != nil {
		return nil, errcode.InvalidParam
	}
	var tplBody, tplCode string
	if in.TemplateID != "" && s.templates != nil {
		tpl, err := s.templates.GetByCode(ctx, in.TemplateID)
		if err != nil {
			return nil, errcode.NotFound
		}
		if !tpl.Status.Usable() {
			return nil, errcode.TemplateNotUsable
		}
		tplBody, tplCode = tpl.Body, tpl.Code
	} else {
		tplBody, tplCode = task.TemplateBody, task.TemplateID
	}
	extra, _ := json.Marshal(in.AudienceExtra)
	payload, _ := json.Marshal(in.Payload)
	chsJSON, _ := json.Marshal(chList)
	windowsJSON, _ := json.Marshal(in.SendWindows)
	fields := map[string]any{
		"title":          firstNonEmpty(in.Title, task.Title),
		"biz_scene":      firstNonEmpty(in.BizScene, task.BizScene),
		"channel":        primary,
		"channels":       string(chsJSON),
		"channel_mode":   mode,
		"template_id":    tplCode,
		"template_body":  tplBody,
		"audience_ref":   firstNonEmpty(in.AudienceRef, task.AudienceRef),
		"audience_extra": string(extra),
		"payload":        string(payload),
		"webhook_url":    in.WebhookURL,
		"send_windows":   string(windowsJSON),
		"pace_qps":       in.PaceQPS,
		"scheduled_at":   in.ScheduledAt,
	}
	if in.CreatedBy != "" {
		fields["created_by"] = in.CreatedBy
	}
	if in.Priority.Valid() {
		fields["priority"] = domain.ResolvePriority(in.Priority, in.BizScene, s.highBizScenes)
	}
	if err := s.tasks.UpdateMainTaskFields(ctx, id, fields); err != nil {
		return nil, err
	}
	return &CreateResult{TaskID: id, BizID: task.BizID, Status: domain.TaskStatusDraft}, nil
}

func (s *Service) Copy(ctx context.Context, id uint64, in domain.CopyCampaignInput) (*CreateResult, error) {
	src, err := s.tasks.GetMainTask(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	if in.BizID == "" {
		return nil, errcode.InvalidParam
	}
	asDraft := true
	if in.AsDraft != nil {
		asDraft = *in.AsDraft
	}
	status := domain.TaskStatusPending
	if asDraft {
		status = domain.TaskStatusDraft
	}
	title := in.Title
	if title == "" {
		title = src.Title + " (copy)"
	}
	copied := src.ID
	task := &domain.MainTask{
		BizID:           in.BizID,
		BizScene:        src.BizScene,
		Priority:        src.Priority,
		Title:           title,
		Channel:         src.Channel,
		Channels:        src.Channels,
		ChannelMode:     src.ChannelMode,
		TemplateID:      src.TemplateID,
		TemplateBody:    src.TemplateBody,
		AudienceRef:     src.AudienceRef,
		AudienceExtra:   src.AudienceExtra,
		Payload:         src.Payload,
		WebhookURL:      src.WebhookURL,
		SendWindowsJSON: src.SendWindowsJSON,
		PaceQPS:         src.PaceQPS,
		CreatedBy:       firstNonEmpty(in.CreatedBy, src.CreatedBy),
		CopiedFromID:    &copied,
		Status:          status,
		ScheduledAt:     src.ScheduledAt,
	}
	if err := s.tasks.CreateMainTask(ctx, task); err != nil {
		if exist, e2 := s.tasks.GetMainTaskByBizID(ctx, in.BizID); e2 == nil && exist != nil {
			return nil, errcode.Conflict
		}
		return nil, err
	}
	return &CreateResult{TaskID: task.ID, BizID: task.BizID, Status: task.Status}, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// ---- 漏斗 / 失败分析 / 流水 ----

func (s *Service) Funnel(ctx context.Context, id uint64) (*FunnelView, error) {
	task, err := s.tasks.GetMainTask(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	pipe, err := s.pushRepo.CountStatusFunnel(ctx, id)
	if err != nil {
		return nil, err
	}
	outcomes, err := s.pushRepo.CountUserOutcomes(ctx, id)
	if err != nil {
		return nil, err
	}
	enqueued := pipe.Queued + pipe.Sending + pipe.Sent + pipe.Delivered + pipe.Clicked +
		pipe.Failed + pipe.Suppressed + pipe.Unreachable + pipe.Cancelled + pipe.Expired + pipe.QuotaRejected
	return &FunnelView{
		TaskID:                 id,
		AudienceRawCount:       task.AudienceRawCount,
		AudienceFilteredCount:  task.AudienceFilteredCount,
		AudienceReachableCount: task.AudienceReachableCount,
		EnqueuedUsers:          enqueued,
		Pipeline:               pipe,
		UserOutcomes:           outcomes,
	}, nil
}

func (s *Service) FailureAnalysis(ctx context.Context, id uint64) (*FailureAnalysisResult, error) {
	if _, err := s.tasks.GetMainTask(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	items, err := s.pushRepo.AggregateFailures(ctx, id)
	if err != nil {
		return nil, err
	}
	return &FailureAnalysisResult{TaskID: id, Items: items}, nil
}

func (s *Service) ListRecords(ctx context.Context, id uint64, q domain.ListPushRecordQuery) (*RecordListResult, error) {
	if _, err := s.tasks.GetMainTask(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	list, total, err := s.pushRepo.ListPushRecords(ctx, id, q)
	if err != nil {
		return nil, err
	}
	page, size := q.Page, q.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	return &RecordListResult{Total: total, Page: page, PageSize: size, Items: list}, nil
}

func (s *Service) CreateExport(ctx context.Context, id uint64, kind, createdBy string) (*domain.ExportJob, error) {
	if _, err := s.tasks.GetMainTask(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = domain.ExportKindRecords
	}
	if kind != domain.ExportKindRecords && kind != domain.ExportKindFailures {
		return nil, errcode.InvalidParam
	}
	job := &domain.ExportJob{
		MainTaskID: id,
		Kind:       kind,
		Status:     domain.ExportStatusPending,
		CreatedBy:  createdBy,
	}
	if err := s.pushRepo.CreateExportJob(ctx, job); err != nil {
		return nil, err
	}
	go s.runExport(job.ID)
	return job, nil
}

func (s *Service) GetExport(ctx context.Context, jobID uint64) (*domain.ExportJob, error) {
	job, err := s.pushRepo.GetExportJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	return job, nil
}

func (s *Service) runExport(jobID uint64) {
	ctx := context.Background()
	job, err := s.pushRepo.GetExportJob(ctx, jobID)
	if err != nil {
		return
	}
	_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{"status": domain.ExportStatusRunning})
	dir := s.exportDir
	if dir == "" {
		dir = "data/exports"
	}
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fmt.Sprintf("export_%d_%s_%d.csv", job.MainTaskID, job.Kind, job.ID))
	f, err := os.Create(path)
	if err != nil {
		_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
			"status": domain.ExportStatusFailed, "error_msg": err.Error(),
		})
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	var rows int64
	switch job.Kind {
	case domain.ExportKindFailures:
		_ = w.Write([]string{"channel", "provider", "error_msg", "count"})
		items, err := s.pushRepo.AggregateFailures(ctx, job.MainTaskID)
		if err != nil {
			_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
				"status": domain.ExportStatusFailed, "error_msg": err.Error(),
			})
			return
		}
		for _, it := range items {
			_ = w.Write([]string{string(it.Channel), it.Provider, it.ErrorMsg, strconv.FormatInt(it.Count, 10)})
			rows++
		}
	default:
		_ = w.Write([]string{"id", "user_id", "channel", "status", "provider", "provider_id", "error_msg", "sent_at", "created_at"})
		err := s.pushRepo.IterPushRecords(ctx, job.MainTaskID, func(rec domain.PushRecord) error {
			sent := ""
			if rec.SentAt != nil {
				sent = rec.SentAt.Format(time.RFC3339)
			}
			_ = w.Write([]string{
				strconv.FormatUint(rec.ID, 10),
				rec.UserID,
				string(rec.Channel),
				string(rec.Status),
				rec.Provider,
				rec.ProviderIDValue(),
				rec.ErrorMsg,
				sent,
				rec.CreatedAt.Format(time.RFC3339),
			})
			rows++
			return nil
		})
		if err != nil {
			_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
				"status": domain.ExportStatusFailed, "error_msg": err.Error(),
			})
			return
		}
	}
	w.Flush()
	now := time.Now()
	_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
		"status":      domain.ExportStatusSuccess,
		"file_path":   path,
		"file_url":    "/api/v1/exports/" + strconv.FormatUint(jobID, 10) + "/download",
		"row_count":   rows,
		"finished_at": now,
	})
}

// SyncExportCSV 小结果集同步导出（返回 CSV 字节）
func (s *Service) SyncExportCSV(ctx context.Context, id uint64, kind string) ([]byte, string, error) {
	if _, err := s.tasks.GetMainTask(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", errcode.NotFound
		}
		return nil, "", err
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	filename := fmt.Sprintf("campaign_%d_%s.csv", id, kind)
	switch strings.ToLower(kind) {
	case "failures":
		_ = w.Write([]string{"channel", "provider", "error_msg", "count"})
		items, err := s.pushRepo.AggregateFailures(ctx, id)
		if err != nil {
			return nil, "", err
		}
		for _, it := range items {
			_ = w.Write([]string{string(it.Channel), it.Provider, it.ErrorMsg, strconv.FormatInt(it.Count, 10)})
		}
	default:
		_ = w.Write([]string{"id", "user_id", "channel", "status", "provider", "error_msg"})
		err := s.pushRepo.IterPushRecords(ctx, id, func(rec domain.PushRecord) error {
			return w.Write([]string{
				strconv.FormatUint(rec.ID, 10), rec.UserID, string(rec.Channel),
				string(rec.Status), rec.Provider, rec.ErrorMsg,
			})
		})
		if err != nil {
			return nil, "", err
		}
	}
	w.Flush()
	return []byte(b.String()), filename, nil
}
