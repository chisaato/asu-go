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
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl font-bold tracking-tight">系统设置与状态</h1>
        <p class="text-xs text-muted">查看当前服务生效配置、容器执行引擎健康度与管理会话</p>
      </div>

      <div class="flex items-center gap-2">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          @click="fetchSettings"
        >
          刷新
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
      <UCard class="p-5 space-y-4">
        <div class="flex items-center justify-between border-b border-default pb-3">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-container" class="size-5 text-primary" />
            <h2 class="text-sm font-semibold">容器引擎状态 (Runtime Engine)</h2>
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

        <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 text-xs">
          <div>
            <span class="text-muted text-[10px]">运行时类型</span>
            <p class="font-mono font-bold mt-1 text-sm capitalize">{{ settings.engine?.type || settings.runner.engine_type }}</p>
          </div>
          <div>
            <span class="text-muted text-[10px]">引擎版本</span>
            <p class="font-mono font-medium mt-1">{{ settings.engine?.version || '5.x' }}</p>
          </div>
          <div>
            <span class="text-muted text-[10px]">Socket 延迟</span>
            <p class="font-mono font-medium mt-1 text-success">{{ settings.engine?.latency_ms ?? 1 }} ms</p>
          </div>
          <div>
            <span class="text-muted text-[10px]">活跃构建容器</span>
            <p class="font-mono font-medium mt-1">{{ settings.engine?.containers ?? 0 }} 个</p>
          </div>
        </div>

        <div class="pt-2 text-[11px] text-muted space-y-1 font-mono">
          <p><span class="text-muted/60">Socket 路径: </span>{{ settings.engine?.socket || settings.runner.engine_socket }}</p>
          <p><span class="text-muted/60">容器网络: </span>{{ settings.runner.network }}</p>
        </div>
      </UCard>

      <!-- 2. 生效配置摘要 (只读，绝不显示密钥) -->
      <UCard class="p-5 space-y-4">
        <div class="flex items-center justify-between border-b border-default pb-3">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-sliders-horizontal" class="size-5 text-muted" />
            <h2 class="text-sm font-semibold">服务生效配置摘要</h2>
          </div>
          <span class="text-[10px] text-muted">只读模式 · 安全脱敏</span>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4 text-xs">
          <div class="space-y-1 p-3 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">HTTP 监听地址</span>
            <p class="font-mono font-medium">{{ settings.server.listen }}</p>
          </div>

          <div class="space-y-1 p-3 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">并发构建限制 (Slots)</span>
            <p class="font-mono font-medium">{{ settings.runner.concurrency }} 个并行槽位</p>
          </div>

          <div class="space-y-1 p-3 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">产物存储目录 (Data Dir)</span>
            <p class="font-mono font-medium truncate">{{ settings.storage.data_dir }}</p>
          </div>

          <div class="space-y-1 p-3 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">日志级别与格式</span>
            <p class="font-mono font-medium uppercase">{{ settings.server.log_level }} · {{ settings.server.log_format }}</p>
          </div>

          <div class="space-y-1 p-3 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">默认发行版</span>
            <p class="font-mono font-medium">{{ settings.build.default_distro }}</p>
          </div>

          <div class="space-y-1 p-3 rounded-lg border border-default bg-elevated/40">
            <span class="text-muted text-[10px]">元数据驱动</span>
            <p class="font-mono font-medium">{{ settings.storage.driver }}</p>
          </div>
        </div>

        <div class="p-3 rounded-lg border border-default bg-elevated/40 text-xs">
          <span class="text-muted text-[10px]">CORS 允许的跨域来源</span>
          <p class="font-mono mt-1 text-muted">{{ settings.server.cors_origins.join(', ') || '未配置跨域' }}</p>
        </div>
      </UCard>

      <!-- 3. 管理会话与版本信息 -->
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <!-- 会话卡 -->
        <UCard class="p-5 flex flex-col justify-between">
          <div class="space-y-2">
            <h3 class="text-xs font-semibold text-muted flex items-center gap-1.5">
              <UIcon name="i-lucide-user-check" class="size-3.5" />
              <span>当前管理会话</span>
            </h3>
            <div class="text-sm font-bold">{{ currentUser }}</div>
            <p class="text-xs text-muted">认证方式：HttpOnly 安全 Cookie 凭证</p>
          </div>
          <div class="pt-4">
            <UButton color="error" variant="soft" size="xs" icon="i-lucide-log-out" @click="handleLogout">
              退出当前会话
            </UButton>
          </div>
        </UCard>

        <!-- 版本卡 -->
        <UCard class="p-5 space-y-2">
          <h3 class="text-xs font-semibold text-muted flex items-center gap-1.5">
            <UIcon name="i-lucide-git-commit" class="size-3.5" />
            <span>实例版本信息</span>
          </h3>
          <div class="space-y-1 text-xs font-mono">
            <p><span class="text-muted">二进制版本: </span>{{ settings.version.version }}</p>
            <p><span class="text-muted">构建时间: </span>{{ settings.version.build_time }}</p>
            <p><span class="text-muted">Go 运行时: </span>{{ settings.version.go_version }}</p>
          </div>
        </UCard>
      </div>
    </template>
  </div>
</template>
