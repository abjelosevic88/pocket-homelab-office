import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, Settings as S, Source, SourceKind, SourceOptions, User } from '../lib/types'
import { ago, sourceIcon, sourceLabel } from '../lib/format'
import { Card, Confirm, Field, Modal, PageHeader, Segmented, Tabs, useAsync, useToast } from '../components/ui'

type Tab = 'sources' | 'server' | 'account' | 'users' | 'about'

export default function Settings() {
  const { tab } = useParams()
  const navigate = useNavigate()
  const { settings, user } = useApp()
  const tabs: { id: Tab; label: string }[] = [{ id: 'sources', label: 'Places' }, ...(settings?.admin ? [{ id: 'server' as Tab, label: 'Document Server' }] : []), { id: 'account', label: 'Account & API' }, ...(settings?.admin ? [{ id: 'users' as Tab, label: 'People' }] : []), { id: 'about', label: 'About' }]
  const cur = (tabs.find(t => t.id === tab)?.id || 'sources') as Tab
  return (
    <div className="page">
      <PageHeader title="Settings" sub={`Signed in as ${user?.email}`} />
      <Tabs tabs={tabs} value={cur} onChange={t => navigate(`/settings/${t}`)} />
      <div className="mt">
        {cur === 'sources' && <SourcesTab />}
        {cur === 'server' && settings?.admin && <ServerTab />}
        {cur === 'account' && <AccountTab />}
        {cur === 'users' && settings?.admin && <UsersTab />}
        {cur === 'about' && <AboutTab />}
      </div>
    </div>
  )
}

// ---------- Places ----------

