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

export type ChannelMode = 'single' | 'fallback' | 'parallel' | ''

export type Template = {
  id: number
  code: string
  name: string
  body: string
  biz_scene: string
  channel_hint?: ChannelType
  status: TemplateStatus
  version: number
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
  biz_scene: string
  title: string
  template_id: string
  audience_ref: string
  channel?: ChannelType
  channels?: ChannelType[]
  channel_mode?: ChannelMode
  priority?: Priority
  audience_extra?: Record<string, unknown>
  payload?: Record<string, unknown>
  webhook_url?: string
  scheduled_at?: string
  pace_qps?: number
}

export type CreateCampaignResult = {
  task_id: number
  biz_id: string
  status: TaskStatus
}

export type CampaignListItem = {
  id: number
  biz_id: string
  biz_scene: string
  title: string
  channel: ChannelType
  channels?: ChannelType[]
  channel_mode: ChannelMode
  priority: Priority
  template_id: string
  status: TaskStatus
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
