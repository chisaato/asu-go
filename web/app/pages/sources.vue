<script setup lang="ts">
import { type Source, useSources } from '~/composables/useSources'
import { relativeTime, absoluteTime } from '~/utils/jobs'

const {
  sources,
  loading,
  error,
  fetchSources,
  createSource,
  updateSource,
  deleteSource,
  importSources
} = useSources()

// 表单抽屉状态
const drawerOpen = ref(false)
const isEditing = ref(false)
const currentId = ref<number | null>(null)
const formBusy = ref(false)
const formError = ref('')

// 表单字段
const formDistribution = ref('')
const formBranch = ref('')
const formVersion = ref('')
const formUpstreamUrl = ref('')
const formImage = ref('')
const formTagTemplate = ref('{target}-{version_tag}')
const formPackageManager = ref('opkg')
const formRolling = ref(false)
const formEnabled = ref(true)

// 保留词列表
const RESERVED_WORDS = ['admin', 'api', 'json', 'store', 'healthz']

const distroError = computed(() => {
  const d = formDistribution.value.trim().toLowerCase()
  if (RESERVED_WORDS.includes(d)) {
    return `"${d}" 是系统保留路径前缀，不能作为发行版标识`
  }
  return ''
})

const tagTemplateError = computed(() => {
  const t = formTagTemplate.value
  const validPlaceholders = ['{target}', '{version}', '{version_tag}']
  const matches = t.match(/\{[^}]+\}/g) || []
  for (const m of matches) {
    if (!validPlaceholders.includes(m)) {
      return `未知占位符 "${m}"，仅支持 {target}、{version}、{version_tag}`
    }
  }
  return ''
})

function openCreateDrawer() {
  isEditing.value = false
  currentId.value = null
  formDistribution.value = ''
  formBranch.value = ''
  formVersion.value = ''
  formUpstreamUrl.value = 'https://downloads.openwrt.org'
  formImage.value = 'ghcr.io/openwrt/imagebuilder'
  formTagTemplate.value = '{target}-{version_tag}'
  formPackageManager.value = 'opkg'
  formRolling.value = false
  formEnabled.value = true
  formError.value = ''
  drawerOpen.value = true
}

function openEditDrawer(item: Source) {
  isEditing.value = true
  currentId.value = item.id ?? null
  formDistribution.value = item.distribution
  formBranch.value = item.branch
  formVersion.value = item.version
  formUpstreamUrl.value = item.upstream_url
  formImage.value = item.image
  formTagTemplate.value = item.tag_template
  formPackageManager.value = item.package_manager || 'opkg'
  formRolling.value = item.rolling
  formEnabled.value = item.enabled
  formError.value = ''
  drawerOpen.value = true
}

async function saveSource() {
  if (distroError.value || tagTemplateError.value) return
  formBusy.value = true
  formError.value = ''
  try {
    const payload = {
      distribution: formDistribution.value.trim(),
      branch: formBranch.value.trim(),
      version: formVersion.value.trim(),
      upstream_url: formUpstreamUrl.value.trim(),
      image: formImage.value.trim(),
      tag_template: formTagTemplate.value.trim(),
      package_manager: formPackageManager.value,
      rolling: formRolling.value,
      enabled: formEnabled.value
    }

    if (isEditing.value && currentId.value) {
      await updateSource(currentId.value, payload)
    } else {
      await createSource(payload)
    }
    drawerOpen.value = false
    await fetchSources()
  } catch (cause) {
    formError.value = getApiErrorMessage(cause, '保存来源配置失败')
  } finally {
    formBusy.value = false
  }
}

// 快速切换启用状态
async function toggleEnabled(item: Source) {
  if (!item.id) return
  try {
    await updateSource(item.id, { enabled: !item.enabled })
    item.enabled = !item.enabled
  } catch {
    /* 忽略 */
  }
}

// 删除来源
const deleteDialogOpen = ref(false)
const deletingId = ref<number | null>(null)
const deletingBusy = ref(false)

function confirmDelete(id: number) {
  deletingId.value = id
  deleteDialogOpen.value = true
}

async function executeDelete() {
  if (!deletingId.value) return
  deletingBusy.value = true
  try {
    await deleteSource(deletingId.value)
    deleteDialogOpen.value = false
    deletingId.value = null
    await fetchSources()
  } finally {
    deletingBusy.value = false
  }
}

