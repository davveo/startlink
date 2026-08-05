import { NavLink, Outlet } from 'react-router-dom'
import { cn } from '../lib/cn'

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'rounded-full px-3.5 py-2 text-sm transition',
    isActive ? 'bg-teal/18 text-white' : 'text-white/70 hover:bg-teal/18 hover:text-white',
  )

export function Layout() {
  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr_auto]">
      <header className="sticky top-0 z-20 flex flex-wrap items-center justify-between gap-6 bg-ink px-6 py-4 text-[#f7f8fa]">
        <div className="flex items-baseline gap-2.5">
          <div className="font-display text-[1.35rem] font-extrabold tracking-wide">
            STAR<span className="text-teal">LINK</span>
          </div>
          <div className="text-sm text-white/55">推送运营台</div>
        </div>
        <nav className="flex flex-wrap gap-1">
          <NavLink to="/" end className={navClass}>
            概览
          </NavLink>
          <NavLink to="/tasks" className={navClass}>
            任务
          </NavLink>
          <NavLink to="/templates" className={navClass}>
            模板
          </NavLink>
          <NavLink to="/campaigns" className={navClass}>
            活动
          </NavLink>
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
