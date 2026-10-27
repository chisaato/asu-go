<script setup lang="ts">
import { useSources } from '~/composables/useSources'

const router = useRouter()
const { sources, fetchSources } = useSources()

// 表单状态
const distro = ref('immortalwrt')
const version = ref('24.10.6')
const target = ref('mediatek/filogic')
const profile = ref('glinet_gl-mt3000')

// 发行版选项
const distroOptions = [
  { label: 'ImmortalWrt', value: 'immortalwrt' },
  { label: 'OpenWrt 官方', value: 'openwrt' }
]

// 过滤后的版本列表：只保留 24.x 和 25.x，彻底剔除快照与 -rc，降序且每个大版本最多 3 个
const versionOptions = computed(() => {
  const currentDistro = distro.value || 'immortalwrt'
  const matched = sources.value.filter(s => s.distribution === currentDistro && s.enabled !== false)
  const list = matched.map(s => s.version).filter(v => {
    return (v.startsWith('24.') || v.startsWith('25.')) && !v.includes('SNAPSHOT') && !v.includes('-rc')
  })

  // 按大版本分组，各取最新 3 个
  const grouped: Record<string, string[]> = {}
  for (const v of list) {
    const major = v.startsWith('25.') ? '25' : '24'
    grouped[major] = grouped[major] || []
    grouped[major].push(v)
  }

  const result: string[] = []
  for (const major of ['25', '24']) {
    const arr = grouped[major] || []
    arr.sort((a, b) => b.localeCompare(a, undefined, { numeric: true }))
    result.push(...arr.slice(0, 3))
  }

  // 默认 fallback
  if (result.length === 0) {
    return currentDistro === 'immortalwrt'
      ? ['25.12.2', '25.12.1', '25.12.0', '24.10.6', '24.10.5', '24.10.4']
      : ['25.12.6', '25.12.5', '25.12.4', '24.10.8', '24.10.7', '24.10.6']
  }
  return Array.from(new Set(result))
})

// 当切换 distro 时，如果当前 version 不在列表中，自动切换为最新版
watch(distro, () => {
  if (versionOptions.value.length > 0 && !versionOptions.value.includes(version.value)) {
    version.value = versionOptions.value[0]
  }
})

// 目标架构列表：优先从当前 source 的 targets 获取，若无则给常见推荐
const targetOptions = computed(() => {
  const s = sources.value.find(item => item.distribution === distro.value && item.version === version.value)
  if (s?.targets && typeof s.targets === 'object') {
    const keys = Object.keys(s.targets)
    if (keys.length > 0) return keys.sort()
  }
  return [
    'mediatek/filogic',
    'x86/64',
    'ramips/mt7621',
    'qualcommax/ipq807x',
    'rockchip/armv8',
    'bcm27xx/bcm2711',
    'ath79/generic'
  ]
})

// 设备 Profile 列表
const profileOptions = computed(() => {
  const s = sources.value.find(item => item.distribution === distro.value && item.version === version.value)
  if (s?.targets && typeof s.targets === 'object') {
    const t = (s.targets as Record<string, unknown>)[target.value]
    if (t && typeof t === 'object' && 'profiles' in t) {
      const p = (t as { profiles: Record<string, unknown> | string[] }).profiles
      if (Array.isArray(p)) return p
      if (p && typeof p === 'object') return Object.keys(p).sort()
    }
  }
  return [
    'glinet_gl-mt3000',
    'generic',
    'xiaomi_redmi-router-ax6000',
    'tplink_archer-ax23',
    'friendlyarm_nanopi-r4s'
  ]
})

// 包配置
const packages = ref<string[]>(['luci-ssl', 'curl', 'htop'])
const defaultPackages = ref<string[]>([
  'base-files', 'busybox', 'dnsmasq', 'dropbear', 'firewall4', 'fstools',
  'libc', 'libgcc', 'nftables', 'odhcp6c', 'odhcpd-ipv6only', 'opkg', 'ppp',
  'ppp-mod-pppoe', 'procd', 'ucode', 'uci', 'urandom-seed', 'urngd'
])

// 高级配置
const showAdvanced = ref(false)
const diffPackages = ref(false)
const filesystem = ref('squashfs')
const rootfsSizeMb = ref<number | undefined>(undefined)
const defaultsScript = ref('')
const customRepos = ref('')
const repoKeys = ref('')
const repositoriesMode = ref<'extend' | 'replace'>('extend')

// 提交状态
const submitting = ref(false)
const submitError = ref('')

// 本地草稿存储 key
const DRAFT_KEY = 'asu_build_form_draft'

// 从 localStorage 加载草稿
onMounted(async () => {
  await fetchSources()
  if (typeof window !== 'undefined') {
    const saved = localStorage.getItem(DRAFT_KEY)
    if (saved) {
      try {
        const d = JSON.parse(saved)
        if (d.distro) distro.value = d.distro
        if (d.version) version.value = d.version
        if (d.target) target.value = d.target
        if (d.profile) profile.value = d.profile
        if (Array.isArray(d.packages)) packages.value = d.packages
        if (d.defaultsScript) defaultsScript.value = d.defaultsScript
      } catch {
        /* 忽略无效草稿 */
      }
    }
  }
})

