<script setup lang="ts">
import { type Job, statuses, relativeTime, absoluteTime } from '~/utils/jobs'
import { parseClient } from '~/utils/format'

const route = useRoute()
const router = useRouter()
const api = useAdminApi()
const { on: onEvent } = useEventStream()

// 查询参数同步到 URL
const page = ref(Number(route.query.page) || 1)
const statusFilter = ref((route.query.status as string) || 'all')
const distroFilter = ref((route.query.distro as string) || '')
const searchKeyword = ref((route.query.q as string) || '')
const pageSize = 25

// 选中的批量操作 ID 集合
const selectedIds = ref<Set<string>>(new Set())

// 对话框状态
const replayDialogOpen = ref(false)
const replayingJob = ref<Job | null>(null)
const retainContainer = ref(false)
const replayBusy = ref(false)

const cancelDialogOpen = ref(false)
const cancelingId = ref<string | null>(null)
const cancelBusy = ref(false)

const deleteBatchDialogOpen = ref(false)
const deleteBusy = ref(false)

// 监听并同步到 URL Query
watch([page, statusFilter, distroFilter, searchKeyword], () => {
  const query: Record<string, string | number> = {}
  if (page.value > 1) query.page = page.value
  if (statusFilter.value && statusFilter.value !== 'all') query.status = statusFilter.value
  if (distroFilter.value) query.distro = distroFilter.value
  if (searchKeyword.value) query.q = searchKeyword.value
  void router.replace({ query })
})

// 数据获取
const { data, status: loadStatus, error, refresh } = await useAsyncData('admin-jobs', async () => {
  const queryParams: Record<string, string | number> = {
    page: page.value,
    page_size: pageSize
  }
  if (statusFilter.value && statusFilter.value !== 'all') queryParams.status = statusFilter.value
  if (distroFilter.value) queryParams.distro = distroFilter.value
  if (searchKeyword.value) queryParams.q = searchKeyword.value

  return await api<{ items: Job[], total: number }>('/jobs', {
    query: queryParams
  })
}, { watch: [page, statusFilter, distroFilter] })

// 搜索按键回车即触发刷新
function onSearch() {
  page.value = 1
  void refresh()
}

// 订阅事件流自动刷新
onMounted(() => {
  onEvent('job.created', () => { void refresh() })
  onEvent('job.updated', () => { void refresh() })
  onEvent('job.finished', () => { void refresh() })
})

// 多选管理
const isAllSelected = computed(() => {
  const items = data.value?.items || []
  return items.length > 0 && items.every(j => selectedIds.value.has(j.id))
})

function toggleSelectAll() {
  const items = data.value?.items || []
  if (isAllSelected.value) {
    selectedIds.value.clear()
  } else {
    items.forEach(j => selectedIds.value.add(j.id))
  }
}

function toggleSelectRow(id: string) {
  if (selectedIds.value.has(id)) {
    selectedIds.value.delete(id)
  } else {
    selectedIds.value.add(id)
  }
}

// 行操作：取消
function openCancelDialog(id: string) {
  cancelingId.value = id
  cancelDialogOpen.value = true
}

async function executeCancel() {
  if (!cancelingId.value) return
  cancelBusy.value = true
  try {
    await api(`/jobs/${encodeURIComponent(cancelingId.value)}/cancel`, { method: 'POST' })
    cancelDialogOpen.value = false
    cancelingId.value = null
    await refresh()
  } finally {
    cancelBusy.value = false
  }
}

// 行操作：重放诊断
function openReplayDialog(job: Job) {
  replayingJob.value = job
  retainContainer.value = false
  replayDialogOpen.value = true
}

async function executeReplay() {
  if (!replayingJob.value) return
  replayBusy.value = true
  try {
    const res = await api<{ id?: string, request_hash?: string }>(`/jobs/${encodeURIComponent(replayingJob.value.id)}/replay`, {
      method: 'POST',
      body: { retain_container: retainContainer.value }
    })
    replayDialogOpen.value = false
    const newId = res.id || res.request_hash
    if (newId) {
      await navigateTo(`/jobs/${newId}`)
    } else {
      await refresh()
    }
  } finally {
    replayBusy.value = false
  }
}

