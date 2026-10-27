<script setup lang="ts">
import type { Job } from '~/utils/jobs'
import { absoluteTime, prettyJSON, statusLabel } from '~/utils/jobs'

const props = defineProps<{ id: string | null }>()
const emit = defineEmits<{ close: [] }>()
const api = useAdminApi()
const open = computed({ get: () => !!props.id, set: (value) => { if (!value) emit('close') } })
const job = ref<Job | null>(null)
const loading = ref(false)
const error = ref('')
watch(() => props.id, async (id, _previous, onCleanup) => {
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  job.value = null
  error.value = ''
  loading.value = !!id
  if (!id) return
  try {
    const result = await api<Job>(`/jobs/${encodeURIComponent(id)}`, { signal: controller.signal })
    if (!controller.signal.aborted) job.value = result
  } catch (cause) {
    if (!controller.signal.aborted) error.value = getHttpStatus(cause) === 404 ? '任务不存在或已清理。' : '无法加载任务详情，请关闭后重试。'
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
})
</script>

<template>
  <USlideover v-model:open="open" title="任务详情" description="只读诊断信息，不包含实时日志" :ui="{ content: 'sm:max-w-2xl' }">
    <template #body>
      <p v-if="loading" role="status">正在加载…</p>
      <UAlert v-else-if="error" color="error" :title="error" role="alert" />
      <div v-else-if="job" class="space-y-6">
        <div>
          <p class="break-all font-mono text-sm">{{ job.id }}</p>
          <p class="mt-2 text-muted">{{ statusLabel(job.status) }} · {{ job.stage || '尚未开始' }}</p>
        </div>
        <section>
          <h2 class="mb-3 font-semibold">阶段时间线</h2>
          <ol class="space-y-3 border-l border-default pl-4 text-sm">
            <li>入队 · {{ absoluteTime(job.enqueued_at) }}</li>
            <li v-if="job.started_at">开始 · {{ absoluteTime(job.started_at) }}</li>
            <li v-for="(stage, index) in job.stages ?? []" :key="index">{{ stage.name }} · {{ absoluteTime(stage.at) }}</li>
            <li v-if="job.finished_at">结束 · {{ absoluteTime(job.finished_at) }}</li>
          </ol>
        </section>
        <section v-for="field in (['request', 'result', 'error'] as const)" :key="field">
          <h2 class="mb-2 font-semibold">{{ { request: '请求', result: '结果', error: '错误' }[field] }}</h2>
          <pre class="overflow-auto rounded-lg bg-elevated p-4 text-xs">{{ prettyJSON(job[field]) }}</pre>
        </section>
      </div>
    </template>
  </USlideover>
</template>
