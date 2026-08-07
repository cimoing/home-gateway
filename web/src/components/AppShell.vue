<script setup lang="ts">
import { ref } from 'vue'
import DNSManager from './DNSManager.vue'
import StorageManager from './storage/StorageManager.vue'

defineProps<{ userName: string }>()
const emit = defineEmits<{ logout: [] }>()
const module = ref<'dns' | 'storage'>('storage')

const titles: Record<'dns' | 'storage', string> = {
  dns: 'DNS 管理',
  storage: '存储管理',
}
</script>

<template>
  <section class="dashboard">
    <header class="dashboard-header">
      <div>
        <p class="eyebrow">HOME GATEWAY</p>
        <h1>{{ titles[module] }}</h1>
        <p class="muted">你好，{{ userName }}</p>
      </div>
      <button class="secondary-button header-button" type="button" @click="emit('logout')">
        退出登录
      </button>
    </header>
    <nav class="module-tabs" aria-label="功能导航">
      <button :class="{ active: module === 'storage' }" @click="module = 'storage'">存储管理</button>
      <button :class="{ active: module === 'dns' }" @click="module = 'dns'">DNS 管理</button>
    </nav>
    <StorageManager v-if="module === 'storage'" />
    <DNSManager v-else />
  </section>
</template>
