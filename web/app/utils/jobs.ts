export interface Job {
  id: string
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
}

export const statuses = [
  { label: '全部状态', value: 'all' },
  { label: '排队中', value: 'queued' },
  { label: '运行中', value: 'running' },
  { label: '已完成', value: 'done' },
  { label: '失败', value: 'failed' },
  { label: '已结束', value: 'finished' }
]

export function statusLabel(status: string) {
  return statuses.find(item => item.value === status)?.label ?? status
}

export function statusColor(status: string) {
  if (status === 'done') return 'success'
  if (status === 'failed') return 'error'
  if (status === 'running') return 'info'
  if (status === 'queued') return 'warning'
  return 'neutral'
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

export function duration(job: Pick<Job, 'started_at' | 'finished_at'>, now = Date.now()): string {
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
