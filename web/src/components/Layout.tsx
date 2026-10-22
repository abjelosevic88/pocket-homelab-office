// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useMatch, useNavigate, useSearchParams } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ErrorBoundary } from './ErrorBoundary'
import { GlobalDrop, useDraftDrop } from './DropZone'
import { NEW_TYPES, newIcon, PlaceIcon, useNewFile } from './NewFile'
import { Avatar, type Anchor, Menu, type MenuItem, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'po-sidebar'

export default function Layout() {
  const { sources } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)
  const { create } = useNewFile()
  const { openFiles, busy: opening } = useDraftDrop()
  const fileInput = useRef<HTMLInputElement>(null)
  const openDevice = () => { closeDrawer(); fileInput.current?.click() }
  const places = sources.filter(s => s.kind !== 'drafts')
  const drafts = sources.find(s => s.kind === 'drafts')
  // a folder view has its own New (into that folder), so no FAB there
  const inPlace = loc.pathname.startsWith('/s/')

  const act = useRef({ create, openDevice, places })
  act.current = { create, openDevice, places }
  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('po:shortcuts', open)
    return () => window.removeEventListener('po:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t?.closest?.('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const nt = NEW_TYPES.find(n => n.key === e.key)
      if (nt) { e.preventDefault(); act.current.create(nt.type); return }
      if (/^[1-9]$/.test(e.key)) { const p = act.current.places[Number(e.key) - 1]; if (p) go(`/s/${p.id}`); return }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'o': e.preventDefault(); act.current.openDevice(); break
        case 'h': go('/'); break
        case 'w': if (drafts) go(`/s/${drafts.id}`); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const newItems: MenuItem[] = [
    { heading: 'New in My files' },
    ...NEW_TYPES.map(n => ({ label: n.label, icon: newIcon(n.kind), hint: n.key.toUpperCase(), onSelect: () => { closeDrawer(); create(n.type) } })),
    'sep',
    { label: opening ? 'Opening…' : 'Open a file from this device…', icon: 'upload', hint: 'O', disabled: opening, onSelect: openDevice },
  ]
  const commands: PaletteCommand[] = [
    ...NEW_TYPES.map(n => ({ id: 'new-' + n.type, label: `New ${n.label.toLowerCase()}`, icon: newIcon(n.kind), hint: n.key.toUpperCase(), keywords: `create blank ${n.type} ${n.kind}`, run: () => create(n.type) })),
    { id: 'open-device', label: 'Open a file from this device', icon: 'upload', hint: 'O', keywords: 'upload drop draft', run: openDevice },
    { id: 'home', label: 'Home', icon: 'home', hint: 'H', keywords: 'recent', run: () => navigate('/') },
    ...(drafts ? [{ id: 'drafts', label: 'Drafts', icon: 'pencil', hint: 'W', keywords: 'working copies', run: () => navigate(`/s/${drafts.id}`) }] : []),
    { id: 'add-place', label: 'Add a place', icon: 'plus', keywords: 'nextcloud webdav documents files connect', run: () => navigate('/settings/places') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
      {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
      <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
        <Sidebar newItems={newItems} onNavigate={closeDrawer} onPalette={() => { closeDrawer(); setPalette(true) }} />
      </div>
      <div className="main">
        <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} />
        <main className="content"><div className="content-inner"><ErrorBoundary key={loc.pathname.split('/')[1]} where={loc.pathname}><Outlet /></ErrorBoundary></div></main>
      </div>
      {mobile && !inPlace && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
      {fabMenu && <Menu anchor={fabMenu} items={newItems} align="right" width={250} onClose={() => setFabMenu(null)} />}
      <input ref={fileInput} type="file" multiple hidden onChange={e => { if (e.target.files) openFiles(e.target.files); e.target.value = '' }} />
      {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      <GlobalDrop />
    </div>
  )
}

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean }) {
  const { user, themeMode, setThemeMode, status, setUser, sources } = useApp()
  const navigate = useNavigate()
  const placeMatch = useMatch('/s/:id')
  const [params] = useSearchParams()
  const place = placeMatch ? sources.find(s => String(s.id) === placeMatch.params.id) : undefined
  const path = params.get('path') || ''
  const crumbs = place && path ? path.split('/') : []
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('po:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  const to = (p: string) => `/s/${place!.id}${qs({ path: p })}`
  // phones keep just the folder you are in (the page header says where it is)
  const shown = mobile ? crumbs.slice(-1) : crumbs
  const offset = crumbs.length - shown.length
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {place ? <>
          <PlaceIcon kind={place.kind} size={22} />
          {crumbs.length === 0 ? <span className="ellipsis">{place.name}</span> : <>
            {offset === 0 && <button type="button" className="crumb" onClick={() => navigate(to(''))} title={place.where}>{place.name}</button>}
            {shown.map((c, j) => {
              const i = j + offset
              const last = i === crumbs.length - 1
              return <span key={i} className="crumb-wrap">
                {(j > 0 || offset === 0) && <Icon name="chevron-right" size={15} className="crumb-sep" />}
                {last ? <span className="ellipsis">{c}</span> : <button type="button" className="crumb" onClick={() => navigate(to(crumbs.slice(0, i + 1).join('/')))}>{c}</button>}
              </span>
            })}
          </>}
        </> : placeMatch ? 'Places' : 'Home'}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Find a file (⌘K or /)"><Icon name="search" size={18} /></button>
      {mobile && <AskButton />}
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
