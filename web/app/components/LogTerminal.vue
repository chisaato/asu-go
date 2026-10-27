<script setup lang="ts">
import { parseAnsiLine, type ParsedLine } from '~/utils/ansi'
import { stageInfo } from '~/utils/stages'

const props = withDefaults(defineProps<{
  rawLogs: string
  running?: boolean
  jobId?: string
}>(), {
  running: false,
  jobId: ''
})

const terminalContainer = ref<HTMLElement | null>(null)
const wrapLines = ref(false)
const fontSize = ref<'xs' | 'sm' | 'base'>('xs')
const searchQuery = ref('')
const searchIndex = ref(0)
const isAutoScroll = ref(true)
const isNearBottom = ref(true)

// 将 rawLogs 按行拆分并缓存解析
const parsedLines = computed<ParsedLine[]>(() => {
  if (!props.rawLogs) return []
  const lines = props.rawLogs.split('\n')
  // 避免尾部空行过多
  if (lines.length > 0 && lines[lines.length - 1] === '') {
    lines.pop()
  }
  return lines.map(line => parseAnsiLine(line))
})

// 搜索匹配
const matchedLineIndices = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return []
  const matches: number[] = []
  parsedLines.value.forEach((line, index) => {
    if (line.isStage) return
    const text = line.spans.map(s => s.text).join('').toLowerCase()
    if (text.includes(query)) {
      matches.push(index)
    }
  })
  return matches
})

watch(matchedLineIndices, (matches) => {
  if (matches.length > 0) {
    searchIndex.value = 0
    scrollToMatch(matches[0])
  } else {
    searchIndex.value = 0
  }
})

function prevMatch() {
  if (matchedLineIndices.value.length === 0) return
  searchIndex.value = (searchIndex.value - 1 + matchedLineIndices.value.length) % matchedLineIndices.value.length
  scrollToMatch(matchedLineIndices.value[searchIndex.value])
}

function nextMatch() {
  if (matchedLineIndices.value.length === 0) return
  searchIndex.value = (searchIndex.value + 1) % matchedLineIndices.value.length
  scrollToMatch(matchedLineIndices.value[searchIndex.value])
}

function scrollToMatch(lineIndex: number | undefined) {
  if (lineIndex === undefined || !terminalContainer.value) return
  isAutoScroll.value = false
  const target = terminalContainer.value.querySelector(`[data-line="${lineIndex}"]`)
  if (target) {
    target.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }
}

// 滚动检测
function onScroll() {
  const el = terminalContainer.value
  if (!el) return
  const threshold = 40
  const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= threshold
  isNearBottom.value = nearBottom
  if (nearBottom) {
    isAutoScroll.value = true
  } else {
    isAutoScroll.value = false
  }
}

function scrollToBottom() {
  const el = terminalContainer.value
  if (!el) return
  el.scrollTop = el.scrollHeight
  isAutoScroll.value = true
  isNearBottom.value = true
}

// 自动跟随
watch(
  () => parsedLines.value.length,
  () => {
    if (isAutoScroll.value) {
      nextTick(() => {
        const el = terminalContainer.value
        if (el) el.scrollTop = el.scrollHeight
      })
    }
  }
)

onMounted(() => {
  nextTick(() => scrollToBottom())
})

// 工具栏功能
const copied = ref(false)
async function copyAll() {
  try {
    await navigator.clipboard.writeText(props.rawLogs)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    /* 忽略 */
  }
}

