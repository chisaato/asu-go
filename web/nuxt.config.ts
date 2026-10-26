import { fileURLToPath } from 'node:url'

export default defineNuxtConfig({
  compatibilityDate: '2026-10-27',
  ssr: false,
  app: {
    baseURL: '/admin/',
    head: { title: 'ASU-Go 管理后台', htmlAttrs: { lang: 'zh-CN' } }
  },
  modules: ['@nuxt/ui', '@nuxt/eslint'],
  css: ['~/assets/css/main.css'],
  ui: { fonts: false },
  nitro: {
    preset: 'static',
    output: { publicDir: fileURLToPath(new URL('./dist', import.meta.url)) },
    devProxy: {
      '/api': { target: 'http://127.0.0.1:8000/api', changeOrigin: true },
      '/admin/api': { target: 'http://127.0.0.1:8000/admin/api', changeOrigin: true },
      '/json': { target: 'http://127.0.0.1:8000/json', changeOrigin: true }
    }
  }
})