// 行操作：重试
async function retryJob(job: Job) {
  try {
    // 提交原始请求体
    const res = await api<{ id?: string, request_hash?: string }>('/replay', {
      method: 'POST',
      body: job.request
    })
    const newId = res.id || res.request_hash
    if (newId) {
      await navigateTo(`/jobs/${newId}`)
    } else {
      await refresh()
    }
  } catch {
    /* 忽略 */
  }
}

// 批量删除产物
async function executeBatchDelete() {
  deleteBusy.value = true
  try {
    for (const id of selectedIds.value) {
      try {
        await api(`/store/${encodeURIComponent(id)}`, { method: 'DELETE' })
      } catch {
        /* 忽略个别失败 */
      }
    }
    selectedIds.value.clear()
    deleteBatchDialogOpen.value = false
    await refresh()
  } finally {
    deleteBusy.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <!-- 头部 -->
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl font-bold tracking-tight">任务历史记录</h1>
        <p class="text-xs text-muted">查询所有历史固件构建、诊断重放与产物留存</p>
      </div>

      <div class="flex items-center gap-2">
        <!-- 批量操作按钮 -->
        <UButton
          v-if="selectedIds.size > 0"
          color="error"
          variant="soft"
          size="sm"
          icon="i-lucide-trash-2"
          @click="deleteBatchDialogOpen = true"
        >
          批量清理产物 ({{ selectedIds.size }})
        </UButton>

        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loadStatus === 'pending'"
          @click="() => refresh()"
        >
          刷新
        </UButton>
      </div>
    </div>

    <!-- 过滤器工具栏 -->
    <div class="flex flex-wrap items-center gap-3 p-3 rounded-lg border border-default bg-elevated/40">
      <!-- 状态筛选 -->
      <div class="w-36">
        <USelect
          v-model="statusFilter"
          :items="statuses"
          size="sm"
        />
      </div>

      <!-- 关键字搜索 -->
      <div class="flex items-center gap-2 flex-1 min-w-[200px]">
        <UInput
          v-model="searchKeyword"
          placeholder="搜索 Profile 或 Hash 前缀..."
          size="sm"
          icon="i-lucide-search"
          class="w-full"
          @keydown.enter="onSearch"
        />
        <UButton color="neutral" variant="ghost" size="sm" @click="onSearch">
          查询
        </UButton>
      </div>
    </div>

    <!-- 错误态 -->
    <ErrorState
      v-if="error"
      title="任务列表获取失败"
      :description="error.message || '请检查网络连接后重试'"
      @retry="() => refresh()"
    />

    <!-- 表格 -->
    <div v-else class="rounded-lg border border-default overflow-x-auto bg-elevated/20">
      <table class="w-full text-xs text-left">
        <thead class="bg-muted/10 border-b border-default text-muted font-medium">
          <tr>
            <th class="p-3 w-8">
              <input
                type="checkbox"
                :checked="isAllSelected"
                class="rounded border-default cursor-pointer"
                @change="toggleSelectAll"
              >
            </th>
            <th class="p-3">状态</th>
            <th class="p-3">任务 Hash</th>
            <th class="p-3">Profile / 目标平台</th>
            <th class="p-3">客户端</th>
            <th class="p-3">排队时长</th>
            <th class="p-3">构建耗时</th>
            <th class="p-3">创建时间</th>
            <th class="p-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default">
          <tr
            v-for="job in data?.items ?? []"
            :key="job.id"
            class="hover:bg-muted/5 transition-colors"
          >
            <!-- 勾选框 -->
            <td class="p-3">
              <input
                type="checkbox"
                :checked="selectedIds.has(job.id)"
                class="rounded border-default cursor-pointer"
                @change="toggleSelectRow(job.id)"
              >
            </td>

            <!-- 状态 -->
            <td class="p-3">
              <StatusBadge :status="job.status" />
            </td>

            <!-- Hash -->
            <td class="p-3">
              <HashCell :hash="job.id" />
            </td>

            <!-- 设备与目标 -->
            <td class="p-3">
              <NuxtLink :to="`/jobs/${job.id}`" class="font-semibold text-primary hover:underline">
                {{ job.profile }}
              </NuxtLink>
              <div class="text-[10px] text-muted">
                {{ job.distro || 'openwrt' }} · {{ job.version }} · {{ job.target }}
              </div>
            </td>

            <!-- 客户端 -->
            <td class="p-3 text-muted">
              {{ parseClient(job.client) }}
            </td>

            <!-- 排队时长 -->
            <td class="p-3 text-muted">
              {{ job.queued_ms ? `${Math.round(job.queued_ms / 1000)} 秒` : '—' }}
            </td>

            <!-- 构建耗时 -->
            <td class="p-3">
              <DurationText :started-at="job.started_at" :finished-at="job.finished_at" :duration-ms="job.duration_ms" />
            </td>

            <!-- 创建时间 -->
            <td class="p-3 text-muted" :title="absoluteTime(job.enqueued_at)">
              {{ relativeTime(job.enqueued_at) }}
            </td>

            <!-- 行操作栏 -->
            <td class="p-3 text-right whitespace-nowrap space-x-1">
              <!-- 查看详情 -->
              <UButton
                :to="`/jobs/${job.id}`"
                color="neutral"
                variant="ghost"
                size="xs"
              >
                详情
              </UButton>

              <!-- 取消（仅运行中或排队中） -->
              <UButton
                v-if="['queued', 'running'].includes(job.status)"
                color="error"
                variant="ghost"
                size="xs"
                @click="openCancelDialog(job.id)"
              >
                取消
              </UButton>

              <!-- 重试（仅失败或超时） -->
              <UButton
                v-if="['failed', 'timeout'].includes(job.status)"
                color="warning"
                variant="ghost"
                size="xs"
                @click="retryJob(job)"
              >
                重试
              </UButton>

              <!-- 诊断重放 -->
              <UButton
                v-if="['failed', 'done'].includes(job.status)"
                color="neutral"
                variant="ghost"
                size="xs"
                @click="openReplayDialog(job)"
              >
                重放
              </UButton>
            </td>
          </tr>

          <!-- 空态 -->
          <tr v-if="!data?.items || data.items.length === 0">
            <td colspan="9" class="p-12 text-center">
              <EmptyState title="暂无符合条件的任务记录" description="可尝试修改状态筛选条件或清空搜索关键词" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 分页器 -->
    <div class="flex flex-wrap items-center justify-between gap-4 pt-2">
      <p class="text-xs text-muted">
        共 {{ data?.total ?? 0 }} 条任务记录 · 每页 {{ pageSize }} 条
      </p>
      <UPagination
        v-model:page="page"
        :total="data?.total ?? 0"
        :items-per-page="pageSize"
        :sibling-count="1"
        size="sm"
      />
    </div>

    <!-- 取消确认对话框 -->
    <ConfirmDialog
      v-model:open="cancelDialogOpen"
      title="取消构建任务"
      description="确定要取消此排队/运行中的构建吗？容器将被清理并释放槽位。"
      :loading="cancelBusy"
      @confirm="executeCancel"
      @cancel="cancelDialogOpen = false"
    />

    <!-- 批量删除产物对话框 -->
    <ConfirmDialog
      v-model:open="deleteBatchDialogOpen"
      title="批量删除构建产物"
      :description="`将永久删除选中的 ${selectedIds.size} 个任务的磁盘固件产物，释放磁盘空间。任务记录仍保留在数据库中。`"
      :items="Array.from(selectedIds)"
      :loading="deleteBusy"
      @confirm="executeBatchDelete"
      @cancel="deleteBatchDialogOpen = false"
    />

    <!-- 诊断重放模态框 -->
    <UModal v-model:open="replayDialogOpen" title="发起诊断重放 (Replay)" description="基于相同的构建参数重新启动容器以诊断构建问题">
      <template #body>
        <div class="space-y-4">
          <div class="rounded-lg bg-muted/10 p-3 text-xs space-y-1">
            <p><span class="text-muted">原始任务：</span><span class="font-mono">{{ replayingJob?.id }}</span></p>
            <p><span class="text-muted">设备型号：</span><span>{{ replayingJob?.profile }}</span></p>
          </div>
          <div class="flex items-center gap-2">
            <USwitch v-model="retainContainer" label="保留容器环境（便于管理员进入容器调试）" />
          </div>
        </div>
      </template>
      <template #footer>
        <div class="flex justify-end gap-2 w-full">
          <UButton color="neutral" variant="ghost" :disabled="replayBusy" @click="replayDialogOpen = false">取消</UButton>
          <UButton color="primary" :loading="replayBusy" @click="executeReplay">立即重放</UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
