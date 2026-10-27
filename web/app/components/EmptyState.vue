<script setup lang="ts">
withDefaults(defineProps<{
  title?: string
  description?: string
  icon?: string
}>(), {
  title: '暂无数据',
  description: '没有找到匹配的记录',
  icon: 'i-lucide-inbox'
})
</script>

<template>
  <div class="flex flex-col items-center justify-center p-8 text-center text-muted">
    <div class="p-3 bg-muted/10 rounded-full mb-3">
      <UIcon :name="icon" class="size-8 text-muted" />
    </div>
    <h3 class="text-sm font-semibold text-default">{{ title }}</h3>
    <p class="mt-1 text-xs text-muted max-w-sm">{{ description }}</p>
    <div v-if="$slots.action" class="mt-4">
      <slot name="action" />
    </div>
  </div>
</template>
