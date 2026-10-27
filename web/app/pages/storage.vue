<script setup lang="ts">
import { formatBytes } from '~/utils/format'
import { relativeTime, absoluteTime } from '~/utils/jobs'

const {
  overview,
  artifacts,
  loading,
  error,
  fetchOverview,
  fetchArtifacts,
  cleanStorage,
  deleteArtifact
} = useStorage()

// 清理弹窗与 dry-run 状态
type CleanScope = 'expired' | 'failed' | 'orphans'
const cleanDialogOpen = ref(false)
const currentScope = ref<CleanScope>('expired')
const dryRunResult = ref<{ items: number, bytes: number } | null>(null)
const dryRunLoading = ref(false)
const executingClean = ref(false)

const scopeTitles: Record<CleanScope, string> = {
  expired: '清理过期固件产物',
  failed: '清理失败构建残留',
  orphans: '清理孤儿工作目录'
}

const scopeDescriptions: Record<CleanScope, string> = {
  expired: '根据 TTL 策略，清理已超过保留期限的产物。',
  failed: '清理由于构建异常、失败或中断留下的未完成文件。',
  orphans: '清理无关联数据库任务的无主临时目录。'
}

async function startCleanFlow(scope: CleanScope) {
  currentScope.value = scope
  dryRunResult.value = null
  cleanDialogOpen.value = true
  dryRunLoading.value = true
  try {
    dryRunResult.value = await cleanStorage(scope, true)
  } finally {
    dryRunLoading.value = false
  }
}

async function confirmExecuteClean() {
  executingClean.value = true
  try {
    await cleanStorage(currentScope.value, false)
    cleanDialogOpen.value = false
    await Promise.all([fetchOverview(), fetchArtifacts()])
  } finally {
    executingClean.value = false
  }
}

// 单个产物删除
const deleteItemHash = ref<string | null>(null)
const deleteDialogOpen = ref(false)
const deleting = ref(false)

function confirmDeleteItem(hash: string) {
  deleteItemHash.value = hash
  deleteDialogOpen.value = true
}

async function executeDeleteItem() {
  if (!deleteItemHash.value) return
  deleting.value = true
  try {
    await deleteArtifact(deleteItemHash.value)
    deleteDialogOpen.value = false
    deleteItemHash.value = null
    await Promise.all([fetchOverview(), fetchArtifacts()])
  } finally {
    deleting.value = false
  }
}

// 展开查看文件清单
const expandedRows = ref<Set<string>>(new Set())
function toggleExpand(hash: string) {
  if (expandedRows.value.has(hash)) {
    expandedRows.value.delete(hash)
  } else {
    expandedRows.value.add(hash)
  }
}

onMounted(() => {
  void fetchOverview()
  void fetchArtifacts()
})
</script>

