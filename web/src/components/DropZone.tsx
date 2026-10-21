import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { qs, upload, V1 } from '../lib/api'
import type { Entry } from '../lib/types'
import { useToast } from './ui'
import { Icon } from './icons'
import { announceChange } from '../lib/events'

/** Drop a file anywhere (outside a folder view): it lands in Drafts and opens at once. */
export function useDraftDrop() {
  const { toast } = useToast()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const openFiles = async (files: FileList | File[]) => {
    const arr = Array.from(files).filter(f => f.size > 0)
    if (!arr.length || busy) return
    setBusy(true)
    try {
      const r = await upload<{ source: number; saved: Entry[]; failed: { file: string; error: string }[] }>(`${V1}/drafts`, arr)
      if (r.failed?.length) toast(r.failed.map(f => `${f.file}: ${f.error}`).join('; '), 'err')
      if (r.saved.length) announceChange()
      if (r.saved.length === 1 && r.saved[0].opens) navigate(`/edit/${r.source}${qs({ path: r.saved[0].path })}`)
      else if (r.saved.length) { toast(`${r.saved.length} files in Drafts`); navigate(`/s/${r.source}`) }
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { openFiles, busy }
}

export function GlobalDrop() {
  const loc = useLocation()
  const { openFiles } = useDraftDrop()
  const [over, setOver] = useState(0)
  const inFolder = loc.pathname.startsWith('/s/') || loc.pathname.startsWith('/edit/')
  useEffect(() => {
    if (inFolder) return
    const has = (e: DragEvent) => Array.from(e.dataTransfer?.types || []).includes('Files')
    const enter = (e: DragEvent) => { if (has(e)) { e.preventDefault(); setOver(o => o + 1) } }
    const overH = (e: DragEvent) => { if (has(e)) e.preventDefault() }
    const leave = (e: DragEvent) => { if (has(e)) setOver(o => Math.max(0, o - 1)) }
    const drop = (e: DragEvent) => { if (!has(e)) return; e.preventDefault(); setOver(0); if (e.dataTransfer?.files.length) openFiles(e.dataTransfer.files) }
    window.addEventListener('dragenter', enter); window.addEventListener('dragover', overH); window.addEventListener('dragleave', leave); window.addEventListener('drop', drop)
    return () => { window.removeEventListener('dragenter', enter); window.removeEventListener('dragover', overH); window.removeEventListener('dragleave', leave); window.removeEventListener('drop', drop) }
  }, [inFolder, openFiles])
  if (inFolder || over <= 0) return null
  return <div className="global-drop"><div className="global-drop-card"><span className="global-drop-ico"><Icon name="upload" size={26} /></span><b>Drop to open</b><small>The file becomes a working copy in Drafts until you save it to a place.</small></div></div>
}
