import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('登录页骨架', () => {
  it('保留占位提示和禁用的登录按钮，不连接业务 API', () => {
    const page = readFileSync(new URL('../app/pages/index.vue', import.meta.url), 'utf8')
    expect(page).toContain('登录页占位')
    expect(page).toContain('disabled')
    expect(page).not.toContain('$fetch')
  })
})
