export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: Record<string, unknown> | unknown[] | unknown
  query?: Record<string, string | number | boolean | undefined>
  headers?: Record<string, string>
  signal?: AbortSignal
  redirectOnUnauthorized?: boolean
  _retry?: boolean
}

export interface ApiError {
  status?: number
  statusCode?: number
  data?: { detail?: string }
  message?: string
  requestId?: string
}

let refreshPromise: Promise<boolean> | null = null

export function resetRefreshState() {
  refreshPromise = null
}

async function trySilentRefresh(): Promise<boolean> {
  if (refreshPromise) return refreshPromise
  refreshPromise = (async () => {
    try {
      await $fetch('/admin/api/auth/refresh', {
        baseURL: '',
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'X-Requested-With': 'asu-admin'
        },
        retry: 0
      })
      return true
    } catch {
      return false
    } finally {
      refreshPromise = null
    }
  })()
  return refreshPromise
}

function safeGetRoutePath(): string | undefined {
  try {
    // 兼容 Nuxt 环境与 Vitest 独立运行环境
    // @ts-expect-error useRoute may not be declared in pure node test
    if (typeof useRoute === 'function') {
      // @ts-expect-error useRoute call
      const r = useRoute()
      return r?.fullPath
    }
  } catch {
    /* 忽略非 Nuxt 上下文 */
  }
  return undefined
}

export function useAdminApi() {
  return async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { redirectOnUnauthorized = true, _retry = false, ...fetchOptions } = options
    try {
      return await $fetch<T>(`/admin/api${path}`, {
        baseURL: '',
        ...fetchOptions,
        credentials: 'same-origin',
        headers: {
          'X-Requested-With': 'asu-admin',
          ...fetchOptions.headers
        },
        retry: 0
      })
    } catch (error) {
      if (getHttpStatus(error) === 401) {
        const isAuthEndpoint = path.startsWith('/auth/') || path === '/login' || path === '/refresh'
        // 普通业务接口遇到 401 时尝试一次静默刷新并重发
        if (!isAuthEndpoint && !_retry) {
          const refreshed = await trySilentRefresh()
          if (refreshed) {
            return await request<T>(path, { ...options, _retry: true })
          }
        }
        if (redirectOnUnauthorized) {
          const fullPath = safeGetRoutePath()
          const redirect = fullPath && fullPath !== '/login' ? `?redirect=${encodeURIComponent(fullPath)}` : ''
          await navigateTo(`/login${redirect}`, { replace: true })
        }
      }
      throw error
    }
  }
}

export function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  return (error as { status?: number, statusCode?: number }).status
    ?? (error as { statusCode?: number }).statusCode
}

export function getApiErrorMessage(error: unknown, fallback = '操作失败，请稍后重试'): string {
  if (typeof error !== 'object' || error === null) return fallback
  const err = error as { data?: { detail?: string }, message?: string }
  return err.data?.detail || err.message || fallback
}
