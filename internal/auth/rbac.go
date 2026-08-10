package auth

import (
	"sort"
	"strings"
)

// 权限码约定：菜单 menu.*；写操作与审计动作对齐（campaign.* / template.* / …）。

const (
	PermMenuOverview      = "menu.overview"
	PermMenuTasks         = "menu.tasks"
	PermMenuTemplates     = "menu.templates"
	PermMenuNotifications = "menu.notifications"
	PermMenuAudit         = "menu.audit"
	PermMenuSettings      = "menu.settings"
	PermMenuSegments      = "menu.segments"
	PermMenuPreferences   = "menu.preferences"
	PermMenuSchedules     = "menu.schedules"
	PermMenuChannels      = "menu.channels"

	PermCampaignCreate    = "campaign.create"
	PermCampaignUpdate    = "campaign.update"
	PermCampaignPublish   = "campaign.publish"
	PermCampaignCancel    = "campaign.cancel"
	PermCampaignPause     = "campaign.pause"
	PermCampaignResume    = "campaign.resume"
	PermCampaignRetry     = "campaign.retry"
	PermCampaignCopy      = "campaign.copy"
	PermCampaignBatch     = "campaign.batch"
	PermCampaignExport    = "campaign.export"
	PermCampaignPreflight = "campaign.preflight"
	PermCampaignDryRun    = "campaign.dry_run"
	PermCampaignSimulate  = "campaign.simulate"
	PermAudienceEstimate  = "audience.estimate"

	PermSegmentManage     = "segment.manage"
	PermSuppressionManage = "suppression.manage"
	PermPreferenceView    = "preference.view"
	PermPreferenceManage  = "preference.manage"
	PermScheduleManage    = "schedule.manage"
	PermChannelManage     = "channel.manage"

	PermTemplateCreate   = "template.create"
	PermTemplateEdit     = "template.edit"
	PermTemplateDelete   = "template.delete"
	PermTemplateSubmit   = "template.submit"
	PermTemplateApprove  = "template.approve"
	PermTemplateReject   = "template.reject"
	PermTemplateDisable  = "template.disable"
	PermTemplateEnable   = "template.enable"
	PermTemplateRollback = "template.rollback"

	PermNotificationRead = "notification.read"
	PermAuditView        = "audit.view"
	PermRBACManage       = "rbac.manage"
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// PermissionMeta 权限码中文说明（运营台展示用）。
type PermissionMeta struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Group string `json:"group"`
	// Kind menu=菜单可见性；action=按钮/写操作
	Kind string `json:"kind"`
}

