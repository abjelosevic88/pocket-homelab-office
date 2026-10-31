// /open/files?path=… — a document from Pocket Files (its WebDAV path), opened in the editor.
// Pocket Files loads this in a frame (embed=1); the first time, the person's "Pocket Files"
// place is made through Pocket Core.
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { Icon } from '../components/icons'
import { Loading } from '../components/ui'

export default function OpenFiles() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [err, setErr] = useState('')
  const path = (params.get('path') || '').replace(/^\/+/, '')
  const embed = params.get('embed') === '1' ? '1' : ''
  useEffect(() => {
    api.post<{ id: number }>(`${V1}/sources/files/ensure`)
      .then(src => navigate(`/edit/${src.id}${qs({ path, embed })}`, { replace: true }))
      .catch(e => setErr((e as Error).message))
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  if (err) return (
    <div className="editor-overlay"><div className="editor-error">
      <span className="empty-ico"><Icon name="alert" size={22} /></span>
      <h3>Pocket Office cannot reach Pocket Files</h3><p className="muted">{err}</p>
      {embed && <button type="button" className="btn" onClick={() => window.parent.postMessage({ type: 'pocket-office:close' }, '*')}>Close</button>}
    </div></div>
  )
  return <Loading text="Opening…" />
}
