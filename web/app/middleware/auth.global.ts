export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/login') return
  try {
    await useAdminApi()('/me', { redirectOnUnauthorized: false })
  } catch (error) {
    if (getHttpStatus(error) === 401) return navigateTo('/login', { replace: true })
    return abortNavigation(createError({ statusCode: 503, statusMessage: '无法连接管理服务，请稍后重试' }))
  }
})
