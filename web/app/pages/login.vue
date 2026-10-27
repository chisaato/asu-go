<script setup lang="ts">
definePageMeta({
  layout: 'auth'
})

const api = useAdminApi()
const route = useRoute()
const name = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function login() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api('/login', {
      method: 'POST',
      body: { name: name.value, password: password.value },
      redirectOnUnauthorized: false
    })
    password.value = ''
    const redirect = (route.query.redirect as string) || '/'
    await navigateTo(redirect)
  } catch (cause) {
    const status = getHttpStatus(cause)
    error.value = status === 429
      ? '登录尝试过于频繁，请 5 分钟后重试。'
      : status === 401
        ? '用户名或密码错误。'
        : '登录失败，请检查网络或稍后重试。'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="w-full max-w-sm">
    <UCard class="w-full shadow-lg">
      <template #header>
        <div class="flex items-center gap-3">
          <div class="flex size-9 items-center justify-center rounded-lg bg-primary text-white font-bold text-base shadow">
            A
          </div>
          <div>
            <h1 class="text-lg font-semibold">ASU-Go 管理后台</h1>
            <p class="text-xs text-muted">OpenWrt 固件调度与任务管理</p>
          </div>
        </div>
      </template>
      <form class="space-y-4" @submit.prevent="login">
        <UFormField label="用户名" name="name" required>
          <UInput
            v-model="name"
            name="username"
            autocomplete="username"
            required
            class="w-full"
            :disabled="busy"
            placeholder="请输入管理员账号"
          />
        </UFormField>
        <UFormField label="密码" name="password" required>
          <UInput
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
            class="w-full"
            :disabled="busy"
            placeholder="请输入密码"
          />
        </UFormField>
        <UAlert v-if="error" color="error" :title="error" role="alert" />
        <UButton type="submit" block :loading="busy">
          登录控制台
        </UButton>
      </form>
    </UCard>
  </main>
</template>