<template>
  <div class="space-y-6">
    <!-- 头部 -->
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl font-bold tracking-tight">存储与产物自治</h1>
        <p class="text-xs text-muted">监控磁盘占用，根据 TTL 策略自动或手动清理过期固件与残留</p>
      </div>

      <div class="flex items-center gap-2">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          @click="() => { fetchOverview(); fetchArtifacts(); }"
        >
          刷新
        </UButton>
      </div>
    </div>

    <!-- 错误态 -->
    <ErrorState
      v-if="error"
      title="获取存储信息失败"
      :description="error"
      @retry="() => { fetchOverview(); fetchArtifacts(); }"
    />

    <template v-else>
      <!-- 概览卡片 (4张) -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <!-- 1. 产物总占用 -->
        <UCard class="p-4">
          <div class="flex items-center justify-between text-muted text-xs">
            <span>固件产物总占用</span>
            <UIcon name="i-lucide-folder-archive" class="size-4 text-primary" />
          </div>
          <div class="mt-2 text-2xl font-bold font-mono">
            {{ formatBytes(overview?.artifacts?.bytes) }}
          </div>
          <p class="mt-1 text-xs text-muted">
            共计 {{ overview?.artifacts?.count ?? 0 }} 个构建产物包
          </p>
        </UCard>

        <!-- 2. 孤儿与临时占用 -->
        <UCard class="p-4">
          <div class="flex items-center justify-between text-muted text-xs">
            <span>孤儿与残留目录</span>
            <UIcon name="i-lucide-trash" class="size-4 text-warning" />
          </div>
          <div class="mt-2 text-2xl font-bold font-mono">
            {{ formatBytes(overview?.orphans?.bytes) }}
          </div>
          <p class="mt-1 text-xs text-muted">
            检测到 {{ overview?.orphans?.count ?? 0 }} 个无主残留目录
          </p>
        </UCard>

        <!-- 3. 最近一次清理 -->
        <UCard class="p-4">
          <div class="flex items-center justify-between text-muted text-xs">
            <span>最近清理活动</span>
            <UIcon name="i-lucide-sparkles" class="size-4 text-success" />
          </div>
          <div class="mt-2 text-base font-bold font-mono truncate">
            {{ overview?.last_clean ? relativeTime(overview.last_clean.at) : '暂无清理记录' }}
          </div>
          <p v-if="overview?.last_clean" class="mt-1 text-xs text-success">
            释放了 {{ formatBytes(overview.last_clean.freed_bytes) }} ({{ overview.last_clean.removed }} 项)
          </p>
          <p v-else class="mt-1 text-xs text-muted">尚未触发自动化清理</p>
        </UCard>

        <!-- 4. 清理操作入口 -->
        <UCard class="p-4 flex flex-col justify-between">
          <div class="text-xs font-semibold text-default">
            快速清理操作
          </div>
          <div class="flex flex-wrap gap-1.5 mt-2">
            <UButton
              color="primary"
              variant="soft"
              size="xs"
              icon="i-lucide-clock"
              @click="startCleanFlow('expired')"
            >
              清理过期
            </UButton>
            <UButton
              color="warning"
              variant="soft"
              size="xs"
              icon="i-lucide-alert-triangle"
              @click="startCleanFlow('failed')"
            >
              清理失败
            </UButton>
            <UButton
              color="error"
              variant="soft"
              size="xs"
              icon="i-lucide-folder-x"
              @click="startCleanFlow('orphans')"
            >
              清理孤儿
            </UButton>
          </div>
        </UCard>
      </div>

      <!-- TTL 策略卡（只读） -->
      <UCard class="p-4">
        <div class="flex items-center justify-between mb-2">
          <h2 class="text-xs font-semibold text-muted flex items-center gap-1.5">
            <UIcon name="i-lucide-shield-alert" class="size-3.5" />
            <span>当前生效的生命周期策略 (TTL Policy)</span>
          </h2>
          <span class="text-[10px] text-muted">修改请编辑配置文件并重启服务</span>
        </div>
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
          <div class="p-2.5 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">通用默认保留期 (defaults)</span>
            <p class="font-mono font-bold mt-1">{{ overview?.ttl?.defaults || '7d' }}</p>
          </div>
          <div class="p-2.5 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">版本化镜像保留期 (versioned)</span>
            <p class="font-mono font-bold mt-1">{{ overview?.ttl?.versioned || '30d' }}</p>
          </div>
          <div class="p-2.5 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">失败任务残留清理 (failed)</span>
            <p class="font-mono font-bold mt-1">{{ overview?.ttl?.failed || '1d' }}</p>
          </div>
          <div class="p-2.5 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">诊断容器保留 (purge_at)</span>
            <p class="font-mono font-bold mt-1">{{ overview?.ttl?.purge_at || '24h' }}</p>
          </div>
        </div>
      </UCard>

      <!-- 产物列表表格 -->
      <section class="space-y-3">
        <h2 class="text-sm font-semibold">磁盘固件产物列表</h2>

        <div class="rounded-lg border border-default overflow-x-auto bg-elevated/20">
          <table class="w-full text-xs text-left">
            <thead class="bg-muted/10 border-b border-default text-muted font-medium">
              <tr>
                <th class="p-3 w-8" />
                <th class="p-3">Profile / 设备型号</th>
                <th class="p-3">发行版 / 目标</th>
                <th class="p-3">产物大小</th>
                <th class="p-3">文件数</th>
                <th class="p-3">生成时间</th>
                <th class="p-3">过期时间</th>
                <th class="p-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-default">
              <template v-for="item in artifacts" :key="item.request_hash">
                <tr class="hover:bg-muted/5 transition-colors">
                  <!-- 展开按钮 -->
                  <td class="p-3">
                    <button
                      type="button"
                      class="text-muted hover:text-default cursor-pointer"
                      :title="expandedRows.has(item.request_hash) ? '收起文件清单' : '展开文件清单'"
                      @click="toggleExpand(item.request_hash)"
                    >
                      <UIcon
                        :name="expandedRows.has(item.request_hash) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                        class="size-4"
                      />
                    </button>
                  </td>

                  <!-- 设备型号 -->
                  <td class="p-3 font-semibold text-default">
                    {{ item.profile }}
                    <div class="text-[10px] text-muted font-mono">{{ item.request_hash }}</div>
                  </td>

                  <!-- 发行版 -->
                  <td class="p-3 text-muted">
                    {{ item.distro }} · {{ item.version }} · {{ item.target }}
                  </td>

                  <!-- 大小 -->
                  <td class="p-3 font-mono font-medium">
                    {{ formatBytes(item.bytes) }}
                  </td>

                  <!-- 文件数 -->
                  <td class="p-3 text-muted font-mono">
                    {{ item.files.length }} 个文件
                  </td>

                  <!-- 生成时间 -->
                  <td class="p-3 text-muted" :title="absoluteTime(item.created_at)">
                    {{ relativeTime(item.created_at) }}
                  </td>

                  <!-- 过期时间 -->
                  <td class="p-3 text-muted" :title="absoluteTime(item.expires_at)">
                    {{ item.expires_at ? relativeTime(item.expires_at) : '永久保留' }}
                  </td>

                  <!-- 操作 -->
                  <td class="p-3 text-right">
                    <UButton
                      color="error"
                      variant="ghost"
                      size="xs"
                      icon="i-lucide-trash-2"
                      @click="confirmDeleteItem(item.request_hash)"
                    >
                      删除
                    </UButton>
                  </td>
                </tr>

                <!-- 展开的详细文件清单 -->
                <tr v-if="expandedRows.has(item.request_hash)" class="bg-muted/5">
                  <td colspan="8" class="p-3 pl-12">
                    <p class="text-[11px] font-medium text-muted mb-1.5">包含的文件清单：</p>
                    <ul class="space-y-1 font-mono text-xs">
                      <li v-for="file in item.files" :key="file" class="flex items-center gap-2">
                        <UIcon name="i-lucide-file-archive" class="size-3.5 text-muted" />
                        <NuxtLink
                          :to="`/store/${item.request_hash}/${file}`"
                          target="_blank"
                          class="hover:underline text-primary"
                        >
                          {{ file }}
                        </NuxtLink>
                      </li>
                    </ul>
                  </td>
                </tr>
              </template>

              <!-- 空态 -->
              <tr v-if="artifacts.length === 0">
                <td colspan="8" class="p-12 text-center">
                  <EmptyState title="暂无固件产物记录" description="所有构建产物已清理或尚未产生" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>

    <!-- 清理预演与确认弹窗 (Dry Run) -->
    <UModal v-model:open="cleanDialogOpen" :title="scopeTitles[currentScope]">
      <template #body>
        <div class="space-y-4">
          <p class="text-xs text-muted">{{ scopeDescriptions[currentScope] }}</p>

          <div v-if="dryRunLoading" class="py-6 text-center text-muted">
            <UIcon name="i-lucide-loader-2" class="size-6 animate-spin mx-auto mb-2 text-primary" />
            <p class="text-xs">正在分析磁盘占用与预估释放空间 (Dry Run)…</p>
          </div>

          <div v-else-if="dryRunResult" class="rounded-lg border border-default p-4 bg-muted/5 space-y-2">
            <h4 class="text-xs font-semibold text-default">预演清理结果：</h4>
            <div class="flex items-center justify-between text-xs">
              <span class="text-muted">预计删除条目数：</span>
              <span class="font-mono font-bold">{{ dryRunResult.items }} 项</span>
            </div>
            <div class="flex items-center justify-between text-xs">
              <span class="text-muted">预计释放磁盘空间：</span>
              <span class="font-mono font-bold text-success">{{ formatBytes(dryRunResult.bytes) }}</span>
            </div>
          </div>
        </div>
      </template>

      <template #footer>
        <div class="flex justify-end gap-2 w-full">
          <UButton color="neutral" variant="ghost" :disabled="executingClean" @click="cleanDialogOpen = false">
            取消
          </UButton>
          <UButton
            color="error"
            :loading="executingClean"
            :disabled="dryRunLoading || dryRunResult?.items === 0"
            @click="confirmExecuteClean"
          >
            确认立即清理
          </UButton>
        </div>
      </template>
    </UModal>

    <!-- 单个产物删除对话框 -->
    <ConfirmDialog
      v-model:open="deleteDialogOpen"
      title="删除产物"
      description="确定要彻底删除该任务对应的固件镜像文件吗？删除后文件不可恢复。"
      :loading="deleting"
      @confirm="executeDeleteItem"
      @cancel="deleteDialogOpen = false"
    />
  </div>
</template>
