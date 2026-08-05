import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { CampaignListItem, TaskStatus } from '../api/types'
import { StatusChip } from '../components/StatusChip'

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

export function TasksPage() {
  const [items, setItems] = useState<CampaignListItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<TaskStatus | ''>('')
  const [keyword, setKeyword] = useState('')
  const [bizScene, setBizScene] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const pageSize = 20

  const load = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      const res = await api.listCampaigns({
        status,
        keyword: keyword.trim() || undefined,
        biz_scene: bizScene.trim() || undefined,
        page,
        page_size: pageSize,
      })
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载任务列表失败')
    } finally {
      setBusy(false)
    }
  }, [bizScene, keyword, page, status])

  useEffect(() => {
    void load()
  }, [load])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div>
      <div className="page-head">
        <div>
          <h1>任务列表</h1>
          <p>查看主任务状态，进入子任务页追踪每个分片执行情况。共 {total} 条。</p>
        </div>
        <Link className="btn btn-primary" to="/campaigns">
          创建活动
        </Link>
      </div>

      {err ? <div className="toast toast-error">{err}</div> : null}

      <div className="panel">
        <div className="grid-3">
          <div className="field">
            <label>状态</label>
            <select
              value={status}
              onChange={(e) => {
                setPage(1)
                setStatus(e.target.value as TaskStatus | '')
              }}
            >
              <option value="">全部</option>
              <option value="pending">pending</option>
              <option value="running">running</option>
              <option value="paused">paused</option>
              <option value="success">success</option>
              <option value="partial">partial</option>
              <option value="failed">failed</option>
              <option value="cancelled">cancelled</option>
              <option value="retrying">retrying</option>
            </select>
          </div>
          <div className="field">
            <label>biz_scene</label>
            <input
              value={bizScene}
              onChange={(e) => setBizScene(e.target.value)}
              placeholder="demo"
            />
          </div>
          <div className="field">
            <label>关键词（biz_id / title）</label>
            <input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="camp- / Demo"
            />
          </div>
        </div>
        <div className="btn-row">
          <button
            className="btn btn-ink"
            type="button"
            disabled={busy}
            onClick={() => {
              setPage(1)
              void load()
            }}
          >
            查询
          </button>
          <button className="btn btn-ghost" type="button" disabled={busy} onClick={() => void load()}>
            刷新
          </button>
        </div>
      </div>

      <div className="panel">
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>标题 / biz_id</th>
                <th>场景</th>
                <th>渠道</th>
                <th>状态</th>
                <th>用户</th>
                <th>子任务</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((t) => (
                <tr key={t.id}>
                  <td className="mono">{t.id}</td>
                  <td>
                    <div>{t.title}</div>
                    <small className="mono" style={{ color: 'var(--muted)' }}>
                      {t.biz_id}
                    </small>
                  </td>
                  <td>{t.biz_scene}</td>
                  <td className="mono">
                    {(t.channels && t.channels.length > 0 ? t.channels : [t.channel]).join(', ')}
                    <div>
                      <small style={{ color: 'var(--muted)' }}>
                        {t.channel_mode} · {t.priority}
                      </small>
                    </div>
                  </td>
                  <td>
                    <StatusChip status={t.status} />
                  </td>
                  <td className="mono">
                    {t.success_count}/{t.fail_count}/{t.total_count}
                    <div>
                      <small style={{ color: 'var(--muted)' }}>成功/失败/总量</small>
                    </div>
                  </td>
                  <td className="mono">
                    {t.sub_task_done}/{t.sub_task_total}
                  </td>
                  <td>
                    <small>{formatTime(t.created_at)}</small>
                  </td>
                  <td>
                    <div className="btn-row">
                      <Link className="btn btn-primary" to={`/tasks/${t.id}/subtasks`}>
                        子任务
                      </Link>
                      <Link className="btn btn-ghost" to={`/campaigns?task=${t.id}`}>
                        进度
                      </Link>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {items.length === 0 ? <div className="empty">暂无任务</div> : null}
        </div>

        <div className="btn-row" style={{ marginTop: '1rem', justifyContent: 'space-between' }}>
          <span style={{ color: 'var(--muted)' }}>
            第 {page} / {totalPages} 页
          </span>
          <div className="btn-row">
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy || page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              上一页
            </button>
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy || page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
