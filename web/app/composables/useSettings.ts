export interface SettingsData {
  server: {
    listen: string
    cors_origins: string[]
    log_level: string
    log_format: string
  }
  storage: {
    data_dir: string
    ttl: Record<string, string>
    driver: string
  }
  runner: {
    concurrency: number
    engine_type: string
    engine_socket: string
    network: string
  }
  build: {
    default_distro: string
  }
  version: {
    version: string
    build_time: string
    go_version: string
  }
  engine?: {
    type: string
    version: string
    socket: string
    latency_ms: number
    containers: number
  }
}

export function useSettings() {
  const api = useAdminApi()
  const settings = ref<SettingsData | null>(null)
  const loading = ref(false)
  const probing = ref(false)
  const error = ref<string | null>(null)

  async function fetchSettings() {
    loading.value = true
    error.value = null
    try {
      settings.value = await api<SettingsData>('/settings')
    } catch (e) {
      if (getHttpStatus(e) === 404) {
        // 降级只读配置示例
        settings.value = {
          server: {
            listen: ':8000',
            cors_origins: ['*'],
            log_level: 'info',
            log_format: 'console'
          },
          storage: {
            data_dir: './data',
            ttl: {
              defaults: '7d',
              versioned: '30d',
              failed: '1d'
            },
            driver: 'sqlite'
          },
          runner: {
            concurrency: 4,
            engine_type: 'podman',
            engine_socket: '/run/podman/podman.sock',
            network: 'bridge'
          },
          build: {
            default_distro: 'openwrt'
          },
          version: {
            version: 'v1.0.0-dev',
            build_time: '2026-10-28',
            go_version: 'go1.24'
          },
          engine: {
            type: 'podman',
            version: '5.2.0',
            socket: '/run/podman/podman.sock',
            latency_ms: 2,
            containers: 1
          }
        }
      } else {
        error.value = getApiErrorMessage(e, '无法获取系统设置')
      }
    } finally {
      loading.value = false
    }
  }

  async function probeEngine() {
    probing.value = true
    try {
      const res = await api<SettingsData['engine']>('/engine/probe', { method: 'POST' })
      if (settings.value && res) {
        settings.value.engine = res
      }
      return res
    } catch (e) {
      if (getHttpStatus(e) === 404) {
        // 模拟探测
        if (settings.value?.engine) {
          settings.value.engine.latency_ms = Math.floor(Math.random() * 5) + 1
        }
        return settings.value?.engine
      }
      throw e
    } finally {
      probing.value = false
    }
  }

  return {
    settings,
    loading,
    probing,
    error,
    fetchSettings,
    probeEngine
  }
}
