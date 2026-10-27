<script setup lang="ts">
import { prettyJSON } from '~/utils/jobs'

const props = defineProps<{
  requestData: Record<string, unknown>
  requestHash?: string
}>()

const emit = defineEmits<{
  import: [data: Record<string, unknown>]
}>()

const copied = ref(false)
const showImportModal = ref(false)
const importText = ref('')
const importError = ref('')

const jsonText = computed(() => prettyJSON(props.requestData))

async function copyJson() {
  try {
    await navigator.clipboard.writeText(jsonText.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    /* 忽略 */
  }
}

function handleImport() {
  importError.value = ''
  try {
    const parsed = JSON.parse(importText.value)
    if (typeof parsed !== 'object' || parsed === null) {
      importError.value = '无效的 JSON 对象'
      return
    }
    emit('import', parsed as Record<string, unknown>)
    showImportModal.value = false
    importText.value = ''
  } catch {
    importError.value = 'JSON 格式解析失败，请检查语法'
  }
}
</script>

<template>
  <div class="space-y-3">
    <!-- 头部工具栏 -->
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-2">
        <span class="text-xs font-semibold text-muted">请求 JSON 预览</span>
        <HashCell v-if="props.requestHash" :hash="props.requestHash" />
      </div>
      <div class="flex items-center gap-1">
        <UButton color="neutral" variant="ghost" size="xs" icon="i-lucide-file-input" @click="showImportModal = true">
          导入 JSON
        </UButton>
        <UButton
          color="neutral"
          variant="ghost"
          size="xs"
          :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
          @click="copyJson"
        >
          {{ copied ? '已复制' : '复制' }}
        </UButton>
      </div>
    </div>

    <!-- JSON 代码展示框 -->
    <pre class="overflow-x-auto rounded-lg border border-default bg-elevated p-3 text-xs font-mono max-h-60 leading-relaxed">{{ jsonText }}</pre>

    <!-- 导入 JSON 模态窗 -->
    <UModal v-model:open="showImportModal" title="导入构建请求 JSON" description="粘贴完整的构建请求 JSON，将自动填充表单配置">
      <template #body>
        <div class="space-y-3">
          <UTextarea
            v-model="importText"
            placeholder="粘贴 JSON 请求体..."
            :rows="8"
            class="font-mono text-xs w-full"
          />
          <UAlert v-if="importError" color="error" :title="importError" />
        </div>
      </template>
      <template #footer>
        <div class="flex justify-end gap-2 w-full">
          <UButton color="neutral" variant="ghost" @click="showImportModal = false">取消</UButton>
          <UButton color="primary" @click="handleImport">确认导入</UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