function SourcesTab() {
  const { sources, reloadSources, settings } = useApp()
  const { toast } = useToast()
  const opts = useAsync(() => api.get<SourceOptions>(`${V1}/sources/options`), [])
  const [edit, setEdit] = useState<Partial<Source> & { kind: SourceKind; secret?: string } | null>(null)
  const [del, setDel] = useState<Source | null>(null)
  const [testing, setTesting] = useState<number | null>(null)
  const [result, setResult] = useState<Record<number, string>>({})

  const test = async (s: Source) => {
    setTesting(s.id)
    try { const r = await api.post<{ where: string; files: number; folders: number; ms: number }>(`${V1}/sources/${s.id}/test`); setResult(x => ({ ...x, [s.id]: `✓ ${r.where}: ${r.folders} folders, ${r.files} files (${r.ms} ms)` })) } catch (e) { setResult(x => ({ ...x, [s.id]: '✕ ' + (e as Error).message })) } finally { setTesting(null) }
  }
  const remove = async (s: Source) => { setDel(null); try { await api.del(`${V1}/sources/${s.id}`); await reloadSources(); toast('Removed') } catch (e) { toast((e as Error).message, 'err') } }

  const o = opts.data
  return (
    <div className="grid" style={{ gap: 16 }}>
      <Card title="Your places" flush>
        {sources.map(s => (
          <div key={s.id} className="integration">
            <div className="integration-head">
              <span className="integration-icon">{sourceIcon[s.kind]}</span>
              <div className="integration-text"><b>{s.name}</b><br /><span className="muted small">{sourceLabel[s.kind]} · {s.where}</span></div>
              <div className="integration-actions">
                <button className="btn sm" disabled={testing === s.id} onClick={() => test(s)}>{testing === s.id ? 'Testing…' : 'Test'}</button>
                {s.kind !== 'builtin' && s.kind !== 'drafts' && <button className="btn sm" onClick={() => setEdit({ ...s, secret: '' })}>Edit</button>}
                {s.kind !== 'builtin' && s.kind !== 'drafts' && <button className="btn ghost sm danger-text" onClick={() => setDel(s)}>Remove</button>}
              </div>
            </div>
            {result[s.id] && <div className={`integration-result small ${result[s.id].startsWith('✓') ? 'ok' : 'bad'}`}>{result[s.id]}</div>}
          </div>
        ))}
      </Card>
      <Card title="Add a place">
        <div className="storage-kinds">
          <button className="storage-kind" onClick={() => setEdit({ kind: 'nextcloud', via_core: !!o?.core_nextcloud?.ok, folder: '' })}>
            <b>☁️ Nextcloud</b>
            <span className="muted small">{o?.core_nextcloud?.ok ? `Your account through Pocket Core (${o.core_nextcloud.user})` : o?.core ? `Through Pocket Core: ${o.core_nextcloud_error || 'no account yet'}; or type an app password` : 'Address, username and an app password'}</span>
          </button>
          <button className="storage-kind" onClick={() => setEdit({ kind: 'documents', via_core: !!o?.core_documents })}>
            <b>🗂 Pocket Documents</b>
            <span className="muted small">{o?.core_documents ? 'Linked through Pocket Core' : o?.core ? `Through Pocket Core: ${o.core_documents_error || 'link is off'}; or type an API token` : 'Address and an API token'}</span>
          </button>
          {o?.core && <button className="storage-kind" onClick={() => setEdit({ kind: 'files', via_core: true, folder: '' })}>
            <b>🗃 Pocket Files</b>
            <span className="muted small">{o.core_files ? 'Your files, linked through Pocket Core' : `Through Pocket Core: ${o.core_files_error || 'link is off'}`}</span>
          </button>}
          <button className="storage-kind" onClick={() => setEdit({ kind: 'webdav' })}>
            <b>🌐 WebDAV</b>
            <span className="muted small">Any WebDAV folder: another Nextcloud, ownCloud, a NAS…</span>
          </button>
          <button className="storage-kind" disabled={!o?.folder_roots?.length} onClick={() => setEdit({ kind: 'folder', folder: o?.folder_roots?.[0] || '' })}>
            <b>📁 Folder on the box</b>
            <span className="muted small">{o?.folder_roots?.length ? `Under ${o.folder_roots.join(', ')}` : 'None offered (FOLDER_ROOTS is empty)'}</span>
          </button>
        </div>
        {settings?.core && <p className="muted small mt-s">Through Pocket Core means nothing to type: Core hands this app your own Nextcloud app password or Pocket Documents and Pocket Files access, and you or the admin can switch that off there any time.</p>}
      </Card>
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
    <Modal title={`${initial.id ? 'Edit' : 'Add'} ${sourceLabel[kind]}`} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field label="Name" help="How it shows in the sidebar"><input value={f.name} onChange={set('name')} placeholder={sourceLabel[kind]} /></Field>
        {(kind === 'nextcloud' || kind === 'documents') && options?.core && (
          <Field label="Connect" group>
            <Segmented value={f.via_core ? 'core' : 'direct'} onChange={v => setF(x => ({ ...x, via_core: v === 'core' }))} options={[{ value: 'core', label: 'Through Pocket Core' }, { value: 'direct', label: 'Type it in' }]} />
            {f.via_core && !canCore && <span className="help bad">{kind === 'nextcloud' ? options.core_nextcloud_error || 'Pocket Core has no Nextcloud account for you' : options.core_documents_error || 'The Office → Documents link is off in Pocket Core'}</span>}
            {f.via_core && canCore && kind === 'nextcloud' && <span className="help ok">✓ {options.core_nextcloud?.user} at {options.core_nextcloud?.url}</span>}
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
            {options?.core_files ? <span className="help ok">✓ Through Pocket Core, as you</span> : <span className="help bad">{options?.core_files_error || 'The Office → Files link is off in Pocket Core'}</span>}
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
        {err && <div className="callout danger">{err}</div>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Checking…' : initial.id ? 'Save' : 'Connect'}</button></div>
      </form>
    </Modal>
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
    <div className="grid" style={{ gap: 16 }}>
      {settings && settings.engines.length > 1 && <Card title="Default engine for everyone">
        <p className="muted small">New people and anyone who has not picked an engine under Account & API get this one. It is also listed first.</p>
        <Segmented value={settings.default_engine} onChange={setDefault} options={settings.engines.map(e => ({ value: e, label: e === 'collabora' ? 'Collabora Online' : 'ONLYOFFICE' }))} />
      </Card>}
      <Card title="ONLYOFFICE Document Server">
        <p className="muted small">Pocket Office does not render documents itself: an <a href="https://github.com/ONLYOFFICE/DocumentServer" target="_blank" rel="noreferrer">ONLYOFFICE Document Server</a> does, in the browser. Point this at one (the same one Nextcloud uses is fine). These settings override the <code>ONLYOFFICE_*</code> environment{ds?.from_env?.url ? ` (currently ${ds.from_env.url})` : ''}.</p>
        <form onSubmit={save} className="grid mt" style={{ gap: 14 }}>
          <Field label="Address browsers use" help="e.g. https://onlyoffice.example.com — the editor script is loaded from here"><input value={f.url} onChange={set('url')} placeholder="https://onlyoffice.example.com" /></Field>
          <Field label="Address this server uses" help="Optional: how this app reaches Document Server for conversions (e.g. http://onlyoffice:80). Empty = the same as above"><input value={f.internal_url} onChange={set('internal_url')} /></Field>
          <div className="two-col">
            <Field label="JWT secret" help={ds?.secret_set ? 'A secret is set; type one to replace it' : 'JWT_SECRET of the Document Server'}><input type="password" value={f.secret} onChange={set('secret')} placeholder={ds?.secret_set ? '•••••••• (kept)' : ''} /></Field>
            <Field label="JWT header"><input value={f.header} onChange={set('header')} /></Field>
          </div>
          <Field label="How Document Server reaches this app" help="Document Server downloads files from and reports saves to this address, so it must be reachable from the Document Server container (e.g. http://host.docker.internal:8186 or the public address). Empty = CALLBACK_URL, else BASE_URL"><input value={f.callback_url} onChange={set('callback_url')} placeholder={settings?.base_url} /></Field>
          <div className="form-actions"><button type="button" className="btn" disabled={busy} onClick={test}>Test</button><button className="btn primary" disabled={busy}>Save</button></div>
        </form>
        {res && <div className={`callout mt ${res.ok ? 'success' : 'danger'}`}>{res.ok ? <>✓ Document Server answers in {String(res.health_ms)} ms and converted a probe fetched from {String(res.callback_url)} to PDF in {String(res.convert_ms)} ms.</> : <>✕ {String(res.error)}</>}</div>}
      </Card>
      <CollaboraCard />
    </div>
  )
}

function CollaboraCard() {
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
    <Card title="Collabora Online (optional second engine)">
      <p className="muted small">A <a href="https://www.collaboraonline.com/code/" target="_blank" rel="noreferrer">Collabora Online</a> server can edit the same files, drawn on the server instead of in the browser. Pocket Office speaks WOPI to it: Collabora must list this app's WOPI address in its <code>aliasgroup</code> settings (e.g. <code>aliasgroup2: {co?.wopi_url || settings?.base_url}</code>), and only that address's origin may embed Collabora, so it is normally the public address. People then choose their engine under Account & API, or per file from the ⋯ menu.</p>
      <form onSubmit={save} className="grid mt" style={{ gap: 14 }}>
        <Field label="Address browsers use" help="e.g. https://collabora.example.com; empty = Collabora off"><input value={f.url} onChange={e => setF(x => ({ ...x, url: e.target.value }))} placeholder="https://collabora.example.com" /></Field>
        <Field label="Address this server uses" help="Optional: how this app fetches hosting/discovery (e.g. http://collabora:9980)"><input value={f.internal_url} onChange={e => setF(x => ({ ...x, internal_url: e.target.value }))} /></Field>
        <Field label="How Collabora reaches this app (WOPI)" help="Must be reachable from the Collabora container and match the address people open; empty = the public address"><input value={f.wopi_url} onChange={e => setF(x => ({ ...x, wopi_url: e.target.value }))} placeholder={settings?.base_url} /></Field>
        <div className="form-actions"><button type="button" className="btn" disabled={busy || !f.url} onClick={test}>Test</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
      {res && <div className={`callout mt ${res.ok ? 'success' : 'danger'}`}>{res.ok ? <>✓ Collabora answers; discovery lists {String(res.types)} file types ({String(res.discovery_ms)} ms). The WOPI round trip is proven on the first open.</> : <>✕ {String(res.error)}</>}</div>}
    </Card>
  )
}

// ---------- Account ----------

function AccountTab() {
  const { user, refresh, settings, reloadSettings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', current_password: '', new_password: '' })
  const tokens = useAsync(() => api.get<APIToken[]>(`${V1}/auth/tokens`), [])
  const [newTok, setNewTok] = useState<{ token: string; info: APIToken } | null>(null)
  const [tokName, setTokName] = useState('')
  const save = async (e: React.FormEvent) => { e.preventDefault(); try { await api.put(`${V1}/auth/me`, f); await refresh(); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } }
  const mint = async (e: React.FormEvent) => { e.preventDefault(); try { setNewTok(await api.post(`${V1}/auth/tokens`, { name: tokName || 'token' })); setTokName(''); tokens.reload() } catch (e) { toast((e as Error).message, 'err') } }
  const setEngine = async (e: string) => { try { await api.put(`${V1}/settings`, { me: { engine: e } }); await reloadSettings(); toast('Saved') } catch (err) { toast((err as Error).message, 'err') } }
  return (
    <div className="two-col">
      {settings && settings.engines.length > 1 && <Card title="Editor" className="span-2">
        <p className="muted small">Two engines are set up. ONLYOFFICE draws the document in your browser (snappy once loaded, heavier on the machine); Collabora draws it on the server and streams the picture (light on the browser, needs a good connection). Pick your default; the ⋯ menu in any open file switches for that file.</p>
        <Segmented value={settings.engine || settings.engines[0]} onChange={setEngine} options={settings.engines.map(e => ({ value: e, label: e === 'collabora' ? 'Collabora Online' : 'ONLYOFFICE' }))} />
      </Card>}
      <Card title="You">
        <form onSubmit={save} className="grid" style={{ gap: 12 }}>
          <Field label="Email"><input value={user?.email || ''} disabled /></Field>
          <Field label="Name"><input value={f.name} onChange={e => setF(x => ({ ...x, name: e.target.value }))} /></Field>
          <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF(x => ({ ...x, current_password: e.target.value }))} autoComplete="current-password" /></Field>
          <Field label="New password" help="Leave empty to keep it"><input type="password" value={f.new_password} onChange={e => setF(x => ({ ...x, new_password: e.target.value }))} autoComplete="new-password" /></Field>
          <div className="form-actions"><button className="btn primary">Save</button></div>
        </form>
      </Card>
      <Card title="API tokens">
        <p className="muted small">Bearer tokens for scripts and the Homepage widget: <code>Authorization: Bearer po_…</code> against <code>/api/v1/…</code>. Pocket Core device tokens work too.</p>
        <form onSubmit={mint} className="row mt-s"><input value={tokName} onChange={e => setTokName(e.target.value)} placeholder="What is it for?" /><button className="btn">Create</button></form>
        {newTok && <div className="callout success mt-s small">Copy it now, it is not shown again:<br /><code style={{ wordBreak: 'break-all' }}>{newTok.token}</code></div>}
        <table className="table compact mt-s"><tbody>
          {tokens.data?.map(t => <tr key={t.id}><td>{t.name}</td><td className="muted mono">{t.prefix}…</td><td className="muted small">{t.last_used_at ? 'used ' + ago(t.last_used_at) : 'never used'}</td><td className="right"><button className="btn ghost xs danger-text" onClick={async () => { await api.del(`${V1}/auth/tokens/${t.id}`); tokens.reload() }}>Revoke</button></td></tr>)}
          {tokens.data?.length === 0 && <tr><td className="muted">No tokens yet</td></tr>}
        </tbody></table>
      </Card>
    </div>
  )
}

// ---------- People ----------

function UsersTab() {
  const { toast } = useToast()
  const { settings, user: meUser } = useApp()
  const users = useAsync(() => api.get<User[]>(`${V1}/users`), [])
  const [f, setF] = useState({ email: '', name: '', password: '', role: 'member' })
  const [del, setDel] = useState<User | null>(null)
  const add = async (e: React.FormEvent) => { e.preventDefault(); try { await api.post(`${V1}/users`, f); setF({ email: '', name: '', password: '', role: 'member' }); users.reload(); toast('Added') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="grid" style={{ gap: 16 }}>
      <Card title="People" flush>
        <table className="table"><thead><tr><th>Name</th><th>Email</th><th>Role</th><th></th></tr></thead><tbody>
          {users.data?.map(u => <tr key={u.id}><td>{u.name || '—'}</td><td>{u.email}</td><td>
            <select value={u.role} disabled={u.id === meUser?.id} onChange={async e => { try { await api.put(`${V1}/users/${u.id}`, { role: e.target.value }); users.reload() } catch (err) { toast((err as Error).message, 'err') } }}><option value="member">member</option><option value="admin">admin</option></select>
          </td><td className="right">{u.id !== meUser?.id && <button className="btn ghost xs danger-text" onClick={() => setDel(u)}>Remove</button>}</td></tr>)}
        </tbody></table>
      </Card>
      <Card title="Add a person">
        {settings?.core && <p className="muted small">On a box with Pocket Core nobody needs adding: a person signs in with Pocket Core and gets an account on first sight. This is for accounts with a password.</p>}
        <form onSubmit={add} className="integration-form mt-s">
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF(x => ({ ...x, email: e.target.value }))} required /></Field>
          <Field label="Name"><input value={f.name} onChange={e => setF(x => ({ ...x, name: e.target.value }))} /></Field>
          <Field label="Password" help="At least 8 characters; empty = Pocket Core sign-in only"><input type="password" value={f.password} onChange={e => setF(x => ({ ...x, password: e.target.value }))} /></Field>
          <Field label="Role"><select value={f.role} onChange={e => setF(x => ({ ...x, role: e.target.value }))}><option value="member">member</option><option value="admin">admin</option></select></Field>
          <div className="form-actions" style={{ gridColumn: '1 / -1' }}><button className="btn primary">Add</button></div>
        </form>
      </Card>
      {del && <Confirm title={`Remove ${del.email}?`} action="Remove" message="Their places, recent files and tokens are deleted here. Their files stay where they are (built-in files remain on disk)." onCancel={() => setDel(null)} onConfirm={async () => { const u = del; setDel(null); try { await api.del(`${V1}/users/${u.id}`); users.reload() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function AboutTab() {
  const { settings, status } = useApp()
  const sys = useAsync(() => api.get<{ version: string; uptime_s: number; core: { configured: boolean; registered: boolean; error: string; public_url: string }; document_server: { configured: boolean; url: string } }>(`${V1}/system`), [])
  return (
    <Card title="About">
      <table className="table info"><tbody>
        <tr><th>Version</th><td>{status?.version}</td></tr>
        <tr><th>Engine</th><td>{sys.data?.document_server.configured ? <a href={sys.data.document_server.url} target="_blank" rel="noreferrer">{sys.data.document_server.url}</a> : 'no Document Server set'}</td></tr>
        <tr><th>Pocket Core</th><td>{sys.data?.core.configured ? (sys.data.core.registered ? <>registered · <a href={sys.data.core.public_url} target="_blank" rel="noreferrer">{sys.data.core.public_url}</a></> : 'not registered: ' + sys.data.core.error) : 'not on this box'}</td></tr>
        <tr><th>Uploads</th><td>up to {settings?.max_upload} MB per file</td></tr>
        <tr><th>Source</th><td><a href="https://github.com/abjelosevic88/pocket-homelab-office" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-office</a></td></tr>
      </tbody></table>
    </Card>
  )
}