// RoleDef 角色定义（可被 override 覆盖 / 扩展自定义角色）。
type RoleDef struct {
	Role        string   `json:"role" yaml:"-"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Permissions []string `json:"permissions" yaml:"permissions"`
	Builtin     bool     `json:"builtin" yaml:"-"`
}

// AllPermissions 全量权限码（admin / auth 关闭时使用）。
func AllPermissions() []string {
	return []string{
		PermMenuOverview, PermMenuTasks, PermMenuTemplates, PermMenuNotifications, PermMenuAudit, PermMenuSettings,
		PermMenuSegments, PermMenuPreferences, PermMenuSchedules, PermMenuChannels,
		PermCampaignCreate, PermCampaignUpdate, PermCampaignPublish, PermCampaignCancel,
		PermCampaignPause, PermCampaignResume, PermCampaignRetry, PermCampaignCopy,
		PermCampaignBatch, PermCampaignExport, PermCampaignPreflight, PermCampaignDryRun,
		PermCampaignSimulate, PermAudienceEstimate,
		PermSegmentManage, PermSuppressionManage, PermPreferenceView, PermPreferenceManage,
		PermScheduleManage, PermChannelManage,
		PermTemplateCreate, PermTemplateEdit, PermTemplateDelete, PermTemplateSubmit,
		PermTemplateApprove, PermTemplateReject, PermTemplateDisable, PermTemplateEnable,
		PermTemplateRollback,
		PermNotificationRead, PermAuditView, PermRBACManage,
	}
}

func operatorPermissions() []string {
	return []string{
		PermMenuOverview, PermMenuTasks, PermMenuTemplates, PermMenuNotifications,
		PermMenuSegments, PermMenuPreferences, PermMenuSchedules, PermMenuChannels,
		PermCampaignCreate, PermCampaignUpdate, PermCampaignPublish, PermCampaignCancel,
		PermCampaignPause, PermCampaignResume, PermCampaignRetry, PermCampaignCopy,
		PermCampaignBatch, PermCampaignExport, PermCampaignPreflight, PermCampaignDryRun,
		PermCampaignSimulate, PermAudienceEstimate,
		PermSegmentManage, PermSuppressionManage, PermPreferenceView, PermPreferenceManage,
		PermScheduleManage, PermChannelManage,
		PermTemplateCreate, PermTemplateEdit, PermTemplateDelete, PermTemplateSubmit,
		PermTemplateApprove, PermTemplateReject, PermTemplateDisable, PermTemplateEnable,
		PermTemplateRollback,
		PermNotificationRead,
	}
}

func viewerPermissions() []string {
	return []string{
		PermMenuOverview, PermMenuTasks, PermMenuTemplates, PermMenuNotifications,
		PermMenuSegments, PermMenuSchedules, PermMenuChannels,
	}
}

// DefaultRoleDefs 内置角色默认定义（可被 override 覆盖权限集合）。
func DefaultRoleDefs() map[string]RoleDef {
	return map[string]RoleDef{
		RoleAdmin: {
			Role:        RoleAdmin,
			Name:        "管理员",
			Description: "全部菜单与写操作，含审计与权限/用户/角色配置。",
			Permissions: append([]string(nil), AllPermissions()...),
			Builtin:     true,
		},
		RoleOperator: {
			Role:        RoleOperator,
			Name:        "运营",
			Description: "活动/模板/通知的日常运营；无审计与系统配置。",
			Permissions: append([]string(nil), operatorPermissions()...),
			Builtin:     true,
		},
		RoleViewer: {
			Role:        RoleViewer,
			Name:        "只读",
			Description: "概览与列表查询；无写按钮、无审计与系统配置。",
			Permissions: append([]string(nil), viewerPermissions()...),
			Builtin:     true,
		},
	}
}

// NormalizeRole 空角色默认 viewer。
func NormalizeRole(role string) string {
	if role == "" {
		return RoleViewer
	}
	return role
}

func HasPermission(perms []string, need string) bool {
	if need == "" {
		return true
	}
	for _, p := range perms {
		if p == "*" || p == need {
			return true
		}
	}
	return false
}

// KnownPermission 是否在内置目录中（seed 用；运行时以库表为准）。
func KnownPermission(code string) bool {
	for _, p := range BuiltinPermissionCatalog() {
		if p.Code == code {
			return true
		}
	}
	return false
}

// SanitizePermissions 过滤未知码并去重排序；known 为空时用内置目录。
func SanitizePermissions(in []string, known ...[]string) []string {
	allow := map[string]struct{}{}
	if len(known) > 0 && len(known[0]) > 0 {
		for _, c := range known[0] {
			allow[c] = struct{}{}
		}
	} else {
		for _, p := range BuiltinPermissionCatalog() {
			allow[p.Code] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := allow[c]; !ok {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// BuiltinPermissionCatalog 内置权限码中文目录（seed 进 auth_permissions）。
func BuiltinPermissionCatalog() []PermissionMeta {
	items := []PermissionMeta{
		{PermMenuOverview, "菜单·运营概览", "菜单", "menu"},
		{PermMenuTasks, "菜单·任务管理", "菜单", "menu"},
		{PermMenuTemplates, "菜单·模板中心", "菜单", "menu"},
		{PermMenuNotifications, "菜单·通知管理", "菜单", "menu"},
		{PermMenuAudit, "菜单·审计日志", "菜单", "menu"},
		{PermMenuSettings, "菜单·系统配置（角色/权限/用户）", "菜单", "menu"},
		{PermMenuSegments, "菜单·人群资产", "菜单", "menu"},
		{PermMenuPreferences, "菜单·用户偏好", "菜单", "menu"},
		{PermMenuSchedules, "菜单·周期活动", "菜单", "menu"},
		{PermMenuChannels, "菜单·渠道运营", "菜单", "menu"},
		{PermCampaignCreate, "创建活动", "活动", "action"},
		{PermCampaignUpdate, "更新活动草稿", "活动", "action"},
		{PermCampaignPublish, "发布活动", "活动", "action"},
		{PermCampaignCancel, "取消活动", "活动", "action"},
		{PermCampaignPause, "暂停活动", "活动", "action"},
		{PermCampaignResume, "恢复活动", "活动", "action"},
		{PermCampaignRetry, "失败重推", "活动", "action"},
		{PermCampaignCopy, "复制活动", "活动", "action"},
		{PermCampaignBatch, "批量操作", "活动", "action"},
		{PermCampaignExport, "导出流水", "活动", "action"},
		{PermCampaignPreflight, "活动预检", "活动", "action"},
		{PermCampaignDryRun, "Dry-run / 测试发送", "活动", "action"},
		{PermCampaignSimulate, "全链路投放仿真", "活动", "action"},
		{PermAudienceEstimate, "人群试算", "活动", "action"},
		{PermSegmentManage, "管理人群段与排除名单", "人群", "action"},
		{PermSuppressionManage, "管理黑名单与退订名单", "人群", "action"},
		{PermPreferenceView, "查看用户偏好", "偏好", "action"},
		{PermPreferenceManage, "修改用户偏好", "偏好", "action"},
		{PermScheduleManage, "管理周期活动", "周期活动", "action"},
		{PermChannelManage, "渠道降级与健康度处置", "渠道", "action"},
		{PermTemplateCreate, "创建模板", "模板", "action"},
		{PermTemplateEdit, "编辑模板", "模板", "action"},
		{PermTemplateDelete, "删除模板", "模板", "action"},
		{PermTemplateSubmit, "提交审核", "模板", "action"},
		{PermTemplateApprove, "审核通过", "模板", "action"},
		{PermTemplateReject, "审核驳回", "模板", "action"},
		{PermTemplateDisable, "停用模板", "模板", "action"},
		{PermTemplateEnable, "启用模板", "模板", "action"},
		{PermTemplateRollback, "模板回滚", "模板", "action"},
		{PermNotificationRead, "通知已读", "通知", "action"},
		{PermAuditView, "查看审计", "审计", "action"},
		{PermRBACManage, "管理系统角色/权限/用户", "系统", "action"},
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Group == items[j].Group {
			return items[i].Code < items[j].Code
		}
		return items[i].Group < items[j].Group
	})
	return items
}

// PermissionCatalog 兼容旧名。
func PermissionCatalog() []PermissionMeta { return BuiltinPermissionCatalog() }
