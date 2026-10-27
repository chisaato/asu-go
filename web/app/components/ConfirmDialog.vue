<script setup lang="ts">
const props = withDefaults(defineProps<{
  open: boolean
  title: string
  description?: string
  danger?: boolean
  confirmText?: string
  cancelText?: string
  items?: string[]
  loading?: boolean
}>(), {
  description: '',
  danger: true,
  confirmText: '确认执行',
  cancelText: '取消',
  items: () => [],
  loading: false
})

const emit = defineEmits<{
  'update:open': [value: boolean]
  confirm: []
  cancel: []
}>()

const isOpen = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val)
})

function handleConfirm() {
  emit('confirm')
}

function handleCancel() {
  isOpen.value = false
  emit('cancel')
}
</script>

<template>
  <UModal v-model:open="isOpen" :title="props.title" :description="props.description">
    <template #body>
      <div class="space-y-3">
        <p v-if="props.description" class="text-sm text-muted">
          {{ props.description }}
        </p>

        <div v-if="props.items && props.items.length > 0" class="rounded-md border border-default p-3 bg-muted/5 max-h-48 overflow-y-auto">
          <p class="text-xs font-medium text-muted mb-1.5">将影响以下项目：</p>
          <ul class="space-y-1 text-xs font-mono text-default">
            <li v-for="(item, index) in props.items" :key="index" class="truncate">
              • {{ item }}
            </li>
          </ul>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex justify-end gap-2 w-full">
        <UButton color="neutral" variant="ghost" :disabled="props.loading" @click="handleCancel">
          {{ props.cancelText }}
        </UButton>
        <UButton :color="props.danger ? 'error' : 'primary'" :loading="props.loading" @click="handleConfirm">
          {{ props.confirmText }}
        </UButton>
      </div>
    </template>
  </UModal>
</template>
