import { ApiError } from './client'

export type TraceSummary = {
  trace_id: string
  biz_id?: string
  main_task_id?: number
  title?: string
  status?: string
  event_count: number
  error_count: number
  warn_count: number
  first_at?: string
  last_at?: string
  last_event?: string
  last_message?: string
}

export type TraceEventView = {
  id: number
  trace_id: string
  biz_id?: string
  main_task_id?: number
  sub_task_id?: number
  msg_id?: string
  record_id?: number
  user_id?: string
  channel?: string
  stage: string
  event: string
  level: string
  service?: string
  message?: string
  detail?: Record<string, unknown>
  created_at: string
}

export type TraceStageStat = {
  stage: string
  count: number
  error: number
  warn: number
}

export type TraceDetail = {
  trace_id: string
  biz_id?: string
  main_task_id?: number
  title?: string
  status?: string
  event_count: number
  error_count: number
  warn_count: number
  stages?: TraceStageStat[]
  events: TraceEventView[]
  filtered_total: number
  page: number
  page_size: number
}

export type TraceSummaryList = {
  items: TraceSummary[]
  total: number
  page: number
  page_size: number
}

export type TraceEventList = {
  items: TraceEventView[]
  total: number
  page: number
  page_size: number
}

type ApiBody<T> = { code: number; message?: string; data?: T }

async function request<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: 'include' })
  let body: ApiBody<T>
  try {
    body = (await res.json()) as ApiBody<T>
  } catch {
    throw new ApiError(res.status, `HTTP ${res.status}`)
  }
  if (res.status === 401 || body.code === 40101) {
    throw new ApiError(body.code || 40101, body.message || 'unauthorized')
  }
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message || 'request failed')
  }
  if (body.data === undefined || body.data === null) {
    throw new ApiError(body.code, `接口返回缺少 data：${path}`)
  }
  return body.data
}

function qs(params: Record<string, string | number | boolean | undefined>) {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '' || v === false) continue
    p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

export const traceApi = {
  list: (q: {
    trace_id?: string
    biz_id?: string
    main_task_id?: number
    user_id?: string
    level?: string
    page?: number
    page_size?: number
  }) =>
    request<TraceSummaryList>(
      `/api/v1/traces${qs({
        trace_id: q.trace_id,
        biz_id: q.biz_id,
        main_task_id: q.main_task_id,
        user_id: q.user_id,
        level: q.level,
        page: q.page,
        page_size: q.page_size,
      })}`,
    ),

  get: (
    traceId: string,
    q?: {
      page?: number
      page_size?: number
      level?: string
      stage?: string
      user_id?: string
      sub_task_id?: number
      anomaly_only?: boolean
      order?: 'asc' | 'desc'
    },
  ) =>
    request<TraceDetail>(
      `/api/v1/traces/${encodeURIComponent(traceId)}${qs({
        page: q?.page,
        page_size: q?.page_size,
        level: q?.level,
        stage: q?.stage,
        user_id: q?.user_id,
        sub_task_id: q?.sub_task_id,
        anomaly_only: q?.anomaly_only ? '1' : undefined,
        order: q?.order ?? 'asc',
      })}`,
    ),

  listEvents: (q: {
    trace_id?: string
    biz_id?: string
    main_task_id?: number
    sub_task_id?: number
    user_id?: string
    stage?: string
    event?: string
    level?: string
    anomaly_only?: boolean
    order?: 'asc' | 'desc'
    page?: number
    page_size?: number
  }) =>
    request<TraceEventList>(
      `/api/v1/trace-events${qs({
        trace_id: q.trace_id,
        biz_id: q.biz_id,
        main_task_id: q.main_task_id,
        sub_task_id: q.sub_task_id,
        user_id: q.user_id,
        stage: q.stage,
        event: q.event,
        level: q.level,
        anomaly_only: q.anomaly_only ? '1' : undefined,
        order: q.order,
        page: q.page,
        page_size: q.page_size,
      })}`,
    ),
}

export const stageLabel: Record<string, string> = {
  campaign: '活动',
  split: '拆分',
  worker: '调度',
  pusher: '推送',
  callback: '回执',
  aggregator: '聚合',
  dryrun: '仿真',
}
