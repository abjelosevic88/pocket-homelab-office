import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Engine, Entry, OpenResult } from '../lib/types'
import { isMobileUA, isPhone, kindLabel } from '../lib/format'
import { Confirm, download, KindBadge, Loading, type MenuItem, MenuButton, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { DownloadAs } from '../components/DownloadAs'
import { SaveTo } from '../components/SaveTo'
import { ReviewBadge, SendToCollab } from '../components/Collab'

declare global {
  interface Window { DocsAPI?: { DocEditor: new (id: string, config: Record<string, unknown>) => { destroyEditor: () => void; downloadAs?: (t?: string) => void } } }
}

const scripts = new Map<string, Promise<void>>()
function loadScript(src: string): Promise<void> {
  if (scripts.has(src)) return scripts.get(src)!
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
  const engineParam = (params.get('engine') || '') as Engine | ''
  const navigate = useNavigate()
  const { sources, theme } = useApp()
  const { toast } = useToast()
  const [res, setRes] = useState<OpenResult | null>(null)
  const [err, setErr] = useState('')
  const [state, setState] = useState<'loading' | 'ready' | 'saving' | 'error'>('loading')
  const [confirmCopy, setConfirmCopy] = useState(false)
  const [dl, setDl] = useState(false)
  const [saveTo, setSaveTo] = useState(false)
  const [collab, setCollab] = useState(false)
  const [desktop, setDesktop] = useState(() => sessionStorage.getItem('po-desktop') === '1')
  const editor = useRef<{ destroyEditor: () => void } | null>(null)
  const onlyOfficeHost = useRef<HTMLDivElement>(null)
  const coForm = useRef<HTMLFormElement>(null)
  const coFrame = useRef<HTMLIFrameElement>(null)
  const saveWaiters = useRef<(() => void)[]>([])
  const src = sources.find(s => String(s.id) === id)
  const phone = isPhone()

  useEffect(() => {
    let dead = false
    const host = onlyOfficeHost.current!
    setRes(null); setErr(''); setState('loading')
    api.post<OpenResult>(`${V1}/sources/${id}/open`, { path, mobile: isMobileUA(), desktop, dark: theme === 'dark', lang: navigator.language.split('-')[0], engine: engineParam })
      .then(async r => {
        if (dead) return
        setRes(r)
        if (r.engine_note) toast(r.engine_note)
        if (r.engine === 'collabora') return // the form below posts into the frame
        await loadScript(r.api_js!)
        if (dead || !window.DocsAPI) return
        const events = {
          onAppReady: () => { if (!dead) setState('ready') },
          onDocumentStateChange: (e: { data: boolean }) => { if (!dead) setState(e.data ? 'saving' : 'ready') },
          onRequestClose: () => { if (!dead) navigate(backTo(id!, path)) },
          onError: (e: { data: { errorCode: number; errorDescription: string } }) => { if (!dead) { setErr(e.data?.errorDescription || 'Document Server reported an error'); setState('error') } },
          onWarning: (e: { data: { warningDescription: string } }) => { if (!dead) toast(e.data?.warningDescription || 'Warning from Document Server', 'err') },
          onRequestSaveAs: () => { if (!dead) toast('Use the file menu’s Download to save a copy') },
        }
        // The SDK replaces its target with an iframe, and replaces it again on
        // destroy. Keep those nodes outside React's reconciliation tree.
        const target = document.createElement('div')
        target.id = 'po-editor'
        host.replaceChildren(target)
        editor.current = new window.DocsAPI.DocEditor('po-editor', { ...r.config, events })
      })
      .catch(e => { if (!dead) { setErr((e as Error).message); setState('error') } })
    return () => { dead = true; try { editor.current?.destroyEditor() } catch {} editor.current = null; host.replaceChildren() }
  }, [id, path, desktop, engineParam]) // eslint-disable-line react-hooks/exhaustive-deps

  // Collabora: post the access token into the frame, then talk WOPI postMessage.
  useEffect(() => {
    if (res?.engine !== 'collabora') return
    coForm.current?.submit()
    const origin = res.collabora!.origin
    const post = (MessageId: string, Values: Record<string, unknown> = {}) => coFrame.current?.contentWindow?.postMessage(JSON.stringify({ MessageId, SendTime: Date.now(), Values }), origin)
    const onMsg = (e: MessageEvent) => {
      if (e.origin !== origin) return
      let m: { MessageId?: string; Values?: Record<string, unknown> } = {}
      try { m = typeof e.data === 'string' ? JSON.parse(e.data) : e.data } catch { return }
      switch (m.MessageId) {
        case 'App_LoadingStatus':
          if (m.Values?.Status === 'Frame_Ready') post('Host_PostmessageReady')
          if (m.Values?.Status === 'Document_Loaded') setState('ready')
          if (m.Values?.Status === 'Failed') { setErr('Collabora could not load the document'); setState('error') }
          break
        case 'Doc_ModifiedStatus': setState(m.Values?.Modified ? 'saving' : 'ready'); break
        case 'Action_Save_Resp': saveWaiters.current.splice(0).forEach(fn => fn()); break
        case 'UI_Close': case 'close': navigate(backTo(id!, path)); break
        case 'Action_Load_Resp': if (m.Values?.success === false) { setErr(String(m.Values?.errorMsg || 'Collabora refused the document')); setState('error') }; break
      }
    }
    window.addEventListener('message', onMsg)
    ;(window as unknown as { __poCollaboraPost?: typeof post }).__poCollaboraPost = post
    return () => { window.removeEventListener('message', onMsg); delete (window as unknown as { __poCollaboraPost?: typeof post }).__poCollaboraPost }
  }, [res]) // eslint-disable-line react-hooks/exhaustive-deps

  // Before "Save to…" on a draft, make the engine write its latest state.
  const flush = useCallback(() => new Promise<void>(resolve => {
    const post = (window as unknown as { __poCollaboraPost?: (id: string, v?: Record<string, unknown>) => void }).__poCollaboraPost
    if (res?.engine !== 'collabora' || !post) return resolve()
    const t = setTimeout(resolve, 4000)
    saveWaiters.current.push(() => { clearTimeout(t); setTimeout(resolve, 300) })
    post('Action_Save', { DontTerminateEdit: true, DontSaveIfUnmodified: true, Notify: true })
  }), [res])

  const switchEngine = (e: Engine) => navigate(`/edit/${id}${qs({ path, engine: e })}`, { replace: true })

  // Leaving the page: Document Server saves on its own; the bar just goes back.
  const name = res?.file.name || path.split('/').pop() || ''
  const folder = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''

  const inReview = !!res?.file.review && !['approved', 'rejected', 'cancelled'].includes(res.file.review.status)
  const moreItems = (): MenuItem[] => {
    const out: MenuItem[] = [{ label: 'Download original', icon: 'download', onSelect: () => download(`${V1}/sources/${id}/file${qs({ path })}`) }]
    if (res && (res.exports?.length ?? 0) > 0) out.push({ label: `Download as… (${res.exports.length} types)`, icon: 'layers', onSelect: () => setDl(true) })
    if (res?.collab?.available) {
      out.push('sep', { label: inReview ? 'Send new revision to Pocket Review…' : 'Send for review in Pocket Review…', icon: 'send', onSelect: () => setCollab(true) })
      if (res.file.review) out.push({ label: 'Open the review', icon: 'external', onSelect: () => window.open(res.file.review!.url, '_blank', 'noopener') })
    }
    if (res && res.mode === 'view' && res.convertible) out.push('sep', { label: `Edit a copy as .${res.editable_as}`, icon: 'copy', onSelect: () => setConfirmCopy(true) })
    if (res && (res.engines?.length ?? 0) > 1) {
      out.push('sep', { heading: 'Engine' })
      for (const e of res.engines.filter(e => e !== res.engine)) {
        const no = e === 'collabora' && res.collabora_opens === false
        out.push({ label: `Open with ${e === 'collabora' ? 'Collabora' : 'ONLYOFFICE'}${no ? ` (no .${res.ext})` : ''}`, icon: 'refresh', disabled: no, onSelect: () => switchEngine(e) })
      }
    }
    out.push('sep')
    if (isMobileUA() && res?.engine !== 'collabora') out.push(desktop
      ? { label: 'Back to the phone viewer', icon: 'smartphone', onSelect: () => { sessionStorage.removeItem('po-desktop'); setDesktop(false) } }
      : { label: 'Open the desktop editor', icon: 'monitor', onSelect: () => { sessionStorage.setItem('po-desktop', '1'); setDesktop(true) } })
    out.push({ label: 'Reload the editor', icon: 'refresh', onSelect: () => location.reload() })
    return out
  }

  const editCopy = async () => {
    setConfirmCopy(false)
    try { const n = await api.post<Entry>(`${V1}/sources/${id}/edit-copy`, { path }); navigate(`/edit/${id}${qs({ path: n.path })}`) } catch (e) { toast((e as Error).message, 'err') }
  }

  return (
    <div className={`editor-page ${phone ? 'phone' : ''}`}>
      <header className="editor-bar">
        <Link className="btn ghost sm back" to={backTo(id!, path)} title="Back to files" aria-label="Back to files"><Icon name="chevron-left" size={17} />{phone ? '' : 'Files'}</Link>
        <KindBadge kind={res?.kind} ext={res?.ext || path.split('.').pop()} />
        <span className="editor-title grow">
          <span className="ellipsis">{name}</span>
          <small className="muted ellipsis">{src ? src.name : ''}{folder ? ` / ${folder}` : ''}{res?.draft ? ' · working copy' : ''}{res?.engine === 'collabora' ? ' · Collabora' : res?.engine_note ? ' · ONLYOFFICE (Collabora has no .' + res.ext + ')' : ''}{res?.phone_view ? ' · viewing' : res?.mode === 'view' ? ' · view only' : res?.lossy ? ' · saves as .' + res.ext + ' (plain format)' : ''}</small>
        </span>
        {state === 'saving' && <span className="editor-saving" title="Saving…"><span className="spinner" />{phone ? '' : 'Saving…'}</span>}
        {res?.file.review && <ReviewBadge review={res.file.review} />}
        {res?.draft && <button type="button" className="btn primary sm" onClick={() => setSaveTo(true)} title="This is a working copy in Drafts; put it into a place"><Icon name="folder-plus" size={15} />Save to…</button>}
        {res && res.mode === 'view' && res.convertible && <button type="button" className="btn primary sm" onClick={() => setConfirmCopy(true)}><Icon name="copy" size={15} />Edit a copy</button>}
        {res?.phone_view && <button type="button" className="btn primary sm" onClick={() => { sessionStorage.setItem('po-desktop', '1'); setDesktop(true) }} title="The free Document Server views on phones; this opens the full desktop editor here"><Icon name="edit" size={15} />Edit</button>}
        <MenuButton label="More" className="btn ghost icon-only sm" align="right" width={250} items={moreItems}><Icon name="more" size={17} /></MenuButton>
      </header>
      <div className="editor-host">
        {state === 'loading' && !err && <div className="editor-overlay"><Loading text={res ? `Starting the ${kindLabel[res.kind].toLowerCase()} editor…` : 'Opening…'} /></div>}
        {err && <div className="editor-overlay"><div className="editor-error"><span className="empty-ico"><Icon name="alert" size={22} /></span><h3>Could not open {name}</h3><p className="muted">{err}</p><div className="row"><button type="button" className="btn primary" onClick={() => location.reload()}>Try again</button><Link className="btn" to={backTo(id!, path)}>Back to files</Link></div></div></div>}
        <div ref={onlyOfficeHost} className="onlyoffice-host" hidden={res?.engine === 'collabora'} />
        {res?.engine === 'collabora' && <>
          <form ref={coForm} method="post" action={res.collabora!.action_url} target="po-collabora" style={{ display: 'none' }}>
            <input type="hidden" name="access_token" value={res.collabora!.access_token} />
            <input type="hidden" name="access_token_ttl" value={res.collabora!.access_token_ttl} />
          </form>
          <iframe ref={coFrame} name="po-collabora" title="Collabora Online" allow="clipboard-read *; clipboard-write *; fullscreen *" />
        </>}
      </div>
      {collab && res && <SendToCollab sourceId={id!} file={res.file} beforeSend={flush} onClose={() => setCollab(false)} onSent={() => api.get<{ review: typeof res.file.review }>(`${V1}/sources/${id}/collab?path=${encodeURIComponent(path)}`).then(r => setRes(x => x ? { ...x, file: { ...x.file, review: r.review } } : x)).catch(() => {})} />}
      {saveTo && res && <SaveTo path={path} name={name} beforeSave={flush} onClose={() => setSaveTo(false)} />}
      {dl && res && <DownloadAs sourceId={id!} path={path} name={name} exports={res.exports} onClose={() => setDl(false)} />}
      {confirmCopy && res && <Confirm danger={false} action={`Make a .${res.editable_as}`} title={`Edit a copy of ${name}?`} message={`.${res.ext} files open for viewing. Pocket Office makes a .${res.editable_as} copy next to it through Document Server, and opens that for editing. The original stays as it is.`} onCancel={() => setConfirmCopy(false)} onConfirm={editCopy} />}
    </div>
  )
}

function backTo(id: string, path: string) {
  const dir = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
  return `/s/${id}${qs({ path: dir })}`
}
