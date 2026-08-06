import type {
  ApiBody,
  CampaignListResult,
  ChannelType,
  CreateCampaignInput,
  CreateCampaignResult,
  ProgressView,
  SubTaskListResult,
  SubTaskView,
  TaskStatus,
  Template,
  TemplateListResult,
  TemplateStatus,
} from './types'

export class ApiError extends Error {
  code: number
  constructor(code: number, message: string) {
    super(message)
    this.code = code
  }
}

function redirectToLoginIfNeeded(path: string) {
  if (typeof window === 'undefined') return
  if (window.location.pathname.startsWith('/login')) return
  // /auth/me 由 AuthContext 自行处理，避免启动时整页跳转闪烁
  if (path.includes('/auth/me') || path.includes('/auth/login')) return
  const next = encodeURIComponent(window.location.pathname + window.location.search)
  window.location.assign(`/login?next=${next}`)
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })
  let body: ApiBody<T>
  try {
    body = (await res.json()) as ApiBody<T>
  } catch {
    if (res.status === 401) {
      redirectToLoginIfNeeded(path)
    }
    throw new ApiError(res.status, `HTTP ${res.status}`)
  }
  if (res.status === 401 || body.code === 40101) {
    redirectToLoginIfNeeded(path)
    throw new ApiError(body.code || 40101, body.message || 'unauthorized')
  }
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message || 'request failed')
  }
  return body.data as T
}

