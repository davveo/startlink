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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })
  let body: ApiBody<T>
  try {
    body = (await res.json()) as ApiBody<T>
  } catch {
    throw new ApiError(res.status, `HTTP ${res.status}`)
  }
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message || 'request failed')
  }
  return body.data as T
}

export const api = {
  healthz: () => request<{ status: string }>('/healthz'),
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

  createTemplate: (body: {
    code?: string
    name: string
    body: string
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

  deleteTemplate: (id: number) =>
    request<{ deleted: boolean }>(`/api/v1/templates/${id}`, { method: 'DELETE' }),

  submitTemplate: (id: number, operator = 'console') =>
    request<Template>(`/api/v1/templates/${id}/submit`, {
      method: 'POST',
      body: JSON.stringify({ operator }),
    }),

  approveTemplate: (id: number, reviewed_by = 'console') =>
    request<Template>(`/api/v1/templates/${id}/approve`, {
      method: 'POST',
      body: JSON.stringify({ reviewed_by }),
    }),

  rejectTemplate: (id: number, reject_reason: string, reviewed_by = 'console') =>
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
}