// 自动持久化草稿
watch([distro, version, target, profile, packages, defaultsScript], () => {
  if (typeof window !== 'undefined') {
    const draft = {
      distro: distro.value,
      version: version.value,
      target: target.value,
      profile: profile.value,
      packages: packages.value,
      defaultsScript: defaultsScript.value
    }
    localStorage.setItem(DRAFT_KEY, JSON.stringify(draft))
  }
}, { deep: true })

// 实时生成的构建请求 JSON
const requestPayload = computed(() => {
  const req: Record<string, unknown> = {
    profile: profile.value,
    target: target.value,
    version: version.value,
    packages: packages.value
  }
  if (distro.value && distro.value !== 'openwrt') {
    req.distro = distro.value
  }
  if (diffPackages.value) {
    req.diff_packages = true
  }
  if (filesystem.value && filesystem.value !== 'squashfs') {
    req.filesystem = filesystem.value
  }
  if (rootfsSizeMb.value) {
    req.rootfs_size_mb = rootfsSizeMb.value
  }
  if (defaultsScript.value.trim()) {
    req.defaults = defaultsScript.value.trim()
  }
  if (customRepos.value.trim()) {
    req.custom_repositories = customRepos.value.split('\n').filter(Boolean)
  }
  if (repoKeys.value.trim()) {
    req.repository_keys = repoKeys.value.split('\n').filter(Boolean)
  }
  if (repositoriesMode.value !== 'extend') {
    req.repositories_mode = repositoriesMode.value
  }
  return req
})

// 从 RequestPreview 反向导入
function handleImportJson(data: Record<string, unknown>) {
  if (typeof data.distro === 'string') distro.value = data.distro
  if (typeof data.version === 'string') version.value = data.version
  if (typeof data.target === 'string') target.value = data.target
  if (typeof data.profile === 'string') profile.value = data.profile
  if (Array.isArray(data.packages)) packages.value = data.packages as string[]
  if (typeof data.defaults === 'string') defaultsScript.value = data.defaults
  if (typeof data.filesystem === 'string') filesystem.value = data.filesystem
  if (typeof data.rootfs_size_mb === 'number') rootfsSizeMb.value = data.rootfs_size_mb
}

