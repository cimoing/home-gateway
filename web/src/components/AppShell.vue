<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import BTManager from './bt/BTManager.vue'
import DNSManager from './DNSManager.vue'
import StorageManager from './storage/StorageManager.vue'

defineProps<{ userName: string }>()
const emit = defineEmits<{ logout: [] }>()

type Module = 'dns' | 'bt' | 'storage'
const module = ref<Module>('storage')
const btEnabled = ref(false)

const titles: Record<Module, string> = {
  bt: 'BT 下载',
  dns: 'DNS 管理',
  storage: '存储管理',
}

async function loadFeatures() {
  try {
    const response = await fetch('/api/system/features')
    if (!response.ok) return
    const data = (await response.json()) as { features?: { bt?: boolean } }
    btEnabled.value = Boolean(data.features?.bt)
  } catch {
    btEnabled.value = false
  }
}

watch(btEnabled, (enabled) => {
  if (!enabled && module.value === 'bt') {
    module.value = 'storage'
  }
})

onMounted(() => {
  void loadFeatures()
})
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
      <button
        v-if="btEnabled"
        :class="{ active: module === 'bt' }"
        @click="module = 'bt'"
      >
        BT 下载
      </button>
      <button :class="{ active: module === 'storage' }" @click="module = 'storage'">存储管理</button>
      <button :class="{ active: module === 'dns' }" @click="module = 'dns'">DNS 管理</button>
    </nav>
    <BTManager v-if="btEnabled && module === 'bt'" />
    <StorageManager v-else-if="module === 'storage'" />
    <DNSManager v-else />
  </section>
</template>
