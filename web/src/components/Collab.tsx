import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { Entry, ReviewHit } from '../lib/types'
import { ago } from '../lib/format'
import { Field, Modal, Segmented, Switch, useToast } from './ui'
import { Icon } from './icons'

export const reviewStatusLabel: Record<string, string> = { draft: 'Review draft', in_review: 'In review', changes_requested: 'Changes requested', approved: 'Approved', rejected: 'Rejected', cancelled: 'Review cancelled' }

/** The badge next to a file that is in Pocket Collaborator; opens the review. */
export function ReviewBadge({ review, small }: { review?: ReviewHit | null; small?: boolean }) {
  if (!review) return null
  const open = review.open_threads > 0 ? ` · ${review.open_threads} open` : ''
  return <a className={`review-badge ${review.status} ${small ? 'sm' : ''}`} href={review.url} target="_blank" rel="noreferrer" onClick={e => e.stopPropagation()} title={`${review.title} — v${review.revisions}, ${reviewStatusLabel[review.status] || review.status}, updated ${ago(review.updated_at)}. Open in Pocket Collaborator`}>
    <span className="dot" />{reviewStatusLabel[review.status] || review.status}{!small && open}
  </a>
}

/** Send a file to Pocket Collaborator: a new review or a new revision. */
export function SendToCollab({ sourceId, file, onClose, beforeSend, onSent }: { sourceId: number | string; file: Entry; onClose: () => void; beforeSend?: () => Promise<void>; onSent?: (hit: { url: string; created: boolean; revision: number }) => void }) {
  const { toast } = useToast()
  const [status, setStatus] = useState<{ available: boolean; error?: string; review?: ReviewHit | null } | null>(null)
  const [mode, setMode] = useState<'revision' | 'new'>('revision')
  const [f, setF] = useState({ title: file.name.replace(/\.[^.]+$/, ''), description: '', note: '', due_date: '', start: true })
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState<{ url: string; created: boolean; revision: number } | null>(null)
  useEffect(() => { api.get<typeof status>(`${V1}/sources/${sourceId}/collab?path=${encodeURIComponent(file.path)}`).then(s => { setStatus(s); if (!s?.review || ['approved', 'rejected', 'cancelled'].includes(s.review.status)) setMode('new') }).catch(e => setStatus({ available: false, error: (e as Error).message })) }, [sourceId, file.path])
  const existing = status?.review && !['approved', 'rejected', 'cancelled'].includes(status.review.status) ? status.review : null
  const send = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try {
      if (beforeSend) await beforeSend()
      const r = await api.post<{ url: string; created: boolean; revision: number }>(`${V1}/sources/${sourceId}/collab/send`, { path: file.path, mode: existing ? mode : 'new', title: f.title, description: f.description, note: f.note, due_date: f.due_date, start: f.start })
      setDone(r); onSent?.(r); toast(r.created ? 'Review started in Pocket Collaborator' : `Sent as revision v${r.revision}`)
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  const isPDF = /\.pdf$/i.test(file.name)
  return (
    <Modal title={`Send “${file.name}” for review`} onClose={onClose}>
      {!status && <div className="loading"><span className="spinner" /> Checking Pocket Collaborator…</div>}
      {status && !status.available && <div className="callout danger"><Icon name="alert" size={15} /><span>Pocket Collaborator is not reachable: {status.error || 'not connected through Pocket Core'}.</span></div>}
      {done && <div className="stack">
        <div className="callout success"><Icon name="check-circle" size={16} className="ok-text" /><span>{done.created ? 'A review was created' : `Revision v${done.revision} was added`}{isPDF ? '' : ' from a PDF rendering of this file'}.</span></div>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Close</button><a className="btn primary" href={done.url} target="_blank" rel="noreferrer">Open in Collaborator<Icon name="external" size={14} /></a></div>
      </div>}
      {status?.available && !done && <form onSubmit={send} className="stack">
        {existing && <Field label="This file is already in review" group>
          <div className="row wrap" style={{ gap: 8 }}><ReviewBadge review={existing} /><span className="muted small">{existing.title} · v{existing.revisions}</span></div>
          <Segmented value={mode} onChange={setMode} options={[{ value: 'revision', label: `Add as revision v${existing.revisions + 1}` }, { value: 'new', label: 'Start a separate review' }]} />
        </Field>}
        {(!existing || mode === 'new') && <>
          <Field label="Review title"><input value={f.title} onChange={e => setF(x => ({ ...x, title: e.target.value }))} required /></Field>
          <Field label="What should people look at?"><textarea value={f.description} onChange={e => setF(x => ({ ...x, description: e.target.value }))} rows={3} placeholder="Optional" /></Field>
          <div className="two-col tight">
            <Field label="Due date"><input type="date" value={f.due_date} onChange={e => setF(x => ({ ...x, due_date: e.target.value }))} /></Field>
            <div className="switch-row" style={{ alignSelf: 'end', paddingBottom: 7 }}><Switch checked={f.start} onChange={v => setF(x => ({ ...x, start: v }))} label="Start the review right away" /><span>Start the review right away</span></div>
          </div>
        </>}
        {existing && mode === 'revision' && <Field label="What changed?"><input value={f.note} onChange={e => setF(x => ({ ...x, note: e.target.value }))} placeholder="Optional note for the reviewers" /></Field>}
        <p className="muted small">{isPDF ? 'The PDF is sent as it is.' : 'Collaborator reviews PDFs: the file is rendered to PDF by the editor engine and sent; the original stays here and the review links back to it.'}{!existing && ' People and roles are added in Collaborator.'}</p>
        {err && <div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Sending…' : existing && mode === 'revision' ? 'Send revision' : 'Send for review'}</button></div>
      </form>}
    </Modal>
  )
}
