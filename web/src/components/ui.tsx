import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'

type Toast = { id: number; msg: string; kind: 'ok' | 'err' }
const ToastCtx = createContext<{ toast: (msg: string, kind?: 'ok' | 'err') => void }>({ toast: () => {} })
export function useToast() { return useContext(ToastCtx) }
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = useState<Toast[]>([])
  const toast = useCallback((msg: string, kind: 'ok' | 'err' = 'ok') => {
    const id = Date.now() + Math.random()
    setItems(t => [...t, { id, msg, kind }])
    setTimeout(() => setItems(t => t.filter(x => x.id !== id)), kind === 'err' ? 6000 : 2500)
  }, [])
  return (
    <ToastCtx.Provider value={{ toast }}>
      {children}
      <div className="toasts">{items.map(t => <div key={t.id} className={`toast ${t.kind}`}>{t.msg}</div>)}</div>
    </ToastCtx.Provider>
  )
}

export function PageHeader({ title, sub, actions }: { title: React.ReactNode; sub?: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="page-header">
      <div><h1>{title}</h1>{sub && <div className="sub">{sub}</div>}</div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  )
}

export function Card({ title, actions, children, flush, className = '' }: { title?: React.ReactNode; actions?: React.ReactNode; children: React.ReactNode; flush?: boolean; className?: string }) {
  return (
    <section className={`card ${className}`}>
      {(title || actions) && <div className="card-head"><h2>{title}</h2>{actions && <div className="row">{actions}</div>}</div>}
      <div className={`card-body ${flush ? 'flush' : ''}`}>{children}</div>
    </section>
  )
}

export function Field({ label, help, children, className = '', group = false }: { label: React.ReactNode; help?: React.ReactNode; children: React.ReactNode; className?: string; group?: boolean }) {
  const Tag = group ? 'div' : 'label'
  return <Tag className={`field ${className}`}><span className="label">{label}</span>{children}{help && <span className="help">{help}</span>}</Tag>
}

export function Loading({ text = 'Loading…' }: { text?: string }) { return <div className="loading"><span className="spinner" /> {text}</div> }

export function Empty({ title, children }: { title: string; children?: React.ReactNode }) {
  return <div className="empty"><h3>{title}</h3>{children && <div className="muted">{children}</div>}</div>
}

export function Modal({ title, onClose, children, size = '' }: { title: React.ReactNode; onClose: () => void; children: React.ReactNode; size?: '' | 'lg' }) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])
  return (
    <div className="modal-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className={`modal ${size}`} role="dialog" aria-modal="true">
        <div className="modal-head"><h2>{title}</h2><button type="button" className="btn ghost icon" onClick={onClose} aria-label="Close">✕</button></div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  )
}

export function Confirm({ title, message, onConfirm, onCancel, danger = true, action = 'Confirm' }: { title: string; message: React.ReactNode; onConfirm: () => void; onCancel: () => void; danger?: boolean; action?: string }) {
  return (
    <Modal title={title} onClose={onCancel}>
      <p>{message}</p>
      <div className="form-actions">
        <button type="button" className="btn" onClick={onCancel}>Cancel</button>
        <button type="button" className={`btn ${danger ? 'danger' : 'primary'}`} onClick={onConfirm}>{action}</button>
      </div>
    </Modal>
  )
}

export function Prompt({ title, label, initial = '', action = 'OK', onDone, onClose, selectStem = false }: { title: string; label?: string; initial?: string; action?: string; onDone: (v: string) => void; onClose: () => void; selectStem?: boolean }) {
  const [v, setV] = useState(initial)
  return (
    <Modal title={title} onClose={onClose}>
      <form onSubmit={e => { e.preventDefault(); if (v.trim()) onDone(v.trim()) }} className="grid" style={{ gap: 14 }}>
        <Field label={label || 'Name'}><input autoFocus value={v} onChange={e => setV(e.target.value)} onFocus={e => { if (selectStem) { const i = e.target.value.lastIndexOf('.'); e.target.setSelectionRange(0, i > 0 ? i : e.target.value.length) } else e.target.select() }} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">{action}</button></div>
      </form>
    </Modal>
  )
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: string }[]; value: T; onChange: (t: T) => void }) {
  return <div className="tabs">{tabs.map(t => <button key={t.id} type="button" className={t.id === value ? 'active' : ''} onClick={() => onChange(t.id)}>{t.label}</button>)}</div>
}

export function Segmented<T extends string | number>({ options, value, onChange, size = '' }: { options: { value: T; label: React.ReactNode; title?: string }[]; value: T | null; onChange: (v: T) => void; size?: '' | 'sm' }) {
  return (
    <div className={`segmented ${size}`} role="radiogroup">
      {options.map(o => <button key={String(o.value)} type="button" role="radio" aria-checked={o.value === value} title={o.title} className={o.value === value ? 'active' : ''} onClick={() => onChange(o.value)}>{o.label}</button>)}
    </div>
  )
}

export function Menu({ button, children, align = 'right' }: { button: React.ReactNode; children: (close: () => void) => React.ReactNode; align?: 'left' | 'right' }) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open) return
    const h = () => setOpen(false)
    window.addEventListener('click', h)
    return () => window.removeEventListener('click', h)
  }, [open])
  return (
    <span className="menu-wrap" onClick={e => e.stopPropagation()}>
      <span onClick={() => setOpen(o => !o)}>{button}</span>
      {open && <div className={`menu ${align}`}>{children(() => setOpen(false))}</div>}
    </span>
  )
}

export function useAsync<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const run = useCallback(() => {
    setLoading(true)
    return fn().then(d => { setData(d); setError('') }).catch(e => setError((e as Error).message)).finally(() => setLoading(false))
  }, deps) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { run() }, [run])
  return { data, setData, error, loading, reload: run }
}

export function useDebounce<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value)
  useEffect(() => { const t = setTimeout(() => setV(value), ms); return () => clearTimeout(t) }, [value, ms])
  return v
}

/** A file-type badge: W / X / P / PDF in the family's colour. */
export function KindBadge({ kind, ext, size = '' }: { kind?: string; ext?: string; size?: '' | 'lg' }) {
  const k = kind || 'other'
  const letter = k === 'word' ? 'W' : k === 'cell' ? 'X' : k === 'slide' ? 'P' : k === 'pdf' ? 'PDF' : k === 'diagram' ? 'V' : (ext || '?').slice(0, 3).toUpperCase()
  return <span className={`kind-badge ${k} ${size}`} title={ext ? '.' + ext : ''}>{letter}</span>
}
