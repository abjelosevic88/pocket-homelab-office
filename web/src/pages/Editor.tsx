import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, OpenResult } from '../lib/types'
import { isMobileUA, isPhone, kindLabel, sourceIcon } from '../lib/format'
import { Confirm, KindBadge, Loading, Menu, useToast } from '../components/ui'
import { DownloadAs } from '../components/DownloadAs'
import { SaveTo } from '../components/SaveTo'

declare global {
  interface Window { DocsAPI?: { DocEditor: new (id: string, config: Record<string, unknown>) => { destroyEditor: () => void; downloadAs?: (t?: string) => void } } }
}

const scripts = new Map<string, Promise<void>>()
function loadScript(src: string): Promise<void> {
  if (window.DocsAPI && scripts.has(src)) return scripts.get(src)!
  const p = new Promise<void>((resolve, reject) => {
    const s = document.createElement('script')
    s.src = src
    s.async = true
    s.onload = () => resolve()
    s.onerror = () => { scripts.delete(src); reject(new Error('The Document Server script could not be loaded from ' + src + '. Is onlyoffice reachable from this browser?')) }
    document.head.appendChild(s)
  })
  scripts.set(src, p)
  return p
}

/** The editor page: Document Server's editor filling the window under a slim bar. */
export default function Editor() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const path = params.get('path') || ''
  const navigate = useNavigate()
  const { sources, theme } = useApp()
  const { toast } = useToast()
  const [res, setRes] = useState<OpenResult | null>(null)
  const [err, setErr] = useState('')
  const [state, setState] = useState<'loading' | 'ready' | 'saving' | 'error'>('loading')
  const [confirmCopy, setConfirmCopy] = useState(false)
  const [dl, setDl] = useState(false)
  const [saveTo, setSaveTo] = useState(false)
  const [desktop, setDesktop] = useState(() => sessionStorage.getItem('po-desktop') === '1')
  const editor = useRef<{ destroyEditor: () => void } | null>(null)
  const src = sources.find(s => String(s.id) === id)
  const phone = isPhone()

  useEffect(() => {
    let dead = false
    setRes(null); setErr(''); setState('loading')
    api.post<OpenResult>(`${V1}/sources/${id}/open`, { path, mobile: isMobileUA(), desktop, dark: theme === 'dark', lang: navigator.language.split('-')[0] })
      .then(async r => {
        if (dead) return
        setRes(r)
        await loadScript(r.api_js)
        if (dead || !window.DocsAPI) return
        const events = {
          onAppReady: () => setState('ready'),
          onDocumentStateChange: (e: { data: boolean }) => setState(e.data ? 'saving' : 'ready'),
          onRequestClose: () => navigate(backTo(id!, path)),
          onError: (e: { data: { errorCode: number; errorDescription: string } }) => { setErr(e.data?.errorDescription || 'Document Server reported an error'); setState('error') },
          onWarning: (e: { data: { warningDescription: string } }) => toast(e.data?.warningDescription || 'Warning from Document Server', 'err'),
          onRequestSaveAs: (e: { data: { title: string; url: string; fileType: string } }) => toast('Use the file menu’s Download to save a copy'),
        }
        editor.current = new window.DocsAPI.DocEditor('po-editor', { ...r.config, events })
      })
      .catch(e => { if (!dead) { setErr((e as Error).message); setState('error') } })
    return () => { dead = true; try { editor.current?.destroyEditor() } catch {} editor.current = null }
  }, [id, path, desktop]) // eslint-disable-line react-hooks/exhaustive-deps

  // Leaving the page: Document Server saves on its own; the bar just goes back.
  const name = res?.file.name || path.split('/').pop() || ''
  const folder = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''

  const editCopy = async () => {
    setConfirmCopy(false)
    try { const n = await api.post<Entry>(`${V1}/sources/${id}/edit-copy`, { path }); navigate(`/edit/${id}${qs({ path: n.path })}`) } catch (e) { toast((e as Error).message, 'err') }
  }

  return (
    <div className={`editor-page ${phone ? 'phone' : ''}`}>
      <header className="editor-bar">
        <Link className="btn ghost sm back" to={backTo(id!, path)} title="Back to files">‹ {phone ? '' : 'Files'}</Link>
        <KindBadge kind={res?.kind} ext={res?.ext || path.split('.').pop()} />
        <span className="editor-title grow">
          <span className="ellipsis">{name}</span>
          <small className="muted ellipsis">{src ? `${sourceIcon[src.kind]} ${src.name}` : ''}{folder ? ` / ${folder}` : ''}{res?.draft ? ' · working copy' : ''}{res?.phone_view ? ' · viewing' : res?.mode === 'view' ? ' · view only' : res?.lossy ? ' · saves as .' + res.ext + ' (plain format)' : ''}{state === 'saving' ? ' · saving…' : ''}</small>
        </span>
        {res?.draft && <button className="btn primary sm" onClick={() => setSaveTo(true)} title="This is a working copy in Drafts; put it into a place">Save to…</button>}
        {res && res.mode === 'view' && res.convertible && <button className="btn primary sm" onClick={() => setConfirmCopy(true)}>Edit a copy</button>}
        {res?.phone_view && <button className="btn primary sm" onClick={() => { sessionStorage.setItem('po-desktop', '1'); setDesktop(true) }} title="The free Document Server views on phones; this opens the full desktop editor here">Edit</button>}
        <Menu button={<button className="btn ghost sm" aria-label="More">⋯</button>}>{close => <>
          <a href={`${V1}/sources/${id}/file${qs({ path })}`} onClick={close}>Download original</a>
          {res && res.exports.length > 0 && <button onClick={() => { close(); setDl(true) }}>Download as… ({res.exports.length} types)</button>}
          {res && res.mode === 'view' && res.convertible && <><hr /><button onClick={() => { close(); setConfirmCopy(true) }}>Edit a copy as .{res.editable_as}</button></>}
          <hr />
          {isMobileUA() && (desktop ? <button onClick={() => { close(); sessionStorage.removeItem('po-desktop'); setDesktop(false) }}>Back to the phone viewer</button> : <button onClick={() => { close(); sessionStorage.setItem('po-desktop', '1'); setDesktop(true) }}>Open the desktop editor</button>)}
          <button onClick={() => { close(); location.reload() }}>Reload the editor</button>
        </>}</Menu>
      </header>
      <div className="editor-host">
        {state === 'loading' && !err && <div className="editor-overlay"><Loading text={res ? `Starting the ${kindLabel[res.kind].toLowerCase()} editor…` : 'Opening…'} /></div>}
        {err && <div className="editor-overlay"><div className="callout danger" style={{ maxWidth: 520 }}><b>Could not open {name}</b><br />{err}<div className="row mt"><button className="btn" onClick={() => location.reload()}>Try again</button><Link className="btn" to={backTo(id!, path)}>Back to files</Link></div></div></div>}
        <div id="po-editor" />
      </div>
      {saveTo && res && <SaveTo path={path} name={name} onClose={() => setSaveTo(false)} />}
      {dl && res && <DownloadAs sourceId={id!} path={path} name={name} exports={res.exports} onClose={() => setDl(false)} />}
      {confirmCopy && res && <Confirm danger={false} action={`Make a .${res.editable_as}`} title={`Edit a copy of ${name}?`} message={`.${res.ext} files open for viewing. Pocket Office makes a .${res.editable_as} copy next to it through Document Server, and opens that for editing. The original stays as it is.`} onCancel={() => setConfirmCopy(false)} onConfirm={editCopy} />}
    </div>
  )
}

function backTo(id: string, path: string) {
  const dir = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
  return `/s/${id}${qs({ path: dir })}`
}
