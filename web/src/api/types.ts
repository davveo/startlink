export type ApiBody<T> = {
  code: number
  message: string
  data?: T
}

export type ChannelType =
  | 'app_push'
  | 'sms'
  | 'email'
  | 'inbox'
  | 'wecom'
  | 'dingtalk'
  | string

export type TemplateStatus =
  | 'draft'
  | 'pending_review'
  | 'approved'
  | 'rejected'
  | 'disabled'

export type TaskStatus =
  | 'draft'
  | 'pending'
  | 'running'
  | 'paused'
  | 'success'
  | 'partial'
  | 'failed'
  | 'cancelled'
  | 'retrying'
  | string

export type Priority = 'high' | 'normal' | ''

export type ChannelMode =
  | 'single'
  | 'fallback'
  | 'parallel'
  | 'all_success'
  | 'conditional'
  | 'cost_priority'
  | ''

export type RouteCondition = {
  var: string
  op?: string
  value?: string
}

export type ChannelRouteRule = {
  when?: RouteCondition
  channels: ChannelType[]
}

export type MissingVarPolicy = 'error' | 'keep' | 'default' | 'empty' | ''

export type ChannelContent = {
  title?: string
  body?: string
  extra?: Record<string, unknown>
}

export type VarDef = {
  name: string
  type?: string
  required?: boolean
  default?: string
  example?: string
  sensitive?: boolean
}

export type Template = {
  id: number
  code: string
  name: string
  body: string
  contents?: Record<string, ChannelContent>
  var_schema?: VarDef[]
  missing_var_policy?: MissingVarPolicy
  default_locale?: string
  locales?: Record<string, { body?: string; contents?: Record<string, ChannelContent> }>
  biz_scene: string
  channel_hint?: ChannelType
  status: TemplateStatus
  version: number
  revision?: number
  reject_reason?: string
  created_by?: string
  updated_by?: string
  reviewed_by?: string
  reviewed_at?: string
  created_at: string
  updated_at: string
}

export type TemplateListResult = {
  total: number
  page: number
  page_size: number
  items: Template[]
}

