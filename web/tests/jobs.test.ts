import { describe, expect, it } from 'vitest'
import { absoluteTime, duration, prettyJSON, relativeTime, statusColor, statusLabel } from '../app/utils/jobs'

describe('任务格式化', () => {
  const start = '2026-10-27T12:00:00Z'
  const now = Date.parse('2026-10-27T12:02:05Z')
  it('相对时间中文本地化，缺失或无效时间使用占位', () => {
    expect(relativeTime(start, now)).toBe('2分钟前')
    expect(relativeTime(null, now)).toBe('—')
    expect(relativeTime('invalid', now)).toBe('—')
    expect(absoluteTime(null)).toBe('—')
    expect(absoluteTime(start)).not.toBe('—')
  })
  it('区分运行耗时、完成耗时和尚未开始', () => {
    expect(duration({ started_at: start, finished_at: null }, now)).toBe('2 分 5 秒')
    expect(duration({ started_at: start, finished_at: '2026-10-27T12:00:10Z' }, now)).toBe('10 秒')
    expect(duration({ started_at: null, finished_at: null }, now)).toBe('—')
  })
  it('格式化对象、JSON 字符串、普通错误和空结果', () => {
    expect(prettyJSON({ a: 1 })).toBe('{\n  "a": 1\n}')
    expect(prettyJSON('{"a":1}')).toBe(prettyJSON({ a: 1 }))
    expect(prettyJSON('构建失败')).toBe('"构建失败"')
    expect(prettyJSON(null)).toBe('null')
  })
  it('状态徽章及未知状态回退', () => {
    expect(statusLabel('queued')).toBe('排队中')
    expect(statusColor('failed')).toBe('error')
    expect(statusLabel('future')).toBe('future')
    expect(statusColor('future')).toBe('neutral')
  })
})
