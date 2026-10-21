// Left column: New, Home / Drafts / Find, the places, settings — the Pocket Calendar sidebar.
import { useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, Listing, Source } from '../lib/types'
import { sourceLabel } from '../lib/format'
import { Icon } from './icons'
import { NEW_TYPES, newIcon, PlaceIcon } from './NewFile'
import { useOnChange } from '../lib/events'
import { type Anchor, type MenuItem, Menu, rectOf, useToast } from './ui'

export function Sidebar({ newItems, onNavigate, onPalette }: { newItems: MenuItem[]; onNavigate: () => void; onPalette: () => void }) {
  const { sources, status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [open, setOpen] = useState(true)
  const loc = useLocation()
  const drafts = sources.find(s => s.kind === 'drafts')
  const places = sources.filter(s => s.kind !== 'drafts')
  // how many working copies wait in Drafts
  const [draftCount, setDraftCount] = useState(0)
  const countDrafts = () => { if (drafts) api.get<Listing>(`${V1}/sources/${drafts.id}/ls`).then(l => setDraftCount((l.entries || []).filter(e => !e.is_dir).length)).catch(() => {}) }
  useEffect(countDrafts, [drafts?.id, loc.pathname]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(countDrafts)
  const title = settings?.title || status?.title || 'Pocket Office'

  return (
    <aside className="sidebar" aria-label="Office">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Office' ? title : <>Pocket <b>Office</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={newItems} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        <NavLink to="/" end onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}><Icon name="home" size={16} />Home</NavLink>
        {drafts && <NavLink to={`/s/${drafts.id}`} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`} title="Working copies: dropped or opened from your device, until you save them to a place">
          <Icon name="pencil" size={16} />Drafts{draftCount > 0 && <span className="badge accent" title={`${draftCount} working ${draftCount === 1 ? 'copy' : 'copies'}`}>{draftCount}</span>}
        </NavLink>}
        <button type="button" className="sb-nav-item" onClick={onPalette}><Icon name="search" size={16} />Find a file<kbd className="sb-kbd">⌘K</kbd></button>
      </nav>

      <section className="sb-sec">
        <h4 className="sb-h">
          <button type="button" className="sb-h-btn" aria-expanded={open} onClick={() => setOpen(o => !o)}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />Places</button>
          <Link to="/settings/places" className="btn ghost icon-only xs" aria-label="Add a place" title="Add a place" onClick={onNavigate}><Icon name="plus" size={14} /></Link>
        </h4>
        {open && <ul className="cal-list">{places.map((s, i) => <PlaceRow key={s.id} s={s} n={i + 1} onNavigate={onNavigate} />)}</ul>}
        {open && places.length <= 1 && <Link to="/settings/places" className="sb-src-note" onClick={onNavigate}><Icon name="info" size={14} /><span>Add your Nextcloud, Pocket Documents, Pocket Files or a WebDAV folder.</span></Link>}
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('po:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function PlaceRow({ s, n, onNavigate }: { s: Source; n: number; onNavigate: () => void }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const make = async (type: string) => {
    onNavigate()
    try { const e = await api.post<Entry>(`${V1}/sources/${s.id}/new`, { path: '', type }); navigate(`/edit/${s.id}${qs({ path: e.path })}`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const items: MenuItem[] = [
    { label: 'Open', icon: 'folder-open', onSelect: () => { onNavigate(); navigate(`/s/${s.id}`) } },
    ...(s.writable ? ['sep' as const, { heading: `New in ${s.name}` }, ...NEW_TYPES.map(t => ({ label: t.label, icon: newIcon(t.kind), onSelect: () => make(t.type) }))] : []),
    'sep',
    { label: 'Manage places', icon: 'settings', onSelect: () => { onNavigate(); navigate('/settings/places') } },
  ]
  return (
    <li className="cal-row place-row">
      <NavLink to={`/s/${s.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={`${s.name} — ${sourceLabel[s.kind]} · ${s.where}${n <= 9 ? ` (${n})` : ''}`}>
        <PlaceIcon kind={s.kind} size={22} />
        <span className="cal-name ellipsis">{s.name}</span>
        {s.error ? <span className="src-warn" title={s.error}><Icon name="alert" size={14} /></span> : null}
      </NavLink>
      <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${s.name}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={220} />}
    </li>
  )
}
