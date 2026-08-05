import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ChannelMode, ChannelType, Priority, ProgressView, Template } from '../api/types'
import { StatusChip } from '../components/StatusChip'

export function CampaignsPage() {
  const [searchParams] = useSearchParams()
  const [templates, setTemplates] = useState<Template[]>([])
  const [channels, setChannels] = useState<ChannelType[]>([])
  const [progress, setProgress] = useState<ProgressView | null>(null)
  const [lookup, setLookup] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)

  const [form, setForm] = useState({
    biz_id: `camp-${Date.now()}`,
    biz_scene: 'demo',
    title: 'Demo 投放',
    template_id: '',
    audience_ref: 'demo',
    audience_total: '20',
    channel: 'inbox' as ChannelType,
    channelsText: '',
    channel_mode: 'single' as ChannelMode,
    priority: 'normal' as Priority,
    pace_qps: '',
  })

  useEffect(() => {
    void (async () => {
      try {
        const [tpl, ch] = await Promise.all([
          api.listTemplates({ status: 'approved', page_size: 100 }),
          api.listChannels(),
        ])
        setTemplates(tpl.items ?? [])
        setChannels(ch.channels ?? [])
        if ((tpl.items?.length ?? 0) > 0) {
          setForm((f) => ({ ...f, template_id: f.template_id || tpl.items[0].code }))
        }
      } catch (e) {
        setErr(e instanceof ApiError ? e.message : '初始化失败')
      }
    })()
  }, [])

  useEffect(() => {
    const task = searchParams.get('task')
    if (!task) return
    setLookup(task)
    if (/^\d+$/.test(task)) {
      void (async () => {
        try {
          const p = await api.getProgress(Number(task))
          setProgress(p)
        } catch (e) {
          setErr(e instanceof ApiError ? e.message : '加载任务进度失败')
        }
      })()
    }
  }, [searchParams])

  useEffect(() => {
    if (!autoRefresh || !progress || progress.finished) return
    const timer = window.setInterval(() => {
      void refreshProgress(progress.task_id)
    }, 2000)
    return () => window.clearInterval(timer)
  }, [autoRefresh, progress])

  const channelOptions = useMemo(() => channels, [channels])

  async function refreshProgress(id: number) {
    try {
      const p = await api.getProgress(id)
      setProgress(p)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '刷新进度失败')
    }
  }

  async function runAction(action: () => Promise<unknown>, ok: string) {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await action()
      setMsg(ok)
      if (progress) await refreshProgress(progress.task_id)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    const multi = form.channelsText
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean) as ChannelType[]

    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await api.createCampaign({
        biz_id: form.biz_id,
        biz_scene: form.biz_scene,
        title: form.title,
        template_id: form.template_id,
        audience_ref: form.audience_ref,
        channel: multi.length ? undefined : form.channel,
        channels: multi.length ? multi : undefined,
        channel_mode: multi.length > 1 ? form.channel_mode || 'fallback' : 'single',
        priority: form.priority || 'normal',
        audience_extra: {
          total: Number(form.audience_total) || 20,
        },
        pace_qps: form.pace_qps ? Number(form.pace_qps) : undefined,
      })
      setMsg(`活动已创建 task_id=${res.task_id}`)
      setLookup(String(res.task_id))
      await refreshProgress(res.task_id)
      setForm((f) => ({ ...f, biz_id: `camp-${Date.now()}` }))
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  async function onLookup(e: FormEvent) {
    e.preventDefault()
    const q = lookup.trim()
    if (!q) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      if (/^\d+$/.test(q)) {
        await refreshProgress(Number(q))
      } else {
        const p = await api.getProgressByBiz(q)
        setProgress(p)
      }
      setMsg('已加载活动进度')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '查询失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <div className="page-head">
        <div>
          <h1>活动投放</h1>
          <p>创建营销/事务活动，按 task_id 或 biz_id 查询进度，支持暂停、恢复、取消与失败重推。</p>
        </div>
      </div>

      {err ? <div className="toast toast-error">{err}</div> : null}
      {msg ? <div className="toast toast-ok">{msg}</div> : null}

      <div className="grid-2">
        <form className="panel" onSubmit={onCreate}>
          <h2>创建活动</h2>
          <div className="field">
            <label>biz_id（幂等键）</label>
            <input
              required
              className="mono"
              value={form.biz_id}
              onChange={(e) => setForm({ ...form, biz_id: e.target.value })}
            />
          </div>
          <div className="field">
            <label>标题</label>
            <input
              required
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
            />
          </div>
          <div className="field">
            <label>biz_scene</label>
            <input
              required
              value={form.biz_scene}
              onChange={(e) => setForm({ ...form, biz_scene: e.target.value })}
            />
            <small>demo/dev 使用内置 Demo 人群；txn 等可走高优映射</small>
          </div>
          <div className="field">
            <label>模板 code（须 approved）</label>
            <select
              required
              value={form.template_id}
              onChange={(e) => setForm({ ...form, template_id: e.target.value })}
            >
              <option value="" disabled>
                选择模板
              </option>
              {templates.map((t) => (
                <option key={t.id} value={t.code}>
                  {t.code} · {t.name}
                </option>
              ))}
            </select>
            {templates.length === 0 ? <small>暂无已通过模板，请先到模板中心审核</small> : null}
          </div>
          <div className="field">
            <label>audience_ref</label>
            <input
              required
              value={form.audience_ref}
              onChange={(e) => setForm({ ...form, audience_ref: e.target.value })}
            />
          </div>
          <div className="field">
            <label>Demo 人群总量 audience_extra.total</label>
            <input
              type="number"
              min={1}
              value={form.audience_total}
              onChange={(e) => setForm({ ...form, audience_total: e.target.value })}
            />
          </div>
          <div className="field">
            <label>主渠道 channel</label>
            <select value={form.channel} onChange={(e) => setForm({ ...form, channel: e.target.value })}>
              {channelOptions.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>多渠道 channels（逗号分隔，优先于 channel）</label>
            <input
              className="mono"
              placeholder="sms,inbox"
              value={form.channelsText}
              onChange={(e) => setForm({ ...form, channelsText: e.target.value })}
            />
          </div>
          <div className="field">
            <label>channel_mode</label>
            <select
              value={form.channel_mode}
              onChange={(e) => setForm({ ...form, channel_mode: e.target.value as ChannelMode })}
            >
              <option value="single">single</option>
              <option value="fallback">fallback</option>
              <option value="parallel">parallel</option>
            </select>
          </div>
          <div className="field">
            <label>priority</label>
            <select
              value={form.priority}
              onChange={(e) => setForm({ ...form, priority: e.target.value as Priority })}
            >
              <option value="normal">normal</option>
              <option value="high">high</option>
            </select>
          </div>
          <div className="field">
            <label>pace_qps（可选）</label>
            <input
              type="number"
              min={0}
              value={form.pace_qps}
              onChange={(e) => setForm({ ...form, pace_qps: e.target.value })}
            />
          </div>
          <button className="btn btn-primary" type="submit" disabled={busy || !form.template_id}>
            创建并开始拆分
          </button>
        </form>

        <div className="panel">
          <h2>查询进度</h2>
          <form onSubmit={onLookup}>
            <div className="field">
              <label>task_id 或 biz_id</label>
              <input
                className="mono"
                value={lookup}
                onChange={(e) => setLookup(e.target.value)}
                placeholder="1 或 camp-xxx"
              />
            </div>
            <div className="btn-row">
              <button className="btn btn-ink" type="submit" disabled={busy}>
                查询
              </button>
              <label className="chip chip-muted" style={{ cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={autoRefresh}
                  onChange={(e) => setAutoRefresh(e.target.checked)}
                />
                自动刷新
              </label>
            </div>
          </form>

          {progress ? (
            <div style={{ marginTop: '1.25rem' }}>
              <div className="btn-row" style={{ marginBottom: '0.75rem' }}>
                <StatusChip status={progress.status} />
                <span className="mono">{progress.progress_text}</span>
              </div>
              <div className="progress-track">
                <div className="progress-bar" style={{ width: `${progress.progress_percent}%` }} />
              </div>
              <div className="grid-3">
                <div className="stat">
                  <div className="label">成功</div>
                  <div className="value">{progress.success_users}</div>
                </div>
                <div className="stat">
                  <div className="label">失败</div>
                  <div className="value">{progress.fail_users}</div>
                </div>
                <div className="stat">
                  <div className="label">进行中</div>
                  <div className="value">{progress.in_progress_users}</div>
                </div>
              </div>
              <div className="grid-3" style={{ marginTop: '0.75rem' }}>
                <div className="stat">
                  <div className="label">抑制</div>
                  <div className="value">{progress.suppressed_users ?? 0}</div>
                </div>
                <div className="stat">
                  <div className="label">不可达</div>
                  <div className="value">{progress.unreachable_users ?? 0}</div>
                </div>
                <div className="stat">
                  <div className="label">取消</div>
                  <div className="value">{progress.cancelled_users}</div>
                </div>
              </div>
              <p className="mono" style={{ color: 'var(--muted)', marginTop: '1rem' }}>
                task=#{progress.task_id} · {progress.biz_id} · {progress.channel} ·{' '}
                {(progress.channels ?? []).join(',') || '-'} · {progress.channel_mode} ·{' '}
                {progress.priority}
              </p>
              <div className="btn-row" style={{ marginTop: '1rem' }}>
                <button
                  className="btn btn-ghost"
                  type="button"
                  disabled={busy}
                  onClick={() => void refreshProgress(progress.task_id)}
                >
                  刷新
                </button>
                <Link className="btn btn-ink" to={`/tasks/${progress.task_id}/subtasks`}>
                  查看子任务
                </Link>
                <button
                  className="btn btn-ghost"
                  type="button"
                  disabled={busy}
                  onClick={() => void runAction(() => api.pauseCampaign(progress.task_id), '已暂停')}
                >
                  暂停
                </button>
                <button
                  className="btn btn-ghost"
                  type="button"
                  disabled={busy}
                  onClick={() => void runAction(() => api.resumeCampaign(progress.task_id), '已恢复')}
                >
                  恢复
                </button>
                <button
                  className="btn btn-danger"
                  type="button"
                  disabled={busy}
                  onClick={() => void runAction(() => api.cancelCampaign(progress.task_id), '已取消')}
                >
                  取消
                </button>
                <button
                  className="btn btn-primary"
                  type="button"
                  disabled={busy}
                  onClick={() => void runAction(() => api.retryCampaign(progress.task_id), '已触发重推')}
                >
                  失败重推
                </button>
              </div>
            </div>
          ) : (
            <div className="empty">创建或查询后展示进度</div>
          )}
        </div>
      </div>
    </div>
  )
}
