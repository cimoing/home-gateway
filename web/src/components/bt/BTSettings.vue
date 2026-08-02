<script setup lang="ts">
import { ref, watch } from 'vue'
import type { BTSettings } from './types'

const props = defineProps<{ settings: BTSettings | null; busy: boolean }>()
const emit = defineEmits<{
  save: [payload: {
    downloadLimitBps: number
    uploadLimitBps: number
    seedRatioLimit: number
    syncStrategy: string
    syncConcurrency: number
  }]
}>()

const downloadLimitKib = ref(0)
const uploadLimitKib = ref(0)
const seedRatioLimit = ref(0)
const syncStrategy = ref('complete')
const syncConcurrency = ref(2)

watch(
  () => props.settings,
  (settings) => {
    if (!settings) return
    downloadLimitKib.value = Math.round((settings.downloadLimitBps || 0) / 1024)
    uploadLimitKib.value = Math.round((settings.uploadLimitBps || 0) / 1024)
    seedRatioLimit.value = settings.seedRatioLimit || 0
    syncStrategy.value = settings.syncStrategy || 'complete'
    syncConcurrency.value = settings.syncConcurrency || 2
  },
  { immediate: true },
)

function submit() {
  emit('save', {
    downloadLimitBps: Math.max(0, Math.round(downloadLimitKib.value)) * 1024,
    uploadLimitBps: Math.max(0, Math.round(uploadLimitKib.value)) * 1024,
    seedRatioLimit: Math.max(0, Number(seedRatioLimit.value) || 0),
    syncStrategy: syncStrategy.value,
    syncConcurrency: Math.min(32, Math.max(1, Math.round(syncConcurrency.value) || 1)),
  })
}
</script>

<template>
  <section class="panel settings-panel">
    <div class="panel-heading">
      <h2>BT 引擎设置</h2>
      <p>下载目录与监听端口来自 YAML；速度限制、同步策略与并发可在此修改并写回配置。</p>
    </div>
    <dl v-if="settings" class="detail-grid">
      <div><dt>引擎</dt><dd>{{ settings.running ? '运行中' : '未运行' }}</dd></div>
      <div><dt>启用</dt><dd>{{ settings.enabled ? '是' : '否' }}</dd></div>
      <div><dt>监听端口</dt><dd>{{ settings.listenPort }} / TCP + UDP</dd></div>
      <div>
        <dt>默认存储后端</dt>
        <dd>{{ settings.storageBackend || '本地文件系统' }}</dd>
      </div>
      <div>
        <dt>配置下载目录</dt>
        <dd>{{ settings.downloadDir || (settings.storageBackend ? '（后端根目录）' : '—') }}</dd>
      </div>
      <div><dt>引擎本地目录</dt><dd>{{ settings.downloadRoot }}</dd></div>
    </dl>
    <form v-if="settings" class="settings-form" @submit.prevent="submit">
      <label>
        <span>下载速度限制（KiB/s，0 表示不限）</span>
        <input v-model.number="downloadLimitKib" type="number" min="0" step="1" />
      </label>
      <label>
        <span>上传速度限制（KiB/s，0 表示不限）</span>
        <input v-model.number="uploadLimitKib" type="number" min="0" step="1" />
      </label>
      <label>
        <span>暂停做种分享率（0 表示关闭，例如 2）</span>
        <input v-model.number="seedRatioLimit" type="number" min="0" step="0.1" />
      </label>
      <label>
        <span>默认同步策略（远程存储）</span>
        <select v-model="syncStrategy">
          <option value="complete">全部下载完毕后同步</option>
          <option value="per_file">逐个同步（完成一个同步一个）</option>
        </select>
      </label>
      <label>
        <span>同时上传文件数（1–32）</span>
        <input v-model.number="syncConcurrency" type="number" min="1" max="32" step="1" />
      </label>
      <button type="submit" :disabled="busy">保存设置</button>
    </form>
    <p v-else class="empty-state">正在读取设置…</p>
  </section>
</template>
