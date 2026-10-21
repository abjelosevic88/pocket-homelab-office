import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { AuthStatus, Settings, Source, User } from './types'

interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  sources: Source[]
  theme: 'light' | 'dark'
  toggleTheme: () => void
  refresh: () => Promise<void>
  reloadSources: () => Promise<void>
  reloadSettings: () => Promise<void>
  setUser: (u: User | null) => void
}

const Ctx = createContext<AppState>(null!)
export function useApp() { return useContext(Ctx) }

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [sources, setSources] = useState<Source[]>([])
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (localStorage.getItem('po-theme') as 'light' | 'dark') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'))

  useEffect(() => { document.documentElement.dataset.theme = theme; localStorage.setItem('po-theme', theme) }, [theme])
  useEffect(() => { if (settings?.title) document.title = settings.title }, [settings?.title])

  const reloadSources = useCallback(async () => { setSources(await api.get<Source[]>(`${V1}/sources`)) }, [])
  const reloadSettings = useCallback(async () => { setSettings(await api.get<Settings>(`${V1}/settings`)) }, [])

  const refresh = useCallback(async () => {
    const st = await api.get<AuthStatus>(`${V1}/auth/status`)
    setStatus(st)
    setUser(st.user)
    if (st.authenticated) await Promise.all([reloadSettings(), reloadSources()])
  }, [reloadSettings, reloadSources])

  useEffect(() => { refresh().catch(() => setStatus({ needs_setup: false, auth_mode: 'local', authenticated: false, user: null, version: '', title: 'Pocket Office' })) }, [refresh])
  useEffect(() => {
    const h = () => { setUser(null); setStatus(s => s ? { ...s, authenticated: false, user: null } : s) }
    window.addEventListener('po:unauthorized', h)
    return () => window.removeEventListener('po:unauthorized', h)
  }, [])

  return <Ctx.Provider value={{ status, user, settings, sources, theme, toggleTheme: () => setTheme(t => t === 'dark' ? 'light' : 'dark'), refresh, reloadSources, reloadSettings, setUser }}>{children}</Ctx.Provider>
}