// 批量导入
const importModalOpen = ref(false)
const importRaw = ref('')
const importBusy = ref(false)
const importError = ref('')

async function executeImport() {
  importError.value = ''
  importBusy.value = true
  try {
    const parsed = JSON.parse(importRaw.value)
    if (!Array.isArray(parsed)) {
      importError.value = '必须是包含 Source 对象的 JSON 数组'
      return
    }
    await importSources(parsed)
    importModalOpen.value = false
    importRaw.value = ''
    await fetchSources()
  } catch {
    importError.value = 'JSON 解析失败，请确认语法'
  } finally {
    importBusy.value = false
  }
}

onMounted(() => {
  void fetchSources()
})
</script>

<template>
  <div class="space-y-6">
    <!-- 头部 -->
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h1 class="text-xl sm:text-2xl font-bold tracking-tight">发行版与来源目录 (Sources)</h1>
        <p class="text-xs sm:text-sm text-muted">配置多发行版支持（OpenWrt / ImmortalWrt 等）、镜像模板与上游同步</p>
      </div>

      <div class="flex items-center gap-2">
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-refresh-cw"
          :loading="loading"
          @click="fetchSources"
        >
          刷新
        </UButton>
        <UButton
          color="neutral"
          variant="outline"
          size="sm"
          icon="i-lucide-file-code"
          @click="importModalOpen = true"
        >
          批量导入
        </UButton>
        <UButton
          color="primary"
          size="sm"
          icon="i-lucide-plus"
          @click="openCreateDrawer"
        >
          添加新来源
        </UButton>
      </div>
    </div>

    <!-- 错误态 -->
    <ErrorState
      v-if="error"
      title="无法加载来源目录"
      :description="error"
      @retry="fetchSources"
    />

    <!-- 表格 -->
    <div v-else class="rounded-xl border border-default overflow-x-auto bg-elevated/20">
      <table class="w-full text-xs sm:text-sm text-left min-w-[720px]">
        <thead class="bg-muted/10 border-b border-default text-muted font-medium">
          <tr>
            <th class="p-3">启用</th>
            <th class="p-3">发行版</th>
            <th class="p-3">版本 / 分支</th>
            <th class="p-3">滚动版本</th>
            <th class="p-3">镜像模板</th>
            <th class="p-3">上游 URL</th>
            <th class="p-3">最近同步</th>
            <th class="p-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default">
          <tr v-for="item in sources" :key="item.id" class="hover:bg-muted/5 transition-colors">
            <!-- 启用开关 -->
            <td class="p-3">
              <USwitch
                :model-value="item.enabled"
                @update:model-value="toggleEnabled(item)"
              />
            </td>

            <!-- 发行版 -->
            <td class="p-3 font-semibold text-default">
              {{ item.distribution }}
              <span v-if="item.origin" class="ml-1 text-[10px] text-muted">({{ item.origin }})</span>
            </td>

            <!-- 版本与分支 -->
            <td class="p-3 font-mono">
              {{ item.version }}
              <span v-if="item.branch" class="text-muted text-[10px]">/ {{ item.branch }}</span>
            </td>

            <!-- 滚动更新 -->
            <td class="p-3">
              <UBadge :color="item.rolling ? 'info' : 'neutral'" variant="subtle" size="xs">
                {{ item.rolling ? '滚动构建' : '固定发布' }}
              </UBadge>
            </td>

            <!-- 镜像模板 -->
            <td class="p-3 font-mono text-[11px] text-muted truncate max-w-xs" :title="`${item.image}:${item.tag_template}`">
              {{ item.image }}:{{ item.tag_template }}
            </td>

            <!-- 上游 URL -->
            <td class="p-3 text-muted truncate max-w-xs font-mono text-[10px]">
              {{ item.upstream_url }}
            </td>

            <!-- 最近同步 -->
            <td class="p-3 text-muted" :title="absoluteTime(item.synced_at || null)">
              {{ relativeTime(item.synced_at || null) }}
            </td>

            <!-- 操作 -->
            <td class="p-3 text-right space-x-1 whitespace-nowrap">
              <UButton
                color="neutral"
                variant="ghost"
                size="xs"
                icon="i-lucide-edit-3"
                @click="openEditDrawer(item)"
              >
                编辑
              </UButton>
              <UButton
                v-if="item.id"
                color="error"
                variant="ghost"
                size="xs"
                icon="i-lucide-trash-2"
                @click="confirmDelete(item.id)"
              >
                删除
              </UButton>
            </td>
          </tr>

          <!-- 空态 -->
          <tr v-if="sources.length === 0">
            <td colspan="8" class="p-12 text-center">
              <EmptyState title="来源目录为空" description="尚未配置任何 OpenWrt 发行版来源，点击上方添加新来源" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 编辑/添加抽屉 -->
    <USlideover
      v-model:open="drawerOpen"
      :title="isEditing ? '编辑来源配置' : '添加新来源目录'"
      description="配置生效后将直接影响新提交的固件构建请求"
      :ui="{ content: 'sm:max-w-xl' }"
    >
      <template #body>
        <form class="space-y-4" @submit.prevent="saveSource">
          <UFormField label="发行版标识 (Distribution)" required :error="distroError">
            <UInput v-model="formDistribution" placeholder="例如 openwrt 或 immortalwrt" required />
            <template #help>
              <span class="text-[10px] text-muted">不能使用 admin、api、json、store 等系统保留词</span>
            </template>
          </UFormField>

          <div class="grid grid-cols-2 gap-3">
            <UFormField label="版本 (Version)" required>
              <UInput v-model="formVersion" placeholder="24.10.0" required />
            </UFormField>
            <UFormField label="分支 (Branch)">
              <UInput v-model="formBranch" placeholder="openwrt-24.10" />
            </UFormField>
          </div>

          <UFormField label="上游下载基础 URL (Upstream URL)" required>
            <UInput v-model="formUpstreamUrl" placeholder="https://downloads.openwrt.org" required />
          </UFormField>

          <UFormField label="ImageBuilder 基础镜像 (Image)" required>
            <UInput v-model="formImage" placeholder="ghcr.io/openwrt/imagebuilder" required />
          </UFormField>

          <UFormField label="镜像 Tag 模板 (Tag Template)" required :error="tagTemplateError">
            <UInput v-model="formTagTemplate" placeholder="{target}-{version_tag}" required />
            <template #help>
              <span class="text-[10px] text-muted">仅允许占位符：{target}、{version}、{version_tag}</span>
            </template>
          </UFormField>

          <div class="grid grid-cols-2 gap-3">
            <UFormField label="包管理器">
              <UInput v-model="formPackageManager" placeholder="opkg 或 apk" />
            </UFormField>
            <div class="flex items-center pt-6">
              <USwitch v-model="formRolling" label="滚动构建 (Rolling)" />
            </div>
          </div>

          <div class="pt-2">
            <USwitch v-model="formEnabled" label="默认启用此来源" />
          </div>

          <UAlert v-if="formError" color="error" :title="formError" />

          <div class="flex justify-end gap-2 pt-4">
            <UButton color="neutral" variant="ghost" :disabled="formBusy" @click="drawerOpen = false">
              取消
            </UButton>
            <UButton type="submit" color="primary" :loading="formBusy" :disabled="!!distroError || !!tagTemplateError">
              保存配置
            </UButton>
          </div>
        </form>
      </template>
    </USlideover>

    <!-- 批量导入模态框 -->
    <UModal v-model:open="importModalOpen" title="批量导入来源目录" description="粘贴 JSON 格式的来源对象列表，整批原子同步">
      <template #body>
        <div class="space-y-3">
          <UTextarea
            v-model="importRaw"
            placeholder="[ { &quot;distribution&quot;: &quot;openwrt&quot;, &quot;version&quot;: &quot;24.10.0&quot;, ... } ]"
            :rows="10"
            class="font-mono text-xs w-full"
          />
          <UAlert v-if="importError" color="error" :title="importError" />
        </div>
      </template>
      <template #footer>
        <div class="flex justify-end gap-2 w-full">
          <UButton color="neutral" variant="ghost" @click="importModalOpen = false">取消</UButton>
          <UButton color="primary" :loading="importBusy" @click="executeImport">确认导入</UButton>
        </div>
      </template>
    </UModal>

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:open="deleteDialogOpen"
      title="删除来源目录"
      description="确定要删除此发行版来源吗？已关联历史任务的 source_id 将自动置空，新构建将无法选择此来源。"
      :loading="deletingBusy"
      @confirm="executeDelete"
      @cancel="deleteDialogOpen = false"
    />
  </div>
</template>
