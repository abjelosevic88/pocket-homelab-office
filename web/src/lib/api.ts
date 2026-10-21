// Minimal fetch wrapper for the JSON API.
export class ApiError extends Error {
  status: number
  data: Record<string, unknown>
  constructor(status: number, message: string, data: Record<string, unknown> = {}) {
    super(message)
    this.status = status
    this.data = data
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'X-Pocket-Source': 'web' }
  const init: RequestInit = { method, headers, credentials: 'same-origin' }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  const res = await fetch(url, init)
  if (res.status === 401 && !url.includes('/auth/')) {
    window.dispatchEvent(new CustomEvent('po:unauthorized'))
  }
  if (!res.ok) {
    let msg = res.statusText
    let data: Record<string, unknown> = {}
    try {
      data = await res.json()
      msg = (data.error as string) || msg
    } catch {}
    throw new ApiError(res.status, msg, data)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  get: <T>(url: string) => request<T>('GET', url),
  post: <T>(url: string, body?: unknown) => request<T>('POST', url, body),
  put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body),
  del: <T>(url: string) => request<T>('DELETE', url),
}

export const V1 = '/api/v1'

/** POST files as multipart/form-data, reporting progress. */
export function upload<T>(url: string, files: File[], onProgress?: (pct: number) => void): Promise<T> {
  return new Promise((resolve, reject) => {
    const fd = new FormData()
    for (const f of files) fd.append('file', f, f.name)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', url)
    xhr.setRequestHeader('X-Pocket-Source', 'web')
    xhr.upload.onprogress = e => { if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100)) }
    xhr.onload = () => {
      let j: Record<string, unknown> = {}
      try { j = JSON.parse(xhr.responseText) } catch {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(j as T)
      else reject(new ApiError(xhr.status, (j.error as string) || xhr.statusText, j))
    }
    xhr.onerror = () => reject(new ApiError(0, 'upload failed'))
    xhr.send(fd)
  })
}

export const qs = (o: Record<string, string | number | boolean | undefined | null>) => {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== null && v !== '' && v !== false) p.set(k, String(v))
  const s = p.toString()
  return s ? '?' + s : ''
}
