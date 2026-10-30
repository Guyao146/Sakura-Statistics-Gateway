export interface Application {
  id: string; name: string; type: 'web' | 'desktop'; enabled: boolean
  allowed_origins: string[]; created_at: string; last_event_at: string | null
}
export interface Metrics { active: number; starts: number; sessions: number; page_views: number; new_visitors: number }
export interface Daily extends Metrics { date: string }
export interface AppMetrics extends Metrics {
  id: string; name: string; type: 'web' | 'desktop'; enabled: boolean; last_event_at: string | null
}
interface StatsBase {
  timezone: string; from: string; to: string; totals: Metrics; daily: Daily[]
}
export interface OverviewStats extends StatsBase {
  apps: AppMetrics[]; versions?: never; pages?: never; last_event_at?: never
}
export interface ApplicationStats extends StatsBase {
  apps?: never
  versions: { version: string; platform: string; active: number; starts: number }[]
  pages: { path: string; page_views: number }[]
  last_event_at: string | null
}
export type Stats = OverviewStats | ApplicationStats
export interface ServiceStatus { status: string; database: string; time: string }
export interface ErrorResponse { error: { code: string; message: string } }
export class ApiError extends Error {
  constructor(message: string, public status: number, public code: string) { super(message) }
}
const errorMessages: Record<string, string> = {
  invalid_credentials: '用户名或密码错误', unauthorized: '登录已失效，请重新登录',
  origin_forbidden: '请求来源不被允许，请检查服务端 PUBLIC_ORIGIN 或应用来源配置',
  rate_limited: '请求过于频繁，请稍后重试', app_not_found: '应用不存在或已被移除',
  invalid_date_range: '日期范围无效，请使用不超过 366 天的完整日期范围',
  invalid_app: '应用名称或允许的来源不符合要求', invalid_json: '请求格式无效',
  internal_error: '服务器暂时无法处理请求，请稍后重试', not_found: '请求的资源不存在',
  method_not_allowed: '服务端不支持此请求方法',
}
export async function api<T>(path: string, init: RequestInit = {}, acceptedStatuses: readonly number[] = []): Promise<T> {
  const response = await fetch(`/api${path}`, {
    ...init, credentials: 'same-origin',
    headers: { ...(init.body ? { 'Content-Type': 'application/json' } : {}), ...init.headers },
  })
  const body = await response.json().catch(() => null)
  if (!response.ok && !acceptedStatuses.includes(response.status)) {
    if (response.status === 401 && path !== '/auth/login' && path !== '/auth/me') window.dispatchEvent(new Event('sakura:unauthorized'))
    const code = typeof body?.error?.code === 'string' ? body.error.code : 'HTTP_ERROR'
    throw new ApiError(errorMessages[code] || body?.error?.message || `请求失败（${response.status}）`, response.status, code)
  }
  if (body === null) throw new ApiError('服务端返回了无效的 JSON 响应', response.status, 'INVALID_RESPONSE')
  return body as T
}
// /api/status returns its normal status document even when the database is unavailable (503).
export function getStatus(): Promise<ServiceStatus> { return api('/status', {}, [503]) }
export function message(error: unknown) { return error instanceof Error ? error.message : '请求失败，请稍后重试' }
export function localDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
export function rangeDays(from: string, to: string): number {
  return (Date.parse(to) - Date.parse(from)) / 86400000 + 1
}
