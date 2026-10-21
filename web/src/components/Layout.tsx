import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { sourceIcon } from '../lib/format'
import { ErrorBoundary } from './ErrorBoundary'
import { Search } from './Search'
import { GlobalDrop } from './DropZone'

export default function Layout() {
  const { user, settings, status, theme, toggleTheme, setUser, sources } = useApp()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState(false)
  const navigate = useNavigate()
  const loc = useLocation()
  const title = settings?.title || 'Pocket Office'

  useEffect(() => { setOpen(false) }, [loc.pathname, loc.search])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setSearch(true) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  const logout = async () => { await api.post(`${V1}/auth/logout`); setUser(null); navigate('/login') }

  return (
    <div className="app">
      <aside className={`sidebar ${open ? 'open' : ''}`}>
        <div className="brand"><span className="logo"><img src="/favicon.svg" alt="" /></span><span className="brand-name">{title}</span></div>
        <div className="side-actions">
          <button className="btn grow" onClick={() => setSearch(true)} title="Find a file (⌘K)"><span>⌕</span> Find a file… <kbd>⌘K</kbd></button>
        </div>
        <nav className="side-nav">
          <NavLink to="/" end className="nav-item"><span className="ico">⌂</span>Home</NavLink>
        </nav>
        <div className="side-scroll">
          <div className="side-head"><span>Places</span><NavLink to="/settings/sources" className="btn ghost xs" title="Add a place">＋</NavLink></div>
          <nav className="side-nav">
            {sources.map(s => <NavLink key={s.id} to={`/s/${s.id}`} className="nav-item" title={s.where}><span className="ico">{sourceIcon[s.kind]}</span><span className="ellipsis grow">{s.name}</span></NavLink>)}
          </nav>
        </div>
        <nav className="side-nav bottom">
          <NavLink to="/settings" className="nav-item"><span className="ico">⚙</span>Settings</NavLink>
        </nav>
        <div className="foot">
          <span className="row" title={user?.email}><span className="avatar">{(user?.name || user?.email || '?').slice(0, 1).toUpperCase()}</span><span className="foot-id"><span>{user?.name || user?.email}</span>{status?.version && <a className="ver" href="https://github.com/abjelosevic88/pocket-homelab-office/releases" target="_blank" rel="noreferrer">v{status.version.replace(/^v/, '')}</a>}</span></span>
          <span className="row">
            <button className="btn ghost sm" onClick={toggleTheme} title="Toggle theme">{theme === 'dark' ? '☀' : '☾'}</button>
            {status?.auth_mode === 'local' && <button className="btn ghost sm" onClick={logout} title="Sign out">⎋</button>}
          </span>
        </div>
      </aside>
      {open && <div className="scrim" onClick={() => setOpen(false)} />}
      <div className="main">
        <header className="topbar">
          <button className="btn ghost icon" onClick={() => setOpen(o => !o)} aria-label="Menu">☰</button>
          <span className="topbar-title">{title}</span>
          <span className="spacer" />
          <button className="btn ghost icon" onClick={() => setSearch(true)} aria-label="Find">⌕</button>
        </header>
        <main className="content"><ErrorBoundary key={loc.pathname.split('/')[1]} where={loc.pathname}><Outlet /></ErrorBoundary></main>
      </div>
      {search && <Search onClose={() => setSearch(false)} />}
      <GlobalDrop />
    </div>
  )
}
