import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import type { Recent, SearchHit } from '../lib/types'
import { ago } from '../lib/format'
import { KindBadge, useDebounce } from './ui'

/** ⌘K: a name across every place; recent files while the box is empty. */
export function Search({ onClose }: { onClose: () => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [hits, setHits] = useState<SearchHit[]>([])
  const [recent, setRecent] = useState<Recent[]>([])
  const [busy, setBusy] = useState(false)
  const [sel, setSel] = useState(0)
  const navigate = useNavigate()
  const box = useRef<HTMLInputElement>(null)

  useEffect(() => { api.get<Recent[]>(`${V1}/recent`).then(setRecent).catch(() => {}) }, [])
  useEffect(() => {
    if (dq.trim().length < 2) { setHits([]); return }
    setBusy(true)
    api.get<SearchHit[]>(`${V1}/search${qs({ q: dq })}`).then(h => { setHits(h); setSel(0) }).catch(() => setHits([])).finally(() => setBusy(false))
  }, [dq])

  const rows = q.trim().length >= 2
    ? hits.map(h => ({ key: `${h.source_id}:${h.path}`, name: h.name, kind: h.kind, ext: h.ext, sub: `${h.source_name} · ${h.path}`, when: h.modified, go: () => navigate(`/edit/${h.source_id}${qs({ path: h.path })}`) }))
    : recent.map(r => ({ key: `${r.source_id}:${r.path}`, name: r.name, kind: r.kind, ext: r.ext, sub: `${r.source_name} · ${r.path}`, when: r.opened_at, go: () => navigate(`/edit/${r.source_id}${qs({ path: r.path })}`) }))

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSel(s => Math.min(rows.length - 1, s + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel(s => Math.max(0, s - 1)) }
    else if (e.key === 'Enter' && rows[sel]) { rows[sel].go(); onClose() }
  }

  return (
    <div className="modal-backdrop top" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="switcher" onKeyDown={onKey}>
        <input ref={box} autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a document, spreadsheet or presentation…" />
        <div className="switcher-rows">
          {rows.length === 0 && <div className="muted pad center">{busy ? 'Searching…' : q.trim().length >= 2 ? 'Nothing with that name' : 'Nothing opened yet'}</div>}
          {rows.map((r, i) => (
            <button key={r.key} className={`switcher-row ${i === sel ? 'sel' : ''}`} onMouseEnter={() => setSel(i)} onClick={() => { r.go(); onClose() }}>
              <KindBadge kind={r.kind} ext={r.ext} />
              <span className="grow"><span className="ellipsis">{r.name}</span><small className="muted">{r.sub}</small></span>
              <small className="muted">{ago(r.when)}</small>
            </button>
          ))}
        </div>
        <div className="switcher-foot muted small"><span>{q.trim().length >= 2 ? `${rows.length} found` : 'Recent'}</span><span><kbd>↑↓</kbd> <kbd>↵</kbd> <kbd>esc</kbd></span></div>
      </div>
    </div>
  )
}
