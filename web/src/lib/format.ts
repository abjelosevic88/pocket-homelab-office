import type { Kind, SourceKind } from './types'

export function ago(iso: string | undefined, now = Date.now()): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (isNaN(t)) return ''
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 45) return 'just now'
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.round(m / 60)
  if (h < 24) return `${h} h ago`
  const d = Math.round(h / 24)
  if (d < 7) return d === 1 ? 'yesterday' : `${d} days ago`
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d > 300 ? 'numeric' : undefined })
}

export function bytes(n: number | undefined): string {
  if (!n && n !== 0) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export const kindLabel: Record<Kind, string> = { word: 'Document', cell: 'Spreadsheet', slide: 'Presentation', pdf: 'PDF', diagram: 'Diagram' }
export const kindLetter: Record<Kind, string> = { word: 'W', cell: 'X', slide: 'P', pdf: 'PDF', diagram: 'V' }

export const sourceIcon: Record<SourceKind, string> = { builtin: '🗄', drafts: '📝', folder: '📁', webdav: '🌐', nextcloud: '☁️', documents: '🗂', files: '🗃' }
export const sourceLabel: Record<SourceKind, string> = { builtin: 'Built in', drafts: 'Working copies', folder: 'Server folder', webdav: 'WebDAV', nextcloud: 'Nextcloud', documents: 'Pocket Documents', files: 'Pocket Files' }

export function stem(name: string): string {
  const i = name.lastIndexOf('.')
  return i > 0 ? name.slice(0, i) : name
}

export const isPhone = () => window.matchMedia('(max-width: 760px)').matches
export const isMobileUA = () => /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent) || (navigator.maxTouchPoints > 1 && /Macintosh/.test(navigator.userAgent) && window.innerWidth < 1100)
