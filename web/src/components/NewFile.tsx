import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Entry, SourceKind } from '../lib/types'
import { sourceColor, sourceIcon } from '../lib/format'
import { Icon } from './icons'
import { useToast } from './ui'

export type NewType = 'docx' | 'xlsx' | 'pptx'
export const NEW_TYPES: { type: NewType; kind: string; letter: string; label: string; key: string }[] = [
  { type: 'docx', kind: 'word', letter: 'W', label: 'Document', key: 'd' },
  { type: 'xlsx', kind: 'cell', letter: 'X', label: 'Spreadsheet', key: 's' },
  { type: 'pptx', kind: 'slide', letter: 'P', label: 'Presentation', key: 'p' },
]

export const newIcon = (kind: string) => kind === 'word' ? 'file-text' : kind === 'cell' ? 'sheet' : 'slides'

export function NewFileButtons({ onPick, busy }: { onPick: (type: NewType) => void; busy?: boolean }) {
  return (
    <div className="new-grid">
      {NEW_TYPES.map(t => (
        <button key={t.type} type="button" className={`new-tile ${t.kind}`} disabled={busy} onClick={() => onPick(t.type)}>
          <span className={`kind-badge ${t.kind} lg`}>{t.letter}</span>
          <span className="new-tile-text"><b>{t.label}</b><small>.{t.type}</small></span>
        </button>
      ))}
    </div>
  )
}

/** A new blank file in the built-in place ("My files"), opened right away. */
export function useNewFile() {
  const { sources } = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const builtin = sources.find(s => s.kind === 'builtin')
  const create = async (type: NewType) => {
    if (!builtin || busy) return
    setBusy(true)
    try {
      const e = await api.post<Entry>(`${V1}/sources/${builtin.id}/new`, { path: '', type })
      navigate(`/edit/${builtin.id}${qs({ path: e.path })}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { create, busy, builtin }
}

/** The kind of place as a small coloured tile with its icon. */
export function PlaceIcon({ kind, size = 22 }: { kind: SourceKind; size?: number }) {
  return <span className="place-ico" style={{ width: size, height: size, ['--c' as string]: sourceColor[kind] }}><Icon name={sourceIcon[kind]} size={Math.round(size * 0.62)} strokeWidth={2} /></span>
}
