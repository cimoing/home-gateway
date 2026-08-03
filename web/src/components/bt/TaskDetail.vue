<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import FileSelectionTree from './FileSelectionTree.vue'
import type { BTFile, BTPeer, BTTask } from './types'
import {
  formatBytes,
  formatRate,
  formatSyncProgress,
  syncStatusLabel,
  syncStrategyLabel,
} from './types'

export type PeerBlockType = 'ip' | 'client' | 'port' | 'peerId'
type DetailTab = 'info' | 'peers' | 'files'

const props = defineProps<{
  task: BTTask
  files: BTFile[]
  peers: BTPeer[]
  busy: boolean
}>()
const emit = defineEmits<{
  close: []
  saveFiles: [files: Array<{ index: number; priority: number }>]
  blockPeer: [payload: { type: PeerBlockType; value: string; label: string }]
}>()

const activeTab = ref<DetailTab>('info')
const priorities = ref<Record<number, number>>({})
const menu = ref<{
  x: number
  y: number
  items: Array<{ type: PeerBlockType; value: string; label: string }>
} | null>(null)

watch(
  () => props.files,
  (files) => {
    priorities.value = Object.fromEntries(
      files.map((file) => [file.index, file.selected ? file.priority : 0]),
    )
  },
  { immediate: true },
)

watch(
  () => props.task.id,
  () => {
    activeTab.value = 'info'
    menu.value = null
  },
)

const selectedBytes = computed(() =>
  props.files.reduce(
    (total, file) => total + (Number(priorities.value[file.index]) > 0 ? file.length : 0),
    0,
  ),
)

