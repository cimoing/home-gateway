<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { BTFile, BTPeer, BTTask } from './types'
import { formatBytes, formatRate, syncStatusLabel, syncStrategyLabel } from './types'

const props = defineProps<{
  task: BTTask
  files: BTFile[]
  peers: BTPeer[]
  busy: boolean
}>()
const emit = defineEmits<{
  close: []
  saveFiles: [files: Array<{ index: number; priority: number }>]
}>()
const priorities = ref<Record<number, number>>({})

watch(
  () => props.files,
  (files) => {
    priorities.value = Object.fromEntries(
      files.map((file) => [file.index, file.selected ? file.priority : 0]),
    )
  },
  { immediate: true },
)

const selectedBytes = computed(() =>
  props.files.reduce(
    (total, file) => total + (Number(priorities.value[file.index]) > 0 ? file.length : 0),
    0,
  ),
)

const VIDEO_EXTENSIONS = new Set([
  '3g2', '3gp', 'asf', 'avi', 'flv', 'm2ts', 'm4v', 'mkv', 'mov', 'mp4', 'mpeg',
  'mpg', 'mts', 'rm', 'rmvb', 'ts', 'vob', 'webm', 'wmv',
])
const LARGE_FILE_BYTES = 10 * 1024 * 1024

function fileExtension(path: string) {
  const name = path.split(/[/\\]/).pop() || path
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) return ''
  return name.slice(dot + 1).toLowerCase()
}

function isVideoFile(path: string) {
  return VIDEO_EXTENSIONS.has(fileExtension(path))
}

function selectMatching(match: (file: BTFile) => boolean) {
  priorities.value = Object.fromEntries(
    props.files.map((file) => {
      if (!match(file)) return [file.index, 0]
      const current = Number(priorities.value[file.index] || 0)
      return [file.index, current > 0 ? current : 1]
    }),
  )
}

function selectVideosOnly() {
  selectMatching((file) => isVideoFile(file.path))
}

function selectLargeFilesOnly() {
  selectMatching((file) => file.length >= LARGE_FILE_BYTES)
}

function save() {
  emit(
    'saveFiles',
    props.files.map((file) => ({
      index: file.index,
      priority: Number(priorities.value[file.index] || 0),
    })),
  )
}

function sourceLabel(source: string) {
  const labels: Record<string, string> = {
    Tr: 'Tracker',
    I: '传入',
    Hg: 'DHT',
    Ha: 'DHT 宣布',
    X: 'PEX',
    M: '磁力',
    C: '打洞',
  }
  return labels[source] || source || '—'
}
</script>

<template>
  <div class="detail-backdrop" @click.self="emit('close')">
    <section class="panel task-detail" role="dialog" aria-modal="true">
      <header>
        <div>
          <p class="eyebrow">任务详情</p>
          <h2>{{ task.name || task.infoHash }}</h2>
        </div>
        <button class="small-button secondary-button" @click="emit('close')">关闭</button>
      </header>
      <dl class="detail-grid">
        <div><dt>Info Hash</dt><dd>{{ task.infoHash }}</dd></div>
        <div><dt>保存目录</dt><dd>{{ task.saveSubdir || '下载根目录' }}</dd></div>
        <div><dt>已上传</dt><dd>{{ formatBytes(task.uploadedBytes) }}</dd></div>
        <div>
          <dt>分享率</dt>
          <dd>
            {{ task.ratio.toFixed(2) }}
            <span v-if="task.seedingPaused" class="seed-paused">已暂停做种</span>
          </dd>
        </div>
        <div v-if="task.syncStatus && task.syncStatus !== 'none'">
          <dt>同步策略</dt>
          <dd>{{ syncStrategyLabel(task.syncStrategy) }}</dd>
        </div>
        <div v-if="task.syncStatus && task.syncStatus !== 'none'">
          <dt>同步状态</dt>
          <dd>{{ syncStatusLabel(task.syncStatus) || task.syncStatus }}</dd>
        </div>
      </dl>

      <div class="file-heading">
        <div>
          <h3>Peers（{{ peers.length }}）</h3>
          <p>当前已连接的节点</p>
        </div>
      </div>
      <p v-if="!peers.length" class="empty-state">暂无已连接的 peer。</p>
      <div v-else class="record-table-wrap file-table peers-table">
        <table>
          <thead>
            <tr>
              <th>地址</th>
              <th>网络</th>
              <th>来源</th>
              <th>下载</th>
              <th>上传</th>
              <th>Peer ID</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="peer in peers" :key="`${peer.address}-${peer.peerId}`">
              <td class="content-cell">{{ peer.address || '—' }}</td>
              <td>{{ peer.network || '—' }}</td>
              <td>{{ sourceLabel(peer.source) }}</td>
              <td>{{ formatRate(peer.downloadRate) }}</td>
              <td>{{ formatRate(peer.uploadRate) }}</td>
              <td class="content-cell peer-id">{{ peer.peerId || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="file-heading">
        <div><h3>文件选择</h3><p>已选择 {{ formatBytes(selectedBytes) }}</p></div>
        <div class="file-heading-actions">
          <button
            type="button"
            class="small-button secondary-button"
            :disabled="busy || !files.length"
            @click="selectVideosOnly"
          >
            只下载视频
          </button>
          <button
            type="button"
            class="small-button secondary-button"
            :disabled="busy || !files.length"
            @click="selectLargeFilesOnly"
          >
            只下载 10MB 以上
          </button>
          <button :disabled="busy || !files.length" @click="save">保存选择</button>
        </div>
      </div>
      <p v-if="!files.length" class="empty-state">元数据尚未就绪。</p>
      <div v-else class="record-table-wrap file-table">
        <table>
          <thead>
            <tr>
              <th>文件</th>
              <th>大小</th>
              <th>进度</th>
              <th>同步</th>
              <th>优先级</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="file in files" :key="file.id">
              <td class="content-cell">{{ file.path }}</td>
              <td>{{ formatBytes(file.length) }}</td>
              <td>{{ file.length ? ((file.completedBytes / file.length) * 100).toFixed(1) : 100 }}%</td>
              <td>
                <span v-if="syncStatusLabel(file.syncStatus)">{{ syncStatusLabel(file.syncStatus) }}</span>
                <span v-else>—</span>
              </td>
              <td>
                <select v-model.number="priorities[file.index]">
                  <option :value="0">不下载</option>
                  <option :value="1">普通</option>
                  <option :value="2">高</option>
                </select>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
