import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, qs, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, Listing } from '../lib/types'
import { ago, bytes, sourceLabel, stem } from '../lib/format'
import { announceChange } from '../lib/events'
import { type Anchor, Confirm, download, Empty, KindBadge, Loading, Menu, type MenuItem, MenuButton, Modal, Prompt, rectOf, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { NewFileButtons, PlaceIcon } from '../components/NewFile'
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
  const [menu, setMenu] = useState<{ e: Entry; at: Anchor; align: 'left' | 'right' } | null>(null)
  const mobile = useIsMobile()
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

  if (!src) return <div className="page"><Empty icon="folder" title="That place is gone">Pick another one from the sidebar.</Empty></div>

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
    try { await fn(); if (ok) toast(ok); list.reload(); if (src.kind === 'drafts') announceChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  const fileURL = (e: Entry) => `${V1}/sources/${id}/file${qs({ path: e.path })}`
  const itemsFor = (e: Entry): MenuItem[] => {
    const inReview = e.review && !['approved', 'rejected', 'cancelled'].includes(e.review.status)
    const out: MenuItem[] = [{ label: e.is_dir ? 'Open folder' : e.edit ? 'Edit' : e.opens ? 'View' : 'Download', icon: e.is_dir ? 'folder-open' : e.edit ? 'edit' : e.opens ? 'eye' : 'download', onSelect: () => open(e) }]
    if (!e.is_dir) {
      if (src.kind === 'drafts') out.push({ label: 'Save to…', icon: 'folder-plus', onSelect: () => setDlg({ kind: 'saveto', e }) })
      if (e.opens && settings?.collab?.available) out.push({ label: inReview ? 'Send new revision to Pocket Review…' : 'Send for review…', icon: 'send', onSelect: () => setDlg({ kind: 'collab', e }) })
      if (e.review) out.push({ label: 'Open the review', icon: 'external', onSelect: () => window.open(e.review!.url, '_blank', 'noopener') })
      out.push('sep', { label: 'Download', icon: 'download', onSelect: () => download(fileURL(e)) })
      if (e.opens && e.ext !== 'pdf') out.push({ label: 'Download as PDF', icon: 'file', onSelect: () => download(`${V1}/sources/${id}/export${qs({ path: e.path, to: 'pdf' })}`) })
      if (e.opens && (formats[e.ext || '']?.exports?.length || 0) > 0) out.push({ label: 'Download as…', icon: 'layers', onSelect: () => setDlg({ kind: 'download', e }) })
      if (e.opens && !e.edit) out.push({ label: 'Edit a copy…', icon: 'copy', onSelect: () => act(async () => { const n = await api.post<Entry>(`${V1}/sources/${id}/edit-copy`, { path: e.path }); navigate(`/edit/${id}${qs({ path: n.path })}`) }) })
    }
    if (list.data?.writable) {
      out.push('sep', { label: 'Rename…', icon: 'pencil', onSelect: () => setDlg({ kind: 'rename', e }) })
      if (src.tree) out.push({ label: 'Move to…', icon: 'move', onSelect: () => setDlg({ kind: 'move', e }) })
      if (!e.is_dir && src.tree) out.push({ label: 'Duplicate', icon: 'copy', onSelect: () => act(() => api.post(`${V1}/sources/${id}/copy`, { from: e.path }), 'Copied') })
      out.push({ label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setDlg({ kind: 'delete', e }) })
    }
    return out
  }
  const here = path ? path.split('/').pop()! : src.name
  const sortItems = (): MenuItem[] => [
    { heading: 'Sort by' },
    ...(['name', 'modified', 'size'] as Sort[]).map(o => ({ label: o === 'name' ? 'Name' : o === 'modified' ? 'Last modified' : 'Size', checked: o === sort, onSelect: () => setSort(o) })),
    'sep',
    { label: 'Show every file', checked: showAll, onSelect: () => setShowAll(a => !a) },
  ]

  return (
    <div className={`page browse ${drag ? 'drag' : ''}`}
      onDragOver={e => { if (list.data?.writable) { e.preventDefault(); setDrag(true) } }}
      onDragLeave={() => setDrag(false)}
      onDrop={e => { e.preventDefault(); setDrag(false); if (e.dataTransfer.files.length) doUpload(e.dataTransfer.files) }}>
      <div className="browse-head">
        <div className="browse-title">
          {path ? <button type="button" className="btn ghost icon-only" onClick={() => go(crumbs.slice(0, -1).join('/'))} aria-label={`Up to ${crumbs.length > 1 ? crumbs[crumbs.length - 2] : src.name}`} title={`Up to ${crumbs.length > 1 ? crumbs[crumbs.length - 2] : src.name}`}><Icon name="chevron-left" size={18} /></button> : <PlaceIcon kind={src.kind} size={34} />}
          <div className="grow">
            <h1 className="ellipsis">{here}</h1>
            <div className="sub ellipsis">{path ? [src.name, ...crumbs.slice(0, -1)].join(' / ') : <>{sourceLabel[src.kind]} · {src.where}</>}</div>
          </div>
        </div>
        <div className="browse-tools">
          <label className="filter-input"><Icon name="search" size={14} /><input placeholder="Filter…" value={filter} onChange={e => setFilter(e.target.value)} aria-label="Filter this folder" /></label>
          <MenuButton label="Sort and show" className="btn sm" items={sortItems} align="right" width={200}><Icon name="sort" size={14} />{mobile ? '' : sort === 'name' ? 'Name' : sort === 'modified' ? 'Modified' : 'Size'}<Icon name="chevron-down" size={13} /></MenuButton>
          {list.data?.writable && <>
            {src.tree && <button type="button" className="btn sm" onClick={() => setDlg({ kind: 'folder' })} title="New folder"><Icon name="folder-plus" size={15} />{mobile ? '' : 'New folder'}</button>}
            <button type="button" className="btn sm" onClick={() => fileInput.current?.click()} title="Upload"><Icon name="upload" size={15} />{mobile ? '' : 'Upload'}</button>
            <button type="button" className="btn primary sm" onClick={() => setDlg({ kind: 'new' })}><Icon name="plus" size={15} strokeWidth={2.2} />New</button>
            <input ref={fileInput} type="file" multiple hidden onChange={e => { if (e.target.files) doUpload(e.target.files); e.target.value = '' }} />
          </>}
        </div>
      </div>
      {src.kind === 'drafts' && <div className="callout info mb"><Icon name="pencil" size={15} /><span>Working copies: files you drop or open from your device live here until you <b>Save to…</b> a place (right-click a file, or the button while editing). Edits are kept meanwhile.</span></div>}
      {progress !== null && <div className="bar lg mb"><span className="fill" style={{ width: `${progress}%` }} /></div>}
      {list.loading && !list.data && <Loading text={`Reading ${src.name}…`} />}
      {list.error && <div className="callout danger"><Icon name="alert" size={15} /><span className="grow">{list.error}</span><button type="button" className="btn sm" onClick={list.reload}>Try again</button></div>}
      {list.data && entries.length === 0 && !list.loading && (
        <Empty icon={filter ? 'search' : 'folder'} title={filter ? 'Nothing matches' : path ? 'This folder is empty' : `Nothing in ${src.name} yet`}>
          {!filter && list.data.writable && <span>Drop files here, <button type="button" className="link-btn" onClick={() => fileInput.current?.click()}>upload</button> or <button type="button" className="link-btn" onClick={() => setDlg({ kind: 'new' })}>start a new one</button>.{!showAll && ' Only files that open here are listed; "Show every file" lists the rest.'}</span>}
        </Empty>
      )}
      {entries.length > 0 && (
        <div className="files-wrap"><table className="files">
          <thead><tr><th>Name</th><th className="col-mod">Modified</th><th className="col-size">Size</th><th className="col-act"></th></tr></thead>
          <tbody>
            {entries.map(e => (
              <tr key={e.path} className={`${!e.is_dir && !e.opens ? 'muted' : ''} ${menu?.e.path === e.path ? 'menu-on' : ''}`} onDoubleClick={() => open(e)} onContextMenu={ev => { ev.preventDefault(); setMenu({ e, at: { x: ev.clientX, y: ev.clientY, w: 0, h: 0 }, align: 'left' }) }}>
                <td><button type="button" className="file-name" onClick={() => open(e)}>
                  {e.is_dir ? <span className="folder-ico"><Icon name="folder" size={18} /></span> : <KindBadge kind={e.kind} ext={e.ext} />}
                  <span className="ellipsis">{e.name}</span>
                  {!e.is_dir && e.opens && !e.edit && <small className="badge" title="Opens for viewing; use Edit a copy to change it">view</small>}
                  {e.review && <ReviewBadge review={e.review} small />}
                </button></td>
                <td className="col-mod muted nowrap">{e.modified && !e.modified.startsWith('0001') ? ago(e.modified) : ''}</td>
                <td className="col-size muted nowrap">{e.is_dir ? '' : bytes(e.size)}</td>
                <td className="col-act"><button type="button" className="btn ghost icon-only sm row-more" onClick={ev => setMenu({ e, at: rectOf(ev.currentTarget), align: 'right' })} aria-label={`Actions for ${e.name}`} aria-haspopup="menu" aria-expanded={menu?.e.path === e.path}><Icon name="more" size={16} /></button></td>
              </tr>
            ))}
          </tbody>
        </table></div>
      )}

      {menu && <Menu anchor={menu.at} align={menu.align} width={230} items={itemsFor(menu.e)} onClose={() => setMenu(null)} />}

      {dlg?.kind === 'new' && <Modal title={`New file in ${here}`} onClose={() => setDlg(null)}>
        <NewFileButtons onPick={type => { setDlg(null); act(async () => { const e = await api.post<Entry>(`${V1}/sources/${id}/new`, { path, type }); navigate(`/edit/${id}${qs({ path: e.path })}`) }) }} />
      </Modal>}
      {dlg?.kind === 'collab' && dlg.e && <SendToCollab sourceId={id!} file={dlg.e} onClose={() => { setDlg(null); list.reload() }} />}
      {dlg?.kind === 'saveto' && dlg.e && <SaveTo path={dlg.e.path} name={dlg.e.name} onClose={() => setDlg(null)} onSaved={() => list.reload()} />}
      {dlg?.kind === 'download' && dlg.e && <DownloadAs sourceId={id!} path={dlg.e.path} name={dlg.e.name} exports={formats[dlg.e.ext || '']?.exports || []} onClose={() => setDlg(null)} />}
      {dlg?.kind === 'folder' && <Prompt title="New folder" onClose={() => setDlg(null)} action="Make" onDone={name => { setDlg(null); act(() => api.post(`${V1}/sources/${id}/mkdir`, { path, name })) }} />}
      {dlg?.kind === 'rename' && dlg.e && <Prompt title={`Rename “${dlg.e.name}”`} initial={dlg.e.name} selectStem action="Rename" onClose={() => setDlg(null)} onDone={name => { const e = dlg.e!; setDlg(null); if (name === e.name) return; if (!e.is_dir && stem(name) === name && e.ext) name += '.' + e.ext; act(() => api.post(`${V1}/sources/${id}/rename`, { from: e.path, name })) }} />}
      {dlg?.kind === 'move' && dlg.e && <Prompt title={`Move “${dlg.e.name}” to`} label="Folder path (empty = top level)" initial={path} action="Move" onClose={() => setDlg(null)} onDone={to => { const e = dlg.e!; setDlg(null); act(() => api.post(`${V1}/sources/${id}/rename`, { from: e.path, to: (to.replace(/^\/+|\/+$/g, '') ? to.replace(/^\/+|\/+$/g, '') + '/' : '') + e.name }), 'Moved') }} />}
      {dlg?.kind === 'delete' && dlg.e && <Confirm title={`Delete “${dlg.e.name}”?`} action="Delete" message={dlg.e.is_dir ? 'The folder and everything in it are deleted from the storage.' : src.kind === 'documents' ? 'The document goes to the Pocket Documents trash.' : src.kind === 'nextcloud' ? 'The file goes to the Nextcloud trash.' : src.kind === 'files' ? 'The file goes to the Pocket Files trash.' : 'The file is deleted from the storage.'} onCancel={() => setDlg(null)} onConfirm={() => { const e = dlg.e!; setDlg(null); act(() => api.post(`${V1}/sources/${id}/delete`, { path: e.path }), 'Deleted') }} />}
      {drag && <div className="drop-hint"><Icon name="upload" size={16} />Drop to upload into {here}</div>}
    </div>
  )
}