const selectedCount = computed(
  () => props.files.filter((file) => Number(priorities.value[file.index]) > 0).length,
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

function selectAll() {
  priorities.value = Object.fromEntries(
    props.files.map((file) => {
      const current = Number(priorities.value[file.index] || 0)
      return [file.index, current > 0 ? current : 1]
    }),
  )
}

function selectNone() {
  priorities.value = Object.fromEntries(props.files.map((file) => [file.index, 0]))
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

function peerHost(address: string) {
  const value = address.trim()
  if (!value) return ''
  if (value.startsWith('[')) {
    const end = value.indexOf(']')
    if (end > 1) return value.slice(1, end)
  }
  const colon = value.lastIndexOf(':')
  if (colon > 0 && value.includes('.') && !value.includes('::')) {
    return value.slice(0, colon)
  }
  if (colon > 0) return value.slice(0, colon)
  return value
}

function peerPort(address: string) {
  const value = address.trim()
  if (!value) return ''
  if (value.startsWith('[')) {
    const end = value.indexOf(']:')
    if (end >= 0) return value.slice(end + 2)
    return ''
  }
  const colon = value.lastIndexOf(':')
  if (colon <= 0) return ''
  return value.slice(colon + 1)
}

function openPeerMenu(event: MouseEvent, peer: BTPeer) {
  event.preventDefault()
  const items: Array<{ type: PeerBlockType; value: string; label: string }> = []
  const host = peerHost(peer.address || '')
  const port = peerPort(peer.address || '')
  if (host) items.push({ type: 'ip', value: host, label: `屏蔽此 IP（${host}）` })
  if (peer.client) {
    items.push({
      type: 'client',
      value: peer.client,
      label: `屏蔽此客户端（${peer.client}）`,
    })
  }
  if (port) items.push({ type: 'port', value: port, label: `屏蔽此端口（${port}）` })
  if (peer.peerId) {
    items.push({
      type: 'peerId',
      value: peer.peerId,
      label: `屏蔽此 Peer ID（${peer.peerId}）`,
    })
  }
  if (!items.length) {
    menu.value = null
    return
  }
  menu.value = { x: event.clientX, y: event.clientY, items }
}

function closeMenu() {
  menu.value = null
}

function chooseBlock(item: { type: PeerBlockType; value: string; label: string }) {
  menu.value = null
  emit('blockPeer', item)
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') closeMenu()
}

onMounted(() => {
  window.addEventListener('click', closeMenu)
  window.addEventListener('keydown', onWindowKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('click', closeMenu)
  window.removeEventListener('keydown', onWindowKeydown)
})
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

      <nav class="tabs detail-tabs" aria-label="任务详情分类">
        <button :class="{ active: activeTab === 'info' }" @click="activeTab = 'info'">
          基本信息
        </button>
        <button :class="{ active: activeTab === 'peers' }" @click="activeTab = 'peers'">
          Peers（{{ peers.length }}）
        </button>
        <button :class="{ active: activeTab === 'files' }" @click="activeTab = 'files'">
          文件（{{ selectedCount }}/{{ files.length }}）
        </button>
      </nav>

      <div v-if="activeTab === 'info'" class="detail-tab-panel">
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
            <dd>
              {{ syncStatusLabel(task.syncStatus) || task.syncStatus }}
              <span
                v-if="formatSyncProgress(task.syncedBytes, task.syncTotalBytes)"
                class="sync-progress"
              >
                {{ formatSyncProgress(task.syncedBytes, task.syncTotalBytes) }}
              </span>
            </dd>
          </div>
          <div v-if="task.syncError">
            <dt>同步错误</dt>
            <dd class="error-message">{{ task.syncError }}</dd>
          </div>
          <div>
            <dt>Peers</dt>
            <dd>{{ peers.length }}</dd>
          </div>
          <div>
            <dt>文件</dt>
            <dd>{{ files.length }}（已选 {{ selectedCount }}）</dd>
          </div>
        </dl>
      </div>

      <div v-else-if="activeTab === 'peers'" class="detail-tab-panel">
        <div class="file-heading">
          <div>
            <h3>已连接 Peers</h3>
            <p>右键节点可屏蔽 IP / 客户端 / 端口 / Peer ID</p>
          </div>
        </div>
        <p v-if="!peers.length" class="empty-state">暂无已连接的 peer。</p>
        <div v-else class="record-table-wrap file-table peers-table">
          <table>
            <thead>
              <tr>
                <th>地址</th>
                <th>客户端</th>
                <th>版本</th>
                <th>网络</th>
                <th>来源</th>
                <th>下载</th>
                <th>上传</th>
                <th>Peer ID</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="peer in peers"
                :key="`${peer.address}-${peer.peerId}`"
                class="peer-row"
                @contextmenu="openPeerMenu($event, peer)"
              >
                <td class="content-cell">{{ peer.address || '—' }}</td>
                <td>{{ peer.client || '—' }}</td>
                <td>{{ peer.clientVersion || '—' }}</td>
                <td>{{ peer.network || '—' }}</td>
                <td>{{ sourceLabel(peer.source) }}</td>
                <td>{{ formatRate(peer.downloadRate) }}</td>
                <td>{{ formatRate(peer.uploadRate) }}</td>
                <td class="content-cell peer-id">{{ peer.peerId || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-else class="detail-tab-panel">
        <div class="file-heading">
          <div>
            <h3>文件选择</h3>
            <p>已选择 {{ selectedCount }} 个文件，共 {{ formatBytes(selectedBytes) }}</p>
          </div>
          <div class="file-heading-actions">
            <button
              type="button"
              class="small-button secondary-button"
              :disabled="busy || !files.length"
              @click="selectAll"
            >
              全选
            </button>
            <button
              type="button"
              class="small-button secondary-button"
              :disabled="busy || !files.length"
              @click="selectNone"
            >
              全不选
            </button>
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
        <FileSelectionTree
          v-else
          :files="files"
          :priorities="priorities"
          :busy="busy"
          @update:priorities="priorities = $event"
        />
      </div>
    </section>

    <div
      v-if="menu"
      class="peer-context-menu"
      :style="{ left: `${menu.x}px`, top: `${menu.y}px` }"
      role="menu"
      @click.stop
    >
      <button
        v-for="item in menu.items"
        :key="`${item.type}:${item.value}`"
        type="button"
        role="menuitem"
        :disabled="busy"
        @click="chooseBlock(item)"
      >
        {{ item.label }}
      </button>
    </div>
  </div>
</template>