export type ProgressView = {
  task_id: number
  biz_id: string
  biz_scene: string
  title: string
  channel: ChannelType
  channels?: ChannelType[]
  channel_mode: ChannelMode
  priority: Priority
  status: TaskStatus
  total_users: number
  success_users: number
  fail_users: number
  suppressed_users?: number
  unreachable_users?: number
  expired_users?: number
  quota_rejected_users?: number
  cancelled_users: number
  in_progress_users: number
  sub_task_total: number
  sub_task_done: number
  sub_pending: number
  sub_running: number
  sub_success: number
  sub_failed: number
  sub_cancelled: number
  sub_in_progress: number
  progress_percent: number
  progress_text: string
  finished: boolean
  webhook_url?: string
  scheduled_at?: string
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export type CreateCampaignInput = {
  biz_id: string
  biz_scene?: string
  title: string
  template_id: string
  audience_ref?: string
  segment_code?: string
  exclude_segment_code?: string
  channel?: ChannelType
  channels?: ChannelType[]
  channel_mode?: ChannelMode
  priority?: Priority
  audience_extra?: Record<string, unknown>
  payload?: Record<string, unknown>
  webhook_url?: string
  scheduled_at?: string
  expire_at?: string
  experiment_id?: string
  experiment_salt?: string
  experiment_control_percent?: number
  max_fallback?: number
  channel_routes?: ChannelRouteRule[]
  channel_costs?: Partial<Record<ChannelType, number>>
  pace_qps?: number
  created_by?: string
  as_draft?: boolean
}

export type CreateCampaignResult = {
  task_id: number
  biz_id: string
  trace_id?: string
  status: TaskStatus
}

export type CampaignListItem = {
  id: number
  biz_id: string
  trace_id?: string
  biz_scene: string
  title: string
  channel: ChannelType
  channels?: ChannelType[]
  channel_mode: ChannelMode
  priority: Priority
  template_id: string
  status: TaskStatus
  created_by?: string
  copied_from_id?: number
  total_count: number
  success_count: number
  fail_count: number
  sub_task_total: number
  sub_task_done: number
  scheduled_at?: string
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export type CampaignListResult = {
  total: number
  page: number
  page_size: number
  items: CampaignListItem[]
}

export type SubTaskView = {
  id: number
  main_task_id: number
  shard_index: number
  total_count: number
  success_count: number
  fail_count: number
  status: TaskStatus
  retry_count: number
  worker_id?: string
  last_error?: string
  claimed_at?: string
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
}

export type SubTaskListResult = {
  main_task_id: number
  biz_id: string
  title: string
  status: TaskStatus
  total: number
  page: number
  page_size: number
  items: SubTaskView[]
}

export type BatchResult = {
  action: string
  total: number
  success: number
  failed: number
  items: { id: number; ok: boolean; message?: string }[]
}

export type AudienceEstimateResult = {
  raw_count: number
  after_filter: number
  after_ab: number
  reachable_count: number
  skipped_no_channel: number
  pages_scanned: number
  has_more: boolean
  total_hint?: number
  sample?: { user_id: string; channels?: ChannelType[] }[]
  ab_percent?: number
}

export type PreflightResult = {
  estimate: AudienceEstimateResult
  priority: Priority
  channels: ChannelType[]
  channel_mode: ChannelMode
  template_ok: boolean
  template_code?: string
  estimated_seconds?: number
  capacity_risk?: string[]
  cost_hint?: string
  warnings?: string[]
}

export type DryRunResult = {
  rendered_title?: string
  rendered_content: string
  missing_vars?: string[]
  schema_errors?: string[]
  channels?: ChannelType[]
  channel_mode?: ChannelMode
  missing_var_policy?: MissingVarPolicy
  by_channel?: Record<string, { title?: string; content: string }>
  sent: boolean
  send_results?: { success: boolean; error_msg?: string; provider_id?: string }[]
  test_record_ids?: number[]
}

export type TemplatePreviewResult = {
  rendered_title?: string
  rendered_content: string
  missing_vars?: string[]
  schema_errors?: string[]
  channel?: ChannelType
  locale?: string
  missing_var_policy?: MissingVarPolicy
  by_channel?: Record<string, { title?: string; content: string }>
}

export type FunnelView = {
  task_id: number
  audience_raw_count: number
  audience_filtered_count: number
  audience_reachable_count: number
  enqueued_users: number
  pipeline: {
    queued: number
    sending: number
    sent: number
    delivered: number
    clicked: number
    failed: number
    suppressed: number
    unreachable: number
    cancelled: number
    expired: number
    quota_rejected: number
  }
  user_outcomes: {
    SuccessUsers?: number
    success_users?: number
    FailUsers?: number
    fail_users?: number
    HasRecords?: boolean
    has_records?: boolean
  }
}

export type FailureAgg = {
  channel: ChannelType
  provider: string
  error_msg: string
  count: number
}

export type ExperimentGroupMetrics = {
  group: string
  assigned_users: number
  reach_users: number
  success_users: number
  fail_users: number
  suppressed_users: number
  sent_records: number
  delivered_records: number
  clicked_records: number
  failed_records: number
  success_rate: number
}

export type ExperimentMetrics = {
  experiment_id?: string
  groups: ExperimentGroupMetrics[]
}

export type PushRecord = {
  id: number
  main_task_id: number
  user_id: string
  channel: ChannelType
  status: string
  provider?: string
  provider_id?: string
  error_msg?: string
  is_test?: boolean
  sent_at?: string
  created_at: string
}

export type ExportJob = {
  id: number
  main_task_id: number
  kind: string
  status: string
  file_url?: string
  row_count: number
  error_msg?: string
  created_at: string
}

export type OverviewView = {
  campaign_total: number
  by_status: Record<string, number>
  active_count: number
  success_count: number
  partial_count: number
  failed_count: number
  cancelled_count: number
  draft_count: number
  lifetime_success_users: number
  lifetime_fail_users: number
  experiment_tasks: number
  recent_sends: {
    window_hours: number
    total: number
    success: number
    failed: number
    success_rate: number
  }
  recent_campaigns: OverviewCampaign[]
}

export type OverviewCampaign = {
  id: number
  biz_id: string
  biz_scene: string
  title: string
  channel: ChannelType
  channels?: ChannelType[]
  status: TaskStatus
  priority: Priority
  total_count: number
  success_count: number
  fail_count: number
  experiment_id?: string
  created_at: string
  finished_at?: string
}

export type NotificationLevel = 'info' | 'success' | 'warning' | 'error' | string
export type NotificationType = 'task_finished' | string

export type Notification = {
  id: number
  title: string
  body: string
  level: NotificationLevel
  type: NotificationType
  related_task_id?: number
  read_at?: string
  created_by?: string
  created_at: string
}

export type AuditLog = {
  id: number
  operator: string
  action: string
  resource_type?: string
  resource_id?: string
  method: string
  path: string
  ip?: string
  detail?: string
  success: boolean
  created_at: string
}

export type AuditLogListResult = {
  total: number
  page: number
  page_size: number
  items: AuditLog[]
}

export type NotificationListResult = {
  total: number
  page: number
  page_size: number
  items: Notification[]
}

