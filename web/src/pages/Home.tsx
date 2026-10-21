import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, Recent } from '../lib/types'
import { ago, sourceIcon, sourceLabel } from '../lib/format'
import { Card, Empty, KindBadge, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { NewFileButtons } from '../components/NewFile'

export default function Home() {
  const { sources, settings, user } = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const recent = useAsync(() => api.get<Recent[]>(`${V1}/recent`), [])
  const [busy, setBusy] = useState(false)
  const builtin = sources.find(s => s.kind === 'builtin')
  const ds = settings?.document_server

  const create = async (type: string) => {
    if (!builtin) return
    setBusy(true)
    try {
      const e = await api.post<Entry>(`${V1}/sources/${builtin.id}/new`, { path: '', type })
      navigate(`/edit/${builtin.id}${qs({ path: e.path })}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }

  return (
    <div className="page">
      <PageHeader title={`Hello${user?.name ? ', ' + user.name.split(' ')[0] : ''}`} sub="Word, Excel and PowerPoint files from your own storage, edited right here." />
      {ds && !ds.configured && <div className="callout warning mb">No Document Server is set up yet, so files cannot be opened. {settings?.admin ? <Link to="/settings/server">Set one up in Settings → Document Server.</Link> : 'Ask the admin to set one up in Settings → Document Server.'}</div>}

      <Card title="Start something new" className="mb">
        <NewFileButtons onPick={create} busy={busy} />
        <p className="muted small mt-s">New files land in <b>{builtin?.name || 'My files'}</b>; move or copy them anywhere later, or start one inside any place.</p>
      </Card>

      <div className="home-grid">
        <Card title="Recent" flush actions={recent.data && recent.data.length > 0 ? <button className="btn ghost sm" onClick={async () => { await api.del(`${V1}/recent`); recent.reload() }}>Clear</button> : undefined}>
          {recent.loading && <Loading />}
          {recent.data && recent.data.length === 0 && <Empty title="Nothing opened yet">Open a file from one of your places and it shows up here.</Empty>}
          {recent.data && recent.data.length > 0 && <ul className="file-list">
            {recent.data.slice(0, 20).map(r => (
              <li key={`${r.source_id}:${r.path}`}>
                <Link className="file-row" to={`/edit/${r.source_id}${qs({ path: r.path })}`}>
                  <KindBadge kind={r.kind} ext={r.ext} />
                  <span className="grow"><span className="ellipsis">{r.name}</span><small className="muted ellipsis">{sourceIcon[r.source_kind]} {r.source_name}{r.path.includes('/') ? ' · ' + r.path.slice(0, r.path.lastIndexOf('/')) : ''}</small></span>
                  <small className="muted nowrap">{ago(r.opened_at)}</small>
                </Link>
              </li>
            ))}
          </ul>}
        </Card>
        <Card title="Places" flush actions={<Link className="btn ghost sm" to="/settings/sources">Manage</Link>}>
          <ul className="file-list">
            {sources.map(s => (
              <li key={s.id}>
                <Link className="file-row" to={`/s/${s.id}`}>
                  <span className="src-ico">{sourceIcon[s.kind]}</span>
                  <span className="grow"><span className="ellipsis">{s.name}</span><small className="muted ellipsis">{sourceLabel[s.kind]} · {s.where}</small></span>
                  <span className="muted">›</span>
                </Link>
              </li>
            ))}
          </ul>
          {sources.length <= 1 && <div className="pad muted small">Add your Nextcloud, Pocket Documents, a WebDAV server or a folder on the box under <Link to="/settings/sources">Settings → Places</Link>.</div>}
        </Card>
      </div>
    </div>
  )
}