// 提交构建
async function submitBuild() {
  submitting.value = true
  submitError.value = ''
  try {
    const url = distro.value && distro.value !== 'openwrt'
      ? `/${distro.value}/api/v1/build`
      : '/api/v1/build'

    const res = await $fetch<{ request_hash?: string, id?: string }>(url, {
      baseURL: '',
      method: 'POST',
      body: requestPayload.value
    })

    const buildId = res.request_hash || res.id
    if (buildId) {
      await router.push(`/jobs/${buildId}`)
    } else {
      await router.push('/jobs')
    }
  } catch (cause) {
    submitError.value = getApiErrorMessage(cause, '提交构建请求失败')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="space-y-6 max-w-5xl">
    <!-- 头部说明 -->
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl sm:text-2xl font-bold tracking-tight">手动固件构建器</h1>
        <p class="text-xs sm:text-sm text-muted">管理员直接在线配置构建参数并发起验证，无需借助 LuCI 或命令行</p>
      </div>
      <UButton
        color="neutral"
        variant="outline"
        size="sm"
        icon="i-lucide-rotate-ccw"
        @click="packages = ['luci-ssl', 'curl', 'htop']"
      >
        重置软件包
      </UButton>
    </div>

    <form class="space-y-6" @submit.prevent="submitBuild">
      <!-- 步骤 1: 目标平台与设备 (纯下拉与联动选择) -->
      <UCard class="p-5 sm:p-6 space-y-5">
        <div class="flex items-center gap-2.5 border-b border-default pb-3">
          <span class="flex size-6 items-center justify-center rounded-full bg-primary text-white text-xs font-bold">1</span>
          <h2 class="text-sm font-semibold tracking-wide">选择发行版与设备 Profile</h2>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <!-- 发行版下拉 -->
          <UFormField label="发行版 (Distro)">
            <USelect
              v-model="distro"
              :items="distroOptions"
              value-key="value"
              size="md"
              class="w-full font-medium"
            />
          </UFormField>

          <!-- 版本下拉 (精简至24/25，剔除快照和rc) -->
          <UFormField label="版本 (Version)">
            <USelect
              v-model="version"
              :items="versionOptions"
              size="md"
              class="w-full font-mono font-medium"
            />
          </UFormField>

          <!-- 目标架构 (支持选择与手动输入) -->
          <UFormField label="目标架构 (Target)">
            <USelectMenu
              v-model="target"
              :items="targetOptions"
              :create-item="true"
              placeholder="选择或输入 Target"
              size="md"
              class="w-full font-mono font-medium"
            />
          </UFormField>

          <!-- 设备 Profile (支持选择与手动输入) -->
          <UFormField label="设备 Profile">
            <USelectMenu
              v-model="profile"
              :items="profileOptions"
              :create-item="true"
              placeholder="选择或输入 Profile"
              size="md"
              class="w-full font-mono font-medium"
            />
          </UFormField>
        </div>
      </UCard>

      <!-- 步骤 2: 软件包定制 -->
      <UCard class="p-5 sm:p-6 space-y-5">
        <div class="flex items-center gap-2.5 border-b border-default pb-3">
          <span class="flex size-6 items-center justify-center rounded-full bg-primary text-white text-xs font-bold">2</span>
          <h2 class="text-sm font-semibold tracking-wide">软件包定制 (Packages)</h2>
        </div>

        <PackageInput
          v-model="packages"
          :default-packages="defaultPackages"
        />
      </UCard>

      <!-- 步骤 3: 高级构建选项 -->
      <UCard class="p-5 sm:p-6 space-y-5">
        <div class="flex items-center justify-between border-b border-default pb-3">
          <div class="flex items-center gap-2.5">
            <span class="flex size-6 items-center justify-center rounded-full bg-primary text-white text-xs font-bold">3</span>
            <h2 class="text-sm font-semibold tracking-wide">高级定制选项</h2>
          </div>
          <UButton
            color="neutral"
            variant="ghost"
            size="sm"
            :icon="showAdvanced ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
            @click="showAdvanced = !showAdvanced"
          >
            {{ showAdvanced ? '收起高级设置' : '展开高级设置' }}
          </UButton>
        </div>

        <div v-show="showAdvanced" class="space-y-5">
          <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
            <UFormField label="文件系统类型 (Filesystem)">
              <USelect
                v-model="filesystem"
                :items="['squashfs', 'ext4', 'initramfs']"
                size="md"
              />
            </UFormField>

            <UFormField label="RootFS 大小限制 (MB)">
              <UInput
                v-model.number="rootfsSizeMb"
                type="number"
                placeholder="例如 128 (可选)"
                size="md"
              />
            </UFormField>

            <UFormField label="软件源叠加模式">
              <USelect
                v-model="repositoriesMode"
                :items="[
                  { label: '追加模式 (extend)', value: 'extend' },
                  { label: '覆盖模式 (replace)', value: 'replace' }
                ]"
                value-key="value"
                size="md"
              />
            </UFormField>
          </div>

          <div class="flex items-center gap-3 p-3.5 rounded-xl border border-default bg-elevated/20">
            <USwitch v-model="diffPackages" />
            <div class="text-xs sm:text-sm">
              <p class="font-medium">仅记录增量包 (diff_packages)</p>
              <p class="text-xs text-muted">开启后生成的镜像将显式记录用户额外安装与剔除的包列表</p>
            </div>
          </div>

          <!-- 自定义脚本 UCI defaults -->
          <UFormField label="首次开机初始化脚本 (UCI /etc/uci-defaults)">
            <UTextarea
              v-model="defaultsScript"
              :rows="4"
              placeholder="#!/bin/sh
# 在此编写构建进固件的首次开机执行命令
uci set system.@system[0].zonename='Asia/Shanghai'
uci commit system
exit 0"
              class="font-mono text-xs sm:text-sm"
            />
          </UFormField>

          <!-- 自定义 OPKG 软件源 -->
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <UFormField label="额外自定义软件源 (每行一条 URL)">
              <UTextarea
                v-model="customRepos"
                :rows="3"
                placeholder="https://example.com/openwrt/packages"
                class="font-mono text-xs sm:text-sm"
              />
            </UFormField>

            <UFormField label="自定义软件源公钥 (每行一个 Key)">
              <UTextarea
                v-model="repoKeys"
                :rows="3"
                placeholder="25120xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
                class="font-mono text-xs sm:text-sm"
              />
            </UFormField>
          </div>
        </div>
      </UCard>

      <!-- 步骤 4: 实时 JSON 预览与导入导出 -->
      <UCard class="p-5 sm:p-6 space-y-5">
        <div class="flex items-center gap-2.5 border-b border-default pb-3">
          <span class="flex size-6 items-center justify-center rounded-full bg-primary text-white text-xs font-bold">4</span>
          <h2 class="text-sm font-semibold tracking-wide">请求报文预览 (API Payload)</h2>
        </div>

        <RequestPreview
          :payload="requestPayload"
          @import="handleImportJson"
        />
      </UCard>

      <!-- 提交与错误提示 -->
      <div class="space-y-3 pt-2">
        <div v-if="submitError" class="p-4 rounded-xl bg-error/10 border border-error/20 text-error text-sm flex items-center gap-2">
          <UIcon name="i-lucide-alert-circle" class="size-5 shrink-0" />
          <span>{{ submitError }}</span>
        </div>

        <div class="flex items-center justify-end gap-3">
          <UButton
            type="button"
            color="neutral"
            variant="outline"
            size="md"
            to="/jobs"
          >
            返回任务列表
          </UButton>
          <UButton
            type="submit"
            color="primary"
            size="md"
            icon="i-lucide-rocket"
            :loading="submitting"
          >
            立即提交构建请求
          </UButton>
        </div>
      </div>
    </form>
  </div>
</template>
