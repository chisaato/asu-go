<script setup lang="ts">
definePageMeta({
  layout: 'auth'
})

interface AuthConfigResponse {
  oidc_enabled?: boolean
  oidc_button_text?: string
  oidc_login_url?: string
}

const api = useAdminApi()
const route = useRoute()
const name = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

const oidcEnabled = ref(false)
const oidcButtonText = ref('使用 SSO 登录')
const oidcLoginUrl = ref('/admin/api/auth/oidc/login')

onMounted(async () => {
  try {
    const cfg = await api<AuthConfigResponse>('/auth/config', {
      redirectOnUnauthorized: false
    })
    if (cfg?.oidc_enabled) {
      oidcEnabled.value = true
      if (cfg.oidc_button_text) {
        oidcButtonText.value = cfg.oidc_button_text
      }
      if (cfg.oidc_login_url) {
        oidcLoginUrl.value = cfg.oidc_login_url
      }
    }
  } catch {
    // 探测失败时降级为仅显示本地账号密码登录
  }
})

function loginWithOIDC() {
  if (typeof window !== 'undefined') {
    window.location.href = oidcLoginUrl.value
  }
}

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

      <div v-if="oidcEnabled" class="mt-4 pt-2">
        <div class="relative my-4 flex items-center justify-center">
          <div class="border-border absolute inset-0 flex items-center">
            <div class="w-full border-t border-muted/30" />
          </div>
          <span class="bg-background relative px-2 text-xs text-muted">或外部身份</span>
        </div>
        <UButton
          block
          color="neutral"
          variant="outline"
          icon="i-lucide-shield-check"
          @click="loginWithOIDC"
        >
          {{ oidcButtonText }}
        </UButton>
      </div>
    </UCard>
  </main>
</template>
