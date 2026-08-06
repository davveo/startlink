package auth

// 权限码约定：菜单 menu.*；写操作与审计动作对齐（campaign.* / template.* / …）。

const (
	PermMenuOverview      = "menu.overview"
	PermMenuTasks         = "menu.tasks"
	PermMenuTemplates     = "menu.templates"
	PermMenuNotifications = "menu.notifications"
	PermMenuAudit         = "menu.audit"

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
	PermAudienceEstimate  = "audience.estimate"

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
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// AllPermissions 全量权限码（admin / auth 关闭时使用）。
func AllPermissions() []string {
	return []string{
		PermMenuOverview, PermMenuTasks, PermMenuTemplates, PermMenuNotifications, PermMenuAudit,
		PermCampaignCreate, PermCampaignUpdate, PermCampaignPublish, PermCampaignCancel,
		PermCampaignPause, PermCampaignResume, PermCampaignRetry, PermCampaignCopy,
		PermCampaignBatch, PermCampaignExport, PermCampaignPreflight, PermCampaignDryRun,
		PermAudienceEstimate,
		PermTemplateCreate, PermTemplateEdit, PermTemplateDelete, PermTemplateSubmit,
		PermTemplateApprove, PermTemplateReject, PermTemplateDisable, PermTemplateEnable,
		PermTemplateRollback,
		PermNotificationRead, PermAuditView,
	}
}

func operatorPermissions() []string {
	return []string{
		PermMenuOverview, PermMenuTasks, PermMenuTemplates, PermMenuNotifications,
		PermCampaignCreate, PermCampaignUpdate, PermCampaignPublish, PermCampaignCancel,
		PermCampaignPause, PermCampaignResume, PermCampaignRetry, PermCampaignCopy,
		PermCampaignBatch, PermCampaignExport, PermCampaignPreflight, PermCampaignDryRun,
		PermAudienceEstimate,
		PermTemplateCreate, PermTemplateEdit, PermTemplateDelete, PermTemplateSubmit,
		PermTemplateApprove, PermTemplateReject, PermTemplateDisable, PermTemplateEnable,
		PermTemplateRollback,
		PermNotificationRead,
	}
}

func viewerPermissions() []string {
	return []string{
		PermMenuOverview, PermMenuTasks, PermMenuTemplates, PermMenuNotifications,
	}
}

// PermissionsForRole 角色 → 权限列表；未知角色按 viewer。
func PermissionsForRole(role string) []string {
	switch role {
	case RoleAdmin, "*":
		return append([]string(nil), AllPermissions()...)
	case RoleOperator:
		return append([]string(nil), operatorPermissions()...)
	case RoleViewer, "":
		return append([]string(nil), viewerPermissions()...)
	default:
		return append([]string(nil), viewerPermissions()...)
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
