import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, qs, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, Listing } from '../lib/types'
import { ago, bytes, isPhone, sourceIcon, sourceLabel, stem } from '../lib/format'
import { Confirm, Empty, KindBadge, Loading, Menu, Modal, Prompt, useAsync, useToast } from '../components/ui'
import { NewFileButtons } from '../components/NewFile'
import { DownloadAs } from '../components/DownloadAs'
import { SaveTo } from '../components/SaveTo'
import { ReviewBadge, SendToCollab } from '../components/Collab'
import type { Format } from '../lib/types'

type Sort = 'name' | 'modified' | 'size'

export default function Browse() {
  const { id } = useParams()
  const { sources, settings } = useApp()
  const [params, setParams] = useSearchParams()
  const path = params.get('path') || ''
  const navigate = useNavigate()
  const { toast } = useToast()
  const src = sources.find(s => String(s.id) === id)
  const [showAll, setShowAll] = useState(() => localStorage.getItem('po-all') === '1')
  const [sort, setSort] = useState<Sort>(() => (localStorage.getItem('po-sort') as Sort) || 'name')
  const [filter, setFilter] = useState('')
  const [menu, setMenu] = useState<{ e: Entry; x: number; y: number } | null>(null)
  const [dlg, setDlg] = useState<{ kind: 'new' | 'folder' | 'rename' | 'delete' | 'move' | 'download' | 'saveto' | 'collab'; e?: Entry } | null>(null)
  const [formats, setFormats] = useState<Record<string, Format>>({})
  useEffect(() => { api.get<Format[]>(`${V1}/formats`).then(fs => setFormats(Object.fromEntries(fs.map(f => [f.ext, f])))).catch(() => {}) }, [])
  const [progress, setProgress] = useState<number | null>(null)
  const [drag, setDrag] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const list = useAsync(() => api.get<Listing>(`${V1}/sources/${id}/ls${qs({ path, all: showAll ? '1' : '' })}`), [id, path, showAll])

  useEffect(() => { localStorage.setItem('po-all', showAll ? '1' : '0') }, [showAll])
  useEffect(() => { localStorage.setItem('po-sort', sort) }, [sort])
  useEffect(() => { setFilter(''); setMenu(null) }, [id, path])

  if (!src) return <div className="page"><Empty title="That place is gone">Pick another one from the sidebar.</Empty></div>

  const go = (p: string) => setParams(p ? { path: p } : {})
  const open = (e: Entry) => { if (e.is_dir) go(e.path); else if (e.opens) navigate(`/edit/${id}${qs({ path: e.path })}`); else window.open(`${V1}/sources/${id}/file${qs({ path: e.path })}`, '_blank') }
  const crumbs = path ? path.split('/') : []
  const entries = (list.data?.entries || []).filter(e => !filter || e.name.toLowerCase().includes(filter.toLowerCase())).sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
    if (sort === 'modified') return (b.modified || '').localeCompare(a.modified || '')
    if (sort === 'size') return (b.size || 0) - (a.size || 0)
    return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
  })

  const doUpload = async (files: FileList | File[]) => {
    const arr = Array.from(files)
    if (!arr.length) return
    const tooBig = arr.find(f => f.size > (settings?.max_upload || 200) * 1024 * 1024)
    if (tooBig) { toast(`${tooBig.name} is larger than ${settings?.max_upload} MB`, 'err'); return }
    setProgress(0)
    try {
      const r = await upload<{ saved: Entry[]; failed: { file: string; error: string }[] }>(`${V1}/sources/${id}/upload${qs({ path })}`, arr, setProgress)
      if (r.failed?.length) toast(r.failed.map(f => `${f.file}: ${f.error}`).join('; '), 'err')
      else toast(r.saved.length === 1 ? `${r.saved[0].name} uploaded` : `${r.saved.length} files uploaded`)
      list.reload()
    } catch (e) { toast((e as Error).message, 'err') } finally { setProgress(null) }
  }

  const act = async (fn: () => Promise<unknown>, ok?: string) => {
    try { await fn(); if (ok) toast(ok); list.reload() } catch (e) { toast((e as Error).message, 'err') }
  }

  return (
    <div className={`page browse ${drag ? 'drag' : ''}`}
      onDragOver={e => { if (list.data?.writable) { e.preventDefault(); setDrag(true) } }}
      onDragLeave={() => setDrag(false)}
      onDrop={e => { e.preventDefault(); setDrag(false); if (e.dataTransfer.files.length) doUpload(e.dataTransfer.files) }}>
      <div className="browse-head">
        <div className="crumbs">
          <button className="crumb" onClick={() => go('')} title={src.where}>{sourceIcon[src.kind]} {src.name}</button>
          {crumbs.map((c, i) => <span key={i} className="crumb-wrap"><span className="muted">/</span><button className="crumb" onClick={() => go(crumbs.slice(0, i + 1).join('/'))}>{c}</button></span>)}
        </div>
        <div className="actions">
          <input className="filter" placeholder="Filter…" value={filter} onChange={e => setFilter(e.target.value)} />
          <Menu button={<button className="btn sm">Sort: {sort} ▾</button>}>{close => <>
            {(['name', 'modified', 'size'] as Sort[]).map(s => <button key={s} onClick={() => { setSort(s); close() }}>{s === sort ? '✓ ' : ''}{s}</button>)}
            <hr /><button onClick={() => { setShowAll(a => !a); close() }}>{showAll ? '✓ ' : ''}Show every file</button>
          </>}</Menu>
          {list.data?.writable && <>
            {src.tree && <button className="btn sm" onClick={() => setDlg({ kind: 'folder' })}>New folder</button>}
            <button className="btn sm" onClick={() => fileInput.current?.click()}>Upload</button>
            <button className="btn primary sm" onClick={() => setDlg({ kind: 'new' })}>＋ New</button>
            <input ref={fileInput} type="file" multiple hidden onChange={e => { if (e.target.files) doUpload(e.target.files); e.target.value = '' }} />
          </>}
        </div>
      </div>
      {src.kind === 'drafts' && <div className="callout mb small">📝 Working copies: files you drop or open from your device live here until you <b>Save to…</b> a place (right-click a file, or the button while editing). Edits are kept meanwhile.</div>}
      {progress !== null && <div className="bar lg mb"><span className="fill" style={{ width: `${progress}%` }} /></div>}
      {list.loading && !list.data && <Loading text={`Reading ${src.name}…`} />}
      {list.error && <div className="callout danger">{list.error} <button className="btn sm" onClick={list.reload}>Try again</button></div>}
      {list.data && entries.length === 0 && !list.loading && (
        <Empty title={filter ? 'Nothing matches' : path ? 'This folder is empty' : `Nothing in ${src.name} yet`}>
          {!filter && list.data.writable && <span>Drop files here, <button className="link-btn accent" onClick={() => fileInput.current?.click()}>upload</button> or <button className="link-btn accent" onClick={() => setDlg({ kind: 'new' })}>start a new one</button>.{!showAll && ' Only files that open here are listed; "Show every file" lists the rest.'}</span>}
        </Empty>
      )}
      {entries.length > 0 && (
        <table className="files">
          <thead><tr><th>Name</th><th className="col-mod">Modified</th><th className="col-size">Size</th><th className="col-act"></th></tr></thead>
          <tbody>
            {entries.map(e => (
              <tr key={e.path} className={`${!e.is_dir && !e.opens ? 'muted' : ''} ${menu?.e.path === e.path ? 'menu-on' : ''}`} onDoubleClick={() => open(e)} onContextMenu={ev => { ev.preventDefault(); setMenu({ e, x: ev.clientX, y: ev.clientY }) }}>
                <td><button className="file-name" onClick={() => open(e)}>
                  {e.is_dir ? <span className="folder-ico">📁</span> : <KindBadge kind={e.kind} ext={e.ext} />}
                  <span className="ellipsis">{e.name}</span>
                  {!e.is_dir && e.opens && !e.edit && <small className="badge" title="Opens for viewing; use Edit a copy to change it">view</small>}
                  {e.review && <ReviewBadge review={e.review} small />}
                </button></td>
                <td className="col-mod muted nowrap">{e.modified && !e.modified.startsWith('0001') ? ago(e.modified) : ''}</td>
                <td className="col-size muted nowrap">{e.is_dir ? '' : bytes(e.size)}</td>
                <td className="col-act"><button className="btn ghost xs" onClick={ev => { const r = (ev.target as HTMLElement).getBoundingClientRect(); setMenu({ e, x: r.left - 160, y: r.bottom }) }} aria-label="Actions">⋯</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {menu && <><div className="ctx-scrim" onClick={() => setMenu(null)} onContextMenu={e => { e.preventDefault(); setMenu(null) }} />
        <div className="ctx-menu" style={{ left: Math.min(menu.x, window.innerWidth - 230), top: Math.min(menu.y, window.innerHeight - 320) }}>
          <button onClick={() => { open(menu.e); setMenu(null) }}>{menu.e.is_dir ? 'Open folder' : menu.e.edit ? 'Edit' : menu.e.opens ? 'View' : 'Download'}</button>
          {!menu.e.is_dir && src.kind === 'drafts' && <button onClick={() => { setDlg({ kind: 'saveto', e: menu.e }); setMenu(null) }}>Save to…</button>}
          {!menu.e.is_dir && menu.e.opens && settings?.collab?.available && <button onClick={() => { setDlg({ kind: 'collab', e: menu.e }); setMenu(null) }}>{menu.e.review && !['approved', 'rejected', 'cancelled'].includes(menu.e.review.status) ? 'Send new revision to Collaborator…' : 'Send for review…'}</button>}
          {!menu.e.is_dir && menu.e.review && <a href={menu.e.review.url} target="_blank" rel="noreferrer" onClick={() => setMenu(null)}>Open the review ↗</a>}
          {!menu.e.is_dir && <a href={`${V1}/sources/${id}/file${qs({ path: menu.e.path })}`} onClick={() => setMenu(null)}>Download</a>}
          {!menu.e.is_dir && menu.e.opens && menu.e.ext !== 'pdf' && <a href={`${V1}/sources/${id}/export${qs({ path: menu.e.path, to: 'pdf' })}`} onClick={() => setMenu(null)}>Download as PDF</a>}
          {!menu.e.is_dir && menu.e.opens && (formats[menu.e.ext || '']?.exports?.length || 0) > 0 && <button onClick={() => { setDlg({ kind: 'download', e: menu.e }); setMenu(null) }}>Download as…</button>}
          {!menu.e.is_dir && menu.e.opens && !menu.e.edit && <button onClick={() => { const e = menu.e; setMenu(null); act(async () => { const n = await api.post<Entry>(`${V1}/sources/${id}/edit-copy`, { path: e.path }); navigate(`/edit/${id}${qs({ path: n.path })}`) }) }}>Edit a copy…</button>}
          {list.data?.writable && <>
            <hr />
            <button onClick={() => { setDlg({ kind: 'rename', e: menu.e }); setMenu(null) }}>Rename…</button>
            {src.tree && <button onClick={() => { setDlg({ kind: 'move', e: menu.e }); setMenu(null) }}>Move to…</button>}
            {!menu.e.is_dir && src.tree && <button onClick={() => { const e = menu.e; setMenu(null); act(() => api.post(`${V1}/sources/${id}/copy`, { from: e.path }), 'Copied') }}>Duplicate</button>}
            <button className="danger-text" onClick={() => { setDlg({ kind: 'delete', e: menu.e }); setMenu(null) }}>Delete…</button>
          </>}
        </div></>}

      {dlg?.kind === 'new' && <Modal title={`New file in ${path ? path.split('/').pop() : src.name}`} onClose={() => setDlg(null)}>
        <NewFileButtons onPick={type => { setDlg(null); act(async () => { const e = await api.post<Entry>(`${V1}/sources/${id}/new`, { path, type }); navigate(`/edit/${id}${qs({ path: e.path })}`) }) }} />
      </Modal>}
      {dlg?.kind === 'collab' && dlg.e && <SendToCollab sourceId={id!} file={dlg.e} onClose={() => { setDlg(null); list.reload() }} />}
      {dlg?.kind === 'saveto' && dlg.e && <SaveTo path={dlg.e.path} name={dlg.e.name} onClose={() => setDlg(null)} onSaved={() => list.reload()} />}
      {dlg?.kind === 'download' && dlg.e && <DownloadAs sourceId={id!} path={dlg.e.path} name={dlg.e.name} exports={formats[dlg.e.ext || '']?.exports || []} onClose={() => setDlg(null)} />}
      {dlg?.kind === 'folder' && <Prompt title="New folder" onClose={() => setDlg(null)} action="Make" onDone={name => { setDlg(null); act(() => api.post(`${V1}/sources/${id}/mkdir`, { path, name })) }} />}
      {dlg?.kind === 'rename' && dlg.e && <Prompt title={`Rename “${dlg.e.name}”`} initial={dlg.e.name} selectStem action="Rename" onClose={() => setDlg(null)} onDone={name => { const e = dlg.e!; setDlg(null); if (name === e.name) return; if (!e.is_dir && stem(name) === name && e.ext) name += '.' + e.ext; act(() => api.post(`${V1}/sources/${id}/rename`, { from: e.path, name })) }} />}
      {dlg?.kind === 'move' && dlg.e && <Prompt title={`Move “${dlg.e.name}” to`} label="Folder path (empty = top level)" initial={path} action="Move" onClose={() => setDlg(null)} onDone={to => { const e = dlg.e!; setDlg(null); act(() => api.post(`${V1}/sources/${id}/rename`, { from: e.path, to: (to.replace(/^\/+|\/+$/g, '') ? to.replace(/^\/+|\/+$/g, '') + '/' : '') + e.name }), 'Moved') }} />}
      {dlg?.kind === 'delete' && dlg.e && <Confirm title={`Delete “${dlg.e.name}”?`} action="Delete" message={dlg.e.is_dir ? 'The folder and everything in it are deleted from the storage.' : src.kind === 'documents' ? 'The document goes to the Pocket Documents trash.' : src.kind === 'nextcloud' ? 'The file goes to the Nextcloud trash.' : 'The file is deleted from the storage.'} onCancel={() => setDlg(null)} onConfirm={() => { const e = dlg.e!; setDlg(null); act(() => api.post(`${V1}/sources/${id}/delete`, { path: e.path }), 'Deleted') }} />}
      {isPhone() && <div className="muted small mt">{sourceLabel[src.kind]} · {src.where}</div>}
      {drag && <div className="drop-hint">Drop to upload into {path ? path.split('/').pop() : src.name}</div>}
    </div>
  )
}
