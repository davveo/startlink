import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { Notification } from '../api/types'
import { Can } from '../auth/Can'
import { Perm } from '../auth/permissions'
import {
  BtnRow,
  Button,
  Empty,
  Mono,
  PageHead,
  Panel,
  Toast,
} from '../components/ui'
import { useClampPage, useRequestSeq } from '../lib/async'
import { cn } from '../lib/cn'

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

function levelTone(level: string) {
  switch (level) {
    case 'success':
      return 'border-ok/25 bg-ok/8'
    case 'warning':
      return 'border-amber/30 bg-amber/10'
    case 'error':
      return 'border-rose/25 bg-rose/8'
    default:
      return 'border-line bg-white/70'
  }
}

export function NotificationsPage() {
  const [items, setItems] = useState<Notification[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const pageSize = 20

  const seq = useRequestSeq()

  const load = useCallback(async () => {
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const res = await api.listNotifications({
        unread_only: unreadOnly,
        page,
        page_size: pageSize,
      })
      if (!seq.isLatest(s)) return
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载通知失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [page, seq, unreadOnly])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const onChanged = () => void load()
    window.addEventListener('starlink:notifications-changed', onChanged)
    return () => window.removeEventListener('starlink:notifications-changed', onChanged)
  }, [load])

  async function markRead(id: number) {
    setBusy(true)
    setErr('')
    try {
      await api.markNotificationRead(id)
      // 本页已监听该事件并重新拉列表，这里再 load 一次就是重复请求
      window.dispatchEvent(new Event('starlink:notifications-changed'))
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '标记已读失败')
    } finally {
      setBusy(false)
    }
  }

  async function markAll() {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await api.markAllNotificationsRead()
      setMsg(res.updated > 0 ? `已将 ${res.updated} 条标记为已读` : '没有未读通知')
      window.dispatchEvent(new Event('starlink:notifications-changed'))
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '全部已读失败')
    } finally {
      setBusy(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  useClampPage(page, total, pageSize, setPage)

  return (
    <div>
      <PageHead
        title="通知中心"
        description="任务完成、失败或取消时产生的运营通知。可跳转到对应活动的进度与分析页。"
        actions={
          <BtnRow>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => void load()}>
              刷新
            </Button>
            <Can perm={Perm.NotificationRead}>
              <Button type="button" variant="ink" disabled={busy} onClick={() => void markAll()}>
                全部已读
              </Button>
            </Can>
          </BtnRow>
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <label className="inline-flex items-center gap-2 text-sm text-ink-soft">
          <input
            type="checkbox"
            checked={unreadOnly}
            onChange={(e) => {
              setPage(1)
              setUnreadOnly(e.target.checked)
            }}
          />
          仅未读
        </label>
        <span className="text-sm text-muted">共 {total} 条</span>
      </div>

      <Panel>
        {items.length === 0 ? (
          <Empty>{busy ? '加载中…' : '暂无通知'}</Empty>
        ) : (
          <ul className="m-0 grid list-none gap-3 p-0">
            {items.map((n) => {
              const unread = !n.read_at
              return (
                <li
                  key={n.id}
                  className={cn(
                    'rounded-lg border px-4 py-3 transition',
                    levelTone(n.level),
                    unread && 'shadow-[inset_3px_0_0_0_var(--color-teal)]',
                  )}
                >
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-base font-semibold">{n.title}</h3>
                        {unread ? (
                          <span className="rounded-full bg-teal/15 px-2 py-0.5 text-[11px] font-semibold text-teal-deep">
                            未读
                          </span>
                        ) : null}
                      </div>
                      <p className="mt-1 mb-0 text-sm text-ink-soft">{n.body}</p>
                      <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted">
                        <span>{formatTime(n.created_at)}</span>
                        {n.related_task_id ? (
                          <span>
                            任务 <Mono>#{n.related_task_id}</Mono>
                          </span>
                        ) : null}
                      </div>
                    </div>
                    <div className="flex shrink-0 flex-wrap gap-2">
                      {n.related_task_id ? (
                        <>
                          <Link
                            className="rounded-full border border-line px-3 py-1.5 text-xs font-semibold hover:bg-white/80"
                            to={`/progress?task=${n.related_task_id}`}
                            onClick={() => {
                              if (unread) void markRead(n.id)
                            }}
                          >
                            进度
                          </Link>
                          <Link
                            className="rounded-full border border-line px-3 py-1.5 text-xs font-semibold hover:bg-white/80"
                            to={`/ops/${n.related_task_id}`}
                            onClick={() => {
                              if (unread) void markRead(n.id)
                            }}
                          >
                            分析
                          </Link>
                        </>
                      ) : null}
                      {unread ? (
                        <Can perm={Perm.NotificationRead}>
                          <Button
                            type="button"
                            variant="ghost"
                            className="px-3 py-1.5 text-xs"
                            disabled={busy}
                            onClick={() => void markRead(n.id)}
                          >
                            标为已读
                          </Button>
                        </Can>
                      ) : null}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </Panel>

      {totalPages > 1 ? (
        <BtnRow className="mt-4">
          <Button type="button" variant="ghost" disabled={busy || page <= 1} onClick={() => setPage((p) => p - 1)}>
            上一页
          </Button>
          <span className="self-center text-sm text-muted">
            {page} / {totalPages}
          </span>
          <Button
            type="button"
            variant="ghost"
            disabled={busy || page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </BtnRow>
      ) : null}
    </div>
  )
}
