// ⌘K / "/" — find a file by name across every place, jump to a place, or run a command.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Recent, SearchHit } from '../lib/types'
import { ago, sourceLabel } from '../lib/format'
import { Icon } from './icons'
import { PlaceIcon } from './NewFile'
import { KindBadge, Spinner, useDebounce } from './ui'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

const dirOf = (p: string) => p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : ''

export function CommandPalette({ onClose, commands }: { onClose: () => void; commands: PaletteCommand[] }) {
  const { sources } = useApp()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [hi, setHi] = useState(0)
  const [recent, setRecent] = useState<Recent[]>([])
  const [hits, setHits] = useState<SearchHit[]>([])
  const [busy, setBusy] = useState(false)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => { api.get<Recent[]>(`${V1}/recent`).then(setRecent).catch(() => {}) }, [])
  useEffect(() => {
    if (dq.trim().length < 2) { setHits([]); return }
    let live = true
    setBusy(true)
    api.get<SearchHit[]>(`${V1}/search${qs({ q: dq })}`).then(h => { if (live) setHits(h || []) }).catch(() => { if (live) setHits([]) }).finally(() => { if (live) setBusy(false) })
    return () => { live = false }
  }, [dq])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: React.ReactNode; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    const n = q.trim().toLowerCase()
    const words = n.split(/\s+/).filter(Boolean)
    const hit = (text: string) => words.every(w => text.toLowerCase().includes(w))
    const open = (sid: number, path: string) => () => navigate(`/edit/${sid}${qs({ path })}`)
    if (n.length >= 2) {
      for (const h of hits) out.push({ key: `f${h.source_id}:${h.path}`, group: 'Files', icon: <KindBadge kind={h.kind} ext={h.ext} size="sm" />, label: h.name, sub: [h.source_name, dirOf(h.path)].filter(Boolean).join(' · '), hint: <span className="pal-when">{ago(h.modified)}</span>, run: open(h.source_id, h.path) })
    } else if (!n) {
      for (const r of recent.slice(0, 6)) out.push({ key: `r${r.source_id}:${r.path}`, group: 'Recent', icon: <KindBadge kind={r.kind} ext={r.ext} size="sm" />, label: r.name, sub: [r.source_name, dirOf(r.path)].filter(Boolean).join(' · '), hint: <span className="pal-when">{ago(r.opened_at)}</span>, run: open(r.source_id, r.path) })
    }
    sources.forEach(s => {
      if (n && !hit(`${s.name} ${sourceLabel[s.kind]} ${s.where}`)) return
      out.push({ key: `p${s.id}`, group: 'Places', icon: <PlaceIcon kind={s.kind} size={20} />, label: s.name, sub: `${sourceLabel[s.kind]} · ${s.where}`, run: () => navigate(`/s/${s.id}`) })
    })
    for (const c of commands) {
      if (n && !hit(`${c.label} ${c.keywords || ''}`)) continue
      out.push({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint ? <kbd>{c.hint}</kbd> : undefined, run: c.run })
    }
    return out
  }, [q, hits, recent, sources, commands, navigate])
  useEffect(() => setHi(0), [q, hits])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  const searching = q.trim().length >= 2 && (busy || dq !== q)
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Find a file, places and commands">
        <div className="pal-input">
          {searching ? <Spinner size={16} /> : <Icon name="search" size={18} />}
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a document, spreadsheet or presentation…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {q.trim().length >= 2 && !searching && hits.length === 0 && <div className="pal-empty">No file with that name in your places.</div>}
          {rows.length === 0 && q.trim().length < 2 && <div className="pal-empty">Nothing matches.</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label ellipsis">{r.label}</span>{r.sub && <span className="pal-sub ellipsis">{r.sub}</span>}</span>
                  {r.hint}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
