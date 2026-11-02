export interface Application {
  id: string; name: string; type: 'web' | 'desktop'; enabled: boolean
  allowed_origins: string[]
  allowed_path_prefixes: string[]; normalize_numeric_paths: boolean
  created_at: string; last_event_at: string | null
}
export interface Metrics { active: number; starts: number; sessions: number; page_views: number; new_visitors: number; custom_events: number }
export interface Daily extends Metrics { date: string }
export interface AppMetrics extends Metrics {
  id: string; name: string; type: 'web' | 'desktop'; enabled: boolean; last_event_at: string | null
  allowed_path_prefixes: string[]; normalize_numeric_paths: boolean
}
export interface CustomEventStat { name: string; count: number; visitors: number }
interface StatsBase {
  timezone: string; from: string; to: string; totals: Metrics; daily: Daily[]
}
export interface OverviewStats extends StatsBase {
  apps: AppMetrics[]; versions?: never; pages?: never; custom_events?: never; last_event_at?: never
}
export interface ApplicationStats extends StatsBase {
  apps?: never
  versions: { version: string; platform: string; active: number; starts: number }[]
  pages: { path: string; page_views: number }[]
  custom_events: CustomEventStat[]
  last_event_at: string | null
}
export type Stats = OverviewStats | ApplicationStats
export interface ServiceStatus { status: string; database: string; time: string }
export interface ServiceSettings {
  version: string; statistics_timezone: string; retention_days: number
  rate_limit_per_second: number; rate_limit_burst: number
}
export interface CollectCounters { requests: number; accepted: number; duplicates: number; rejected: number; rate_limited: number }
export interface CollectAppCounters extends CollectCounters { id: string; name: string }
export interface DailyCounters extends CollectCounters { date: string }
export interface GatewayMetrics {
  timezone: string; from: string; to: string
  totals: CollectCounters; apps: CollectAppCounters[]; daily: DailyCounters[]
}
export interface RetentionCohort { cohort: string; size: number; retained: number[] }
export interface RetentionReport {
  weeks: number; cohorts: RetentionCohort[]
  timezone?: string; today?: string
}
export interface RebuildResult { rebuilt_days: number; events: number; visitors: number }
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
  invalid_path_rules: '路径规则无效', invalid_weeks: '周数须为 1–12',
  rebuild_outside_retention: '日期超出原始事件保留范围，无法安全重建',
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
export function deleteApp(id: string): Promise<{ ok: boolean }> {
  return api(`/apps/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
export function rebuildApp(id: string, from: string, to: string): Promise<RebuildResult> {
  return api(`/apps/${encodeURIComponent(id)}/rebuild?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`, { method: 'POST' })
}
export function getRetention(id: string, weeks: number): Promise<RetentionReport> {
  return api(`/apps/${encodeURIComponent(id)}/retention?weeks=${weeks}`)
}
export function getMetrics(from: string, to: string): Promise<GatewayMetrics> {
  return api(`/metrics?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`)
}
export function getSettings(): Promise<ServiceSettings> {
  return api('/settings')
}
export function message(error: unknown) { return error instanceof Error ? error.message : '请求失败，请稍后重试' }
export function localDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
export function rangeDays(from: string, to: string): number {
  return (Date.parse(to) - Date.parse(from)) / 86400000 + 1
}
// A retention cell reads "count (percent)"; a missing count or a zero-size cohort has no meaningful value.
export function formatRetentionCell(count: number | null | undefined, size: number): string {
  if (!size || count === null || count === undefined) return '—'
  return `${count} (${Math.round((count / size) * 100)}%)`
}
// Deleting an application is irreversible, so the confirmation input must equal the full name.
export function deleteConfirmationMatches(appName: string, input: string): boolean {
  return appName !== '' && input === appName
}
export function dateInTimezone(date: Date, timezone = 'Asia/Shanghai'): string {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit',
    }).formatToParts(date)
    const value = (type: string) => parts.find(part => part.type === type)?.value || ''
    return `${value('year')}-${value('month')}-${value('day')}`
  } catch {
    return localDate(date)
  }
}
export function retentionWeekState(cohort: string, index: number, today: string): 'future' | 'current' | 'complete' {
  const start = Date.parse(`${cohort}T00:00:00Z`) + index * 7 * 86400000
  const day = Date.parse(`${today}T00:00:00Z`)
  if (!Number.isFinite(start) || !Number.isFinite(day) || start > day) return 'future'
  return day < start + 7 * 86400000 ? 'current' : 'complete'
}
// Client-side mirror of the backend: UTF-8 length, control characters, and a single leading slash.
export function validatePathPrefixes(prefixes: string[]): string {
  if (new Set(prefixes).size > 50) return '路径白名单最多 50 项'
  for (const prefix of prefixes) {
    if (!prefix.startsWith('/')) return '路径前缀必须以 / 开头'
    if (prefix.startsWith('//')) return '路径前缀不能以连续斜杠（//）开头'
    if (new TextEncoder().encode(prefix).length > 256) return '每个路径前缀最多 256 字节'
    if (/[\u0000-\u001f\u007f-\u009f]/.test(prefix)) return '路径前缀不能包含控制字符'
  }
  return ''
}
