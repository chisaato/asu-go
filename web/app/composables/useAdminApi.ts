export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: Record<string, unknown> | unknown[] | unknown
  query?: Record<string, string | number | boolean | undefined>
  headers?: Record<string, string>
  signal?: AbortSignal
  redirectOnUnauthorized?: boolean
}

export interface ApiError {
  status?: number
  statusCode?: number
  data?: { detail?: string }
  message?: string
  requestId?: string
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
    const { redirectOnUnauthorized = true, ...fetchOptions } = options
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
      if (getHttpStatus(error) === 401 && redirectOnUnauthorized) {
        const fullPath = safeGetRoutePath()
        const redirect = fullPath && fullPath !== '/login' ? `?redirect=${encodeURIComponent(fullPath)}` : ''
        await navigateTo(`/login${redirect}`, { replace: true })
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