function downloadLog() {
  const blob = new Blob([props.rawLogs], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `build-${props.jobId || 'log'}.log`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
</script>

<template>
  <div class="relative flex flex-col h-full rounded-lg border border-default bg-[#0d1117] text-[#c9d1d9] overflow-hidden">
    <!-- 终端工具栏 -->
    <div class="flex flex-wrap items-center justify-between gap-2 px-3 py-2 border-b border-white/10 bg-[#161b22] text-xs">
      <!-- 搜索区 -->
      <div class="flex items-center gap-1.5 max-w-xs">
        <UInput
          v-model="searchQuery"
          placeholder="搜索日志..."
          size="xs"
          icon="i-lucide-search"
          class="w-48"
        />
        <span v-if="searchQuery" class="text-[10px] text-muted whitespace-nowrap">
          {{ matchedLineIndices.length ? `${searchIndex + 1}/${matchedLineIndices.length}` : '无匹配' }}
        </span>
        <button
          v-if="matchedLineIndices.length > 0"
          type="button"
          class="p-1 hover:text-white transition-colors"
          title="上一个"
          @click="prevMatch"
        >
          <UIcon name="i-lucide-chevron-up" class="size-3.5" />
        </button>
        <button
          v-if="matchedLineIndices.length > 0"
          type="button"
          class="p-1 hover:text-white transition-colors"
          title="下一个"
          @click="nextMatch"
        >
          <UIcon name="i-lucide-chevron-down" class="size-3.5" />
        </button>
      </div>

      <!-- 右侧控制按钮组 -->
      <div class="flex items-center gap-2">
        <!-- 自动换行切换 -->
        <button
          type="button"
          class="px-2 py-1 rounded hover:bg-white/10 transition-colors"
          :class="{ 'text-primary font-medium': wrapLines }"
          title="自动折行"
          @click="wrapLines = !wrapLines"
        >
          换行
        </button>

        <!-- 字号切换 -->
        <button
          type="button"
          class="px-2 py-1 rounded hover:bg-white/10 transition-colors"
          title="切换字号"
          @click="fontSize = fontSize === 'xs' ? 'sm' : fontSize === 'sm' ? 'base' : 'xs'"
        >
          字号: {{ fontSize }}
        </button>

        <!-- 复制全部 -->
        <button
          type="button"
          class="flex items-center gap-1 px-2 py-1 rounded hover:bg-white/10 transition-colors"
          :title="copied ? '已复制' : '复制全部日志'"
          @click="copyAll"
        >
          <UIcon :name="copied ? 'i-lucide-check' : 'i-lucide-copy'" class="size-3.5" :class="{ 'text-success': copied }" />
          <span>{{ copied ? '已复制' : '复制' }}</span>
        </button>

        <!-- 下载日志 -->
        <button
          type="button"
          class="flex items-center gap-1 px-2 py-1 rounded hover:bg-white/10 transition-colors"
          title="下载日志文件"
          @click="downloadLog"
        >
          <UIcon name="i-lucide-download" class="size-3.5" />
          <span>下载</span>
        </button>
      </div>
    </div>

    <!-- 日志展示主区域 -->
    <div
      ref="terminalContainer"
      class="flex-1 overflow-y-auto p-3 font-mono select-text"
      :class="[
        wrapLines ? 'whitespace-pre-wrap break-all' : 'whitespace-pre overflow-x-auto',
        fontSize === 'xs' ? 'text-xs' : fontSize === 'sm' ? 'text-sm' : 'text-base'
      ]"
      @scroll="onScroll"
    >
      <div v-if="parsedLines.length === 0" class="py-12 text-center text-muted">
        <UIcon v-if="running" name="i-lucide-loader-2" class="size-5 animate-spin mx-auto mb-2 text-primary" />
        <p>{{ running ? '任务已启动，正在等待首行日志输出…' : '暂无日志输出' }}</p>
      </div>

      <div
        v-for="(line, idx) in parsedLines"
        :key="idx"
        :data-line="idx"
        class="leading-relaxed hover:bg-white/5 px-1 rounded transition-colors"
        :class="{ 'bg-primary/20': matchedLineIndices.includes(idx) }"
      >
        <!-- 阶段分隔条 -->
        <div v-if="line.isStage" class="my-2 flex items-center gap-3 py-1 text-primary/90 font-sans border-y border-primary/20 bg-primary/5 px-2 rounded">
          <UIcon name="i-lucide-layers" class="size-4 shrink-0 text-primary" />
          <span class="font-semibold text-xs tracking-wider uppercase">阶段：{{ stageInfo(line.stageName).label }}</span>
          <span class="text-[10px] text-muted">({{ line.stageName }})</span>
          <div class="flex-1 border-t border-primary/20 border-dashed" />
        </div>

        <!-- 普通日志行渲染（无 v-html，防 XSS） -->
        <span v-else>
          <span class="inline-block w-9 select-none text-[10px] text-white/30 text-right pr-2 font-mono">{{ idx + 1 }}</span>
          <template v-for="(span, sIdx) in line.spans" :key="sIdx">
            <span
              :class="[
                span.color,
                span.bgColor,
                { 'font-bold': span.bold, 'italic': span.italic, 'underline': span.underline, 'opacity-70': span.dim }
              ]"
            >{{ span.text }}</span>
          </template>
        </span>
      </div>
    </div>

    <!-- 悬浮的“回到底部/恢复跟随”浮标 -->
    <div
      v-if="!isNearBottom && parsedLines.length > 0"
      class="absolute bottom-4 right-6 z-10"
    >
      <button
        type="button"
        class="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-primary text-white text-xs shadow-lg hover:bg-primary/90 transition-all animate-bounce cursor-pointer"
        @click="scrollToBottom"
      >
        <UIcon name="i-lucide-arrow-down" class="size-3.5" />
        <span>回到底部 (恢复跟随)</span>
      </button>
    </div>
  </div>
</template>