export const api = {
  healthz: () => request<{ status: string }>('/healthz'),

  login: (username: string, password: string) =>
    request<{ username: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  logout: () =>
    request<{ ok: boolean }>('/api/v1/auth/logout', {
      method: 'POST',
      body: '{}',
    }),

  me: () => request<{ username: string; auth_disabled?: boolean }>('/api/v1/auth/me'),

  listChannels: () => request<{ channels: ChannelType[] }>('/api/v1/channels'),

  listTemplates: (q?: {
    biz_scene?: string
    status?: TemplateStatus | ''
    keyword?: string
    page?: number
    page_size?: number
  }) => {
    const params = new URLSearchParams()
    if (q?.biz_scene) params.set('biz_scene', q.biz_scene)
    if (q?.status) params.set('status', q.status)
    if (q?.keyword) params.set('keyword', q.keyword)
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<TemplateListResult>(`/api/v1/templates?${params}`)
  },

  getTemplate: (id: number) => request<Template>(`/api/v1/templates/${id}`),

  createTemplate: (body: {
    code?: string
    name: string
    body?: string
    contents?: Record<string, { title?: string; body?: string }>
    var_schema?: { name: string; type?: string; required?: boolean; default?: string; example?: string; sensitive?: boolean }[]
    missing_var_policy?: string
    default_locale?: string
    biz_scene?: string
    channel_hint?: ChannelType
    created_by?: string
  }) =>
    request<Template>('/api/v1/templates', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  updateTemplate: (
    id: number,
    body: {
      name?: string
      body?: string
      contents?: Record<string, { title?: string; body?: string }>
      var_schema?: { name: string; type?: string; required?: boolean; default?: string; example?: string; sensitive?: boolean }[]
      missing_var_policy?: string
      default_locale?: string
      biz_scene?: string
      channel_hint?: ChannelType
      version?: number
      updated_by?: string
    },
  ) =>
    request<Template>(`/api/v1/templates/${id}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),

  previewTemplate: (body: Record<string, unknown>) =>
    request<import('./types').TemplatePreviewResult>('/api/v1/templates/preview', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  listTemplateVersions: (id: number) =>
    request<{ items: unknown[] }>(`/api/v1/templates/${id}/versions`),

  rollbackTemplate: (id: number, revision: number, updated_by?: string, version?: number) =>
    request<Template>(`/api/v1/templates/${id}/rollback`, {
      method: 'POST',
      body: JSON.stringify({ revision, updated_by, version }),
    }),

  deleteTemplate: (id: number) =>
    request<{ deleted: boolean }>(`/api/v1/templates/${id}`, { method: 'DELETE' }),

  submitTemplate: (id: number, operator: string) =>
    request<Template>(`/api/v1/templates/${id}/submit`, {
      method: 'POST',
      body: JSON.stringify({ operator }),
    }),

  approveTemplate: (id: number, reviewed_by: string) =>
    request<Template>(`/api/v1/templates/${id}/approve`, {
      method: 'POST',
      body: JSON.stringify({ reviewed_by }),
    }),

  rejectTemplate: (id: number, reject_reason: string, reviewed_by: string) =>
    request<Template>(`/api/v1/templates/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reviewed_by, reject_reason }),
    }),

  disableTemplate: (id: number) =>
    request<Template>(`/api/v1/templates/${id}/disable`, { method: 'POST', body: '{}' }),

  enableTemplate: (id: number) =>
    request<Template>(`/api/v1/templates/${id}/enable`, { method: 'POST', body: '{}' }),

  createCampaign: (body: CreateCampaignInput) =>
    request<CreateCampaignResult>('/api/v1/campaigns', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  listCampaigns: (q?: {
    biz_scene?: string
    status?: TaskStatus | ''
    channel?: string
    priority?: string
    created_by?: string
    keyword?: string
    page?: number
    page_size?: number
  }) => {
    const params = new URLSearchParams()
    if (q?.biz_scene) params.set('biz_scene', q.biz_scene)
    if (q?.status) params.set('status', q.status)
    if (q?.channel) params.set('channel', q.channel)
    if (q?.priority) params.set('priority', q.priority)
    if (q?.created_by) params.set('created_by', q.created_by)
    if (q?.keyword) params.set('keyword', q.keyword)
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<CampaignListResult>(`/api/v1/campaigns?${params}`)
  },

  listSubTasks: (
    mainTaskId: number,
    q?: { status?: TaskStatus | ''; page?: number; page_size?: number },
  ) => {
    const params = new URLSearchParams()
    if (q?.status) params.set('status', q.status)
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 50))
    return request<SubTaskListResult>(`/api/v1/campaigns/${mainTaskId}/subtasks?${params}`)
  },

  getSubTask: (mainTaskId: number, subId: number) =>
    request<SubTaskView>(`/api/v1/campaigns/${mainTaskId}/subtasks/${subId}`),

  getProgress: (id: number) => request<ProgressView>(`/api/v1/campaigns/${id}/progress`),

  getProgressByBiz: (bizId: string) =>
    request<ProgressView>(`/api/v1/campaigns/biz/${encodeURIComponent(bizId)}`),

  cancelCampaign: (id: number) =>
    request<unknown>(`/api/v1/campaigns/${id}/cancel`, { method: 'POST', body: '{}' }),

  pauseCampaign: (id: number) =>
    request<unknown>(`/api/v1/campaigns/${id}/pause`, { method: 'POST', body: '{}' }),

  resumeCampaign: (id: number) =>
    request<unknown>(`/api/v1/campaigns/${id}/resume`, { method: 'POST', body: '{}' }),

  retryCampaign: (id: number) =>
    request<unknown>(`/api/v1/campaigns/${id}/retry`, { method: 'POST', body: '{}' }),

  batchAction: (action: 'pause' | 'resume' | 'cancel' | 'retry', ids: number[]) =>
    request<import('./types').BatchResult>(`/api/v1/campaigns/batch/${action}`, {
      method: 'POST',
      body: JSON.stringify({ ids }),
    }),

  preflight: (body: CreateCampaignInput) =>
    request<import('./types').PreflightResult>('/api/v1/campaigns/preflight', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  estimateAudience: (body: {
    biz_scene: string
    audience_ref: string
    audience_extra?: Record<string, unknown>
    channel?: string
    channels?: string[]
    max_pages?: number
    sample_limit?: number
  }) =>
    request<import('./types').AudienceEstimateResult>('/api/v1/audiences/estimate', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  dryRun: (body: {
    template_id: string
    title?: string
    vars?: Record<string, string>
    channel?: string
    channels?: string[]
    user_id?: string
    send?: boolean
  }) =>
    request<import('./types').DryRunResult>('/api/v1/campaigns/dry-run', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  copyCampaign: (id: number, body: { biz_id: string; title?: string; as_draft?: boolean; created_by?: string }) =>
    request<CreateCampaignResult>(`/api/v1/campaigns/${id}/copy`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  publishCampaign: (id: number) =>
    request<CreateCampaignResult>(`/api/v1/campaigns/${id}/publish`, { method: 'POST', body: '{}' }),

  getFunnel: (id: number) => request<import('./types').FunnelView>(`/api/v1/campaigns/${id}/funnel`),

  getFailures: (id: number) =>
    request<{ task_id: number; items: import('./types').FailureAgg[] }>(`/api/v1/campaigns/${id}/failures`),

  getExperimentMetrics: (id: number) =>
    request<import('./types').ExperimentMetrics>(`/api/v1/campaigns/${id}/experiment`),

  listRecords: (
    id: number,
    q?: { user_id?: string; channel?: string; status?: string; keyword?: string; page?: number; page_size?: number },
  ) => {
    const params = new URLSearchParams()
    if (q?.user_id) params.set('user_id', q.user_id)
    if (q?.channel) params.set('channel', q.channel)
    if (q?.status) params.set('status', q.status)
    if (q?.keyword) params.set('keyword', q.keyword)
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<{ total: number; page: number; page_size: number; items: import('./types').PushRecord[] }>(
      `/api/v1/campaigns/${id}/records?${params}`,
    )
  },

  createExport: (id: number, kind = 'records', created_by?: string) =>
    request<import('./types').ExportJob>(`/api/v1/campaigns/${id}/exports`, {
      method: 'POST',
      body: JSON.stringify({ kind, created_by: created_by || undefined }),
    }),

  getExport: (jobId: number) => request<import('./types').ExportJob>(`/api/v1/exports/${jobId}`),

  exportSyncUrl: (id: number, kind = 'records') => `/api/v1/campaigns/${id}/export?kind=${kind}`,

  getOverview: () => request<import('./types').OverviewView>('/api/v1/overview'),

  listNotifications: (q?: { unread_only?: boolean; page?: number; page_size?: number }) => {
    const params = new URLSearchParams()
    if (q?.unread_only) params.set('unread_only', '1')
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<import('./types').NotificationListResult>(`/api/v1/notifications?${params}`)
  },

  unreadNotificationCount: () =>
    request<{ count: number }>('/api/v1/notifications/unread-count'),

  markNotificationRead: (id: number) =>
    request<{ ok: boolean }>(`/api/v1/notifications/${id}/read`, { method: 'POST', body: '{}' }),

  markAllNotificationsRead: () =>
    request<{ updated: number }>('/api/v1/notifications/read-all', { method: 'POST', body: '{}' }),

  listAuditLogs: (q?: {
    operator?: string
    action?: string
    success?: boolean
    since?: string
    until?: string
    page?: number
    page_size?: number
  }) => {
    const params = new URLSearchParams()
    if (q?.operator) params.set('operator', q.operator)
    if (q?.action) params.set('action', q.action)
    if (q?.success === true) params.set('success', '1')
    if (q?.success === false) params.set('success', '0')
    if (q?.since) params.set('since', q.since)
    if (q?.until) params.set('until', q.until)
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<import('./types').AuditLogListResult>(`/api/v1/audit-logs?${params}`)
  },
}
