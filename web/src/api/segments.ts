/**
 * 人群资产化（人群段 + 抑制名单）接口。
 * 复用 client.ts 的响应约定：credentials:'include'、{code,message,data}、code!==0 抛 ApiError。
 */
import { ApiError } from './client'
import type { ApiBody, ChannelType } from './types'

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
    throw new ApiError(res.status, `HTTP ${res.status}`)
  }
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message || 'request failed')
  }
  if (body.data === undefined || body.data === null) {
    throw new ApiError(body.code, `接口返回缺少 data：${path}`)
  }
  return body.data
}

function qs(params: Record<string, string | number | undefined>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '') continue
    sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export type SegmentKind = 'include' | 'exclude'
export type SegmentStatus = 'active' | 'disabled'

export type Segment = {
  id: number
  code: string
  name: string
  kind: SegmentKind
  biz_scene: string
  audience_ref: string
  description?: string
  status: SegmentStatus
  member_count: number
  counted_at?: string
  refresh_error?: string
  created_by?: string
  updated_by?: string
  created_at: string
  updated_at: string
}

export type SegmentDetail = Segment & { campaign_refs: number }

export type SegmentListResult = {
  items: Segment[]
  total: number
  page: number
  page_size: number
}

export type SegmentInput = {
  code?: string
  name: string
  kind?: SegmentKind
  biz_scene: string
  audience_ref: string
  audience_extra?: Record<string, unknown>
  description?: string
  status?: SegmentStatus
}

export type RefreshResult = {
  segment: Segment
  member_count: number
  estimated: boolean
  error?: string
}

export type SuppressionKind = 'blacklist' | 'unsubscribe'

export type SuppressionEntry = {
  id: number
  kind: SuppressionKind
  user_id: string
  channel: string
  reason?: string
  source?: string
  operator?: string
  created_at: string
  updated_at: string
}

export type SuppressionListResult = {
  items: SuppressionEntry[]
  total: number
  page: number
  page_size: number
}

export type SuppressionStats = {
  blacklist: number
  unsubscribe: number
  total: number
}

export type AddSuppressionResult = {
  submitted: number
  added: number
  skipped: number
  synced: boolean
  sync_error?: string
}

export type RemoveSuppressionResult = {
  removed: boolean
  synced: boolean
  sync_error?: string
}

export const segmentApi = {
  list: (params: {
    kind?: string
    biz_scene?: string
    status?: string
    keyword?: string
    page?: number
    page_size?: number
  }) => request<SegmentListResult>(`/api/v1/segments${qs(params)}`),

  get: (code: string) => request<SegmentDetail>(`/api/v1/segments/${encodeURIComponent(code)}`),

  create: (input: SegmentInput) =>
    request<Segment>('/api/v1/segments', { method: 'POST', body: JSON.stringify(input) }),

  update: (code: string, input: SegmentInput) =>
    request<Segment>(`/api/v1/segments/${encodeURIComponent(code)}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),

  remove: (code: string) =>
    request<{ deleted: boolean }>(`/api/v1/segments/${encodeURIComponent(code)}`, { method: 'DELETE' }),

  refresh: (code: string) =>
    request<RefreshResult>(`/api/v1/segments/${encodeURIComponent(code)}/refresh`, {
      method: 'POST',
      body: '{}',
    }),

  listSuppressions: (params: {
    kind?: string
    user_id?: string
    channel?: string
    keyword?: string
    page?: number
    page_size?: number
  }) => request<SuppressionListResult>(`/api/v1/suppressions${qs(params)}`),

  suppressionStats: () => request<SuppressionStats>('/api/v1/suppressions/stats'),

  addSuppressions: (input: {
    kind: SuppressionKind
    user_ids: string[]
    channel?: ChannelType
    reason?: string
    source?: string
  }) => request<AddSuppressionResult>('/api/v1/suppressions', { method: 'POST', body: JSON.stringify(input) }),

  removeSuppression: (params: { kind: SuppressionKind; user_id: string; channel?: string }) =>
    request<RemoveSuppressionResult>(`/api/v1/suppressions${qs(params)}`, { method: 'DELETE' }),
}
