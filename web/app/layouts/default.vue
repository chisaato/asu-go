<script setup lang="ts">
const route = useRoute()
const api = useAdminApi()
const colorMode = useColorMode()

// 侧栏桌面折叠状态与移动端抽屉状态
const isCollapsed = ref(false)
const mobileDrawerOpen = ref(false)
const currentUser = ref('admin')

// 页面导航项
const navItems = [
  { label: '仪表盘', to: '/', icon: 'i-lucide-layout-dashboard' },
  { label: '任务历史', to: '/jobs', icon: 'i-lucide-list-todo' },
  { label: '存储管理', to: '/storage', icon: 'i-lucide-hard-drive' },
  { label: '手动构建', to: '/build', icon: 'i-lucide-hammer' },
  { label: '来源目录', to: '/sources', icon: 'i-lucide-git-branch' },
  { label: '系统设置', to: '/settings', icon: 'i-lucide-settings' }
]

// 全局 SSE 连接状态
const { status: sseStatus } = useEventStream()

async function fetchMe() {
  try {
    const me = await api<{ name: string }>('/me', { redirectOnUnauthorized: false })
    if (me?.name) currentUser.value = me.name
  } catch {
    /* 忽略 */
  }
}

async function logout() {
  try {
    await api('/logout', { method: 'POST' })
    await navigateTo('/login', { replace: true })
  } catch {
    await navigateTo('/login', { replace: true })
  }
}

function toggleColorMode() {
  colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'
}

// 路由切换时自动关闭移动端侧边栏
watch(() => route.path, () => {
  mobileDrawerOpen.value = false
})

onMounted(() => {
  fetchMe()
})
</script>

