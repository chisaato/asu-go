<script setup lang="ts">
import { statusColor, statusIcon, statusLabel } from '~/utils/jobs'

const props = withDefaults(defineProps<{
  status: string
  size?: 'sm' | 'md' | 'lg'
}>(), {
  size: 'sm'
})
</script>

<template>
  <UBadge :color="statusColor(props.status)" variant="subtle" :size="props.size" class="inline-flex items-center gap-1 font-medium">
    <UIcon :name="statusIcon(props.status)" class="size-3.5 shrink-0" :class="{ 'animate-spin': props.status === 'running' }" />
    <span>{{ statusLabel(props.status) }}</span>
  </UBadge>
</template>
