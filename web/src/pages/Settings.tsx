import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, Settings as S, Source, SourceKind, SourceOptions, User } from '../lib/types'
import { ago, sourceLabel } from '../lib/format'
import { Avatar, Confirm, CopyField, Field, Loading, Modal, Segmented, SettingRow, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { PlaceIcon } from '../components/NewFile'

type TabId = 'places' | 'general' | 'server' | 'profile' | 'tokens' | 'people' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string; admin?: boolean }[] = [
  { id: 'places', label: 'Places', icon: 'folder', sub: 'Where your files live: the folder on the box, Drafts, your Nextcloud, Pocket Documents, Pocket Files, WebDAV or a server folder. Nothing is copied — files are read when opened and written back where they came from.' },
  { id: 'general', label: 'Editor & look', icon: 'palette', sub: 'Which engine opens your files, and light or dark.' },
  { id: 'server', label: 'Document Server', icon: 'server', sub: 'The engines that draw the documents: ONLYOFFICE Document Server and, optionally, Collabora Online.', admin: true },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name and password.' },
  { id: 'tokens', label: 'API tokens', icon: 'key', sub: 'Bearer tokens for scripts and the Homepage widget.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.', admin: true },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, engines and Pocket Core.' },
]
// the old section names (and ?tab= links)
const ALIAS: Record<string, TabId> = { sources: 'places', account: 'profile', users: 'people', api: 'tokens', editor: 'general' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const { settings } = useApp()
  const tabs = TABS.filter(t => !t.admin || settings?.admin)
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = tabs.find(t => t.id === (ALIAS[want] || want))?.id || 'places'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  // old names land on the new address
  useEffect(() => { if (want && (ALIAS[want] || legacy)) navigate(`/settings/${ALIAS[want] || want}`, { replace: true }) }, [want]) // eslint-disable-line react-hooks/exhaustive-deps
  const back = () => ((window.history.state?.idx ?? 0) > 0 ? navigate(-1) : navigate('/'))

  useEffect(() => {
    document.querySelector('.set-content')?.scrollTo({ top: 0 })
    document.querySelector('.set-tabs-mobile .active')?.scrollIntoView({ inline: 'center', block: 'nearest' })
  }, [tab, mobile])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (e.key === 'Escape' && !t.closest?.('input, textarea') && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) back()
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={back}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={tabs.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {tabs.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'places' && <PlacesTab />}
            {tab === 'general' && <GeneralTab />}
            {tab === 'server' && settings?.admin && <ServerTab />}
            {tab === 'profile' && <ProfileTab />}
            {tab === 'tokens' && <TokensTab />}
            {tab === 'people' && settings?.admin && <PeopleTab />}
            {tab === 'about' && <AboutTab />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, status, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; status?: 'ok' | 'error' | 'warn' | ''; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3 className="row">{title}{status !== undefined && <span className={`set-status ${status}`} />}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function Result({ ok, children }: { ok: boolean; children: React.ReactNode }) {
  return <div className={`callout ${ok ? 'success' : 'danger'} mt-s`}><Icon name={ok ? 'check-circle' : 'x-circle'} size={16} className={ok ? 'ok-text' : ''} /><span>{children}</span></div>
}

// ---------- Places ----------

function PlacesTab() {
  const { sources, reloadSources, settings } = useApp()
  const { toast } = useToast()
  const opts = useAsync(() => api.get<SourceOptions>(`${V1}/sources/options`), [])
  const [edit, setEdit] = useState<Partial<Source> & { kind: SourceKind; secret?: string } | null>(null)
  const [del, setDel] = useState<Source | null>(null)
  const [testing, setTesting] = useState<number | null>(null)
  const [result, setResult] = useState<Record<number, { ok: boolean; text: string }>>({})

  const test = async (s: Source) => {
    setTesting(s.id)
    try { const r = await api.post<{ where: string; files: number; folders: number; ms: number }>(`${V1}/sources/${s.id}/test`); setResult(x => ({ ...x, [s.id]: { ok: true, text: `${r.where}: ${r.folders} folders, ${r.files} files (${r.ms} ms)` } })) } catch (e) { setResult(x => ({ ...x, [s.id]: { ok: false, text: (e as Error).message } })) } finally { setTesting(null) }
  }
  const remove = async (s: Source) => { setDel(null); try { await api.del(`${V1}/sources/${s.id}`); await reloadSources(); toast('Removed') } catch (e) { toast((e as Error).message, 'err') } }

  const o = opts.data
  const kinds: { kind: SourceKind; name: string; desc: string; disabled?: boolean; show?: boolean; init: Partial<Source> }[] = [
    { kind: 'nextcloud', name: 'Nextcloud', desc: o?.core_nextcloud?.ok ? `Your account through Pocket Core (${o.core_nextcloud.user})` : o?.core ? `Through Pocket Core: ${o.core_nextcloud_error || 'no account yet'}; or type an app password` : 'Address, username and an app password', init: { via_core: !!o?.core_nextcloud?.ok, folder: '' } },
    { kind: 'documents', name: 'Pocket Documents', desc: o?.core_documents ? 'Linked through Pocket Core' : o?.core ? `Through Pocket Core: ${o.core_documents_error || 'link is off'}; or type an API token` : 'Address and an API token', init: { via_core: !!o?.core_documents } },
    { kind: 'files', name: 'Pocket Files', desc: o?.core_files ? 'Your files, linked through Pocket Core' : `Through Pocket Core: ${o?.core_files_error || 'link is off'}`, show: !!o?.core, init: { via_core: true, folder: '' } },
    { kind: 'webdav', name: 'WebDAV', desc: 'Any WebDAV folder: another Nextcloud, ownCloud, a NAS…', init: {} },
    { kind: 'folder', name: 'Folder on the box', desc: o?.folder_roots?.length ? `Under ${o.folder_roots.join(', ')}` : 'None offered (FOLDER_ROOTS is empty)', disabled: !o?.folder_roots?.length, init: { folder: o?.folder_roots?.[0] || '' } },
  ]
  return (
    <div className="stack loose">
      <Section title="Your places" sub="Listed under Places in the sidebar in this order (Drafts has its own entry at the top); keys 1 – 9 jump to them.">
        <div className="acct-list">
          {sources.map(s => (
            <React.Fragment key={s.id}>
              <div className="acct-row">
                <PlaceIcon kind={s.kind} size={36} />
                <div className="grow">
                  <div className="acct-name"><span className="ellipsis">{s.name}</span>{s.via_core && <span className="badge">via Pocket Core</span>}</div>
                  <div className="acct-meta ellipsis">{sourceLabel[s.kind]} · {s.where}</div>
                  {s.error && <div className="acct-error">{s.error}</div>}
                </div>
                <div className="row">
                  <button type="button" className="btn sm" disabled={testing === s.id} onClick={() => test(s)}>{testing === s.id ? 'Testing…' : 'Test'}</button>
                  {s.kind !== 'builtin' && s.kind !== 'drafts' && <button type="button" className="btn sm" onClick={() => setEdit({ ...s, secret: '' })}>Edit</button>}
                  {s.kind !== 'builtin' && s.kind !== 'drafts' && <button type="button" className="btn ghost icon-only sm danger-text" aria-label={`Remove ${s.name}`} title="Remove" onClick={() => setDel(s)}><Icon name="trash" size={15} /></button>}
                </div>
              </div>
              {result[s.id] && <Result ok={result[s.id].ok}>{result[s.id].text}</Result>}
            </React.Fragment>
          ))}
        </div>
      </Section>
      <Section title="Add a place" sub={settings?.core ? 'Through Pocket Core means nothing to type: Core hands this app your own Nextcloud app password or Pocket Documents and Pocket Files access, and you or the admin can switch that off there any time.' : undefined}>
        <div className="acct-providers">
          {kinds.filter(k => k.show !== false).map(k => (
            <button key={k.kind} type="button" className="acct-provider" disabled={k.disabled} onClick={() => setEdit({ kind: k.kind, ...k.init })}>
              <PlaceIcon kind={k.kind} size={34} />
              <span className="acct-provider-name">{k.name}</span>
              <span className="acct-provider-desc">{k.desc}</span>
            </button>
          ))}
        </div>
      </Section>
      {edit && <SourceDialog initial={edit} options={o} onClose={() => setEdit(null)} onSaved={async () => { setEdit(null); await reloadSources() }} />}
      {del && <Confirm title={`Remove “${del.name}”?`} action="Remove" message="Only the connection is removed; the files stay where they are." onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}

function SourceDialog({ initial, options, onClose, onSaved }: { initial: Partial<Source> & { kind: SourceKind; secret?: string }; options: SourceOptions | null; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: initial.name || '', url: initial.url || '', username: initial.username || '', secret: '', folder: initial.folder || '', via_core: !!initial.via_core })
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const kind = initial.kind
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF(x => ({ ...x, [k]: e.target.value }))
  const canCore = kind === 'nextcloud' ? !!options?.core_nextcloud?.ok : kind === 'documents' ? !!options?.core_documents : false
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      const body = { kind, ...f }
      if (initial.id) await api.put(`${V1}/sources/${initial.id}`, body)
      else await api.post(`${V1}/sources`, body)
      toast(initial.id ? 'Saved' : 'Connected')
      onSaved()
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  return (
    <Modal title={<span className="row"><PlaceIcon kind={kind} size={24} />{initial.id ? 'Edit' : 'Add'} {sourceLabel[kind]}</span>} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <Field label="Name" help="How it shows in the sidebar"><input value={f.name} onChange={set('name')} placeholder={sourceLabel[kind]} /></Field>
        {(kind === 'nextcloud' || kind === 'documents') && options?.core && (
          <Field label="Connect" group>
            <Segmented value={f.via_core ? 'core' : 'direct'} onChange={v => setF(x => ({ ...x, via_core: v === 'core' }))} options={[{ value: 'core', label: 'Through Pocket Core' }, { value: 'direct', label: 'Type it in' }]} />
            {f.via_core && !canCore && <span className="help danger-text">{kind === 'nextcloud' ? options.core_nextcloud_error || 'Pocket Core has no Nextcloud account for you' : options.core_documents_error || 'The Office → Documents link is off in Pocket Core'}</span>}
            {f.via_core && canCore && kind === 'nextcloud' && <span className="help ok-text">✓ {options.core_nextcloud?.user} at {options.core_nextcloud?.url}</span>}
          </Field>
        )}
        {kind === 'nextcloud' && !f.via_core && <>
          <Field label="Nextcloud address"><input value={f.url} onChange={set('url')} placeholder="https://cloud.example.com" required /></Field>
          <Field label="Username"><input value={f.username} onChange={set('username')} required /></Field>
          <Field label="App password" help="Nextcloud → Settings → Security → Devices & sessions → Create new app password"><input type="password" value={f.secret} onChange={set('secret')} placeholder={initial.secret_set ? '•••••••• (kept)' : ''} required={!initial.secret_set} /></Field>
        </>}
        {kind === 'nextcloud' && <Field label="Folder" help="A folder inside your files; empty = all of them"><input value={f.folder} onChange={set('folder')} placeholder="Documents" /></Field>}
        {kind === 'files' && <>
          <Field label="Connect" group>
            {options?.core_files ? <span className="help ok-text">✓ Through Pocket Core, as you</span> : <span className="help danger-text">{options?.core_files_error || 'The Office → Files link is off in Pocket Core'}</span>}
          </Field>
          <Field label="Folder" help="A folder inside your Pocket Files; empty = all of them"><input value={f.folder} onChange={set('folder')} placeholder="Documents" /></Field>
        </>}
        {kind === 'documents' && !f.via_core && <>
          <Field label="Pocket Documents address"><input value={f.url} onChange={set('url')} placeholder="https://documents.example.com" required /></Field>
          <Field label="API token" help="Pocket Documents → Settings → API tokens"><input type="password" value={f.secret} onChange={set('secret')} placeholder={initial.secret_set ? '•••••••• (kept)' : ''} required={!initial.secret_set} /></Field>
        </>}
        {kind === 'webdav' && <>
          <Field label="WebDAV address" help="The folder itself, e.g. https://cloud.example.com/remote.php/dav/files/me/Work"><input value={f.url} onChange={set('url')} required /></Field>
          <Field label="Username"><input value={f.username} onChange={set('username')} /></Field>
          <Field label="Password"><input type="password" value={f.secret} onChange={set('secret')} placeholder={initial.secret_set ? '•••••••• (kept)' : ''} /></Field>
        </>}
        {kind === 'folder' && <Field label="Folder on the box" help={`Under ${options?.folder_roots?.join(', ')}`}><input value={f.folder} onChange={set('folder')} required /></Field>}
        {err && <div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Checking…' : initial.id ? 'Save' : 'Connect'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- Editor & look ----------

const engineLabel = (e: string) => e === 'collabora' ? 'Collabora Online' : 'ONLYOFFICE'

function GeneralTab() {
  const { settings, reloadSettings, themeMode, setThemeMode } = useApp()
  const { toast } = useToast()
  const setEngine = async (e: string) => { try { await api.put(`${V1}/settings`, { me: { engine: e } }); await reloadSettings(); toast('Saved') } catch (err) { toast((err as Error).message, 'err') } }
  const engines = settings?.engines || []
  return (
    <div className="stack loose">
      <Section title="Editor">
        {engines.length > 1
          ? <SettingRow title="Open files with" help="ONLYOFFICE draws the document in your browser (snappy once loaded, heavier on the machine); Collabora draws it on the server and streams the picture (light on the browser, needs a good connection). The ⋯ menu in any open file switches for that file.">
              <Segmented value={settings?.engine || engines[0]} onChange={setEngine} options={engines.map(e => ({ value: e, label: engineLabel(e) }))} size="sm" />
            </SettingRow>
          : <SettingRow title="Open files with" help={engines.length === 1 ? 'The only engine set up on this box.' : 'No engine is set up yet.'}><span className="muted">{engines[0] ? engineLabel(engines[0]) : '—'}</span></SettingRow>}
      </Section>
      <Section title="Appearance" sub="Also passed on to the editor when a file opens.">
        <SettingRow title="Theme" help="System follows your device's light or dark setting.">
          <Segmented value={themeMode} onChange={setThemeMode} options={[{ value: 'system', label: 'System' }, { value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }]} size="sm" />
        </SettingRow>
      </Section>
    </div>
  )
}

// ---------- Document Server ----------

function ServerTab() {
  const { settings, reloadSettings } = useApp()
  const { toast } = useToast()
  const ds = settings?.document_server
  const [f, setF] = useState({ url: ds?.url || '', internal_url: ds?.internal_url || '', header: ds?.header || 'AuthorizationJwt', secret: '', callback_url: ds?.callback_url || '' })
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<Record<string, unknown> | null>(null)
  useEffect(() => { if (ds) setF(x => ({ ...x, url: ds.url || '', internal_url: ds.internal_url || '', header: ds.header || 'AuthorizationJwt', callback_url: ds.callback_url || '' })) }, [ds?.url, ds?.internal_url, ds?.header, ds?.callback_url]) // eslint-disable-line react-hooks/exhaustive-deps
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF(x => ({ ...x, [k]: e.target.value }))
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body: Record<string, string> = { url: f.url, internal_url: f.internal_url, header: f.header, callback_url: f.callback_url }
      if (f.secret) body.secret = f.secret
      await api.put<S>(`${V1}/settings`, { document_server: body }); await reloadSettings(); setF(x => ({ ...x, secret: '' })); toast('Saved')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const test = async () => { setBusy(true); setRes(null); try { setRes(await api.post<Record<string, unknown>>(`${V1}/settings/documentserver/test`)) } catch (e) { setRes({ error: (e as Error).message }) } finally { setBusy(false) } }
  const setDefault = async (e: string) => { try { await api.put(`${V1}/settings`, { default_engine: e }); await reloadSettings(); toast(`${e === 'collabora' ? 'Collabora' : 'ONLYOFFICE'} is the default for everyone`) } catch (err) { toast((err as Error).message, 'err') } }
  return (
    <div className="stack loose">
      {settings && (settings.engines?.length ?? 0) > 1 && <Section title="Default engine for everyone">
        <SettingRow title="New people use" help="Anyone who has not picked an engine under Editor & look gets this one. It is also listed first.">
          <Segmented value={settings.default_engine} onChange={setDefault} options={settings.engines.map(e => ({ value: e, label: engineLabel(e) }))} size="sm" />
        </SettingRow>
      </Section>}
      <Section title="ONLYOFFICE Document Server" icon="file-text" status={ds?.configured ? 'ok' : 'warn'} sub={<>Pocket Office does not render documents itself: an <a href="https://github.com/ONLYOFFICE/DocumentServer" target="_blank" rel="noreferrer">ONLYOFFICE Document Server</a> does, in the browser. Point this at one (the same one Nextcloud uses is fine). These settings override the <code>ONLYOFFICE_*</code> environment{ds?.from_env?.url ? ` (currently ${ds.from_env.url})` : ''}.</>}>
        <form onSubmit={save} className="stack">
          <Field label="Address browsers use" help="e.g. https://onlyoffice.example.com — the editor script is loaded from here"><input value={f.url} onChange={set('url')} placeholder="https://onlyoffice.example.com" /></Field>
          <Field label="Address this server uses" help="Optional: how this app reaches Document Server for conversions (e.g. http://onlyoffice:80). Empty = the same as above"><input value={f.internal_url} onChange={set('internal_url')} /></Field>
          <div className="two-col">
            <Field label="JWT secret" help={ds?.secret_set ? 'A secret is set; type one to replace it' : 'JWT_SECRET of the Document Server'}><input type="password" value={f.secret} onChange={set('secret')} placeholder={ds?.secret_set ? '•••••••• (kept)' : ''} /></Field>
            <Field label="JWT header"><input value={f.header} onChange={set('header')} /></Field>
          </div>
          <Field label="How Document Server reaches this app" help="Document Server downloads files from and reports saves to this address, so it must be reachable from the Document Server container (e.g. http://host.docker.internal:8186 or the public address). Empty = CALLBACK_URL, else BASE_URL"><input value={f.callback_url} onChange={set('callback_url')} placeholder={settings?.base_url} /></Field>
          <div className="form-actions"><button type="button" className="btn" disabled={busy} onClick={test}>Test</button><button className="btn primary" disabled={busy}>Save</button></div>
        </form>
        {res && <Result ok={!!res.ok}>{res.ok ? <>Document Server answers in {String(res.health_ms)} ms and converted a probe fetched from {String(res.callback_url)} to PDF in {String(res.convert_ms)} ms.</> : String(res.error)}</Result>}
      </Section>
      <CollaboraSection />
    </div>
  )
}

function CollaboraSection() {
  const { settings, reloadSettings } = useApp()
  const { toast } = useToast()
  const co = settings?.collabora
  const [f, setF] = useState({ url: co?.url || '', internal_url: co?.internal_url || '', wopi_url: co?.wopi_url || '' })
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<Record<string, unknown> | null>(null)
  useEffect(() => { if (co) setF({ url: co.url || '', internal_url: co.internal_url || '', wopi_url: co.wopi_url || '' }) }, [co?.url, co?.internal_url, co?.wopi_url]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (e: React.FormEvent) => { e.preventDefault(); setBusy(true); try { await api.put(`${V1}/settings`, { collabora: f }); await reloadSettings(); toast('Saved') } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) } }
  const test = async () => { setBusy(true); setRes(null); try { setRes(await api.post<Record<string, unknown>>(`${V1}/settings/collabora/test`)) } catch (e) { setRes({ error: (e as Error).message }) } finally { setBusy(false) } }
  return (
    <Section title="Collabora Online" icon="layers" status={co?.configured ? 'ok' : ''} sub={<>Optional second engine. A <a href="https://www.collaboraonline.com/code/" target="_blank" rel="noreferrer">Collabora Online</a> server can edit the same files, drawn on the server instead of in the browser. Pocket Office speaks WOPI to it: Collabora must list this app's WOPI address in its <code>aliasgroup</code> settings (e.g. <code>aliasgroup2: {co?.wopi_url || settings?.base_url}</code>), and only that address's origin may embed Collabora, so it is normally the public address. People then choose their engine under Editor & look, or per file from the ⋯ menu.</>}>
      <form onSubmit={save} className="stack">
        <Field label="Address browsers use" help="e.g. https://collabora.example.com; empty = Collabora off"><input value={f.url} onChange={e => setF(x => ({ ...x, url: e.target.value }))} placeholder="https://collabora.example.com" /></Field>
        <Field label="Address this server uses" help="Optional: how this app fetches hosting/discovery (e.g. http://collabora:9980)"><input value={f.internal_url} onChange={e => setF(x => ({ ...x, internal_url: e.target.value }))} /></Field>
        <Field label="How Collabora reaches this app (WOPI)" help="Must be reachable from the Collabora container and match the address people open; empty = the public address"><input value={f.wopi_url} onChange={e => setF(x => ({ ...x, wopi_url: e.target.value }))} placeholder={settings?.base_url} /></Field>
        <div className="form-actions"><button type="button" className="btn" disabled={busy || !f.url} onClick={test}>Test</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
      {res && <Result ok={!!res.ok}>{res.ok ? <>Collabora answers; discovery lists {String(res.types)} file types ({String(res.discovery_ms)} ms). The WOPI round trip is proven on the first open.</> : String(res.error)}</Result>}
    </Section>
  )
}

// ---------- Profile ----------

function ProfileTab() {
  const { user, refresh } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', current_password: '', new_password: '' })
  const save = async (e: React.FormEvent) => { e.preventDefault(); try { await api.put(`${V1}/auth/me`, f); await refresh(); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="You">
      <form onSubmit={save} className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={64} />
        <div className="stack grow">
          <div className="two-col">
            <Field label="Name"><input value={f.name} onChange={e => setF(x => ({ ...x, name: e.target.value }))} autoComplete="name" /></Field>
            <Field label="Email"><input value={user?.email || ''} disabled /></Field>
          </div>
          <div className="two-col">
            <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF(x => ({ ...x, current_password: e.target.value }))} autoComplete="current-password" /></Field>
            <Field label="New password" help="Leave empty to keep it"><input type="password" value={f.new_password} onChange={e => setF(x => ({ ...x, new_password: e.target.value }))} autoComplete="new-password" /></Field>
          </div>
          <div className="form-actions"><button className="btn primary">Save</button></div>
        </div>
      </form>
    </Section>
  )
}

// ---------- API tokens ----------

function TokensTab() {
  const { toast } = useToast()
  const tokens = useAsync(() => api.get<APIToken[]>(`${V1}/auth/tokens`), [])
  const [newTok, setNewTok] = useState<{ token: string; info: APIToken } | null>(null)
  const [tokName, setTokName] = useState('')
  const [revoke, setRevoke] = useState<APIToken | null>(null)
  const mint = async (e: React.FormEvent) => { e.preventDefault(); try { setNewTok(await api.post(`${V1}/auth/tokens`, { name: tokName || 'token' })); setTokName(''); tokens.reload() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="Personal tokens" sub={<>For scripts and the Homepage widget: <code>Authorization: Bearer po_…</code> against <code>/api/v1/…</code>. Pocket Core device tokens work too.</>}>
      <form onSubmit={mint} className="row"><input value={tokName} onChange={e => setTokName(e.target.value)} placeholder="What is it for?" aria-label="Token name" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
      {newTok && <div className="set-fresh">
        <div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is not shown again.</b></div>
        <CopyField value={newTok.token} label="New token" />
      </div>}
      <div className="set-people mt">
        {tokens.loading && !tokens.data && <Loading />}
        {tokens.data?.length === 0 && <p className="muted small">No tokens yet.</p>}
        {tokens.data?.map(t => (
          <div key={t.id} className="set-person">
            <span className="set-token-ico"><Icon name="key" size={15} /></span>
            <div className="grow"><div className="set-person-name">{t.name}<span className="muted mono small">{t.prefix}…</span></div><div className="muted small">{t.last_used_at ? 'used ' + ago(t.last_used_at) : 'never used'} · created {ago(t.created_at)}</div></div>
            <button type="button" className="btn ghost sm danger-text" onClick={() => setRevoke(t)}>Revoke</button>
          </div>
        ))}
      </div>
      {revoke && <Confirm title={`Revoke “${revoke.name}”?`} action="Revoke" message="Anything using this token stops working right away." onCancel={() => setRevoke(null)} onConfirm={async () => { const t = revoke; setRevoke(null); try { await api.del(`${V1}/auth/tokens/${t.id}`); tokens.reload() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </Section>
  )
}

// ---------- People ----------

const ROLES = [{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]

function PeopleTab() {
  const { toast } = useToast()
  const { settings, user: meUser } = useApp()
  const users = useAsync(() => api.get<User[]>(`${V1}/users`), [])
  const [f, setF] = useState({ email: '', name: '', password: '', role: 'member' })
  const [del, setDel] = useState<User | null>(null)
  const add = async (e: React.FormEvent) => { e.preventDefault(); try { await api.post(`${V1}/users`, f); setF({ email: '', name: '', password: '', role: 'member' }); users.reload(); toast('Added') } catch (e) { toast((e as Error).message, 'err') } }
  const setRole = async (u: User, role: string) => { try { await api.put(`${V1}/users/${u.id}`, { role }); users.reload() } catch (err) { toast((err as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <Section title="People">
        <div className="set-people">
          {users.loading && !users.data && <Loading />}
          {users.data?.map(u => (
            <div key={u.id} className="set-person">
              <Avatar name={u.name} email={u.email} size={34} />
              <div className="grow"><div className="set-person-name">{u.name || u.email}{u.id === meUser?.id && <span className="badge">you</span>}</div><div className="muted small ellipsis">{u.email}</div></div>
              <div className="set-role"><Select value={u.role} onChange={r => setRole(u, r)} options={ROLES} disabled={u.id === meUser?.id} ariaLabel={`Role of ${u.email}`} /></div>
              {u.id !== meUser?.id ? <button type="button" className="btn ghost icon-only sm danger-text" aria-label={`Remove ${u.email}`} title="Remove" onClick={() => setDel(u)}><Icon name="trash" size={15} /></button> : <span className="btn-spacer" />}
            </div>
          ))}
        </div>
      </Section>
      <Section title="Add a person" sub={settings?.core ? 'On a box with Pocket Core nobody needs adding: a person signs in with Pocket Core and gets an account on first sight. This is for accounts with a password.' : undefined}>
        <form onSubmit={add} className="stack">
          <div className="two-col">
            <Field label="Email"><input type="email" value={f.email} onChange={e => setF(x => ({ ...x, email: e.target.value }))} required /></Field>
            <Field label="Name"><input value={f.name} onChange={e => setF(x => ({ ...x, name: e.target.value }))} /></Field>
          </div>
          <div className="two-col">
            <Field label="Password" help="At least 8 characters; empty = Pocket Core sign-in only"><input type="password" value={f.password} onChange={e => setF(x => ({ ...x, password: e.target.value }))} autoComplete="new-password" /></Field>
            <Field label="Role" group><Segmented value={f.role} onChange={v => setF(x => ({ ...x, role: v }))} options={ROLES} /></Field>
          </div>
          <div className="form-actions"><button className="btn primary"><Icon name="plus" size={15} />Add</button></div>
        </form>
      </Section>
      {del && <Confirm title={`Remove ${del.email}?`} action="Remove" message="Their places, recent files and tokens are deleted here. Their files stay where they are (built-in files remain on disk)." onCancel={() => setDel(null)} onConfirm={async () => { const u = del; setDel(null); try { await api.del(`${V1}/users/${u.id}`); users.reload() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

// ---------- About ----------

function AboutTab() {
  const { settings, status } = useApp()
  const sys = useAsync(() => api.get<{ version: string; uptime_s: number; core: { configured: boolean; registered: boolean; error: string; public_url: string }; document_server: { configured: boolean; url: string } }>(`${V1}/system`), [])
  return (
    <div className="stack loose">
      <Section title={<span className="set-about-hero"><img src="/favicon.svg" alt="" width={44} height={44} /><span><h3>Pocket Office</h3><p className="muted small">Version {status?.version || '—'}</p></span></span>} />
      <Section title="This box">
        <dl className="set-dl">
          <div><dt>Version</dt><dd>{status?.version}</dd></div>
          <div><dt>Engine</dt><dd>{sys.data?.document_server.configured ? <a href={sys.data.document_server.url} target="_blank" rel="noreferrer">{sys.data.document_server.url}</a> : sys.data ? 'no Document Server set' : '…'}</dd></div>
          <div><dt>Pocket Core</dt><dd>{!sys.data ? '…' : sys.data.core.configured ? (sys.data.core.registered ? <>registered · <a href={sys.data.core.public_url} target="_blank" rel="noreferrer">{sys.data.core.public_url}</a></> : 'not registered: ' + sys.data.core.error) : 'not on this box'}</dd></div>
          <div><dt>Uploads</dt><dd>up to {settings?.max_upload} MB per file</dd></div>
          <div><dt>Source</dt><dd><a href="https://github.com/abjelosevic88/pocket-homelab-office" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-office</a></dd></div>
        </dl>
      </Section>
    </div>
  )
}
