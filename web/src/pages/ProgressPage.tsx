import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ProgressView } from '../api/types'
import { StatusChip } from '../components/StatusChip'
import {
  BtnRow,
  Button,
  ButtonLink,
  Chip,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  PanelTitle,
  ProgressBar,
  Stat,
  Toast,
} from '../components/ui'
import { channelLabel, channelModeLabel, priorityLabel } from '../lib/labels'

export function ProgressPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const taskParam = searchParams.get('task') || ''
  const bizParam = searchParams.get('biz') || ''

  const [progress, setProgress] = useState<ProgressView | null>(null)
  const [lookup, setLookup] = useState(taskParam || bizParam)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)

  const fetchProgress = useCallback(async (q: string) => {
    const value = q.trim()
    if (!value) return null
    if (/^\d+$/.test(value)) {
      return api.getProgress(Number(value))
    }
    return api.getProgressByBiz(value)
  }, [])

  useEffect(() => {
    const q = taskParam || bizParam
    if (!q) return
    setLookup(q)
    let cancelled = false
    setBusy(true)
    setErr('')
    void (async () => {
      try {
        const p = await fetchProgress(q)
        if (cancelled || !p) return
        setProgress(p)
        if (!taskParam || taskParam !== String(p.task_id)) {
          setSearchParams({ task: String(p.task_id) }, { replace: true })
        }
      } catch (e) {
        if (cancelled) return
        setProgress(null)
        setErr(e instanceof ApiError ? e.message : '查询失败')
      } finally {
        if (!cancelled) setBusy(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [taskParam, bizParam, fetchProgress, setSearchParams])

  useEffect(() => {
    if (!autoRefresh || !progress || progress.finished) return
    const timer = window.setInterval(() => {
      void api
        .getProgress(progress.task_id)
        .then(setProgress)
        .catch(() => undefined)
    }, 2000)
    return () => window.clearInterval(timer)
  }, [autoRefresh, progress])

  async function onLookup(e: FormEvent) {
    e.preventDefault()
    const q = lookup.trim()
    if (!q) return
    setMsg('')
    setErr('')
    setBusy(true)
    try {
      const p = await fetchProgress(q)
      if (!p) return
      setProgress(p)
      setSearchParams({ task: String(p.task_id) }, { replace: true })
      setMsg('已加载活动进度')
    } catch (e) {
      setProgress(null)
      setErr(e instanceof ApiError ? e.message : '查询失败')
    } finally {
      setBusy(false)
    }
  }

  async function runAction(action: () => Promise<unknown>, ok: string) {
    if (!progress) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await action()
      setMsg(ok)
      setProgress(await api.getProgress(progress.task_id))
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="活动进度"
        description="按任务 ID 或业务幂等键查询进度，支持暂停、恢复、取消与失败重推。"
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <PanelTitle>查询进度</PanelTitle>
        <form onSubmit={onLookup}>
          <div className="flex max-w-3xl flex-wrap items-end gap-3">
            <Field label="任务 ID 或业务幂等键" noMargin className="min-w-[14rem] flex-[1_1_16rem]">
              <Input
                className="font-mono text-sm"
                value={lookup}
                onChange={(e) => setLookup(e.target.value)}
                placeholder="1 或 camp-xxx"
              />
            </Field>
            <BtnRow className="shrink-0">
              <Button variant="ink" type="submit" disabled={busy}>
                查询
              </Button>
              <ButtonLink to="/campaigns" variant="ghost">
                去创建活动
              </ButtonLink>
              <label className="inline-flex cursor-pointer items-center gap-2 self-center">
                <Chip tone="muted">
                  <input
                    type="checkbox"
                    checked={autoRefresh}
                    onChange={(e) => setAutoRefresh(e.target.checked)}
                  />
                  自动刷新
                </Chip>
              </label>
            </BtnRow>
          </div>
        </form>

        {progress ? (
          <div className="mt-5">
            <BtnRow className="mb-3">
              <StatusChip status={progress.status} />
              <Mono>{progress.progress_text}</Mono>
            </BtnRow>
            <ProgressBar percent={progress.progress_percent} />
            <div className="grid gap-3.5 md:grid-cols-3">
              <Stat label="成功">{progress.success_users}</Stat>
              <Stat label="失败">{progress.fail_users}</Stat>
              <Stat label="进行中">{progress.in_progress_users}</Stat>
            </div>
            <div className="mt-3 grid gap-3.5 md:grid-cols-3">
              <Stat label="抑制">{progress.suppressed_users ?? 0}</Stat>
              <Stat label="不可达">{progress.unreachable_users ?? 0}</Stat>
              <Stat label="取消">{progress.cancelled_users}</Stat>
            </div>
            <p className="mt-4 text-sm text-muted">
              任务 #{progress.task_id} · {progress.biz_id} ·{' '}
              {channelLabel(progress.channel)}
              {(progress.channels ?? []).length > 1
                ? `（${(progress.channels ?? []).map(channelLabel).join('、')}）`
                : ''}{' '}
              · {channelModeLabel(progress.channel_mode)} · {priorityLabel(progress.priority)}
            </p>
            <BtnRow className="mt-4">
              <Button
                variant="ghost"
                type="button"
                disabled={busy}
                onClick={() =>
                  void api
                    .getProgress(progress.task_id)
                    .then(setProgress)
                    .catch((e) => setErr(e instanceof ApiError ? e.message : '刷新失败'))
                }
              >
                刷新
              </Button>
              <ButtonLink to={`/tasks/${progress.task_id}/subtasks`} variant="ink">
                查看子任务
              </ButtonLink>
              <ButtonLink to={`/ops/${progress.task_id}`} variant="ghost">
                投递分析
              </ButtonLink>
              <Button
                variant="ghost"
                type="button"
                disabled={busy}
                onClick={() => void runAction(() => api.pauseCampaign(progress.task_id), '已暂停')}
              >
                暂停
              </Button>
              <Button
                variant="ghost"
                type="button"
                disabled={busy}
                onClick={() => void runAction(() => api.resumeCampaign(progress.task_id), '已恢复')}
              >
                恢复
              </Button>
              <Button
                variant="danger"
                type="button"
                disabled={busy}
                onClick={() => void runAction(() => api.cancelCampaign(progress.task_id), '已取消')}
              >
                取消
              </Button>
              <Button
                variant="primary"
                type="button"
                disabled={busy}
                onClick={() => void runAction(() => api.retryCampaign(progress.task_id), '已触发重推')}
              >
                失败重推
              </Button>
            </BtnRow>
          </div>
        ) : (
          <Empty>输入任务 ID 或业务幂等键后查询进度</Empty>
        )}
      </Panel>
    </div>
  )
}
