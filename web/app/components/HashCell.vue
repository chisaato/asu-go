<script setup lang="ts">
import { truncateHash } from '~/utils/format'

const props = withDefaults(defineProps<{
  hash: string
  len?: number
  copyable?: boolean
}>(), {
  len: 8,
  copyable: true
})

const copied = ref(false)

async function copyHash() {
  if (!props.copyable || !props.hash) return
  try {
    await navigator.clipboard.writeText(props.hash)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    /* 剪贴板不可用 */
  }
}
</script>

<template>
  <span class="inline-flex items-center gap-1 font-mono text-xs">
    <span :title="props.hash">{{ truncateHash(props.hash, props.len) }}</span>
    <button
      v-if="props.copyable && props.hash"
      type="button"
      class="text-muted hover:text-default transition-colors p-0.5 rounded cursor-pointer"
      :title="copied ? '已复制' : '复制完整 Hash'"
      @click.stop="copyHash"
    >
      <UIcon :name="copied ? 'i-lucide-check' : 'i-lucide-copy'" class="size-3.5" :class="{ 'text-success': copied }" />
    </button>
  </span>
</template>
