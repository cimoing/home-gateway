<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import DNSManager from './components/DNSManager.vue'

interface User {
  id: number
  username: string
  displayName: string
}

const checkingSession = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const user = ref<User | null>(null)
const form = reactive({
  username: '',
  password: '',
})

onMounted(async () => {
  try {
    const response = await fetch('/api/auth/session')
    if (response.ok) {
      const data: { user: User } = await response.json()
      user.value = data.user
    }
  } finally {
    checkingSession.value = false
  }
})

async function login() {
  errorMessage.value = ''
  submitting.value = true
  try {
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    })
    if (!response.ok) {
      errorMessage.value =
        response.status === 429 ? '尝试次数过多，请稍后再试。' : '用户名或密码错误。'
      return
    }

    const data: { user: User } = await response.json()
    user.value = data.user
    form.password = ''
  } catch {
    errorMessage.value = '暂时无法连接服务，请稍后重试。'
  } finally {
    submitting.value = false
  }
}

async function logout() {
  await fetch('/api/auth/logout', { method: 'POST' })
  user.value = null
  form.password = ''
}
</script>

<template>
  <main>
    <section v-if="checkingSession" class="card compact-card" aria-live="polite">
      <div class="status">
        <span class="indicator" />
        正在检查登录状态…
      </div>
    </section>

    <section v-else-if="!user" class="card login-card">
      <div class="brand-mark">HG</div>
      <p class="eyebrow">HOME GATEWAY</p>
      <h1>欢迎回来</h1>
      <p class="description">登录后管理你的家庭网关。</p>

      <form @submit.prevent="login">
        <label for="username">用户名</label>
        <input
          id="username"
          v-model="form.username"
          name="username"
          type="text"
          autocomplete="username"
          maxlength="64"
          required
          autofocus
        />

        <label for="password">密码</label>
        <input
          id="password"
          v-model="form.password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
        />

        <p v-if="errorMessage" class="error-message" role="alert">{{ errorMessage }}</p>
        <button type="submit" :disabled="submitting">
          {{ submitting ? '正在登录…' : '登录' }}
        </button>
      </form>
    </section>

    <DNSManager
      v-else
      :user-name="user.displayName || user.username"
      @logout="logout"
      @session-expired="user = null"
    />
  </main>
</template>
