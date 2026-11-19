export class ApiError extends Error {
  status: number
  code: string
  data: Record<string, unknown>

  constructor(status: number, err?: { code?: string; message?: string; [k: string]: unknown }) {
    super(err?.message || `请求失败（${status}）`)
    this.status = status
    this.code = err?.code || ''
    this.data = err || {}
  }
}

let onUnauthorized = () => {}
export const setUnauthorizedHandler = (f: () => void) => {
  onUnauthorized = f
}

type Method = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'

export async function api<T = Record<string, unknown>>(method: Method, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const init: RequestInit = { method, credentials: 'same-origin', headers }
  if (body instanceof FormData) init.body = body
  else if (method !== 'GET') {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body ?? {})
  }
  let r: Response
  try {
    r = await fetch('/api/v1' + path, init)
  } catch {
    throw new ApiError(0, { code: 'network', message: '网络连接失败' })
  }
  const text = await r.text()
  let data: { error?: { code?: string; message?: string } } & Record<string, unknown> = {}
  try {
    data = text ? JSON.parse(text) : {}
  } catch {
    /* non-JSON body */
  }
  if (!r.ok) {
    if (r.status === 401 && !path.startsWith('/auth/')) onUnauthorized()
    throw new ApiError(r.status, data.error)
  }
  return data as T
}

export const fileUrl = (id: string) => `/api/v1/files/${encodeURIComponent(id)}`

export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}
