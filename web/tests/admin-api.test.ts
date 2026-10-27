import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAdminApi } from '../app/composables/useAdminApi'

afterEach(() => vi.unstubAllGlobals())

describe('管理 API', () => {
  it('使用固定管理路径、cookie 和 CSRF 头，禁用自动重试', async () => {
    const fetch = vi.fn().mockResolvedValue({ name: 'admin' })
    vi.stubGlobal('$fetch', fetch)
    await expect(useAdminApi()('/logout', { method: 'POST' })).resolves.toEqual({ name: 'admin' })
    expect(fetch).toHaveBeenCalledWith('/admin/api/logout', expect.objectContaining({
      method: 'POST', credentials: 'same-origin', retry: 0,
      headers: { 'X-Requested-With': 'asu-admin' }
    }))
  })

  it('会话过期时跳转登录并保留原始错误', async () => {
    const error = { statusCode: 401 }
    const navigate = vi.fn()
    vi.stubGlobal('$fetch', vi.fn().mockRejectedValue(error))
    vi.stubGlobal('navigateTo', navigate)
    await expect(useAdminApi()('/jobs')).rejects.toBe(error)
    expect(navigate).toHaveBeenCalledWith('/login', { replace: true })
  })

  it.each([401, 429, 500])('登录错误 %s 不触发重定向', async (status) => {
    const navigate = vi.fn()
    vi.stubGlobal('$fetch', vi.fn().mockRejectedValue({ status }))
    vi.stubGlobal('navigateTo', navigate)
    await expect(useAdminApi()('/login', { redirectOnUnauthorized: false })).rejects.toEqual({ status })
    expect(navigate).not.toHaveBeenCalled()
  })

  it('非 401 不触发重定向', async () => {
    const navigate = vi.fn()
    vi.stubGlobal('$fetch', vi.fn().mockRejectedValue({ status: 500 }))
    vi.stubGlobal('navigateTo', navigate)
    await expect(useAdminApi()('/jobs')).rejects.toEqual({ status: 500 })
    expect(navigate).not.toHaveBeenCalled()
  })
})
