import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { AuthStatus, Settings, Source, User } from './types'

export type ThemeMode = 'system' | 'light' | 'dark'

interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  sources: Source[]
  themeMode: ThemeMode
  theme: 'light' | 'dark'
  setThemeMode: (m: ThemeMode) => void
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
  // The theme follows the system unless set to light or dark. (A new key: the old one
  // always stored the system's value, which would pin everyone to it.)
  const [themeMode, setThemeModeRaw] = useState<ThemeMode>(() => (localStorage.getItem('po-theme-mode') as ThemeMode) || 'system')
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => setSystemDark(mq.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])
  const theme: 'light' | 'dark' = themeMode === 'system' ? (systemDark ? 'dark' : 'light') : themeMode
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.querySelectorAll('meta[name="theme-color"]').forEach(m => m.setAttribute('content', theme === 'dark' ? '#111113' : '#ffffff'))
  }, [theme])
  const setThemeMode = useCallback((m: ThemeMode) => { setThemeModeRaw(m); if (m === 'system') localStorage.removeItem('po-theme-mode'); else localStorage.setItem('po-theme-mode', m) }, [])
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

  return <Ctx.Provider value={{ status, user, settings, sources, themeMode, theme, setThemeMode, toggleTheme: () => setThemeMode(theme === 'dark' ? 'light' : 'dark'), refresh, reloadSources, reloadSettings, setUser }}>{children}</Ctx.Provider>
}
