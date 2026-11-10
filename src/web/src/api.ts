export interface ApiErrorBody { code: string; message: string; request_id: string; details?: Record<string, unknown> }

import { toastError } from './toast'

export function uuid(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, c => {
    const r = (Math.random() * 16) | 0
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16)
  })
}

const token = () => localStorage.getItem('clothp_access_token')

export async function api<T>(path: string, options: RequestInit = {}, config: { silent?: boolean } = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body) headers.set('Content-Type', 'application/json')
  if (token()) headers.set('Authorization', `Bearer ${token()}`)
  headers.set('X-Request-Id', `web-${uuid()}`)
  const response = await fetch(`/api/v1${path}`, { ...options, headers })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) {
    const skipAuthRedirect = ['/auth/login', '/auth/unified-login', '/platform/auth/login'].includes(path)
      || path.startsWith('/public/trial-auth/') || path.startsWith('/public/trial-enter')
      || path.startsWith('/public/login-qr/') || path.startsWith('/auth/trial/') || path === '/auth/phone-login'
    if (response.status === 401 && !skipAuthRedirect) {
      localStorage.removeItem('clothp_access_token')
      window.location.href = '/login'
    }
    const error = new Error(body.message || `请求失败 (${response.status})`) as Error & { code?: string; details?: unknown }
    error.code = body.code
    error.details = body.details
    console.error('[api]', path, error.code, error.message)
    if (!config.silent) toastError(error.message)
    throw error
  }
  return body as T
}

export async function platformApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body) headers.set('Content-Type', 'application/json')
  const platformToken = localStorage.getItem('clothp_platform_access_token')
  if (platformToken) headers.set('Authorization', `Bearer ${platformToken}`)
  headers.set('X-Request-Id', `platform-web-${uuid()}`)
  const response = await fetch(`/api/v1/platform${path}`, { ...options, headers })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) {
    if (response.status === 401 && path !== '/auth/login') {
      localStorage.removeItem('clothp_platform_access_token')
      window.location.href = '/platform/login'
    }
    const error = new Error(body.message || `请求失败 (${response.status})`) as Error & { code?: string }
    error.code = body.code
    console.error('[platformApi]', path, error.code, error.message)
    toastError(error.message)
    throw error
  }
  return body as T
}

export function command<T>(path: string, payload: unknown, method = 'POST'): Promise<T> {
  return api<T>(path, {
    method,
    headers: { 'Idempotency-Key': uuid() },
    body: JSON.stringify(payload),
  })
}

export function formatTime(micros?: number | null) {
  if (!micros) return '—'
  return new Date(micros / 1000).toLocaleString('zh-CN', { hour12: false })
}

export function formatDuration(totalSeconds: number | null | undefined): string {
  if (totalSeconds === null || totalSeconds === undefined || Number.isNaN(Number(totalSeconds))) return '—'
  let seconds = Math.trunc(Number(totalSeconds))
  const sign = seconds < 0 ? '-' : ''
  seconds = Math.abs(seconds)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const rest = seconds % 60
  return `${sign}${hours}小时${minutes}分${rest}秒`
}

export const statusText: Record<string, string> = {
  draft: '草稿', active: '启用', disabled: '停用', pending_release: '待下发',
  in_production: '生产中', paused: '暂停中', completed: '已完工', cancelled: '已取消',
  unassigned: '待派工', assigned: '已派工', in_progress: '进行中', pending: '待审批',
  approved: '已通过', rejected: '已驳回', confirmed: '已确认', normal: '正常', reversal: '冲销',
}
