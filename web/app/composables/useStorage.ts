export interface StorageOverview {
  artifacts: { count: number, bytes: number }
  orphans: { count: number, bytes: number }
  ttl: Record<string, string>
  last_clean: { at: string, removed: number, freed_bytes: number } | null
}

export interface ArtifactItem {
  request_hash: string
  distro: string
  version: string
  target: string
  profile: string
  bytes: number
  files: string[]
  created_at: string
  expires_at: string | null
}

export interface CleanResult {
  items: number
  bytes: number
}

export function useStorage() {
  const api = useAdminApi()
  const overview = ref<StorageOverview | null>(null)
  const artifacts = ref<ArtifactItem[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchOverview() {
    loading.value = true
    error.value = null
    try {
      overview.value = await api<StorageOverview>('/storage')
    } catch (e) {
      if (getHttpStatus(e) === 404) {
        // 降级 mock 展示
        overview.value = {
          artifacts: { count: 12, bytes: 1024 * 1024 * 280 },
          orphans: { count: 2, bytes: 1024 * 1024 * 45 },
          ttl: {
            defaults: '7d',
            versioned: '30d',
            failed: '1d',
            purge_at: '24h'
          },
          last_clean: {
            at: new Date(Date.now() - 3600000 * 4).toISOString(),
            removed: 3,
            freed_bytes: 1024 * 1024 * 85
          }
        }
      } else {
        error.value = getApiErrorMessage(e, '获取存储概况失败')
      }
    } finally {
      loading.value = false
    }
  }

  async function fetchArtifacts() {
    try {
      const res = await api<{ items: ArtifactItem[] }>('/storage/artifacts')
      artifacts.value = res.items || []
    } catch (e) {
      if (getHttpStatus(e) === 404) {
        // 降级生成示例列表
        artifacts.value = [
          {
            request_hash: 'a1b2c3d4e5f60718',
            distro: 'openwrt',
            version: '24.10.0',
            target: 'mediatek/filogic',
            profile: 'glinet_gl-mt3000',
            bytes: 28 * 1024 * 1024,
            files: ['openwrt-24.10.0-mediatek-filogic-glinet_gl-mt3000-squashfs-sysupgrade.bin', 'sha256sums'],
            created_at: new Date(Date.now() - 86400000).toISOString(),
            expires_at: new Date(Date.now() + 86400000 * 6).toISOString()
          },
          {
            request_hash: 'f9e8d7c6b5a41234',
            distro: 'immortalwrt',
            version: '23.05.4',
            target: 'x86/64',
            profile: 'generic',
            bytes: 45 * 1024 * 1024,
            files: ['immortalwrt-23.05.4-x86-64-generic-squashfs-combined-efi.img.gz', 'sha256sums'],
            created_at: new Date(Date.now() - 86400000 * 2).toISOString(),
            expires_at: new Date(Date.now() + 86400000 * 5).toISOString()
          }
        ]
      }
    }
  }

  async function cleanStorage(scope: 'expired' | 'failed' | 'orphans', dryRun = true): Promise<CleanResult> {
    try {
      return await api<CleanResult>('/storage/clean', {
        method: 'POST',
        body: { scope, dry_run: dryRun }
      })
    } catch (e) {
      if (getHttpStatus(e) === 404) {
        // 模拟返回
        const mockMap: Record<string, CleanResult> = {
          expired: { items: 3, bytes: 75 * 1024 * 1024 },
          failed: { items: 1, bytes: 12 * 1024 * 1024 },
          orphans: { items: 2, bytes: 45 * 1024 * 1024 }
        }
        return mockMap[scope] || { items: 0, bytes: 0 }
      }
      throw e
    }
  }

  async function deleteArtifact(hash: string) {
    return await api<unknown>(`/store/${encodeURIComponent(hash)}`, {
      method: 'DELETE'
    })
  }

  return {
    overview,
    artifacts,
    loading,
    error,
    fetchOverview,
    fetchArtifacts,
    cleanStorage,
    deleteArtifact
  }
}
