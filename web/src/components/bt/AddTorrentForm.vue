<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '../../api/client'
import type { BTSettings, BTTask } from './types'

const props = defineProps<{ settings: BTSettings | null }>()
const emit = defineEmits<{ added: [task: BTTask] }>()
const source = ref<'magnet' | 'torrent'>('magnet')
const file = ref<File | null>(null)
const busy = ref(false)
const error = ref('')
const form = reactive({
  uri: '',
  subdirectory: '',
  syncStrategy: 'complete',
  start: true,
})

const configuredBackend = computed(() => props.settings?.storageBackend || '')
const showSyncStrategy = computed(() => !!configuredBackend.value)

watch(
  () => props.settings?.syncStrategy,
  (strategy) => {
    if (strategy) form.syncStrategy = strategy
  },
  { immediate: true },
)

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
      syncStrategy: showSyncStrategy.value ? form.syncStrategy : '',
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
      if (showSyncStrategy.value) body.set('syncStrategy', form.syncStrategy)
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
    <p class="empty-state">
      存储目标由配置 <code>bt.storage_backend</code> 决定
      <template v-if="configuredBackend">（当前：{{ configuredBackend }}）</template>
      <template v-else>（当前：本地文件系统）</template>
      。
    </p>
    <label v-if="showSyncStrategy">
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
      <small>相对于配置中的下载目录。</small>
    </label>
    <label class="checkbox-label">
      <input v-model="form.start" type="checkbox" />
      元数据就绪后立即开始下载
    </label>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <button type="submit" :disabled="busy">{{ busy ? '正在添加…' : '添加任务' }}</button>
  </form>
</template>
