<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import type { BTSettings } from './types'

const props = defineProps<{ settings: BTSettings | null; busy: boolean }>()
const emit = defineEmits<{
  save: [payload: {
    downloadLimitBps: number
    uploadLimitBps: number
    seedRatioLimit: number
  }]
  refresh: []
}>()

const downloadLimitKib = ref(0)
const uploadLimitKib = ref(0)
const seedRatioLimit = ref(0)

watch(
  () => props.settings,
  (settings) => {
    if (!settings) return
    downloadLimitKib.value = Math.round((settings.downloadLimitBps || 0) / 1024)
    uploadLimitKib.value = Math.round((settings.uploadLimitBps || 0) / 1024)
    seedRatioLimit.value = settings.seedRatioLimit || 0
  },
  { immediate: true },
)

onMounted(() => {
  emit('refresh')
})

function submit() {
  emit('save', {
    downloadLimitBps: Math.max(0, Math.round(downloadLimitKib.value)) * 1024,
    uploadLimitBps: Math.max(0, Math.round(uploadLimitKib.value)) * 1024,
    seedRatioLimit: Math.max(0, Number(seedRatioLimit.value) || 0),
  })
}
</script>

<template>
  <section class="panel settings-panel">
    <div class="panel-heading">
      <h2>BT 引擎设置</h2>
      <p>全部从 Transmission 远程会话读取；保存只写远端，网关重启不会覆盖远程设置。</p>
    </div>
    <dl v-if="settings" class="detail-grid">
      <div>
        <dt>状态</dt>
        <dd>
          {{
            settings.running
              ? '运行中'
              : settings.enabled
                ? '连接中…'
                : '未运行'
          }}
        </dd>
      </div>
      <div><dt>引擎</dt><dd>{{ settings.engine || 'transmission' }}</dd></div>
      <div><dt>启用</dt><dd>{{ settings.enabled ? '是' : '否' }}</dd></div>
      <div><dt>Peer 端口</dt><dd>{{ settings.listenPort }}（远程 daemon）</dd></div>
      <div><dt>下载目录</dt><dd>{{ settings.downloadDir || '—' }}</dd></div>
      <div><dt>引擎根目录</dt><dd>{{ settings.downloadRoot }}</dd></div>
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
      <button type="submit" :disabled="busy">保存设置</button>
    </form>
    <p v-else class="empty-state">正在读取设置…</p>
  </section>
</template>
