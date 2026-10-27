<script setup lang="ts">
import type { TableColumn, TableRow } from '@nuxt/ui'
import { absoluteTime, duration, relativeTime, statuses, statusColor, statusLabel } from '~/utils/jobs'
import type { Job } from '~/utils/jobs'

const api = useAdminApi()
const page = ref(1)
const filter = ref('all')
const autoRefresh = ref(true)
const now = ref(Date.now())
const selected = ref<string | null>(null)
const logoutError = ref('')
const { data, status, error, refresh } = await useAsyncData('jobs', () => api<{ items: Job[], total: number }>('/jobs', {
  query: { page: page.value, page_size: 20, status: filter.value }
}), { watch: [page, filter] })

watch(filter, () => { page.value = 1 }, { flush: 'sync' })
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  timer = setInterval(() => {
    now.value = Date.now()
    if (autoRefresh.value && !document.hidden && status.value !== 'pending' && data.value?.items.some(job => ['queued', 'running'].includes(job.status))) {
      void refresh()
    }
  }, 5000)
})
onUnmounted(() => clearInterval(timer))

const columns: TableColumn<Job>[] = [
  { accessorKey: 'id', header: '任务 ID' },
  { accessorKey: 'version', header: '版本 / 目标 / 设备' },
  { accessorKey: 'status', header: '状态' },
  { accessorKey: 'stage', header: '阶段' },
  { accessorKey: 'enqueued_at', header: '入队时间' },
  { accessorKey: 'started_at', header: '开始时间' },
  { accessorKey: 'finished_at', header: '完成时间' },
  { id: 'duration', header: '耗时' }
]
const timeKeys = ['enqueued_at', 'started_at', 'finished_at'] as const
function selectRow(_event: Event, row: TableRow<Job>) { selected.value = row.original.id }
async function logout() {
  logoutError.value = ''
  try {
    await api('/logout', { method: 'POST' })
    clearNuxtData('jobs')
    await navigateTo('/login', { replace: true })
  } catch { logoutError.value = '登出失败，请稍后重试。' }
}
</script>

<template>
  <main class="mx-auto max-w-screen-2xl space-y-6 p-4 sm:p-8">
    <header class="flex items-center justify-between gap-4">
      <div>
        <p class="text-sm text-muted">ASU-Go 管理后台</p>
        <h1 class="text-2xl font-semibold">构建任务</h1>
      </div>
      <UButton color="neutral" variant="outline" @click="logout">登出</UButton>
    </header>
    <UAlert v-if="logoutError" color="error" :title="logoutError" role="alert" />
    <div class="flex flex-wrap items-center gap-4">
      <USelect v-model="filter" :items="statuses" aria-label="按任务状态筛选" class="w-40" />
      <USwitch v-model="autoRefresh" label="自动刷新（5 秒）" />
      <UButton color="neutral" variant="outline" :loading="status === 'pending'" @click="refresh()">刷新</UButton>
      <p class="text-sm text-muted">仅当前页有排队或运行任务时自动刷新</p>
    </div>
    <UAlert v-if="error" color="error" title="无法加载任务，请检查网络后刷新重试。" role="alert" />
    <UTable :data="data?.items ?? []" :columns="columns" :loading="status === 'pending'" class="rounded-lg border border-default" @select="selectRow">
      <template #id-cell="{ row }">
        <UButton variant="link" class="p-0 font-mono" :title="row.original.id" @click.stop="selected = row.original.id">{{ row.original.id.slice(0, 12) }}</UButton>
      </template>
      <template #version-cell="{ row }">
        <div>{{ row.original.version }}</div>
        <div class="text-xs text-muted">{{ row.original.target }} / {{ row.original.profile }}</div>
      </template>
      <template #status-cell="{ row }">
        <UBadge :color="statusColor(row.original.status)" variant="subtle">{{ statusLabel(row.original.status) }}</UBadge>
      </template>
      <template #stage-cell="{ row }">{{ row.original.stage || '—' }}</template>
      <template v-for="key in timeKeys" :key="key" #[`${key}-cell`]="{ row }">
        <span :title="absoluteTime(row.original[key])">{{ relativeTime(row.original[key], now) }}</span>
      </template>
      <template #duration-cell="{ row }">{{ duration(row.original, now) }}</template>
      <template #empty>暂无符合条件的任务</template>
    </UTable>
    <footer class="flex flex-wrap items-center justify-between gap-4">
      <p class="text-sm text-muted">共 {{ data?.total ?? 0 }} 个任务 · 每页 20 条</p>
      <UPagination v-model:page="page" :total="data?.total ?? 0" :items-per-page="20" :sibling-count="1" />
    </footer>
    <JobDetail :id="selected" @close="selected = null" />
  </main>
</template>
