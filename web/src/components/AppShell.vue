<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import BTManager from './bt/BTManager.vue'
import DNSManager from './DNSManager.vue'
import StorageManager from './storage/StorageManager.vue'

const props = defineProps<{ userName: string }>()
const emit = defineEmits<{ logout: [] }>()

type Module = 'dns' | 'bt' | 'storage'
interface HostMetrics {
  cpuLoad?: number | null
  cpuTempC?: number | null
  cpuPressure?: number | null
  memoryPercent?: number
  memoryUsedBytes?: number
  memoryTotalBytes?: number
}

const module = ref<Module>('storage')
const btEnabled = ref(false)
const metrics = ref<HostMetrics | null>(null)
const userMenuOpen = ref(false)
const userMenuRoot = ref<HTMLElement | null>(null)
let metricsTimer: number | undefined

const metricItems = computed(() => [
  { key: 'load', label: '负载', value: formatLoad(metrics.value?.cpuLoad) },
  { key: 'temp', label: '温度', value: formatTemp(metrics.value?.cpuTempC) },
  { key: 'pressure', label: '压力', value: formatPressure(metrics.value?.cpuPressure) },
  { key: 'memory', label: '内存', value: formatMemory(metrics.value) },
])

function formatLoad(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—'
  return value.toFixed(2)
}

function formatTemp(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—'
  return `${value.toFixed(1)}°C`
}

function formatPressure(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—'
  return `${value.toFixed(1)}%`
}

function formatMemory(value: HostMetrics | null) {
  if (!value || !Number.isFinite(value.memoryPercent)) return '—'
  return `${value.memoryPercent.toFixed(1)}%`
}

async function loadFeatures() {
  try {
    const response = await fetch('/api/system/features', { credentials: 'same-origin' })
    if (!response.ok) return
    const data = (await response.json()) as { features?: { bt?: boolean } }
    btEnabled.value = Boolean(data.features?.bt)
  } catch {
    btEnabled.value = false
  }
}

async function loadMetrics() {
  try {
    const response = await fetch('/api/system/metrics', { credentials: 'same-origin' })
    if (!response.ok) return
    const data = (await response.json()) as { metrics?: HostMetrics }
    metrics.value = data.metrics || null
  } catch {
    // Keep last snapshot when a poll fails.
  }
}

function toggleUserMenu() {
  userMenuOpen.value = !userMenuOpen.value
}

function logout() {
  userMenuOpen.value = false
  emit('logout')
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!userMenuOpen.value || !userMenuRoot.value) return
  const target = event.target
  if (target instanceof Node && !userMenuRoot.value.contains(target)) {
    userMenuOpen.value = false
  }
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') userMenuOpen.value = false
}

watch(btEnabled, (enabled) => {
  if (!enabled && module.value === 'bt') {
    module.value = 'storage'
  }
})

onMounted(() => {
  void loadFeatures()
  void loadMetrics()
  metricsTimer = window.setInterval(() => {
    if (document.hidden) return
    void loadMetrics()
  }, 5000)
  document.addEventListener('pointerdown', onDocumentPointerDown)
  window.addEventListener('keydown', onWindowKeydown)
})

onBeforeUnmount(() => {
  if (metricsTimer) window.clearInterval(metricsTimer)
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<template>
  <section class="dashboard">
    <header class="dashboard-header">
      <div class="brand-row">
        <div class="brand-mark brand-mark-compact" aria-hidden="true">HG</div>
        <dl class="host-metrics" aria-label="主机状态">
          <div v-for="item in metricItems" :key="item.key">
            <dt>{{ item.label }}</dt>
            <dd>{{ item.value }}</dd>
          </div>
        </dl>
      </div>
      <div ref="userMenuRoot" class="user-menu">
        <button
          type="button"
          class="user-menu-trigger"
          :aria-expanded="userMenuOpen"
          aria-haspopup="menu"
          @click="toggleUserMenu"
        >
          {{ props.userName }}
        </button>
        <div v-if="userMenuOpen" class="user-menu-dropdown" role="menu">
          <button type="button" role="menuitem" @click="logout">退出登录</button>
        </div>
      </div>
    </header>
    <nav class="module-tabs" aria-label="功能导航">
      <button :class="{ active: module === 'storage' }" @click="module = 'storage'">存储</button>
      <button :class="{ active: module === 'dns' }" @click="module = 'dns'">DNS</button>
      <button
        v-if="btEnabled"
        :class="{ active: module === 'bt' }"
        @click="module = 'bt'"
      >
        BT
      </button>
    </nav>
    <BTManager v-if="btEnabled && module === 'bt'" />
    <StorageManager v-else-if="module === 'storage'" />
    <DNSManager v-else />
  </section>
</template>
