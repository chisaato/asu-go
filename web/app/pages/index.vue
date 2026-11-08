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
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <div>
        <h1 class="text-xl sm:text-2xl font-bold tracking-tight text-highlighted">集群仪表盘</h1>
        <p class="text-xs text-muted mt-0.5">监控当前固件构建排队、槽位并发与引擎健康度</p>
      </div>
      <div class="flex items-center gap-2 self-start sm:self-auto">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          class="border-default hover:bg-muted/30"
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
      <!-- 顶部指标卡栅格 (6张，移动端2列、平板3列、桌面6列) -->
      <div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-3 sm:gap-4">
        <!-- 1. 并发槽位 -->
        <div class="rounded-xl border border-default bg-elevated/80 p-3.5 sm:p-4 hover:border-default-accented transition-all duration-150 flex flex-col justify-between">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted truncate">并发槽位</span>
            <div class="p-1.5 rounded-lg bg-emerald-500/10 text-emerald-400 shrink-0">
              <UIcon name="i-lucide-cpu" class="size-4" />
            </div>
          </div>
          <div class="mt-2 text-xl sm:text-2xl xl:text-3xl font-bold font-mono tracking-tight text-highlighted">
            {{ overview?.slots?.running ?? 0 }}
            <span class="text-xs sm:text-sm font-normal text-muted">/ {{ overview?.slots?.max ?? 4 }}</span>
          </div>
          <p class="mt-1.5 text-[11px] text-muted truncate">
            {{ (overview?.slots?.running ?? 0) > 0 ? `${overview?.slots?.running} 个槽位占用中` : '所有槽位空闲' }}
          </p>
        </div>

        <!-- 2. 排队中 -->
        <div class="rounded-xl border border-default bg-elevated/80 p-3.5 sm:p-4 hover:border-default-accented transition-all duration-150 flex flex-col justify-between">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted truncate">排队任务</span>
            <div class="p-1.5 rounded-lg bg-amber-500/10 text-amber-400 shrink-0">
              <UIcon name="i-lucide-hourglass" class="size-4" />
            </div>
          </div>
          <div class="mt-2 text-xl sm:text-2xl xl:text-3xl font-bold font-mono tracking-tight text-highlighted">
            {{ overview?.queued ?? 0 }}
          </div>
          <p class="mt-1.5 text-[11px] text-muted truncate">
            等待 Worker 槽位空出
          </p>
        </div>

        <!-- 3. 今日构建与成功率 -->
        <div class="rounded-xl border border-default bg-elevated/80 p-3.5 sm:p-4 hover:border-default-accented transition-all duration-150 flex flex-col justify-between">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted truncate">今日构建</span>
            <div class="p-1.5 rounded-lg bg-sky-500/10 text-sky-400 shrink-0">
              <UIcon name="i-lucide-activity" class="size-4" />
            </div>
          </div>
          <div class="mt-2 text-xl sm:text-2xl xl:text-3xl font-bold font-mono tracking-tight text-highlighted">
            {{ overview?.today?.total ?? 0 }}
          </div>
          <p class="mt-1.5 text-[11px] text-emerald-400 flex items-center gap-1 truncate font-medium">
            <UIcon name="i-lucide-check" class="size-3 shrink-0" />
            <span>成功率 {{ overview?.today?.total ? Math.round(((overview?.today?.success ?? 0) / overview.today.total) * 100) : 100 }}%</span>
          </p>
        </div>

        <!-- 4. 失败/异常 -->
        <div class="rounded-xl border border-default bg-elevated/80 p-3.5 sm:p-4 hover:border-default-accented transition-all duration-150 flex flex-col justify-between">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted truncate">近 24h 失败</span>
            <div class="p-1.5 rounded-lg bg-red-500/10 text-red-400 shrink-0">
              <UIcon name="i-lucide-alert-circle" class="size-4" />
            </div>
          </div>
          <div
            class="mt-2 text-xl sm:text-2xl xl:text-3xl font-bold font-mono tracking-tight"
            :class="(overview?.today?.failed ?? 0) > 0 ? 'text-red-400' : 'text-highlighted'"
          >
            {{ overview?.today?.failed ?? 0 }}
          </div>
          <p class="mt-1.5 text-[11px] text-muted truncate">
            {{ (overview?.today?.failed ?? 0) > 0 ? '需要管理员排查' : '运行状态良好' }}
          </p>
        </div>

        <!-- 5. 磁盘占用 -->
        <div class="rounded-xl border border-default bg-elevated/80 p-3.5 sm:p-4 hover:border-default-accented transition-all duration-150 flex flex-col justify-between">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted truncate">产物占用</span>
            <div class="p-1.5 rounded-lg bg-zinc-500/10 text-zinc-400 shrink-0">
              <UIcon name="i-lucide-hard-drive" class="size-4" />
            </div>
          </div>
          <div class="mt-2 text-xl sm:text-2xl xl:text-3xl font-bold font-mono tracking-tight text-highlighted truncate">
            {{ formatBytes(overview?.disk?.used_bytes) }}
          </div>
          <p class="mt-1.5 text-[11px] text-muted truncate">
            剩余: {{ formatBytes(overview?.disk?.free_bytes) }}
          </p>
        </div>

        <!-- 6. 容器引擎 -->
        <div class="rounded-xl border border-default bg-elevated/80 p-3.5 sm:p-4 hover:border-default-accented transition-all duration-150 flex flex-col justify-between">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted truncate">容器引擎</span>
            <div class="p-1.5 rounded-lg bg-emerald-500/10 text-emerald-400 shrink-0">
              <UIcon name="i-lucide-container" class="size-4" />
            </div>
          </div>
          <div class="mt-2 text-lg sm:text-xl xl:text-2xl font-bold font-mono capitalize text-highlighted truncate">
            {{ overview?.engine?.type || 'Podman' }}
            <span class="text-xs font-normal text-muted font-sans">{{ overview?.engine?.version || '' }}</span>
          </div>
          <p class="mt-1.5 text-[11px] text-muted flex items-center gap-1.5 truncate">
            <span class="size-2 rounded-full bg-emerald-500 inline-block animate-pulse" />
            <span>{{ overview?.engine?.latency_ms ?? 1 }}ms · {{ overview?.engine?.containers ?? 0 }} 容器</span>
          </p>
        </div>
      </div>

      <!-- 核心区域：运行中的构建 (Active Builds) -->
      <section class="space-y-3">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <h2 class="text-sm font-semibold text-highlighted">正在运行的构建</h2>
            <UBadge v-if="overview?.active?.length" color="info" variant="subtle" size="xs">
              {{ overview.active.length }} 个活跃
            </UBadge>
          </div>
          <NuxtLink to="/jobs?status=running" class="text-xs text-primary hover:underline flex items-center gap-1 font-medium">
            <span>任务列表</span>
            <UIcon name="i-lucide-arrow-right" class="size-3" />
          </NuxtLink>
        </div>

        <!-- 运行任务列表卡片 -->
        <div v-if="overview?.active && overview.active.length > 0" class="space-y-3">
          <div
            v-for="job in overview.active"
            :key="job.id"
            class="rounded-xl border border-primary/40 bg-elevated/90 p-4 hover:border-primary/70 transition-all duration-150 shadow-xs"
          >
            <div class="flex flex-col md:flex-row md:items-center justify-between gap-4">
              <!-- 左侧基本信息 -->
              <div class="space-y-2 flex-1 min-w-0">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="font-bold text-sm text-highlighted">{{ job.profile }}</span>
                  <UBadge color="neutral" variant="outline" size="xs" class="font-mono border-default">
                    {{ job.distro || 'openwrt' }} · {{ job.version }} · {{ job.target }}
                  </UBadge>
                  <HashCell :hash="job.id" />
                </div>

                <div class="flex items-center gap-3 text-xs text-muted flex-wrap">
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-laptop" class="size-3.5 text-muted/80" />
                    <span>{{ parseClient(job.client) }}</span>
                  </span>
                  <span class="flex items-center gap-1">
                    <DurationText :started-at="job.started_at" />
                  </span>
                  <span class="flex items-center gap-1 text-emerald-400 font-medium">
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
                  控制台
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
          </div>
        </div>

        <div v-else class="rounded-xl border border-dashed border-default p-8 text-center bg-elevated/40">
          <UIcon name="i-lucide-check-circle" class="size-8 text-emerald-500/70 mx-auto mb-2" />
          <p class="text-xs text-muted font-medium">当前暂无运行中的构建任务，所有 Worker 槽位空闲中</p>
        </div>
      </section>

      <!-- 双栏布局：排队中任务 & 近期完成 -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-5 sm:gap-6">
        <!-- 排队队列 -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h2 class="text-sm font-semibold text-highlighted">排队等待中 ({{ overview?.queue?.length ?? 0 }})</h2>
            <NuxtLink to="/jobs?status=queued" class="text-xs text-muted hover:text-default">查看全部</NuxtLink>
          </div>

          <div v-if="overview?.queue && overview.queue.length > 0" class="rounded-xl border border-default bg-elevated/60 overflow-hidden shadow-xs">
            <div class="overflow-x-auto">
              <table class="w-full text-xs sm:text-sm text-left min-w-[480px]">
                <thead class="bg-muted/40 border-b border-default text-muted">
                  <tr>
                    <th class="p-3 font-semibold">位置</th>
                    <th class="p-3 font-semibold">设备 / 目标</th>
                    <th class="p-3 font-semibold">任务 Hash</th>
                    <th class="p-3 font-semibold">入队时间</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-default">
                  <tr v-for="(job, index) in overview.queue" :key="job.id" class="hover:bg-muted/15 transition-colors">
                    <td class="p-3 font-mono font-medium text-highlighted">#{{ index + 1 }}</td>
                    <td class="p-3 font-medium">{{ job.profile }} ({{ job.target }})</td>
                    <td class="p-3"><HashCell :hash="job.id" /></td>
                    <td class="p-3 text-muted">{{ relativeTime(job.enqueued_at) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-else class="rounded-xl border border-default bg-elevated/40 p-8 text-center text-sm text-muted">
            队列为空，无排队请求
          </div>
        </section>

        <!-- 近期完成 -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h2 class="text-sm font-semibold text-highlighted">近期完成 (最近 10 条)</h2>
            <NuxtLink to="/jobs" class="text-xs text-muted hover:text-default">历史记录</NuxtLink>
          </div>

          <div v-if="overview?.recent && overview.recent.length > 0" class="rounded-xl border border-default bg-elevated/60 overflow-hidden shadow-xs">
            <div class="overflow-x-auto">
              <table class="w-full text-xs sm:text-sm text-left min-w-[540px]">
                <thead class="bg-muted/40 border-b border-default text-muted">
                  <tr>
                    <th class="p-3 font-semibold">状态</th>
                    <th class="p-3 font-semibold">设备 / 目标</th>
                    <th class="p-3 font-semibold">耗时</th>
                    <th class="p-3 font-semibold">完成时间</th>
                    <th class="p-3 font-semibold text-right">操作</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-default">
                  <tr v-for="job in overview.recent" :key="job.id" class="hover:bg-muted/15 transition-colors">
                    <td class="p-3"><StatusBadge :status="job.status" /></td>
                    <td class="p-3">
                      <NuxtLink :to="`/jobs/${job.id}`" class="font-medium text-primary hover:underline">
                        {{ job.profile }}
                      </NuxtLink>
                      <div class="text-[11px] text-muted">{{ job.target }} · {{ job.version }}</div>
                    </td>
                    <td class="p-3"><DurationText :started-at="job.started_at" :finished-at="job.finished_at" :duration-ms="job.duration_ms" /></td>
                    <td class="p-3 text-muted text-xs">{{ relativeTime(job.finished_at) }}</td>
                    <td class="p-3 text-right">
                      <UButton :to="`/jobs/${job.id}`" color="neutral" variant="ghost" size="xs">
                        详情
                      </UButton>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-else class="rounded-xl border border-default bg-elevated/40 p-8 text-center text-sm text-muted">
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
