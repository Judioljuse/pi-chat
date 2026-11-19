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

/** Messages for error responses without a JSON body (proxies, gateways). */
function nonJsonError(status: number): string | undefined {
  if (status === 413) return '文件太大（如果前面有 nginx，默认只允许 1 MB：调大 client_max_body_size）'
  if (status === 502 || status === 503 || status === 504) return '服务器暂时不可用，稍后重试'
  return undefined
}

export async function api<T = Record<string, unknown>>(
  method: Method,
  path: string,
  body?: unknown,
  opts?: { signal?: AbortSignal },
): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const init: RequestInit = { method, credentials: 'same-origin', headers, signal: opts?.signal }
  if (body instanceof FormData) init.body = body
  else if (method !== 'GET') {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body ?? {})
  }
  let r: Response
  try {
    r = await fetch('/api/v1' + path, init)
  } catch (e) {
    if (opts?.signal?.aborted) throw e
    const message = body instanceof FormData ? '网络错误，上传没有完成' : '网络连接失败'
    throw new ApiError(0, { code: 'network', message })
  }
  let text: string
  try {
    text = await r.text()
  } catch (e) {
    if (opts?.signal?.aborted) throw e
    text = ''
  }
  let data: { error?: { code?: string; message?: string } } & Record<string, unknown> = {}
  let json = false
  try {
    data = text ? JSON.parse(text) : {}
    json = !!text
  } catch {
    /* non-JSON body */
  }
  if (!r.ok) {
    if (r.status === 401 && !path.startsWith('/auth/')) onUnauthorized()
    const err = data.error ?? (!json ? { code: 'http', message: nonJsonError(r.status) } : undefined)
    throw new ApiError(r.status, err)
  }
  return data as T
}

export const fileUrl = (id: string) => `/api/v1/files/${encodeURIComponent(id)}`

export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}
