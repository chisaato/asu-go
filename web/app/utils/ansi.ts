export interface AnsiSpan {
  text: string
  color?: string
  bgColor?: string
  bold?: boolean
  dim?: boolean
  italic?: boolean
  underline?: boolean
}

export interface ParsedLine {
  isStage: boolean
  stageName?: string
  spans: AnsiSpan[]
}

const COLOR_MAP: Record<number, string> = {
  30: 'ansi-black',
  31: 'ansi-red',
  32: 'ansi-green',
  33: 'ansi-yellow',
  34: 'ansi-blue',
  35: 'ansi-magenta',
  36: 'ansi-cyan',
  37: 'ansi-white',
  90: 'ansi-bright-black',
  91: 'ansi-bright-red',
  92: 'ansi-bright-green',
  93: 'ansi-bright-yellow',
  94: 'ansi-bright-blue',
  95: 'ansi-bright-magenta',
  96: 'ansi-bright-cyan',
  97: 'ansi-bright-white'
}

const BG_COLOR_MAP: Record<number, string> = {
  40: 'ansi-bg-black',
  41: 'ansi-bg-red',
  42: 'ansi-bg-green',
  43: 'ansi-bg-yellow',
  44: 'ansi-bg-blue',
  45: 'ansi-bg-magenta',
  46: 'ansi-bg-cyan',
  47: 'ansi-bg-white',
  100: 'ansi-bg-bright-black',
  101: 'ansi-bg-bright-red',
  102: 'ansi-bg-bright-green',
  103: 'ansi-bg-bright-yellow',
  104: 'ansi-bg-bright-blue',
  105: 'ansi-bg-bright-magenta',
  106: 'ansi-bg-bright-cyan',
  107: 'ansi-bg-bright-white'
}

/**
 * 解析单个终端行，支持 ANSI 转义序列及 ##asu:stage= 标记
 */
export function parseAnsiLine(line: string): ParsedLine {
  const trimmed = line.replace(/\r$/, '')
  if (trimmed.startsWith('##asu:stage=')) {
    return {
      isStage: true,
      stageName: trimmed.slice('##asu:stage='.length).trim(),
      spans: []
    }
  }

  // 正则匹配 ANSI 序列，如 \x1b[31m 或 \u001b[1;32m
  // eslint-disable-next-line no-control-regex
  const ansiRegex = /\x1b\[([0-9;]*)m/g
  const spans: AnsiSpan[] = []

  let lastIndex = 0
  let currentColor: string | undefined
  let currentBgColor: string | undefined
  let currentBold = false
  let currentDim = false
  let currentItalic = false
  let currentUnderline = false

  let match: RegExpExecArray | null
  while ((match = ansiRegex.exec(trimmed)) !== null) {
    const textChunk = trimmed.slice(lastIndex, match.index)
    if (textChunk.length > 0) {
      spans.push({
        text: textChunk,
        color: currentColor,
        bgColor: currentBgColor,
        bold: currentBold,
        dim: currentDim,
        italic: currentItalic,
        underline: currentUnderline
      })
    }

    // 解析参数
    const codes = match[1] ? match[1].split(';').map(n => parseInt(n, 10)) : [0]
    for (const code of codes) {
      if (code === 0) {
        currentColor = undefined
        currentBgColor = undefined
        currentBold = false
        currentDim = false
        currentItalic = false
        currentUnderline = false
      } else if (code === 1) {
        currentBold = true
      } else if (code === 2) {
        currentDim = true
      } else if (code === 3) {
        currentItalic = true
      } else if (code === 4) {
        currentUnderline = true
      } else if (code === 22) {
        currentBold = false
        currentDim = false
      } else if (code === 23) {
        currentItalic = false
      } else if (code === 24) {
        currentUnderline = false
      } else if (COLOR_MAP[code]) {
        currentColor = COLOR_MAP[code]
      } else if (code === 39) {
        currentColor = undefined
      } else if (BG_COLOR_MAP[code]) {
        currentBgColor = BG_COLOR_MAP[code]
      } else if (code === 49) {
        currentBgColor = undefined
      }
    }

    lastIndex = ansiRegex.lastIndex
  }

  const remaining = trimmed.slice(lastIndex)
  if (remaining.length > 0) {
    spans.push({
      text: remaining,
      color: currentColor,
      bgColor: currentBgColor,
      bold: currentBold,
      dim: currentDim,
      italic: currentItalic,
      underline: currentUnderline
    })
  }

  return {
    isStage: false,
    spans: spans.length > 0 ? spans : [{ text: '' }]
  }
}
