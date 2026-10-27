<script setup lang="ts">
import { formatBytes, parseClient } from '~/utils/format'
import { stageInfo } from '~/utils/stages'
import { relativeTime } from '~/utils/jobs'

const api = useAdminApi()
const { overview, loading, error, refresh } = useOverview()
const { on: onEvent } = useEventStream()

// 终止任务对话框状态
const cancelDialogOpen = ref(false)
const cancelingJobId = ref<string | null>(null)
const cancelingBusy = ref(false)

function confirmCancel(id: string) {
  cancelingJobId.value = id
  cancelDialogOpen.value = true
}

async function executeCancel() {
  if (!cancelingJobId.value) return
  cancelingBusy.value = true
  try {
    await api(`/jobs/${encodeURIComponent(cancelingJobId.value)}/cancel`, { method: 'POST' })
    cancelDialogOpen.value = false
    cancelingJobId.value = null
    await refresh()
  } catch {
    /* 忽略 */
  } finally {
    cancelingBusy.value = false
  }
}

onMounted(() => {
  void refresh()
  // 订阅事件流
  onEvent('job.created', () => { void refresh() })
  onEvent('job.updated', () => { void refresh() })
  onEvent('job.finished', () => { void refresh() })
  onEvent('overview', () => { void refresh() })
})
</script>

