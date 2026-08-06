/** 与后端 internal/auth/rbac.go 保持一致的权限码常量。 */
export const Perm = {
  MenuOverview: 'menu.overview',
  MenuTasks: 'menu.tasks',
  MenuTemplates: 'menu.templates',
  MenuNotifications: 'menu.notifications',
  MenuAudit: 'menu.audit',
  MenuSettings: 'menu.settings',

  CampaignCreate: 'campaign.create',
  CampaignUpdate: 'campaign.update',
  CampaignPublish: 'campaign.publish',
  CampaignCancel: 'campaign.cancel',
  CampaignPause: 'campaign.pause',
  CampaignResume: 'campaign.resume',
  CampaignRetry: 'campaign.retry',
  CampaignCopy: 'campaign.copy',
  CampaignBatch: 'campaign.batch',
  CampaignExport: 'campaign.export',
  CampaignPreflight: 'campaign.preflight',
  CampaignDryRun: 'campaign.dry_run',
  AudienceEstimate: 'audience.estimate',

  TemplateCreate: 'template.create',
  TemplateEdit: 'template.edit',
  TemplateDelete: 'template.delete',
  TemplateSubmit: 'template.submit',
  TemplateApprove: 'template.approve',
  TemplateReject: 'template.reject',
  TemplateDisable: 'template.disable',
  TemplateEnable: 'template.enable',
  TemplateRollback: 'template.rollback',

  NotificationRead: 'notification.read',
  AuditView: 'audit.view',
  RBACManage: 'rbac.manage',
} as const

export type PermCode = (typeof Perm)[keyof typeof Perm]
