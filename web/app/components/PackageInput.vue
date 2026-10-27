<script setup lang="ts">
const props = withDefaults(defineProps<{
  modelValue: string[]
  defaultPackages?: string[]
  availablePackages?: string[]
}>(), {
  defaultPackages: () => [],
  availablePackages: () => []
})

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()

const inputVal = ref('')
const showDropdown = ref(false)

const customPackages = computed(() => {
  return props.modelValue.filter(p => !p.startsWith('-'))
})

const excludedPackages = computed(() => {
  return props.modelValue.filter(p => p.startsWith('-')).map(p => p.slice(1))
})

const suggestions = computed(() => {
  const query = inputVal.value.trim().toLowerCase()
  if (!query) return []
  return props.availablePackages
    .filter(p => p.toLowerCase().includes(query) && !props.modelValue.includes(p))
    .slice(0, 10)
})

function addPackage(pkgName: string) {
  const name = pkgName.trim()
  if (!name) return
  if (!props.modelValue.includes(name)) {
    emit('update:modelValue', [...props.modelValue, name])
  }
  inputVal.value = ''
  showDropdown.value = false
}

function removePackage(name: string) {
  emit('update:modelValue', props.modelValue.filter(p => p !== name && p !== `-${name}`))
}

function toggleExcludeDefault(pkg: string) {
  const isExcluded = excludedPackages.value.includes(pkg)
  if (isExcluded) {
    // 恢复包含：从 modelValue 中移除 -pkg
    emit('update:modelValue', props.modelValue.filter(p => p !== `-${pkg}`))
  } else {
    // 排除：添加 -pkg
    emit('update:modelValue', [...props.modelValue, `-${pkg}`])
  }
}

function handleKeyDown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    if (suggestions.value.length > 0 && showDropdown.value) {
      addPackage(suggestions.value[0])
    } else {
      addPackage(inputVal.value)
    }
  }
}
</script>

<template>
  <div class="space-y-3">
    <!-- 输入框与自动补全 -->
    <div class="relative">
      <div class="flex items-center gap-2">
        <UInput
          v-model="inputVal"
          placeholder="输入软件包名（回车添加，支持 -pkg 排除）..."
          icon="i-lucide-box"
          class="flex-1"
          @focus="showDropdown = true"
          @keydown="handleKeyDown"
        />
        <UButton color="primary" variant="soft" size="sm" @click="addPackage(inputVal)">
          添加
        </UButton>
      </div>

      <!-- 自动补全下拉 -->
      <div
        v-if="showDropdown && suggestions.length > 0"
        class="absolute left-0 right-0 top-full mt-1 z-20 max-h-48 overflow-y-auto rounded-lg border border-default bg-elevated shadow-lg p-1 text-xs"
      >
        <button
          v-for="s in suggestions"
          :key="s"
          type="button"
          class="w-full text-left px-2.5 py-1.5 rounded hover:bg-muted/10 transition-colors cursor-pointer flex items-center justify-between"
          @click="addPackage(s)"
        >
          <span class="font-mono">{{ s }}</span>
          <UIcon name="i-lucide-plus" class="size-3 text-muted" />
        </button>
      </div>
    </div>

    <!-- 已添加的自定义包 -->
    <div v-if="customPackages.length > 0" class="space-y-1.5">
      <p class="text-xs font-medium text-muted">用户新增软件包：</p>
      <div class="flex flex-wrap gap-1.5">
        <UBadge
          v-for="pkg in customPackages"
          :key="pkg"
          color="primary"
          variant="subtle"
          class="flex items-center gap-1 font-mono text-xs pr-1"
        >
          <span>{{ pkg }}</span>
          <button type="button" class="cursor-pointer hover:text-error" @click="removePackage(pkg)">
            <UIcon name="i-lucide-x" class="size-3" />
          </button>
        </UBadge>
      </div>
    </div>

    <!-- 设备默认包列表（可点击剔除） -->
    <div v-if="defaultPackages.length > 0" class="space-y-1.5">
      <div class="flex items-center justify-between">
        <p class="text-xs font-medium text-muted">Profile 预设软件包（点击标签可剔除）：</p>
        <span class="text-[10px] text-muted">已剔除 {{ excludedPackages.length }} 个</span>
      </div>
      <div class="flex flex-wrap gap-1.5 max-h-36 overflow-y-auto p-2 rounded-lg border border-default/50 bg-muted/5">
        <button
          v-for="pkg in defaultPackages"
          :key="pkg"
          type="button"
          class="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-mono transition-all cursor-pointer border"
          :class="[
            excludedPackages.includes(pkg)
              ? 'border-error/40 bg-error/10 text-error line-through'
              : 'border-default bg-elevated hover:border-primary/50 text-default'
          ]"
          :title="excludedPackages.includes(pkg) ? '点击恢复此包' : '点击剔除此包 (-' + pkg + ')'"
          @click="toggleExcludeDefault(pkg)"
        >
          <UIcon :name="excludedPackages.includes(pkg) ? 'i-lucide-minus-circle' : 'i-lucide-check'" class="size-3" />
          <span>{{ pkg }}</span>
        </button>
      </div>
    </div>
  </div>
</template>
