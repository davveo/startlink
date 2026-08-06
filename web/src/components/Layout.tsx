import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { Perm } from '../auth/permissions'
import { cn } from '../lib/cn'
import { Button } from './ui'

const navItems = [
  { to: '/', end: true, label: '运营概览', perm: Perm.MenuOverview },
  { to: '/tasks', label: '任务管理', perm: Perm.MenuTasks },
  { to: '/templates', label: '模板中心', perm: Perm.MenuTemplates },
  { to: '/notifications', label: '通知管理', perm: Perm.MenuNotifications },
  { to: '/audit-logs', label: '审计日志', perm: Perm.MenuAudit },
  { to: '/settings/roles', label: '角色配置', perm: Perm.MenuSettings },
  { to: '/settings/permissions', label: '权限管理', perm: Perm.MenuSettings },
  { to: '/settings/users', label: '用户管理', perm: Perm.MenuSettings },
] as const

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'block rounded-lg px-3 py-2.5 text-sm font-medium transition',
    isActive
      ? 'bg-teal/20 text-white shadow-[inset_3px_0_0_0_var(--color-teal)]'
      : 'text-white/65 hover:bg-white/10 hover:text-white',
  )

export function Layout() {
  const { user, logout, can } = useAuth()
  const navigate = useNavigate()
  const [unread, setUnread] = useState(0)

  useEffect(() => {
    let alive = true
    const refresh = async () => {
      try {
        const res = await api.unreadNotificationCount()
        if (alive) setUnread(res.count ?? 0)
      } catch {
        /* 未登录或接口暂不可用时忽略角标 */
      }
    }

    void refresh()

    // SSE 为主；断线由 EventSource 自动重连，重连时再拉一次未读作兜底
    let es: EventSource | null = null
    let softTimer: number | undefined

    const connect = () => {
      es?.close()
      es = new EventSource('/api/v1/notifications/stream', { withCredentials: true })
      const onPayload = (ev: MessageEvent) => {
        try {
          const data = JSON.parse(String(ev.data)) as { unread_count?: number; type?: string }
          if (typeof data.unread_count === 'number' && alive) {
            setUnread(data.unread_count)
          }
          if (data.type === 'notification') {
            window.dispatchEvent(new Event('starlink:notifications-changed'))
          }
        } catch {
          /* ignore malformed */
        }
      }
      es.addEventListener('notification', onPayload)
      es.addEventListener('unread', onPayload)
      es.onopen = () => {
        void refresh()
      }
      es.onerror = () => {
        // 浏览器会自动重连；保留弱轮询作 scheduler 跨进程兜底
      }
    }
    connect()

    // 弱化兜底：2 分钟拉一次（SSE 正常时几乎无感）
    softTimer = window.setInterval(() => void refresh(), 120_000)

    const onChanged = () => void refresh()
    window.addEventListener('starlink:notifications-changed', onChanged)
    return () => {
      alive = false
      es?.close()
      if (softTimer) window.clearInterval(softTimer)
      window.removeEventListener('starlink:notifications-changed', onChanged)
    }
  }, [])

  async function onLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  const visibleNav = navItems.filter((item) => can(item.perm))

  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-[220px] shrink-0 flex-col bg-ink text-[#f7f8fa]">
        <div className="border-b border-white/10 px-5 py-5">
          <div className="font-display text-[1.25rem] font-extrabold tracking-wide">
            STAR<span className="text-teal">LINK</span>
          </div>
          <div className="mt-1 text-xs text-white/50">推送运营台</div>
        </div>

        <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto px-3 py-4">
          {visibleNav.map((item) => (
            <NavLink key={item.to} to={item.to} end={'end' in item ? item.end : undefined} className={navClass}>
              <span className="inline-flex w-full items-center justify-between gap-2">
                <span>{item.label}</span>
                {item.to === '/notifications' && unread > 0 ? (
                  <span className="min-w-[1.25rem] rounded-full bg-teal px-1.5 py-0.5 text-center text-[10px] font-bold leading-none text-ink">
                    {unread > 99 ? '99+' : unread}
                  </span>
                ) : null}
              </span>
            </NavLink>
          ))}
        </nav>

        <div className="border-t border-white/10 px-4 py-4">
          <div className="mb-0.5 truncate text-sm text-white/70" title={user?.username}>
            {user?.username}
          </div>
          {user?.role ? (
            <div className="mb-2 text-[11px] text-white/40" title={user.permissions.join(', ')}>
              角色 · {user.role}
            </div>
          ) : null}
          <Button
            type="button"
            variant="ghost"
            className="w-full border-white/20 px-3 py-1.5 text-xs text-white hover:enabled:bg-white/10"
            onClick={() => void onLogout()}
          >
            退出
          </Button>
          <p className="mt-3 text-[11px] leading-snug text-white/35">Starlink · /api/v1</p>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <main className="mx-auto w-full max-w-[1120px] flex-1 px-6 py-8 pb-12 animate-rise">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
