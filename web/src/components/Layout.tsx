import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { cn } from '../lib/cn'
import { Button } from './ui'

const navItems = [
  { to: '/', end: true, label: '概览' },
  { to: '/tasks', label: '任务' },
  { to: '/templates', label: '模板' },
] as const

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'block rounded-lg px-3 py-2.5 text-sm font-medium transition',
    isActive
      ? 'bg-teal/20 text-white shadow-[inset_3px_0_0_0_var(--color-teal)]'
      : 'text-white/65 hover:bg-white/10 hover:text-white',
  )

export function Layout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  async function onLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

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
          {navItems.map((item) => (
            <NavLink key={item.to} to={item.to} end={'end' in item ? item.end : undefined} className={navClass}>
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="border-t border-white/10 px-4 py-4">
          <div className="mb-2 truncate text-sm text-white/70" title={user?.username}>
            {user?.username}
          </div>
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
