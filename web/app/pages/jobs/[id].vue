<script setup lang="ts">
import { type Job, prettyJSON } from '~/utils/jobs'
import { parseClient, formatBytes } from '~/utils/format'

const route = useRoute()
const api = useAdminApi()
const jobId = computed(() => route.params.id as string)

// 任务基础数据
const job = ref<Job | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)

// 实时日志状态
const rawLogs = ref('')
const logsLoading = ref(false)

// 标签页定义
const tabItems = [
  { label: '执行日志', value: 'logs', icon: 'i-lucide-terminal' },
  { label: '原始请求', value: 'request', icon: 'i-lucide-file-json' },
  { label: '软件包', value: 'packages', icon: 'i-lucide-package' },
  { label: '产物文件', value: 'artifacts', icon: 'i-lucide-download' },
  { label: '元数据', value: 'meta', icon: 'i-lucide-info' }
]
const activeTab = ref('logs')

// 操作确认框状态
const cancelDialogOpen = ref(false)
const cancelBusy = ref(false)

async function fetchJobDetail() {
  loading.value = true
  error.value = null
  try {
    job.value = await api<Job>(`/jobs/${encodeURIComponent(jobId.value)}`)
  } catch (e) {
    error.value = getApiErrorMessage(e, '无法获取任务详情')
  } finally {
    loading.value = false
  }
}

// 日志拉取与流式处理
let eventSource: EventSource | null = null
function connectLogStream() {
  if (typeof window === 'undefined') return
  const isRunning = job.value?.status === 'running' || job.value?.status === 'queued'

  if (isRunning) {
    // 运行中任务连接实时流
    try {
      eventSource = new EventSource(`/admin/api/jobs/${encodeURIComponent(jobId.value)}/stream`)
      eventSource.addEventListener('log', (e: MessageEvent) => {
        rawLogs.value += e.data + '\n'
      })
      eventSource.addEventListener('stage', (e: MessageEvent) => {
        try {
          const payload = JSON.parse(e.data)
          if (job.value && payload.name) {
            job.value.stage = payload.name
            if (!job.value.stages) job.value.stages = []
            job.value.stages.push({ name: payload.name, at: payload.at || new Date().toISOString() })
          }
        } catch {
          /* 忽略 */
        }
      })
      eventSource.addEventListener('status', (e: MessageEvent) => {
        if (job.value) job.value.status = e.data
      })
      eventSource.addEventListener('end', () => {
        if (eventSource) {
          eventSource.close()
          eventSource = null
        }
        void fetchJobDetail()
      })
      eventSource.onerror = () => {
        // SSE 失败降级为单次普通 log 请求
        if (eventSource) {
          eventSource.close()
          eventSource = null
        }
        void fetchStaticLog()
      }
    } catch {
      void fetchStaticLog()
    }
  } else {
    // 已结束任务单次读取完整日志
    void fetchStaticLog()
  }
}

async function fetchStaticLog() {
  logsLoading.value = true
  try {
    const text = await $fetch<string>(`/admin/api/jobs/${encodeURIComponent(jobId.value)}/log`, {
      credentials: 'same-origin',
      headers: { 'X-Requested-With': 'asu-admin' },
      responseType: 'text'
    })
    rawLogs.value = text
  } catch {
    rawLogs.value = '暂无已归档的日志输出'
  } finally {
    logsLoading.value = false
  }
}

// 取消任务
async function executeCancel() {
  cancelBusy.value = true
  try {
    await api(`/jobs/${encodeURIComponent(jobId.value)}/cancel`, { method: 'POST' })
    cancelDialogOpen.value = false
    await fetchJobDetail()
  } finally {
    cancelBusy.value = false
  }
}

// 诊断重放
async function replayThisJob() {
  try {
    const res = await api<{ id?: string, request_hash?: string }>(`/jobs/${encodeURIComponent(jobId.value)}/replay`, {
      method: 'POST',
      body: { retain_container: false }
    })
    const newId = res.id || res.request_hash
    if (newId) {
      await navigateTo(`/jobs/${newId}`)
    }
  } catch {
    /* 忽略 */
  }
}

// 提取产物列表信息
const artifactsList = computed(() => {
  if (!job.value?.result || typeof job.value.result !== 'object') return []
  const res = job.value.result as { files?: { name: string, size?: number, sha256?: string }[], images?: { name: string, size?: number, sha256?: string }[] }
  return res.files || res.images || []
})

// 提取包列表
const buildCmdPackages = computed<string[]>(() => {
  if (!job.value?.request || typeof job.value.request !== 'object') return []
  const req = job.value.request as { packages?: string[] }
  return req.packages || []
})

