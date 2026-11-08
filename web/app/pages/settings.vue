<script setup lang="ts">
import { useSettings } from '~/composables/useSettings'

const {
  settings,
  loading,
  probing,
  error,
  fetchSettings,
  probeEngine
} = useSettings()

const api = useAdminApi()
const currentUser = ref('admin')

async function loadMe() {
  try {
    const res = await api<{ name: string }>('/me')
    if (res?.name) currentUser.value = res.name
  } catch {
    /* 忽略 */
  }
}

async function handleLogout() {
  try {
    await api('/logout', { method: 'POST' })
  } finally {
    await navigateTo('/login', { replace: true })
  }
}

onMounted(() => {
  void fetchSettings()
  void loadMe()
})
</script>

<template>
  <div class="space-y-6 max-w-5xl">
    <!-- 头部 -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <div>
        <h1 class="text-xl sm:text-2xl font-bold tracking-tight text-highlighted">系统设置与状态</h1>
        <p class="text-xs text-muted mt-0.5">查看当前服务生效配置、容器执行引擎健康度与管理会话</p>
      </div>

      <div class="flex items-center gap-2 self-start sm:self-auto">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          class="border-default hover:bg-muted/30"
          @click="fetchSettings"
        >
          刷新配置
        </UButton>
      </div>
    </div>

    <!-- 错误态 -->
    <ErrorState
      v-if="error"
      title="配置读取失败"
      :description="error"
      @retry="fetchSettings"
    />

    <template v-else-if="settings">
      <!-- 1. 容器执行引擎卡片 -->
      <div class="rounded-xl border border-default bg-elevated/70 shadow-xs overflow-hidden">
        <!-- 头部条 -->
        <div class="flex flex-wrap items-center justify-between gap-2.5 p-4 sm:px-5 border-b border-default bg-muted/20">
          <div class="flex items-center gap-2.5">
            <div class="p-1.5 rounded-lg bg-emerald-500/10 text-emerald-400">
              <UIcon name="i-lucide-container" class="size-4 sm:size-5" />
            </div>
            <div>
              <h2 class="text-sm font-semibold text-highlighted">容器执行引擎 (Runtime Engine)</h2>
              <div class="flex items-center gap-1.5 text-[11px] text-emerald-400 mt-0.5 font-medium">
                <span class="size-1.5 rounded-full bg-emerald-500 inline-block animate-pulse" />
                <span>引擎连接就绪</span>
              </div>
            </div>
          </div>
          <UButton
            color="primary"
            variant="soft"
            size="xs"
            icon="i-lucide-activity"
            :loading="probing"
            @click="probeEngine"
          >
            重新探测引擎
          </UButton>
        </div>

        <div class="p-4 sm:p-5 space-y-4">
          <!-- 4 核心统计指标小块 -->
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <div class="rounded-lg border border-default bg-background/70 dark:bg-zinc-900/60 p-3">
              <span class="text-muted text-[11px] font-medium block">运行时类型</span>
              <p class="font-mono font-bold text-sm sm:text-base text-highlighted mt-1 capitalize">
                {{ settings.engine?.type || settings.runner.engine_type }}
              </p>
            </div>
            <div class="rounded-lg border border-default bg-background/70 dark:bg-zinc-900/60 p-3">
              <span class="text-muted text-[11px] font-medium block">引擎版本</span>
              <p class="font-mono font-bold text-sm sm:text-base text-highlighted mt-1">
                {{ settings.engine?.version || '5.x' }}
              </p>
            </div>
            <div class="rounded-lg border border-default bg-background/70 dark:bg-zinc-900/60 p-3">
              <span class="text-muted text-[11px] font-medium block">Socket 延迟</span>
              <p class="font-mono font-bold text-sm sm:text-base text-emerald-400 mt-1">
                {{ settings.engine?.latency_ms ?? 1 }} ms
              </p>
            </div>
            <div class="rounded-lg border border-default bg-background/70 dark:bg-zinc-900/60 p-3">
              <span class="text-muted text-[11px] font-medium block">活跃构建容器</span>
              <p class="font-mono font-bold text-sm sm:text-base text-highlighted mt-1">
                {{ settings.engine?.containers ?? 0 }} 个
              </p>
            </div>
          </div>

          <!-- 技术规格微栏 -->
          <div class="rounded-lg border border-default/70 bg-muted/20 p-3 text-xs space-y-1.5 font-mono">
            <div class="flex flex-col sm:flex-row sm:items-center gap-1 sm:gap-2">
              <span class="text-muted shrink-0 text-[11px]">Socket 路径:</span>
              <span class="text-default font-medium break-all select-all">{{ settings.engine?.socket || settings.runner.engine_socket }}</span>
            </div>
            <div class="flex flex-col sm:flex-row sm:items-center gap-1 sm:gap-2">
              <span class="text-muted shrink-0 text-[11px]">容器网络:</span>
              <span class="text-default font-medium break-all">{{ settings.runner.network }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 2. 生效配置摘要 (重构：清晰边界、深灰下沉质感、按业务分组，彻底杜绝粘连) -->
      <div class="rounded-xl border border-default bg-elevated/70 shadow-xs overflow-hidden">
        <div class="flex items-center justify-between p-4 sm:px-5 border-b border-default bg-muted/20">
          <div class="flex items-center gap-2.5">
            <div class="p-1.5 rounded-lg bg-sky-500/10 text-sky-400">
              <UIcon name="i-lucide-sliders-horizontal" class="size-4 sm:size-5" />
            </div>
            <h2 class="text-sm font-semibold text-highlighted">服务生效配置摘要</h2>
          </div>
          <UBadge color="neutral" variant="subtle" size="xs" class="font-mono border-default">
            只读安全模式 · 敏感脱敏
          </UBadge>
        </div>

        <div class="p-4 sm:p-5 space-y-4">
          <!-- 网格：每个配置项为独立立体的深灰色小卡片 -->
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3.5">
            <!-- 1. HTTP 监听 -->
            <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 hover:border-default-accented transition-colors">
              <div class="flex items-center gap-2 text-muted text-xs font-medium">
                <UIcon name="i-lucide-globe" class="size-4 text-emerald-400" />
                <span>HTTP 监听地址</span>
              </div>
              <p class="font-mono font-semibold text-sm text-highlighted mt-2">{{ settings.server.listen }}</p>
              <p class="text-[11px] text-muted mt-1">ASU 服务监听端口</p>
            </div>

            <!-- 2. 并发槽位限制 -->
            <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 hover:border-default-accented transition-colors">
              <div class="flex items-center gap-2 text-muted text-xs font-medium">
                <UIcon name="i-lucide-cpu" class="size-4 text-sky-400" />
                <span>并发构建限制</span>
              </div>
              <p class="font-mono font-semibold text-sm text-highlighted mt-2">{{ settings.runner.concurrency }} 个并行槽位</p>
              <p class="text-[11px] text-muted mt-1">最大 Worker 构建线程数</p>
            </div>

            <!-- 3. 产物存储目录 -->
            <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 hover:border-default-accented transition-colors">
              <div class="flex items-center gap-2 text-muted text-xs font-medium">
                <UIcon name="i-lucide-folder" class="size-4 text-amber-400" />
                <span>产物存储目录</span>
              </div>
              <p class="font-mono font-semibold text-sm text-highlighted mt-2 truncate" :title="settings.storage.data_dir">
                {{ settings.storage.data_dir }}
              </p>
              <p class="text-[11px] text-muted mt-1">固件与缓存持久化目录</p>
            </div>

            <!-- 4. 日志级别与格式 -->
            <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 hover:border-default-accented transition-colors">
              <div class="flex items-center gap-2 text-muted text-xs font-medium">
                <UIcon name="i-lucide-file-text" class="size-4 text-indigo-400" />
                <span>日志输出配置</span>
              </div>
              <p class="font-mono font-semibold text-sm text-highlighted mt-2 uppercase">
                {{ settings.server.log_level }} · {{ settings.server.log_format }}
              </p>
              <p class="text-[11px] text-muted mt-1">Zap 结构化日志引擎</p>
            </div>

            <!-- 5. 默认发行版 -->
            <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 hover:border-default-accented transition-colors">
              <div class="flex items-center gap-2 text-muted text-xs font-medium">
                <UIcon name="i-lucide-box" class="size-4 text-emerald-400" />
                <span>默认系统发行版</span>
              </div>
              <p class="font-mono font-semibold text-sm text-highlighted mt-2">{{ settings.build.default_distro }}</p>
              <p class="text-[11px] text-muted mt-1">缺省 ImageBuilder 源</p>
            </div>

            <!-- 6. 元数据存储驱动 -->
            <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 hover:border-default-accented transition-colors">
              <div class="flex items-center gap-2 text-muted text-xs font-medium">
                <UIcon name="i-lucide-database" class="size-4 text-purple-400" />
                <span>元数据持久化驱动</span>
              </div>
              <p class="font-mono font-semibold text-sm text-highlighted mt-2 uppercase">{{ settings.storage.driver }}</p>
              <p class="text-[11px] text-muted mt-1">免 CGO 嵌入式关系库</p>
            </div>
          </div>

          <!-- CORS 跨域策略 (规整微卡，与上方布局完美呼应) -->
          <div class="rounded-xl border border-default bg-background/80 dark:bg-zinc-900/60 p-3.5 flex flex-col sm:flex-row sm:items-center justify-between gap-2">
            <div class="flex items-center gap-2 text-muted text-xs font-medium">
              <UIcon name="i-lucide-shield-check" class="size-4 text-teal-400" />
              <span>CORS 允许跨域来源:</span>
              <span class="font-mono font-medium text-highlighted text-xs">
                {{ settings.server.cors_origins.join(', ') || '未配置跨域（已启用严格同源策略）' }}
              </span>
            </div>
            <span class="text-[11px] text-muted shrink-0">仅供允许的 Web 客户端调用</span>
          </div>
        </div>
      </div>

      <!-- 3. 管理会话与版本信息 (重构：字大点、框小点、高密度紧凑卡片) -->
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <!-- 会话卡 (紧凑小框，用户名大字号) -->
        <div class="rounded-xl border border-default bg-elevated/70 p-4 sm:p-5 shadow-xs flex flex-col justify-between space-y-3">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2 text-xs font-semibold text-muted">
              <UIcon name="i-lucide-user-check" class="size-4 text-emerald-400" />
              <span>当前管理会话</span>
            </div>
            <div class="flex items-center gap-1.5 text-[11px] text-emerald-400 font-medium">
              <span class="size-1.5 rounded-full bg-emerald-500 inline-block" />
              <span>在线</span>
            </div>
          </div>

          <div>
            <!-- 用户名字大点 -->
            <div class="text-xl sm:text-2xl font-bold text-highlighted tracking-tight flex items-center gap-2">
              <span>{{ currentUser }}</span>
              <UBadge color="primary" variant="subtle" size="xs" class="font-normal font-sans">
                管理员
              </UBadge>
            </div>
            <p class="text-xs text-muted mt-1">凭证方式：HttpOnly 安全 Cookie 会话</p>
          </div>

          <div class="pt-1">
            <UButton
              color="error"
              variant="soft"
              size="xs"
              icon="i-lucide-log-out"
              class="hover:bg-red-500/20"
              @click="handleLogout"
            >
              退出当前会话
            </UButton>
          </div>
        </div>

        <!-- 版本卡 (字大点、框小点、紧凑精致) -->
        <div class="rounded-xl border border-default bg-elevated/70 p-4 sm:p-5 shadow-xs flex flex-col justify-between space-y-3">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2 text-xs font-semibold text-muted">
              <UIcon name="i-lucide-git-commit" class="size-4 text-sky-400" />
              <span>实例版本信息</span>
            </div>
            <UBadge color="success" variant="subtle" size="xs" class="font-mono">
              Release
            </UBadge>
          </div>

          <div>
            <!-- 二进制版本号字大点 -->
            <div class="text-2xl sm:text-3xl font-black font-mono tracking-tight text-highlighted">
              {{ settings.version.version }}
            </div>
            <div class="mt-2 space-y-1 text-xs font-mono text-muted">
              <div class="flex items-center gap-1.5">
                <UIcon name="i-lucide-calendar" class="size-3.5 text-muted/80 shrink-0" />
                <span>构建时间: <span class="text-default">{{ settings.version.build_time }}</span></span>
              </div>
              <div class="flex items-center gap-1.5">
                <UIcon name="i-lucide-cpu" class="size-3.5 text-muted/80 shrink-0" />
                <span>Go 运行时: <span class="text-default">{{ settings.version.go_version }}</span></span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
