import { describe, expect, it } from 'vitest'
import { parseAnsiLine } from '../app/utils/ansi'
import { formatBytes, parseClient, truncateHash } from '../app/utils/format'
import { stageInfo } from '../app/utils/stages'

describe('ANSI 解析', () => {
  it('正确解析普通无转义文本', () => {
    const res = parseAnsiLine('Hello world')
    expect(res.isStage).toBe(false)
    expect(res.spans).toEqual([{
      text: 'Hello world',
      color: undefined,
      bgColor: undefined,
      bold: false,
      dim: false,
      italic: false,
      underline: false
    }])
  })

  it('识别 ##asu:stage= 标记行', () => {
    const res = parseAnsiLine('##asu:stage=building_image\r')
    expect(res.isStage).toBe(true)
    expect(res.stageName).toBe('building_image')
    expect(res.spans).toEqual([])
  })

  it('解析 16 种前景色与样式并重置', () => {
    // 红色、粗体，然后重置
    const input = '\x1b[1;31mError message\x1b[0m normal'
    const res = parseAnsiLine(input)
    expect(res.isStage).toBe(false)
    expect(res.spans).toHaveLength(2)
    expect(res.spans[0]).toEqual({
      text: 'Error message',
      color: 'ansi-red',
      bgColor: undefined,
      bold: true,
      dim: false,
      italic: false,
      underline: false
    })
    expect(res.spans[1].text).toBe(' normal')
    expect(res.spans[1].bold).toBe(false)
    expect(res.spans[1].color).toBeUndefined()
  })

  it('不包含恶意 HTML 执行注入点（作为纯文本）', () => {
    const input = '<script>alert(1)</script>'
    const res = parseAnsiLine(input)
    expect(res.spans[0]?.text).toBe('<script>alert(1)</script>')
  })
})

describe('格式化与截断', () => {
  it('字节大小转换', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(1048576)).toBe('1.0 MB')
    expect(formatBytes(1073741824 * 2.5)).toBe('2.5 GB')
    expect(formatBytes(null)).toBe('—')
  })

  it('客户端 User-Agent 解析', () => {
    expect(parseClient('owut/1.2.0 (OpenWrt)')).toBe('owut/1.2.0')
    expect(parseClient('luci/24.10')).toBe('luci/24.10')
    expect(parseClient('')).toBe('未知客户端')
  })

  it('Hash 截断', () => {
    expect(truncateHash('1234567890abcdef', 8)).toBe('12345678')
    expect(truncateHash('12345', 8)).toBe('12345')
    expect(truncateHash(null)).toBe('—')
  })
})

describe('阶段信息映射', () => {
  it('映射已知阶段', () => {
    expect(stageInfo('init').label).toBe('初始化')
    expect(stageInfo('building_image').percent).toBe(80)
    expect(stageInfo('done').percent).toBe(100)
  })

  it('未知阶段优雅回退', () => {
    const unknown = stageInfo('custom_sub_stage')
    expect(unknown.label).toBe('custom_sub_stage')
    expect(unknown.percent).toBe(50)
  })
})
