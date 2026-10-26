<script setup lang="ts">
const api = useAdminApi()
const name = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function login() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api('/login', { method: 'POST', body: { name: name.value, password: password.value }, redirectOnUnauthorized: false })
    password.value = ''
    await navigateTo('/')
  } catch (cause) {
    const status = getHttpStatus(cause)
    error.value = status === 429 ? '登录尝试过于频繁，请 5 分钟后重试。' : status === 401 ? '用户名或密码错误。' : '登录失败，请检查网络或稍后重试。'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center p-6">
    <UCard class="w-full max-w-sm">
      <template #header>
        <h1 class="text-xl font-semibold">ASU-Go 管理登录</h1>
        <p class="mt-1 text-sm text-muted">查看固件构建任务与诊断信息</p>
      </template>
      <form class="space-y-5" @submit.prevent="login">
        <UFormField label="用户名" name="name" required>
          <UInput v-model="name" name="username" autocomplete="username" required class="w-full" :disabled="busy" />
        </UFormField>
        <UFormField label="密码" name="password" required>
          <UInput v-model="password" name="password" type="password" autocomplete="current-password" required class="w-full" :disabled="busy" />
        </UFormField>
        <UAlert v-if="error" color="error" :title="error" role="alert" />
        <UButton type="submit" block :loading="busy">登录</UButton>
      </form>
    </UCard>
  </main>
</template>
