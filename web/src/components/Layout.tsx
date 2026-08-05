import { NavLink, Outlet } from 'react-router-dom'

export function Layout() {
  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <div className="brand-mark">
            STAR<span>LINK</span>
          </div>
          <div className="brand-sub">推送运营台</div>
        </div>
        <nav className="nav">
          <NavLink to="/" end>
            概览
          </NavLink>
          <NavLink to="/tasks">任务</NavLink>
          <NavLink to="/templates">模板</NavLink>
          <NavLink to="/campaigns">活动</NavLink>
        </nav>
      </header>
      <main className="main">
        <Outlet />
      </main>
      <footer className="footer">Starlink Console · API via /api/v1</footer>
    </div>
  )
}
