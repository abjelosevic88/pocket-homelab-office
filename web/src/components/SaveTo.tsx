import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, Listing } from '../lib/types'
import { sourceIcon, stem } from '../lib/format'
import { Field, Modal, useToast } from './ui'

/** Save a draft into a real place: pick the place, walk to a folder, name it. */
export function SaveTo({ path, name, onClose, onSaved }: { path: string; name: string; onClose: () => void; onSaved?: (sourceId: number, file: Entry) => void }) {
  const { sources } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const places = sources.filter(s => s.kind !== 'drafts')
  const [sid, setSid] = useState<number>(() => Number(localStorage.getItem('po-save-place')) || places[0]?.id || 0)
  const [dir, setDir] = useState('')
  const [dirs, setDirs] = useState<Entry[]>([])
  const [fname, setFname] = useState(name)
  const [keep, setKeep] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const place = places.find(p => p.id === sid)

  useEffect(() => {
    if (!sid || !place?.tree) { setDirs([]); return }
    api.get<Listing>(`${V1}/sources/${sid}/ls${qs({ path: dir })}`).then(l => setDirs(l.entries.filter(e => e.is_dir))).catch(() => setDirs([]))
  }, [sid, dir, place?.tree])
  useEffect(() => { setDir('') }, [sid])

  const save = async (overwrite = false) => {
    setBusy(true); setErr('')
    try {
      const r = await api.post<{ source: number; file: Entry }>(`${V1}/drafts/save`, { path, source_id: sid, dir, name: fname, keep, overwrite })
      localStorage.setItem('po-save-place', String(sid))
      toast(`Saved to ${place?.name}${dir ? ' / ' + dir : ''}`)
      onClose()
      if (onSaved) onSaved(r.source, r.file)
      else navigate(`/edit/${r.source}${qs({ path: r.file.path })}`, { replace: true })
    } catch (e) {
      const msg = (e as Error).message
      if (/already there/.test(msg) && !overwrite && confirm(`“${fname}” is already there. Replace it?`)) { setBusy(false); return save(true) }
      setErr(msg)
    } finally { setBusy(false) }
  }
  const crumbs = dir ? dir.split('/') : []

  return (
    <Modal title={`Save “${name}” to…`} onClose={onClose}>
      <form onSubmit={e => { e.preventDefault(); save() }} className="grid" style={{ gap: 14 }}>
        <Field label="Place" group>
          <div className="place-pick">
            {places.map(p => <button key={p.id} type="button" className={`chip ${p.id === sid ? 'active' : ''}`} onClick={() => setSid(p.id)}>{sourceIcon[p.kind]} {p.name}</button>)}
          </div>
        </Field>
        {place?.tree && <Field label="Folder" group>
          <div className="folder-pick">
            <div className="crumbs small">
              <button type="button" className="crumb" onClick={() => setDir('')}>{place.name}</button>
              {crumbs.map((c, i) => <span key={i} className="crumb-wrap"><span className="muted">/</span><button type="button" className="crumb" onClick={() => setDir(crumbs.slice(0, i + 1).join('/'))}>{c}</button></span>)}
            </div>
            <div className="folder-list">
              {dirs.length === 0 && <div className="muted small pad">No folders here — the file goes right here.</div>}
              {dirs.map(d => <button key={d.path} type="button" className="folder-row" onClick={() => setDir(d.path)}>📁 <span className="ellipsis">{d.name}</span><span className="muted">›</span></button>)}
            </div>
          </div>
        </Field>}
        {place && !place.tree && <div className="muted small">{place.name} has no folders; the file becomes a new document there.</div>}
        <Field label="File name"><input value={fname} onChange={e => setFname(e.target.value)} onFocus={e => { const i = e.target.value.lastIndexOf('.'); e.target.setSelectionRange(0, i > 0 ? i : e.target.value.length) }} required /></Field>
        <label className="row small"><input type="checkbox" style={{ width: 'auto' }} checked={keep} onChange={e => setKeep(e.target.checked)} /> Keep the working copy in Drafts too</label>
        {err && <div className="callout danger">{err}</div>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !sid || !stem(fname)}>{busy ? 'Saving…' : 'Save here'}</button></div>
      </form>
    </Modal>
  )
}
