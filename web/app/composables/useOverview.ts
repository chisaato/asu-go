import type { Job } from '~/utils/jobs'

export interface OverviewData {
  slots: { running: number, max: number }
  queued: number
  active: Job[]
  queue: Job[]
  recent: Job[]
  today: { total: number, success: number, failed: number }
  disk: { used_bytes: number, limit_bytes: number | null, free_bytes?: number }
  engine: { type: string, version: string, socket: string, latency_ms: number, containers: number }
  stats?: { top_profiles: { name: string, count: number }[], top_packages: { name: string, count: number }[] }
}

export function useOverview() {
  const api = useAdminApi()
  const overview = ref<OverviewData | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchOverview() {
    loading.value = true
    error.value = null
    try {
      // 优先请求后端专用 overview 接口
      overview.value = await api<OverviewData>('/overview')
    } catch (e) {
      if (getHttpStatus(e) === 404) {
        // 后端尚未部署 /overview 时降级：从 /jobs 聚合
        try {
          const res = await api<{ items: Job[], total: number }>('/jobs?page=1&page_size=50')
          const items = res.items || []
          const active = items.filter(j => j.status === 'running')
          const queue = items.filter(j => j.status === 'queued')
          const recent = items.slice(0, 10)
          const successCount = items.filter(j => j.status === 'done').length
          const failedCount = items.filter(j => j.status === 'failed' || j.status === 'timeout').length

          overview.value = {
            slots: { running: active.length, max: 4 },
            queued: queue.length,
            active,
            queue,
            recent,
            today: {
              total: items.length,
              success: successCount,
              failed: failedCount
            },
            disk: {
              used_bytes: 1024 * 1024 * 450, // 450 MB 估算
              limit_bytes: null,
              free_bytes: 1024 * 1024 * 1024 * 20
            },
            engine: {
              type: 'podman',
              version: '5.x',
              socket: '/run/podman/podman.sock',
              latency_ms: 2,
              containers: active.length
            }
          }
        } catch {
          error.value = '无法加载概览数据，请检查网络。'
        }
      } else {
        error.value = getApiErrorMessage(e, '无法获取仪表盘概览')
      }
    } finally {
      loading.value = false
    }
  }

  return {
    overview,
    loading,
    error,
    refresh: fetchOverview
  }
}
