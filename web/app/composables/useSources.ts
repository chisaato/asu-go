export interface Source {
  id?: number
  distribution: string
  branch: string
  version: string
  channel?: string
  upstream_url: string
  image: string
  tag_template: string
  package_manager?: string
  origin?: string
  rolling: boolean
  enabled: boolean
  overrides?: string[]
  targets?: Record<string, unknown>
  synced_at?: string | null
}

export function useSources() {
  const api = useAdminApi()
  const sources = ref<Source[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchSources() {
    loading.value = true
    error.value = null
    try {
      sources.value = await api<Source[]>('/sources')
    } catch (e) {
      error.value = getApiErrorMessage(e, '获取来源列表失败')
    } finally {
      loading.value = false
    }
  }

  async function createSource(item: Omit<Source, 'id'>) {
    return await api<Source>('/sources', {
      method: 'POST',
      body: item
    })
  }

  async function updateSource(id: number, patch: Partial<Source>) {
    return await api<Source>(`/sources/${id}`, {
      method: 'PATCH',
      body: patch
    })
  }

  async function deleteSource(id: number) {
    return await api<unknown>(`/sources/${id}`, {
      method: 'DELETE'
    })
  }

  async function importSources(items: Source[]) {
    return await api<Source[]>('/sources/import', {
      method: 'POST',
      body: items
    })
  }

  return {
    sources,
    loading,
    error,
    fetchSources,
    createSource,
    updateSource,
    deleteSource,
    importSources
  }
}
