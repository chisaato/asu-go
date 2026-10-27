<script setup lang="ts">
import { STAGE_CONFIG } from '~/utils/stages'
import { absoluteTime } from '~/utils/jobs'

const props = defineProps<{
  currentStage?: string
  status?: string
  stages?: { name: string, at: string }[] | null
  startedAt?: string | null
  finishedAt?: string | null
}>()

// 定义固件构建的核心流程
const steps = [
  { key: 'init', label: '初始化' },
  { key: 'container_setup', label: '准备容器' },
  { key: 'validate_manifest', altKeys: ['manifest', 'validate_revision'], label: '依赖校验' },
  { key: 'building_image', altKeys: ['build'], label: '编译固件' },
  { key: 'collect', label: '提取产物' },
  { key: 'done', label: '完成' }
]

function getStepStage(stepKey: string, altKeys: string[] = []): { name: string, at: string } | undefined {
  if (!props.stages) return undefined
  const allKeys = [stepKey, ...altKeys]
  return props.stages.find(s => allKeys.includes(s.name))
}

const currentPercent = computed(() => {
  if (props.status === 'done') return 100
  if (props.status === 'queued') return 5
  if (!props.currentStage) return 0
  return STAGE_CONFIG[props.currentStage]?.percent ?? 50
})

function getStepStatus(stepKey: string, altKeys: string[] = []): 'completed' | 'active' | 'pending' | 'failed' {
  const isFailed = props.status === 'failed' || props.status === 'timeout'
  const isDone = props.status === 'done'
  if (isDone) return 'completed'

  const allKeys = [stepKey, ...altKeys]
  const currentKey = props.currentStage || ''
  const isCurrent = allKeys.includes(currentKey)

  if (isFailed && isCurrent) return 'failed'
  if (isCurrent) return 'active'

  const currentPercentVal = currentPercent.value
  const stepTargetPercent = STAGE_CONFIG[stepKey]?.percent ?? 50
  if (currentPercentVal > stepTargetPercent) return 'completed'

  return 'pending'
}
</script>

<template>
  <div class="w-full space-y-4">
    <!-- 进度条 -->
    <div class="relative w-full">
      <UProgress :value="currentPercent" :color="status === 'failed' ? 'error' : 'primary'" class="h-2" />
    </div>

    <!-- 步骤卡片横向排列 -->
    <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-2 text-xs">
      <div
        v-for="step in steps"
        :key="step.key"
        class="rounded-lg border p-2.5 transition-colors flex flex-col justify-between"
        :class="{
          'border-primary bg-primary/5 text-primary': getStepStatus(step.key, step.altKeys) === 'active',
          'border-success/40 bg-success/5 text-default': getStepStatus(step.key, step.altKeys) === 'completed',
          'border-error bg-error/5 text-error': getStepStatus(step.key, step.altKeys) === 'failed',
          'border-default text-muted': getStepStatus(step.key, step.altKeys) === 'pending'
        }"
      >
        <div class="flex items-center gap-1.5 font-medium">
          <UIcon
            v-if="getStepStatus(step.key, step.altKeys) === 'completed'"
            name="i-lucide-check-circle"
            class="size-3.5 text-success shrink-0"
          />
          <UIcon
            v-else-if="getStepStatus(step.key, step.altKeys) === 'active'"
            name="i-lucide-loader-2"
            class="size-3.5 text-primary animate-spin shrink-0"
          />
          <UIcon
            v-else-if="getStepStatus(step.key, step.altKeys) === 'failed'"
            name="i-lucide-alert-circle"
            class="size-3.5 text-error shrink-0"
          />
          <UIcon
            v-else
            name="i-lucide-circle"
            class="size-3.5 text-muted shrink-0"
          />
          <span class="truncate">{{ step.label }}</span>
        </div>

        <div class="mt-2 text-[10px] text-muted truncate">
          <span v-if="getStepStage(step.key, step.altKeys)">
            {{ absoluteTime(getStepStage(step.key, step.altKeys)!.at) }}
          </span>
          <span v-else-if="getStepStatus(step.key, step.altKeys) === 'active'">进行中…</span>
          <span v-else>—</span>
        </div>
      </div>
    </div>
  </div>
</template>