onMounted(async () => {
  await fetchJobDetail()
  connectLogStream()
})

onUnmounted(() => {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
})
</script>

<template>
  <div class="space-y-6">
    <!-- 面包屑与快速返回 -->
    <div class="flex items-center gap-2 text-xs text-muted">
      <NuxtLink to="/jobs" class="hover:text-default flex items-center gap-1">
        <UIcon name="i-lucide-arrow-left" class="size-3.5" />
        <span>返回任务列表</span>
      </NuxtLink>
      <span>/</span>
      <span class="font-mono text-default">{{ jobId }}</span>
    </div>

    <!-- 加载与错误态 -->
    <div v-if="loading" class="py-12 text-center text-muted">
      <UIcon name="i-lucide-loader-2" class="size-6 animate-spin mx-auto mb-2 text-primary" />
      <p class="text-xs">正在载入任务详情与构建上下文…</p>
    </div>

    <ErrorState
      v-else-if="error"
      title="任务不存在或已清理"
      :description="error"
      @retry="fetchJobDetail"
    />

    <template v-else-if="job">
      <!-- 头部：信息概览与操作栏 -->
      <div class="flex flex-col lg:flex-row lg:items-center justify-between gap-4 p-5 rounded-xl border border-default bg-elevated/40">
        <div class="space-y-2">
          <div class="flex items-center gap-2.5 flex-wrap">
            <h1 class="text-xl font-bold tracking-tight">{{ job.profile }}</h1>
            <StatusBadge :status="job.status" size="md" />
            <UBadge color="neutral" variant="outline" size="sm" class="font-mono">
              {{ job.distro || 'openwrt' }} · {{ job.version }} · {{ job.target }}
            </UBadge>
          </div>

          <div class="flex flex-wrap items-center gap-4 text-xs text-muted">
            <span class="flex items-center gap-1">
              <UIcon name="i-lucide-laptop" class="size-3.5" />
              <span>客户端: {{ parseClient(job.client) }}</span>
            </span>
            <span class="flex items-center gap-1">
              <UIcon name="i-lucide-clock" class="size-3.5" />
              <span>耗时: </span>
              <DurationText :started-at="job.started_at" :finished-at="job.finished_at" :duration-ms="job.duration_ms" />
            </span>
            <span v-if="job.replay_of" class="flex items-center gap-1 text-primary">
              <UIcon name="i-lucide-history" class="size-3.5" />
              <span>重放自: </span>
              <NuxtLink :to="`/jobs/${job.replay_of}`" class="underline font-mono">{{ job.replay_of.slice(0, 8) }}</NuxtLink>
            </span>
            <span v-if="job.purge_at" class="flex items-center gap-1 text-warning">
              <UIcon name="i-lucide-timer-off" class="size-3.5" />
              <span>环境销毁倒计时</span>
            </span>
          </div>
        </div>

        <!-- 操作按钮组 -->
        <div class="flex items-center gap-2 self-start lg:self-center shrink-0">
          <UButton
            v-if="['queued', 'running'].includes(job.status)"
            color="error"
            variant="soft"
            size="sm"
            icon="i-lucide-square"
            @click="cancelDialogOpen = true"
          >
            终止构建
          </UButton>
          <UButton
            v-if="['failed', 'done', 'timeout'].includes(job.status)"
            color="neutral"
            variant="outline"
            size="sm"
            icon="i-lucide-refresh-cw"
            @click="replayThisJob"
          >
            诊断重放
          </UButton>
        </div>
      </div>

      <!-- 阶段时间线 -->
      <UCard class="p-4">
        <h2 class="text-xs font-semibold text-muted mb-3 flex items-center gap-1.5">
          <UIcon name="i-lucide-milestone" class="size-3.5" />
          <span>构建阶段演进</span>
        </h2>
        <StageTimeline
          :current-stage="job.stage"
          :status="job.status"
          :stages="job.stages"
          :started-at="job.started_at"
          :finished-at="job.finished_at"
        />
      </UCard>

      <!-- 标签页导航与内容 -->
      <div class="space-y-4">
        <!-- 标签切换栏 -->
        <div class="flex items-center gap-1 border-b border-default pb-2">
          <button
            v-for="tab in tabItems"
            :key="tab.value"
            type="button"
            class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors cursor-pointer"
            :class="[
              activeTab === tab.value
                ? 'bg-primary/10 text-primary font-semibold'
                : 'text-muted hover:bg-muted/10 hover:text-default'
            ]"
            @click="activeTab = tab.value"
          >
            <UIcon :name="tab.icon" class="size-3.5" />
            <span>{{ tab.label }}</span>
          </button>
        </div>

        <!-- 标签页内容：1. 实时终端 -->
        <div v-show="activeTab === 'logs'" class="h-[600px]">
          <LogTerminal
            :raw-logs="rawLogs"
            :running="job.status === 'running'"
            :job-id="job.id"
          />
        </div>

        <!-- 标签页内容：2. 原始请求 -->
        <div v-show="activeTab === 'request'">
          <UCard class="p-4">
            <h3 class="text-xs font-semibold text-muted mb-2">原始构建参数 JSON</h3>
            <pre class="overflow-x-auto rounded-lg bg-elevated p-4 text-xs font-mono leading-relaxed max-h-96">{{ prettyJSON(job.request) }}</pre>
          </UCard>
        </div>

        <!-- 标签页内容：3. 软件包清单 -->
        <div v-show="activeTab === 'packages'">
          <UCard class="p-4 space-y-4">
            <div>
              <h3 class="text-xs font-semibold text-muted mb-2">本次构建命令请求包含的软件包清单</h3>
              <div v-if="buildCmdPackages.length > 0" class="flex flex-wrap gap-1.5 max-h-80 overflow-y-auto">
                <UBadge
                  v-for="pkg in buildCmdPackages"
                  :key="pkg"
                  :color="pkg.startsWith('-') ? 'error' : 'primary'"
                  variant="subtle"
                  class="font-mono text-xs"
                >
                  {{ pkg }}
                </UBadge>
              </div>
              <p v-else class="text-xs text-muted">使用默认包集合，未声明额外软件包</p>
            </div>
          </UCard>
        </div>

        <!-- 标签页内容：4. 产物文件 -->
        <div v-show="activeTab === 'artifacts'">
          <UCard class="p-4">
            <h3 class="text-xs font-semibold text-muted mb-3">生成的固件镜像与校验文件</h3>
            <div v-if="artifactsList.length > 0" class="space-y-2">
              <div
                v-for="(file, idx) in artifactsList"
                :key="idx"
                class="flex items-center justify-between p-3 rounded-lg border border-default bg-elevated/40 text-xs"
              >
                <div class="space-y-0.5 truncate pr-4">
                  <p class="font-mono font-medium truncate">{{ file.name }}</p>
                  <p class="text-[10px] text-muted">
                    大小: {{ formatBytes(file.size) }}
                    <span v-if="file.sha256" class="ml-2 font-mono">SHA256: {{ file.sha256.slice(0, 16) }}…</span>
                  </p>
                </div>
                <UButton
                  :to="`/store/${job.id}/${file.name}`"
                  target="_blank"
                  color="primary"
                  variant="soft"
                  size="xs"
                  icon="i-lucide-download"
                >
                  下载
                </UButton>
              </div>
            </div>
            <div v-else class="p-8 text-center text-xs text-muted">
              {{ job.status === 'done' ? '暂未解析到产物列表' : '构建尚未完成或失败，无产物可用' }}
            </div>
          </UCard>
        </div>

        <!-- 标签页内容：5. 元数据 -->
        <div v-show="activeTab === 'meta'">
          <UCard class="p-4">
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs">
              <div class="space-y-1">
                <span class="text-muted">任务 Request Hash:</span>
                <p class="font-mono text-default break-all">{{ job.id }}</p>
              </div>
              <div class="space-y-1">
                <span class="text-muted">容器 Handle:</span>
                <p class="font-mono text-default">{{ job.handle || '未保留（已销毁）' }}</p>
              </div>
              <div class="space-y-1">
                <span class="text-muted">入队时间:</span>
                <p class="font-mono text-default">{{ job.enqueued_at }}</p>
              </div>
              <div class="space-y-1">
                <span class="text-muted">启动时间:</span>
                <p class="font-mono text-default">{{ job.started_at || '尚未开始' }}</p>
              </div>
              <div class="space-y-1">
                <span class="text-muted">完成时间:</span>
                <p class="font-mono text-default">{{ job.finished_at || '尚未结束' }}</p>
              </div>
              <div class="space-y-1">
                <span class="text-muted">异常信息:</span>
                <p class="text-error font-mono">{{ job.error || '无异常' }}</p>
              </div>
            </div>
          </UCard>
        </div>
      </div>
    </template>

    <!-- 取消确认弹窗 -->
    <ConfirmDialog
      v-model:open="cancelDialogOpen"
      title="终止当前构建"
      description="确定要强制终止当前任务吗？容器将被销毁。"
      :loading="cancelBusy"
      @confirm="executeCancel"
      @cancel="cancelDialogOpen = false"
    />
  </div>
</template>
