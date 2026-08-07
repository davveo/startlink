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
	SchemaErrors     []string               `json:"schema_errors,omitempty"`
	EstimatedSeconds float64                `json:"estimated_seconds,omitempty"`
	CapacityRisk     []string               `json:"capacity_risk,omitempty"`
	CostHint         string                 `json:"cost_hint,omitempty"`
	Warnings         []string               `json:"warnings,omitempty"`
}

type DryRunResult struct {
	RenderedTitle    string                   `json:"rendered_title,omitempty"`
	RenderedContent  string                   `json:"rendered_content"`
	MissingVars      []string                 `json:"missing_vars,omitempty"`
	SchemaErrors     []string                 `json:"schema_errors,omitempty"`
	Channels         []domain.ChannelType     `json:"channels,omitempty"`
	ChannelMode      domain.ChannelMode       `json:"channel_mode,omitempty"`
	MissingVarPolicy domain.MissingVarPolicy  `json:"missing_var_policy,omitempty"`
	ByChannel        map[string]ChannelRender `json:"by_channel,omitempty"`
	Sent             bool                     `json:"sent"`
	SendResults      []domain.SendResult      `json:"send_results,omitempty"`
	TestRecordIDs    []uint64                 `json:"test_record_ids,omitempty"`
}

type ChannelRender struct {
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
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

	var tpl *domain.Template
	if s.templates != nil && in.TemplateID != "" {
		t, err := s.templates.GetByCode(ctx, in.TemplateID)
		if err == nil && t.Status.Usable() {
			out.TemplateOK = true
			out.TemplateCode = t.Code
			tpl = t
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

	if tpl != nil && len(tpl.VarSchema) > 0 {
		sampleVars := map[string]string{}
		if len(out.Estimate.Sample) > 0 && out.Estimate.Sample[0].Vars != nil {
			sampleVars = out.Estimate.Sample[0].Vars
		}
		_, schemaErrs := domain.ValidateVarsAgainstSchema(tpl.VarSchema, sampleVars)
		out.SchemaErrors = schemaErrs
		if len(schemaErrs) > 0 {
			out.Warnings = append(out.Warnings, "var_schema: "+strings.Join(schemaErrs, "; "))
		}
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
	vars, schemaErrs := domain.ValidateVarsAgainstSchema(tpl.VarSchema, vars)
	if len(schemaErrs) > 0 && (in.MissingVarPolicy.Normalize() == domain.MissingVarError || tpl.MissingVarPolicy.Normalize() == domain.MissingVarError) {
		return nil, errcode.New(40001, "var_schema: "+strings.Join(schemaErrs, "; "))
	}

	policy := tpl.MissingVarPolicy.Normalize()
	if in.MissingVarPolicy != "" {
		if !in.MissingVarPolicy.Valid() {
			return nil, errcode.InvalidParam
		}
		policy = in.MissingVarPolicy.Normalize()
	}
	defaults := domain.DefaultsFromSchema(tpl.VarSchema)

	body, contents := domain.ResolveLocaleContent(tpl.Body, tpl.Contents, tpl.DefaultLocale, tpl.Locales, in.Locale)

	chs := in.Channels
	if len(chs) == 0 && in.Channel != "" {
		chs = []domain.ChannelType{in.Channel}
	}
	mode := in.ChannelMode.Normalize()
	if len(chs) <= 1 {
		mode = domain.ChannelModeSingle
	}

	primary := in.Channel
	if primary == "" && len(chs) > 0 {
		primary = chs[0]
	}
	titleSrc, bodySrc, _ := domain.ResolveChannelContent(primary, in.Title, body, contents)
	title, err := push.RenderTemplateWithPolicy(titleSrc, vars, policy, defaults)
	if err != nil {
		return nil, errcode.New(40001, err.Error())
	}
	content, err := push.RenderTemplateWithPolicy(bodySrc, vars, policy, defaults)
	if err != nil {
		return nil, errcode.New(40001, err.Error())
	}

	missing := push.MissingVars(bodySrc, vars)
	byCh := map[string]ChannelRender{}
	for _, ch := range chs {
		t, b, _ := domain.ResolveChannelContent(ch, in.Title, body, contents)
		rt, e1 := push.RenderTemplateWithPolicy(t, vars, policy, defaults)
		rc, e2 := push.RenderTemplateWithPolicy(b, vars, policy, defaults)
		if e1 != nil {
			return nil, errcode.New(40001, e1.Error())
		}
		if e2 != nil {
			return nil, errcode.New(40001, e2.Error())
		}
		byCh[string(ch)] = ChannelRender{Title: rt, Content: rc}
	}

	out := &DryRunResult{
		RenderedTitle:    title,
		RenderedContent:  content,
		MissingVars:      missing,
		SchemaErrors:     schemaErrs,
		Channels:         chs,
		ChannelMode:      mode,
		MissingVarPolicy: policy,
		ByChannel:        byCh,
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
		cr := byCh[string(ch)]
		sendTitle, sendContent := cr.Title, cr.Content
		if sendContent == "" {
			sendTitle, sendContent = title, content
		}
		sr, err := sender.Send(ctx, domain.SendRequest{
			MsgID:   fmt.Sprintf("dryrun-%d-%s", time.Now().UnixNano(), ch),
			UserID:  userID,
			Channel: ch,
			Title:   sendTitle,
			Content: sendContent,
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
			Content:    sendContent,
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
	if s.notifier != nil {
		if err := s.notifier.ValidateTarget(task.WebhookURL); err != nil {
			return nil, errcode.New(40001, err.Error())
		}
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
	if s.notifier != nil {
		if err := s.notifier.ValidateTarget(in.WebhookURL); err != nil {
			return nil, errcode.New(40001, err.Error())
		}
	}
	in.ApplyDefaultChannel(s.defaultChannel)
	primary, chList, mode, err := in.NormalizeChannels()
	if err != nil {
		return nil, errcode.InvalidParam
	}
	var tplBody, tplCode string
	var contentsCol, localesCol *string
	policy := task.MissingVarPolicy
	defLocale := task.DefaultLocale
	if in.TemplateID != "" && s.templates != nil {
		tpl, err := s.templates.GetByCode(ctx, in.TemplateID)
		if err != nil {
			return nil, errcode.NotFound
		}
		if !tpl.Status.Usable() {
			return nil, errcode.TemplateNotUsable
		}
		tpl.HydrateJSON()
		tplBody, tplCode = tpl.Body, tpl.Code
		contentsCol = domain.MarshalJSONColumn(tpl.Contents, false)
		localesCol = domain.MarshalJSONColumn(tpl.Locales, false)
		policy = tpl.MissingVarPolicy.Normalize()
		defLocale = tpl.DefaultLocale
	} else {
		tplBody, tplCode = task.TemplateBody, task.TemplateID
		contentsCol = task.TemplateContents
		localesCol = task.TemplateLocales
	}
	extra, _ := json.Marshal(in.AudienceExtra)
	payload, _ := json.Marshal(in.Payload)
	chsJSON, _ := json.Marshal(chList)
	windowsJSON, _ := json.Marshal(in.SendWindows)
	if string(windowsJSON) == "null" {
		windowsJSON = []byte("[]")
	}
	fields := map[string]any{
		"title":                      firstNonEmpty(in.Title, task.Title),
		"biz_scene":                  firstNonEmpty(in.BizScene, task.BizScene),
		"channel":                    primary,
		"channels":                   string(chsJSON),
		"channel_mode":               mode,
		"template_id":                tplCode,
		"template_body":              tplBody,
		"template_contents":          contentsCol,
		"missing_var_policy":         policy,
		"default_locale":             defLocale,
		"template_locales":           localesCol,
		"audience_ref":               firstNonEmpty(in.AudienceRef, task.AudienceRef),
		"audience_extra":             string(extra),
		"payload":                    string(payload),
		"webhook_url":                in.WebhookURL,
		"send_windows":               string(windowsJSON),
		"pace_qps":                   in.PaceQPS,
		"expire_at":                  in.ExpireAt,
		"experiment_id":              in.ExperimentID,
		"experiment_salt":            in.ExperimentSalt,
		"experiment_control_percent": in.ExperimentControlPercent,
		"max_fallback":               in.MaxFallback,
		"channel_routes":             domain.MarshalChannelRoutes(in.ChannelRoutes),
		"channel_costs":              domain.MarshalChannelCosts(in.ChannelCosts),
		"scheduled_at":               in.ScheduledAt,
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
		BizID:                    in.BizID,
		BizScene:                 src.BizScene,
		Priority:                 src.Priority,
		Title:                    title,
		Channel:                  src.Channel,
		Channels:                 src.Channels,
		ChannelMode:              src.ChannelMode,
		TemplateID:               src.TemplateID,
		TemplateBody:             src.TemplateBody,
		TemplateContents:         src.TemplateContents,
		MissingVarPolicy:         src.MissingVarPolicy,
		DefaultLocale:            src.DefaultLocale,
		TemplateLocales:          src.TemplateLocales,
		AudienceRef:              src.AudienceRef,
		AudienceExtra:            src.AudienceExtra,
		Payload:                  src.Payload,
		WebhookURL:               src.WebhookURL,
		SendWindowsJSON:          src.SendWindowsJSON,
		PaceQPS:                  src.PaceQPS,
		ExpireAt:                 src.ExpireAt,
		ExperimentID:             src.ExperimentID,
		ExperimentSalt:           src.ExperimentSalt,
		ExperimentControlPercent: src.ExperimentControlPercent,
		MaxFallback:              src.MaxFallback,
		ChannelRoutesJSON:        src.ChannelRoutesJSON,
		ChannelCostsJSON:         src.ChannelCostsJSON,
		CreatedBy:                firstNonEmpty(in.CreatedBy, src.CreatedBy),
		CopiedFromID:             &copied,
		Status:                   status,
		ScheduledAt:              src.ScheduledAt,
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

func (s *Service) ExperimentMetrics(ctx context.Context, id uint64) (*port.ExperimentMetrics, error) {
	if _, err := s.tasks.GetMainTask(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	m, err := s.pushRepo.AggregateExperiment(ctx, id)
	if err != nil {
		return nil, err
	}
	return &m, nil
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
	select {
	case s.exportSem <- struct{}{}:
	default:
		return nil, errcode.New(42901, "too many export jobs")
	}
	release := true
	defer func() {
		if release {
			<-s.exportSem
		}
	}()
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
	emptyFilter := "{}"
	job.FilterJSON = &emptyFilter
	if err := s.pushRepo.CreateExportJob(ctx, job); err != nil {
		return nil, err
	}
	release = false
	go func() {
		defer func() { <-s.exportSem }()
		s.runExport(job.ID)
	}()
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	job, err := s.pushRepo.GetExportJob(ctx, jobID)
	if err != nil {
		return
	}
	_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{"status": domain.ExportStatusRunning})
	dir := s.exportDir
	if dir == "" {
		dir = "data/exports"
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
			"status": domain.ExportStatusFailed, "error_msg": err.Error(),
		})
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("export_%d_%s_%d.csv", job.MainTaskID, job.Kind, job.ID))
	f, err := os.Create(path)
	if err != nil {
		_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
			"status": domain.ExportStatusFailed, "error_msg": err.Error(),
		})
		return
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	// Excel 打开 UTF-8 CSV 需要 BOM，否则中文表头/内容乱码
	_, _ = f.WriteString("\uFEFF")
	w := csv.NewWriter(f)
	var rows int64
	switch job.Kind {
	case domain.ExportKindFailures:
		_ = w.Write([]string{"渠道", "供应商", "错误信息", "次数"})
		items, err := s.pushRepo.AggregateFailures(ctx, job.MainTaskID)
		if err != nil {
			_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
				"status": domain.ExportStatusFailed, "error_msg": err.Error(),
			})
			return
		}
		for _, it := range items {
			_ = w.Write([]string{
				domain.ChannelLabelZH(it.Channel),
				exportText(it.Provider),
				exportText(it.ErrorMsg),
				strconv.FormatInt(it.Count, 10),
			})
			rows++
		}
	default:
		_ = w.Write([]string{"流水ID", "用户ID", "渠道", "状态", "供应商", "供应商消息ID", "错误信息", "发送时间", "创建时间"})
		err := s.pushRepo.IterPushRecords(ctx, job.MainTaskID, func(rec domain.PushRecord) error {
			sent := ""
			if rec.SentAt != nil {
				sent = rec.SentAt.Format("2006-01-02 15:04:05")
			}
			_ = w.Write([]string{
				strconv.FormatUint(rec.ID, 10),
				exportText(rec.UserID),
				domain.ChannelLabelZH(rec.Channel),
				domain.PushStatusLabelZH(rec.Status),
				exportText(rec.Provider),
				exportText(rec.ProviderIDValue()),
				exportText(rec.ErrorMsg),
				sent,
				rec.CreatedAt.Format("2006-01-02 15:04:05"),
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
	if err := w.Error(); err != nil {
		_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
			"status": domain.ExportStatusFailed, "error_msg": err.Error(),
		})
		return
	}
	if err := f.Sync(); err != nil {
		_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
			"status": domain.ExportStatusFailed, "error_msg": err.Error(),
		})
		return
	}
	if err := f.Close(); err != nil {
		_ = s.pushRepo.UpdateExportJob(ctx, jobID, map[string]any{
			"status": domain.ExportStatusFailed, "error_msg": err.Error(),
		})
		return
	}
	closed = true
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
	// Excel 识别 UTF-8 中文表头需要 BOM
	b.WriteString("\uFEFF")
	w := csv.NewWriter(&b)
	filename := fmt.Sprintf("campaign_%d_%s.csv", id, kind)
	switch strings.ToLower(kind) {
	case "failures":
		_ = w.Write([]string{"渠道", "供应商", "错误信息", "次数"})
		items, err := s.pushRepo.AggregateFailures(ctx, id)
		if err != nil {
			return nil, "", err
		}
		for _, it := range items {
			_ = w.Write([]string{
				domain.ChannelLabelZH(it.Channel),
				exportText(it.Provider),
				exportText(it.ErrorMsg),
				strconv.FormatInt(it.Count, 10),
			})
		}
	default:
		_ = w.Write([]string{"流水ID", "用户ID", "渠道", "状态", "供应商", "错误信息"})
		const maxSyncRows = 10_000
		rows := 0
		err := s.pushRepo.IterPushRecords(ctx, id, func(rec domain.PushRecord) error {
			rows++
			if rows > maxSyncRows {
				return errcode.New(40001, "result too large; use async export")
			}
			return w.Write([]string{
				strconv.FormatUint(rec.ID, 10),
				exportText(rec.UserID),
				domain.ChannelLabelZH(rec.Channel),
				domain.PushStatusLabelZH(rec.Status),
				exportText(rec.Provider),
				exportText(rec.ErrorMsg),
			})
		})
		if err != nil {
			return nil, "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", err
	}
	return []byte(b.String()), filename, nil
}

func exportText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + s
	}
	return s
}