<template>
  <div class="flex h-dvh w-full overflow-hidden bg-background text-default text-sm">
    <!-- 桌面端侧边栏 (md 及以上) -->
    <aside
      class="hidden md:flex flex-col border-r border-default bg-elevated/40 transition-all duration-200 select-none shrink-0"
      :class="isCollapsed ? 'w-20' : 'w-64'"
    >
      <!-- Logo 与产品名 -->
      <div class="flex h-16 items-center justify-between px-4 border-b border-default">
        <NuxtLink to="/" class="flex items-center gap-3 overflow-hidden">
          <div class="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary text-white font-bold text-base shadow-sm">
            A
          </div>
          <div v-if="!isCollapsed" class="truncate font-bold text-base tracking-tight">
            <span>ASU-Go</span>
            <span class="ml-1.5 text-xs text-muted font-normal">Admin</span>
          </div>
        </NuxtLink>
        <button
          type="button"
          class="p-1.5 rounded-lg text-muted hover:text-default hover:bg-muted/10 transition-colors cursor-pointer"
          :title="isCollapsed ? '展开侧栏' : '收起侧栏'"
          @click="isCollapsed = !isCollapsed"
        >
          <UIcon :name="isCollapsed ? 'i-lucide-panel-left-open' : 'i-lucide-panel-left-close'" class="size-5" />
        </button>
      </div>

      <!-- 导航链接 -->
      <nav class="flex-1 space-y-1.5 p-3 overflow-y-auto">
        <NuxtLink
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="flex items-center gap-3.5 px-3.5 py-2.5 rounded-xl text-sm font-medium transition-colors"
          :class="[
            route.path === item.to || (item.to !== '/' && route.path.startsWith(item.to))
              ? 'bg-primary/10 text-primary font-semibold'
              : 'text-muted hover:bg-muted/10 hover:text-default'
          ]"
          :title="isCollapsed ? item.label : undefined"
        >
          <UIcon :name="item.icon" class="size-5 shrink-0" />
          <span v-if="!isCollapsed" class="truncate">{{ item.label }}</span>
        </NuxtLink>
      </nav>

      <!-- 底部用户信息与操作 -->
      <div class="p-3 border-t border-default space-y-2">
        <!-- 主题切换 -->
        <button
          type="button"
          class="flex w-full items-center gap-3 px-3 py-2 rounded-lg text-xs text-muted hover:text-default hover:bg-muted/10 transition-colors cursor-pointer"
          :title="colorMode.value === 'dark' ? '切换浅色模式' : '切换深色模式'"
          @click="toggleColorMode"
        >
          <UIcon :name="colorMode.value === 'dark' ? 'i-lucide-moon' : 'i-lucide-sun'" class="size-4 shrink-0" />
          <span v-if="!isCollapsed" class="truncate">
            {{ colorMode.value === 'dark' ? '深色模式' : '浅色模式' }}
          </span>
        </button>

        <!-- 当前用户与登出 -->
        <div class="flex items-center justify-between px-3 py-2 rounded-xl bg-muted/5">
          <div class="flex items-center gap-2.5 overflow-hidden">
            <UIcon name="i-lucide-user" class="size-4 shrink-0 text-muted" />
            <span v-if="!isCollapsed" class="text-xs truncate font-medium">{{ currentUser }}</span>
          </div>
          <button
            v-if="!isCollapsed"
            type="button"
            class="text-muted hover:text-error transition-colors p-1 rounded-md cursor-pointer"
            title="退出登录"
            @click="logout"
          >
            <UIcon name="i-lucide-log-out" class="size-4" />
          </button>
        </div>
      </div>
    </aside>

    <!-- 移动端侧边抽屉遮罩与面板 (屏幕小于 md 时生效) -->
    <div
      v-if="mobileDrawerOpen"
      class="fixed inset-0 z-50 md:hidden bg-black/60 backdrop-blur-sm transition-opacity"
      @click="mobileDrawerOpen = false"
    />
    <aside
      class="fixed inset-y-0 left-0 z-50 w-72 flex md:hidden flex-col bg-background border-r border-default transform transition-transform duration-200 select-none shadow-2xl"
      :class="mobileDrawerOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <div class="flex h-16 items-center justify-between px-4 border-b border-default">
        <NuxtLink to="/" class="flex items-center gap-3" @click="mobileDrawerOpen = false">
          <div class="flex size-9 items-center justify-center rounded-xl bg-primary text-white font-bold text-base shadow">
            A
          </div>
          <div class="font-bold text-base tracking-tight">
            <span>ASU-Go</span>
            <span class="ml-1.5 text-xs text-muted font-normal">Admin</span>
          </div>
        </NuxtLink>
        <button
          type="button"
          class="p-2 rounded-lg text-muted hover:text-default"
          @click="mobileDrawerOpen = false"
        >
          <UIcon name="i-lucide-x" class="size-5" />
        </button>
      </div>

      <nav class="flex-1 space-y-2 p-4 overflow-y-auto">
        <NuxtLink
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="flex items-center gap-3.5 px-4 py-3 rounded-xl text-sm font-medium transition-colors"
          :class="[
            route.path === item.to || (item.to !== '/' && route.path.startsWith(item.to))
              ? 'bg-primary/10 text-primary font-semibold'
              : 'text-muted hover:bg-muted/10 hover:text-default'
          ]"
          @click="mobileDrawerOpen = false"
        >
          <UIcon :name="item.icon" class="size-5 shrink-0" />
          <span>{{ item.label }}</span>
        </NuxtLink>
      </nav>

      <div class="p-4 border-t border-default space-y-3">
        <button
          type="button"
          class="flex w-full items-center gap-3 px-3.5 py-2.5 rounded-xl text-sm text-muted hover:bg-muted/10"
          @click="toggleColorMode"
        >
          <UIcon :name="colorMode.value === 'dark' ? 'i-lucide-moon' : 'i-lucide-sun'" class="size-4" />
          <span>{{ colorMode.value === 'dark' ? '深色模式' : '浅色模式' }}</span>
        </button>
        <div class="flex items-center justify-between px-3.5 py-2.5 rounded-xl bg-muted/5">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-user" class="size-4 text-muted" />
            <span class="text-sm font-medium">{{ currentUser }}</span>
          </div>
          <button
            type="button"
            class="text-muted hover:text-error p-1 rounded-md"
            title="退出登录"
            @click="logout"
          >
            <UIcon name="i-lucide-log-out" class="size-4" />
          </button>
        </div>
      </div>
    </aside>

    <!-- 主展示区 -->
    <div class="flex flex-1 flex-col overflow-hidden min-w-0">
      <!-- 顶栏 -->
      <header class="flex h-16 items-center justify-between px-4 sm:px-6 border-b border-default bg-elevated/20 shrink-0">
        <!-- 移动端汉堡菜单 + 面包屑 -->
        <div class="flex items-center gap-3">
          <button
            type="button"
            class="md:hidden p-2 -ml-2 rounded-lg text-muted hover:text-default hover:bg-muted/10"
            title="打开菜单"
            @click="mobileDrawerOpen = true"
          >
            <UIcon name="i-lucide-menu" class="size-5" />
          </button>
          <div class="flex items-center gap-2 text-xs sm:text-sm">
            <span class="text-muted hidden sm:inline">OpenWrt Attended Sysupgrade</span>
            <span class="text-muted/40 hidden sm:inline">/</span>
            <span class="font-semibold text-default">
              {{ navItems.find(i => i.to === route.path || (i.to !== '/' && route.path.startsWith(i.to)))?.label ?? '控制台' }}
            </span>
          </div>
        </div>

        <!-- 顶栏右侧徽标 -->
        <div class="flex items-center gap-2.5 sm:gap-3">
          <!-- 连接状态指示灯 -->
          <div class="flex items-center gap-1.5 px-2.5 py-1 rounded-full border border-default/70 text-xs">
            <span
              class="size-2 rounded-full"
              :class="{
                'bg-success animate-pulse': sseStatus === 'connected',
                'bg-warning': sseStatus === 'connecting',
                'bg-muted': sseStatus === 'disconnected'
              }"
            />
            <span class="text-muted text-[11px] sm:text-xs">
              {{ sseStatus === 'connected' ? '实时就绪' : sseStatus === 'connecting' ? '连接中…' : '离线' }}
            </span>
          </div>

          <!-- 版本标识 -->
          <span class="text-xs font-mono text-muted bg-muted/10 px-2 py-0.5 rounded">v1.0.0</span>
        </div>
      </header>

      <!-- 页面内容插槽 -->
      <main class="flex-1 overflow-y-auto p-4 sm:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>
