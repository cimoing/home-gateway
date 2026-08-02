<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api } from '../../api/client'
import type { StorageBackend } from '../storage/types'
import type { BTSettings, BTTask } from './types'

const props = defineProps<{ settings: BTSettings | null }>()
const emit = defineEmits<{ added: [task: BTTask] }>()
const source = ref<'magnet' | 'torrent'>('magnet')
const file = ref<File | null>(null)
const busy = ref(false)
const error = ref('')
const backends = ref<StorageBackend[]>([])
const form = reactive({
  uri: '',
  subdirectory: '',
  storageBackend: '',
  syncStrategy: 'complete',
  start: true,
})

const selectedBackend = computed(() =>
  backends.value.find((item) => item.name === form.storageBackend),
)
const needsSync = computed(
  () => !!selectedBackend.value && selectedBackend.value.type !== 'local',
)

watch(
  () => props.settings?.syncStrategy,
  (strategy) => {
    if (strategy) form.syncStrategy = strategy
  },
  { immediate: true },
)

onMounted(async () => {
  try {
    const data = await api<{ backends: StorageBackend[] }>('/api/storage/backends')
    backends.value = (data.backends || []).filter((item) => item.enabled)
    const local = backends.value.find((item) => item.type === 'local')
    form.storageBackend = local?.name || backends.value[0]?.name || ''
  } catch {
    // Storage module may be unavailable during early boot; keep form usable.
  }
})

function chooseFile(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0] || null
}

async function submit() {
  busy.value = true
  error.value = ''
  try {
    let result: { task: BTTask }
    const payload = {
      uri: form.uri,
      subdirectory: form.subdirectory,
      storageBackend: form.storageBackend,
      syncStrategy: needsSync.value ? form.syncStrategy : '',
      start: form.start,
    }
    if (source.value === 'magnet') {
      result = await api('/api/bt/tasks/magnet', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
      form.uri = ''
    } else {
      if (!file.value) throw new Error('请选择 .torrent 文件。')
      const body = new FormData()
      body.set('torrent', file.value)
      body.set('subdirectory', form.subdirectory)
      if (form.storageBackend) body.set('storageBackend', form.storageBackend)
      if (needsSync.value) body.set('syncStrategy', form.syncStrategy)
      body.set('start', String(form.start))
      result = await api('/api/bt/tasks/torrent', { method: 'POST', body })
      file.value = null
    }
    emit('added', result.task)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '添加任务失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="panel bt-add-form" @submit.prevent="submit">
    <div class="panel-heading">
      <h2>添加下载任务</h2>
      <p>支持磁力链接和最大 10 MiB 的 .torrent 文件。</p>
    </div>
    <div class="source-switch">
      <button type="button" :class="{ active: source === 'magnet' }" @click="source = 'magnet'">
        磁力链接
      </button>
      <button type="button" :class="{ active: source === 'torrent' }" @click="source = 'torrent'">
        种子文件
      </button>
    </div>
    <label v-if="source === 'magnet'">
      Magnet URI
      <textarea v-model="form.uri" rows="4" required placeholder="magnet:?xt=urn:btih:..." />
    </label>
    <label v-else>
      .torrent 文件
      <input type="file" accept=".torrent,application/x-bittorrent" required @change="chooseFile" />
    </label>
    <label>
      存储后端
      <select v-model="form.storageBackend">
        <option value="">默认下载目录</option>
        <option v-for="backend in backends" :key="backend.name" :value="backend.name">
          {{ backend.name }} ({{ backend.type }})
        </option>
      </select>
      <small>名称来自配置文件；本地直写，远程先缓存再按策略同步。</small>
    </label>
    <label v-if="needsSync">
      同步策略
      <select v-model="form.syncStrategy">
        <option value="complete">全部下载完毕后同步</option>
        <option value="per_file">逐个同步（完成一个同步一个）</option>
      </select>
      <small>默认取自全局设置；此处选择会固化到本任务。</small>
    </label>
    <label>
      目标子目录
      <input v-model="form.subdirectory" placeholder="例如 linux/isos（留空使用根目录）" />
      <small>相对于所选存储后端的路径。</small>
    </label>
    <label class="checkbox-label">
      <input v-model="form.start" type="checkbox" />
      元数据就绪后立即开始下载
    </label>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <button type="submit" :disabled="busy">{{ busy ? '正在添加…' : '添加任务' }}</button>
  </form>
</template>
