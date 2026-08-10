import { ApiError } from './client'
import type { ApiBody, ChannelType } from './types'

/** 期望送达小时的清除哨兵，与后端 preference.ClearPreferredHour 一致。 */
export const CLEAR_PREFERRED_HOUR = -1

export const PREFERENCE_CHANNELS: ChannelType[] = [
  'app_push',
  'sms',
  'email',
  'inbox',
  'wecom',
  'dingtalk',
]

export type UserPreference = {
  id: number
  user_id: string
  timezone?: string
  quiet_start?: string
  quiet_end?: string
  preferred_hour?: number
  marketing_opt_out: boolean
  opt_out_channels: ChannelType[]
  opt_out_topics: string[]
  updated_by?: string
  created_at?: string
  updated_at?: string
}

export type ConsentLog = {
  id: number
  user_id: string
  action: 'opt_in' | 'opt_out' | string
  scope: string
  source?: string
  operator?: string
  detail?: string
  created_at: string
}

export type PreferenceListResult = {
  items: UserPreference[]
  total: number
  page: number
  page_size: number
}

export type ConsentListResult = {
  items: ConsentLog[]
  total: number
  page: number
  page_size: number
}

/** nil 语义：只提交需要修改的字段，未出现的字段后端保持原值。 */
export type PreferenceInput = {
  timezone?: string
  quiet_start?: string
  quiet_end?: string
  preferred_hour?: number
  opt_out_channels?: string[]
  opt_out_topics?: string[]
  marketing_opt_out?: boolean
  source?: string
}

// client.ts 未导出内部 request，这里按同一套约定（credentials/{code,message,data}）实现一份。
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
    throw new ApiError(body.code || res.status, body.message || 'request failed')
  }
  if (body.data === undefined || body.data === null) {
    throw new ApiError(body.code, `接口返回缺少 data：${path}`)
  }
  return body.data
}

export const preferenceApi = {
  list: (q?: {
    user_id?: string
    channel?: string
    topic?: string
    marketing_opt_out?: boolean
    page?: number
    page_size?: number
  }) => {
    const params = new URLSearchParams()
    if (q?.user_id) params.set('user_id', q.user_id)
    if (q?.channel) params.set('channel', q.channel)
    if (q?.topic) params.set('topic', q.topic)
    if (q?.marketing_opt_out !== undefined) {
      params.set('marketing_opt_out', q.marketing_opt_out ? '1' : '0')
    }
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<PreferenceListResult>(`/api/v1/preferences?${params}`)
  },

  get: (userID: string) =>
    request<UserPreference>(`/api/v1/preferences/${encodeURIComponent(userID)}`),

  upsert: (userID: string, body: PreferenceInput) =>
    request<UserPreference>(`/api/v1/preferences/${encodeURIComponent(userID)}`, {
      method: 'PUT',
      body: JSON.stringify({ source: 'console', ...body }),
    }),

  remove: (userID: string) =>
    request<{ deleted: boolean }>(`/api/v1/preferences/${encodeURIComponent(userID)}`, {
      method: 'DELETE',
      body: JSON.stringify({ source: 'console' }),
    }),

  listConsentLogs: (q?: {
    user_id?: string
    action?: string
    scope?: string
    since?: string
    until?: string
    page?: number
    page_size?: number
  }) => {
    const params = new URLSearchParams()
    if (q?.user_id) params.set('user_id', q.user_id)
    if (q?.action) params.set('action', q.action)
    if (q?.scope) params.set('scope', q.scope)
    if (q?.since) params.set('since', q.since)
    if (q?.until) params.set('until', q.until)
    params.set('page', String(q?.page ?? 1))
    params.set('page_size', String(q?.page_size ?? 20))
    return request<ConsentListResult>(`/api/v1/consent-logs?${params}`)
  },
}