<template>
  <div class="space-y-6">
    <!-- 页头 -->
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl font-bold tracking-tight">集群仪表盘</h1>
        <p class="text-xs text-muted">监控当前固件构建排队、槽位并发与引擎健康度</p>
      </div>
      <div class="flex items-center gap-2">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          @click="refresh"
        >
          刷新概览
        </UButton>
        <UButton
          to="/build"
          color="primary"
          size="sm"
          icon="i-lucide-plus"
        >
          新建构建
        </UButton>
      </div>
    </div>

    <!-- 错误态 -->
    <ErrorState
      v-if="error"
      title="仪表盘加载失败"
      :description="error"
      @retry="refresh"
    />

    <template v-else>
      <!-- 顶部指标卡栅格 (6张) -->
      <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-4">
        <!-- 1. 并发槽位 -->
        <UCard class="p-4 sm:p-5">
          <div class="flex items-center justify-between text-muted text-xs sm:text-sm">
            <span>并发构建槽位</span>
            <UIcon name="i-lucide-cpu" class="size-4 sm:size-5 text-primary" />
          </div>
          <div class="mt-2 text-2xl sm:text-3xl font-bold font-mono">
            {{ overview?.slots?.running ?? 0 }} <span class="text-sm font-normal text-muted">/ {{ overview?.slots?.max ?? 4 }}</span>
          </div>
          <p class="mt-2 text-xs text-muted truncate">
            {{ (overview?.slots?.running ?? 0) > 0 ? `${overview?.slots?.running} 个槽位占用中` : '所有构建槽位空闲' }}
          </p>
        </UCard>

        <!-- 2. 排队中 -->
        <UCard class="p-4 sm:p-5">
          <div class="flex items-center justify-between text-muted text-xs sm:text-sm">
            <span>当前排队任务</span>
            <UIcon name="i-lucide-hourglass" class="size-4 sm:size-5 text-warning" />
          </div>
          <div class="mt-2 text-2xl sm:text-3xl font-bold font-mono">
            {{ overview?.queued ?? 0 }}
          </div>
          <p class="mt-2 text-xs text-muted truncate">
            等待 Worker 槽位空出
          </p>
        </UCard>

        <!-- 3. 今日构建与成功率 -->
        <UCard class="p-4 sm:p-5">
          <div class="flex items-center justify-between text-muted text-xs sm:text-sm">
            <span>今日构建总数</span>
            <UIcon name="i-lucide-activity" class="size-4 sm:size-5 text-info" />
          </div>
          <div class="mt-2 text-2xl sm:text-3xl font-bold font-mono">
            {{ overview?.today?.total ?? 0 }}
          </div>
          <p class="mt-2 text-xs text-success flex items-center gap-1">
            <UIcon name="i-lucide-check" class="size-3.5" />
            <span>成功率 {{ overview?.today?.total ? Math.round(((overview?.today?.success ?? 0) / overview.today.total) * 100) : 100 }}%</span>
          </p>
        </UCard>

        <!-- 4. 失败/异常 -->
        <UCard class="p-4 sm:p-5">
          <div class="flex items-center justify-between text-muted text-xs sm:text-sm">
            <span>近 24h 失败</span>
            <UIcon name="i-lucide-alert-circle" class="size-4 sm:size-5 text-error" />
          </div>
          <div class="mt-2 text-2xl sm:text-3xl font-bold font-mono text-error">
            {{ overview?.today?.failed ?? 0 }}
          </div>
          <p class="mt-2 text-xs text-muted truncate">
            需要管理员排查日志
          </p>
        </UCard>

        <!-- 5. 磁盘占用 -->
        <UCard class="p-4 sm:p-5">
          <div class="flex items-center justify-between text-muted text-xs sm:text-sm">
            <span>产物磁盘占用</span>
            <UIcon name="i-lucide-hard-drive" class="size-4 sm:size-5 text-muted" />
          </div>
          <div class="mt-2 text-2xl sm:text-3xl font-bold font-mono truncate">
            {{ formatBytes(overview?.disk?.used_bytes) }}
          </div>
          <p class="mt-2 text-xs text-muted truncate">
            剩余: {{ formatBytes(overview?.disk?.free_bytes) }}
          </p>
        </UCard>

        <!-- 6. 容器引擎 -->
        <UCard class="p-4 sm:p-5">
          <div class="flex items-center justify-between text-muted text-xs sm:text-sm">
            <span>容器运行时</span>
            <UIcon name="i-lucide-container" class="size-4 sm:size-5 text-success" />
          </div>
          <div class="mt-2 text-xl sm:text-2xl font-bold font-mono capitalize truncate">
            {{ overview?.engine?.type || 'Podman' }} <span class="text-xs font-normal text-muted">{{ overview?.engine?.version || '' }}</span>
          </div>
          <p class="mt-2 text-xs text-muted flex items-center gap-1 truncate">
            <span class="size-1.5 rounded-full bg-success inline-block" />
            <span>延迟 {{ overview?.engine?.latency_ms ?? 1 }}ms · {{ overview?.engine?.containers ?? 0 }} 容器</span>
          </p>
        </UCard>
      </div>

      <!-- 核心区域：运行中的构建 (Active Builds) -->
      <section class="space-y-3">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <h2 class="text-sm font-semibold">正在运行的构建</h2>
            <UBadge v-if="overview?.active?.length" color="info" variant="subtle" size="xs">
              {{ overview.active.length }} 个活跃
            </UBadge>
          </div>
          <NuxtLink to="/jobs?status=running" class="text-xs text-primary hover:underline flex items-center gap-1">
            <span>在任务列表中查看</span>
            <UIcon name="i-lucide-arrow-right" class="size-3" />
          </NuxtLink>
        </div>

        <!-- 运行任务列表卡片 -->
        <div v-if="overview?.active && overview.active.length > 0" class="space-y-3">
          <UCard
            v-for="job in overview.active"
            :key="job.id"
            class="p-4 hover:border-primary/40 transition-all border border-default"
          >
            <div class="flex flex-col md:flex-row md:items-center justify-between gap-4">
              <!-- 左侧基本信息 -->
              <div class="space-y-1.5 flex-1 min-w-0">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="font-bold text-sm text-default">{{ job.profile }}</span>
                  <UBadge color="neutral" variant="outline" size="xs" class="font-mono">
                    {{ job.distro || 'openwrt' }} · {{ job.version }} · {{ job.target }}
                  </UBadge>
                  <HashCell :hash="job.id" />
                </div>

                <div class="flex items-center gap-3 text-xs text-muted">
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-laptop" class="size-3.5" />
                    <span>{{ parseClient(job.client) }}</span>
                  </span>
                  <span class="flex items-center gap-1">
                    <DurationText :started-at="job.started_at" />
                  </span>
                  <span class="flex items-center gap-1 text-primary">
                    <UIcon name="i-lucide-layers" class="size-3.5" />
                    <span>{{ stageInfo(job.stage).label }}</span>
                  </span>
                </div>

                <!-- 进度条 -->
                <div class="pt-1 max-w-xl">
                  <UProgress :value="stageInfo(job.stage).percent" color="primary" class="h-2" />
                </div>
              </div>

              <!-- 右侧快捷操作按钮 -->
              <div class="flex items-center gap-2 self-start md:self-center shrink-0">
                <UButton
                  :to="`/jobs/${job.id}`"
                  color="primary"
                  variant="solid"
                  size="xs"
                  icon="i-lucide-terminal"
                >
                  打开控制台
                </UButton>
                <UButton
                  color="error"
                  variant="soft"
                  size="xs"
                  icon="i-lucide-square"
                  @click="confirmCancel(job.id)"
                >
                  终止
                </UButton>
              </div>
            </div>
          </UCard>
        </div>

        <div v-else class="rounded-lg border border-default border-dashed p-8 text-center bg-muted/5">
          <UIcon name="i-lucide-check-circle" class="size-8 text-muted mx-auto mb-2 opacity-50" />
          <p class="text-xs text-muted">当前暂无运行中的构建任务，所有 Worker 槽位空闲中</p>
        </div>
      </section>

      <!-- 双栏布局：排队中任务 & 近期完成 -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <!-- 排队队列 -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h2 class="text-sm font-semibold">排队等待中 ({{ overview?.queue?.length ?? 0 }})</h2>
            <NuxtLink to="/jobs?status=queued" class="text-xs text-muted hover:text-default">查看全部</NuxtLink>
          </div>

          <div v-if="overview?.queue && overview.queue.length > 0" class="rounded-xl border border-default overflow-x-auto">
            <table class="w-full text-xs sm:text-sm text-left min-w-[480px]">
              <thead class="bg-muted/10 border-b border-default text-muted">
                <tr>
                  <th class="p-3 font-medium">位置</th>
                  <th class="p-3 font-medium">设备 / 目标</th>
                  <th class="p-3 font-medium">任务 Hash</th>
                  <th class="p-3 font-medium">入队时间</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-default">
                <tr v-for="(job, index) in overview.queue" :key="job.id" class="hover:bg-muted/5">
                  <td class="p-3 font-mono font-medium">#{{ index + 1 }}</td>
                  <td class="p-3 font-medium">{{ job.profile }} ({{ job.target }})</td>
                  <td class="p-3"><HashCell :hash="job.id" /></td>
                  <td class="p-3 text-muted">{{ relativeTime(job.enqueued_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-else class="rounded-xl border border-default p-8 text-center text-sm text-muted">
            队列为空，无排队请求
          </div>
        </section>

        <!-- 近期完成 -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h2 class="text-sm sm:text-base font-semibold">近期完成 (最近 10 条)</h2>
            <NuxtLink to="/jobs" class="text-xs sm:text-sm text-muted hover:text-default">历史记录</NuxtLink>
          </div>

          <div v-if="overview?.recent && overview.recent.length > 0" class="rounded-xl border border-default overflow-x-auto">
            <table class="w-full text-xs sm:text-sm text-left min-w-[560px]">
              <thead class="bg-muted/10 border-b border-default text-muted">
                <tr>
                  <th class="p-3 font-medium">状态</th>
                  <th class="p-3 font-medium">设备 / 目标</th>
                  <th class="p-3 font-medium">耗时</th>
                  <th class="p-3 font-medium">完成时间</th>
                  <th class="p-3 font-medium text-right">操作</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-default">
                <tr v-for="job in overview.recent" :key="job.id" class="hover:bg-muted/5">
                  <td class="p-3"><StatusBadge :status="job.status" /></td>
                  <td class="p-3">
                    <NuxtLink :to="`/jobs/${job.id}`" class="font-medium text-primary hover:underline">
                      {{ job.profile }}
                    </NuxtLink>
                    <div class="text-xs text-muted">{{ job.target }} · {{ job.version }}</div>
                  </td>
                  <td class="p-3"><DurationText :started-at="job.started_at" :finished-at="job.finished_at" :duration-ms="job.duration_ms" /></td>
                  <td class="p-3 text-muted">{{ relativeTime(job.finished_at) }}</td>
                  <td class="p-3 text-right">
                    <UButton :to="`/jobs/${job.id}`" color="neutral" variant="ghost" size="xs">
                      详情
                    </UButton>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-else class="rounded-xl border border-default p-8 text-center text-sm text-muted">
            暂无近期完成记录
          </div>
        </section>
      </div>
    </template>

    <!-- 终止确认对话框 -->
    <ConfirmDialog
      v-model:open="cancelDialogOpen"
      title="终止构建任务"
      description="确定要强制终止该构建任务吗？容器将被立即销毁并释放构建槽位。"
      confirm-text="确认终止"
      :loading="cancelingBusy"
      @confirm="executeCancel"
      @cancel="cancelDialogOpen = false"
    />
  </div>
</template>
