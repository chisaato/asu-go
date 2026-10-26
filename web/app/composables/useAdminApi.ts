interface RequestOptions {
  method?: 'GET' | 'POST'
  body?: Record<string, unknown>
  query?: Record<string, string | number>
  signal?: AbortSignal
  redirectOnUnauthorized?: boolean
}

export function useAdminApi() {
  return async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { redirectOnUnauthorized = true, ...fetchOptions } = options
    try {
      return await $fetch<T>(`/admin/api${path}`, {
        ...fetchOptions,
        credentials: 'same-origin',
        headers: { 'X-Requested-With': 'asu-admin' },
        retry: 0
      })
    } catch (error) {
      if (getHttpStatus(error) === 401 && redirectOnUnauthorized) {
        await navigateTo('/login', { replace: true })
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
