<script setup lang="ts">
const props = withDefaults(defineProps<{
  title?: string
  description?: string
  requestId?: string
  canRetry?: boolean
}>(), {
  title: '加载遇到问题',
  description: '无法加载当前数据，请检查网络连接后重试',
  requestId: '',
  canRetry: true
})

const emit = defineEmits<{
  retry: []
}>()
</script>

<template>
  <div class="flex flex-col items-center justify-center p-8 text-center" role="alert">
    <div class="p-3 bg-error/10 text-error rounded-full mb-3">
      <UIcon name="i-lucide-alert-triangle" class="size-8" />
    </div>
    <h3 class="text-sm font-semibold text-default">{{ props.title }}</h3>
    <p class="mt-1 text-xs text-muted max-w-md">{{ props.description }}</p>
    <p v-if="props.requestId" class="mt-1 font-mono text-[10px] text-muted">
      Request ID: {{ props.requestId }}
    </p>
    <div v-if="props.canRetry" class="mt-4">
      <UButton color="neutral" variant="outline" size="sm" icon="i-lucide-refresh-cw" @click="emit('retry')">
        重试
      </UButton>
    </div>
  </div>
</template>
