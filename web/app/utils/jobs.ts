export interface Job {
  id: string
  request_hash?: string
  distro?: string
  version: string
  target: string
  profile: string
  status: string
  stage: string
  error: string
  request: unknown
  result: unknown
  stages: { name: string, at: string }[] | null
  enqueued_at: string
  started_at: string | null
  finished_at: string | null
  client?: string
  queued_ms?: number
  duration_ms?: number
  artifact_bytes?: number
  replay_of?: string
  purge_at?: string
  handle?: string
}

export const statuses = [
  { label: '全部状态', value: 'all' },
  { label: '排队中', value: 'queued' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'done' },
  { label: '失败', value: 'failed' },
  { label: '超时', value: 'timeout' },
  { label: '已取消', value: 'cancelled' }
]

export function statusLabel(status: string) {
  if (status === 'timeout') return '超时'
  if (status === 'cancelled') return '已取消'
  return statuses.find(item => item.value === status)?.label ?? status
}

export function statusColor(status: string): 'success' | 'error' | 'info' | 'warning' | 'neutral' {
  if (status === 'done') return 'success'
  if (status === 'failed' || status === 'timeout') return 'error'
  if (status === 'running') return 'info'
  if (status === 'queued') return 'warning'
  return 'neutral'
}

export function statusIcon(status: string): string {
  if (status === 'done') return 'i-lucide-check-circle-2'
  if (status === 'failed') return 'i-lucide-x-circle'
  if (status === 'timeout') return 'i-lucide-clock-alert'
  if (status === 'running') return 'i-lucide-loader-2'
  if (status === 'queued') return 'i-lucide-hourglass'
  if (status === 'cancelled') return 'i-lucide-ban'
  return 'i-lucide-help-circle'
}

export function relativeTime(value: string | null, now = Date.now()): string {
  if (!value || !Number.isFinite(Date.parse(value))) return '—'
  const seconds = (Date.parse(value) - now) / 1000
  const formatter = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
  const units = [['day', 86400], ['hour', 3600], ['minute', 60], ['second', 1]] as const
  const [unit, scale] = units.find(([, size]) => Math.abs(seconds) >= size) ?? units[3]!
  return formatter.format(Math.round(seconds / scale), unit)
}

export function absoluteTime(value: string | null): string {
  return value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString('zh-CN') : '—'
}

export function duration(job: Pick<Job, 'started_at' | 'finished_at' | 'duration_ms'>, now = Date.now()): string {
  if (job.duration_ms !== undefined && job.duration_ms !== null) {
    const sec = Math.floor(job.duration_ms / 1000)
    return sec < 60 ? `${sec} 秒` : `${Math.floor(sec / 60)} 分 ${sec % 60} 秒`
  }
  if (!job.started_at) return '—'
  const seconds = Math.floor(((job.finished_at ? Date.parse(job.finished_at) : now) - Date.parse(job.started_at)) / 1000)
  if (!Number.isFinite(seconds)) return '—'
  const safe = Math.max(0, seconds)
  return safe < 60 ? `${safe} 秒` : `${Math.floor(safe / 60)} 分 ${safe % 60} 秒`
}

export function prettyJSON(value: unknown): string {
  if (typeof value === 'string') {
    try { return JSON.stringify(JSON.parse(value), null, 2) } catch { /* 普通错误字符串保持 JSON 字符串格式。 */ }
  }
  return JSON.stringify(value ?? null, null, 2)
}
