import { useRef } from 'react'
import { Link } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Recent } from '../lib/types'
import { ago, sourceLabel } from '../lib/format'
import { useOnChange } from '../lib/events'
import { Card, Empty, KindBadge, Loading, PageHeader, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { NewFileButtons, PlaceIcon, useNewFile } from '../components/NewFile'
import { useDraftDrop } from '../components/DropZone'

export default function Home() {
  const { sources, settings, user } = useApp()
  const recent = useAsync(() => api.get<Recent[]>(`${V1}/recent`), [])
  useOnChange(() => recent.reload())
  const { create, busy, builtin } = useNewFile()
  const { openFiles, busy: opening } = useDraftDrop()
  const fileInput = useRef<HTMLInputElement>(null)
  const ds = settings?.document_server

  return (
    <div className="page home">
      <PageHeader title={`Hello${user?.name ? ', ' + user.name.split(' ')[0] : ''}`} sub="Word, Excel and PowerPoint files from your own storage, edited right here." />
      {ds && !ds.configured && <div className="callout warn mb"><Icon name="alert" size={16} /><span>No Document Server is set up yet, so files cannot be opened. {settings?.admin ? <Link to="/settings/server">Set one up in Settings → Document Server.</Link> : 'Ask the admin to set one up in Settings → Document Server.'}</span></div>}

      <Card title="Start something new" sub={<>New files land in <b>{builtin?.name || 'My files'}</b>; move them anywhere later, or start one inside any place.</>} className="mb">
        <NewFileButtons onPick={create} busy={busy} />
        <div className="open-device">
          <button type="button" className="btn" disabled={opening} onClick={() => fileInput.current?.click()}><Icon name="upload" size={15} />{opening ? 'Opening…' : 'Open a file from this device'}</button>
          <span className="muted small">…or drop one anywhere on this page. It opens right away as a working copy in <b>Drafts</b>; <b>Save to…</b> puts it into a place when you are done.</span>
          <input ref={fileInput} type="file" multiple hidden onChange={e => { if (e.target.files) openFiles(e.target.files); e.target.value = '' }} />
        </div>
      </Card>

      <div className="home-grid">
        <Card title={<><Icon name="history" size={16} />Recent</>} flush actions={recent.data && recent.data.length > 0 ? <button type="button" className="btn ghost sm" onClick={async () => { await api.del(`${V1}/recent`); recent.reload() }}>Clear</button> : undefined}>
          {recent.loading && !recent.data && <Loading />}
          {recent.data && recent.data.length === 0 && <Empty icon="file-text" title="Nothing opened yet">Open a file from one of your places and it shows up here.</Empty>}
          {recent.data && recent.data.length > 0 && <ul className="file-list">
            {recent.data.slice(0, 20).map(r => (
              <li key={`${r.source_id}:${r.path}`}>
                <Link className="file-row" to={`/edit/${r.source_id}${qs({ path: r.path })}`}>
                  <KindBadge kind={r.kind} ext={r.ext} />
                  <span className="file-row-text"><span className="ellipsis">{r.name}</span><small className="muted ellipsis">{r.source_name}{r.path.includes('/') ? ' · ' + r.path.slice(0, r.path.lastIndexOf('/')) : ''}</small></span>
                  <small className="muted nowrap">{ago(r.opened_at)}</small>
                </Link>
              </li>
            ))}
          </ul>}
        </Card>
        <Card title={<><Icon name="folder" size={16} />Places</>} flush actions={<Link className="btn ghost sm" to="/settings/places">Manage</Link>}>
          <ul className="file-list">
            {sources.map(s => (
              <li key={s.id}>
                <Link className="file-row" to={`/s/${s.id}`}>
                  <PlaceIcon kind={s.kind} size={30} />
                  <span className="file-row-text"><span className="ellipsis">{s.name}</span><small className="muted ellipsis">{sourceLabel[s.kind]} · {s.where}</small></span>
                  {s.error ? <span className="src-warn" title={s.error}><Icon name="alert" size={15} /></span> : <Icon name="chevron-right" size={15} className="faint" />}
                </Link>
              </li>
            ))}
          </ul>
          {!sources.some(s => s.kind !== 'drafts' && s.kind !== 'builtin') && <div className="card-note muted small">Add your Nextcloud, Pocket Documents, Pocket Files, a WebDAV server or a folder on the box under <Link to="/settings/places">Settings → Places</Link>.</div>}
        </Card>
      </div>
    </div>
  )
}
