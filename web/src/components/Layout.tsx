import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { cn } from '../lib/cn'
import { Button } from './ui'

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'rounded-full px-3.5 py-2 text-sm transition',
    isActive ? 'bg-teal/18 text-white' : 'text-white/70 hover:bg-teal/18 hover:text-white',
  )

export function Layout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  async function onLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr_auto]">
      <header className="sticky top-0 z-20 flex flex-wrap items-center justify-between gap-6 bg-ink px-6 py-4 text-[#f7f8fa]">
        <div className="flex items-baseline gap-2.5">
          <div className="font-display text-[1.35rem] font-extrabold tracking-wide">
            STAR<span className="text-teal">LINK</span>
          </div>
          <div className="text-sm text-white/55">推送运营台</div>
        </div>
        <nav className="flex flex-wrap items-center gap-1">
          <NavLink to="/" end className={navClass}>
            概览
          </NavLink>
          <NavLink to="/tasks" className={navClass}>
            任务
          </NavLink>
          <NavLink to="/ops" className={navClass}>
            分析
          </NavLink>
          <NavLink to="/templates" className={navClass}>
            模板
          </NavLink>
          <NavLink to="/campaigns" className={navClass}>
            活动
          </NavLink>
          <div className="ml-2 flex items-center gap-2 border-l border-white/15 pl-3">
            <span className="text-sm text-white/70">{user?.username}</span>
            <Button
              type="button"
              variant="ghost"
              className="border-white/20 px-3 py-1.5 text-xs text-white hover:enabled:bg-white/10"
              onClick={() => void onLogout()}
            >
              退出
            </Button>
          </div>
        </nav>
      </header>
      <main className="mx-auto w-[min(1120px,calc(100%-2rem))] py-8 pb-12">
        <Outlet />
      </main>
      <footer className="px-6 pb-6 pt-4 text-center text-sm text-muted">
        Starlink Console · API via /api/v1
      </footer>
    </div>
  )
}
