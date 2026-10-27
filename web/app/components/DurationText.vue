<script setup lang="ts">
import { duration } from '~/utils/jobs'

const props = defineProps<{
  startedAt?: string | null
  finishedAt?: string | null
  durationMs?: number
}>()

const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | null = null

const isRunning = computed(() => !!props.startedAt && !props.finishedAt && props.durationMs === undefined)

const text = computed(() => {
  return duration({
    started_at: props.startedAt ?? null,
    finished_at: props.finishedAt ?? null,
    duration_ms: props.durationMs
  }, now.value)
})

onMounted(() => {
  if (isRunning.value) {
    timer = setInterval(() => {
      now.value = Date.now()
    }, 1000)
  }
})

watch(isRunning, (running) => {
  if (running && !timer) {
    timer = setInterval(() => {
      now.value = Date.now()
    }, 1000)
  } else if (!running && timer) {
    clearInterval(timer)
    timer = null
  }
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <span class="inline-flex items-center gap-1 font-mono text-xs">
    <UIcon v-if="isRunning" name="i-lucide-timer" class="size-3 text-info animate-pulse" />
    <span>{{ text }}</span>
  </span>
</template>
