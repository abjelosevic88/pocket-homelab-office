export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }

export interface AuthStatus {
  needs_setup: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; title: string
  oidc?: boolean; oidc_name?: string; password_login?: boolean; core?: boolean
}

export type SourceKind = 'builtin' | 'drafts' | 'folder' | 'webdav' | 'nextcloud' | 'documents'

export interface Source {
  id: number; kind: SourceKind; name: string; url: string; username: string; secret_set: boolean; folder: string; via_core: boolean
  position: number; created_at: string; where: string; writable: boolean; tree: boolean; error?: string
}

export type Kind = 'word' | 'cell' | 'slide' | 'pdf' | 'diagram'

export interface Entry {
  path: string; name: string; is_dir: boolean; size: number; modified: string; etag?: string; file_id?: string
  kind?: Kind; opens: boolean; edit: boolean; ext?: string; exports?: string[]
}

export interface Format { ext: string; kind: Kind; edit: boolean; lossy?: boolean; convertible: boolean; mime: string; exports: string[] }

export interface Listing { source: number; path: string; entries: Entry[]; writable: boolean; tree: boolean }

export interface Recent { source_id: number; path: string; name: string; opened_at: string; source_name: string; source_kind: SourceKind; kind?: Kind; ext?: string; edit: boolean }

export interface SearchHit extends Entry { source_id: number; source_name: string; source_kind: SourceKind }

export type Engine = 'onlyoffice' | 'collabora'

export interface OpenResult {
  engine: Engine; engines: Engine[]
  collabora?: { action_url: string; access_token: string; access_token_ttl: string; wopi_src: string; edit: boolean; origin: string }
  api_js?: string; config?: Record<string, unknown>; key: string; mode: 'edit' | 'view'; kind: Kind; ext: string; edit: boolean; convertible: boolean
  editable_as: string; lossy?: boolean; exports: string[]; file: Entry; phone_view: boolean; draft?: boolean
}

export interface Settings {
  engines: Engine[]; engine: Engine | ''
  collabora?: { configured: boolean; url: string; internal_url?: string; wopi_url?: string; from_env?: { url: string; internal_url: string; wopi_url: string } }
  title: string; admin: boolean; core: boolean; core_url: string; base_url: string; max_upload: number; me: Record<string, string>
  document_server: { configured: boolean; url: string; internal_url?: string; header?: string; secret_set?: boolean; callback_url?: string; from_env?: { url: string; internal_url: string; callback_url: string; secret_set: boolean } }
  folder_roots?: string[]
}

export interface SourceOptions {
  core: boolean; folder_roots: string[]
  core_nextcloud?: { url: string; user: string; ok: boolean }; core_nextcloud_error?: string
  core_documents?: boolean; core_documents_error?: string
}

export interface APIToken { id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }
