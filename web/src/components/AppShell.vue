<script setup lang="ts">
import { ref } from 'vue'
import BTManager from './bt/BTManager.vue'
import DNSManager from './DNSManager.vue'

defineProps<{ userName: string }>()
const emit = defineEmits<{ logout: [] }>()
const module = ref<'dns' | 'bt'>('bt')
</script>

<template>
  <section class="dashboard">
    <header class="dashboard-header">
      <div>
        <p class="eyebrow">HOME GATEWAY</p>
        <h1>{{ module === 'bt' ? 'BT 下载' : 'DNS 管理' }}</h1>
        <p class="muted">你好，{{ userName }}</p>
      </div>
      <button class="secondary-button header-button" type="button" @click="emit('logout')">
        退出登录
      </button>
    </header>
    <nav class="module-tabs" aria-label="功能导航">
      <button :class="{ active: module === 'bt' }" @click="module = 'bt'">BT 下载</button>
      <button :class="{ active: module === 'dns' }" @click="module = 'dns'">DNS 管理</button>
    </nav>
    <BTManager v-if="module === 'bt'" />
    <DNSManager v-else />
  </section>
</template>
