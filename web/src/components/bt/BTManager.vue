<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../../api/client'
import AddTorrentForm from './AddTorrentForm.vue'
import BTSettingsView from './BTSettings.vue'
import TaskDetail from './TaskDetail.vue'
import TaskList from './TaskList.vue'
import type { BTFile, BTPeer, BTSettings, BTStatus, BTTask } from './types'
import { formatRate } from './types'

type Tab = 'tasks' | 'add' | 'settings'
const activeTab = ref<Tab>('tasks')
const tasks = ref<BTTask[]>([])
const settings = ref<BTSettings | null>(null)
const status = ref<BTStatus | null>(null)
const selectedTask = ref<BTTask | null>(null)
const selectedFiles = ref<BTFile[]>([])
const selectedPeers = ref<BTPeer[]>([])
const selectedIds = ref<number[]>([])
const busy = ref(false)
const loading = ref(true)
const error = ref('')
const message = ref('')
const statusFilter = ref('')
const search = ref('')
let timer: number | undefined
/** Bumped on close/switch so in-flight detail fetches cannot reopen the dialog. */
let selectionEpoch = 0
/** Suppresses click-through onto task cards after the detail overlay closes. */
let ignoreSelectUntil = 0

const selectedTasks = computed(() =>
  tasks.value.filter((task) => selectedIds.value.includes(task.id)),
)
const canPauseSelected = computed(() =>
  selectedTasks.value.some((task) => task.desiredState !== 'paused'),
)
const canResumeSelected = computed(() =>
  selectedTasks.value.some((task) => task.desiredState === 'paused'),
)

onMounted(async () => {
  await Promise.all([loadTasks(), loadSettings(), loadStatus()])
  loading.value = false
  timer = window.setInterval(() => {
    if (document.hidden) return
    void loadTasks(false)
    void loadStatus()
    if (selectedTask.value) {
      const epoch = selectionEpoch
      void refreshSelected(false, selectedTask.value.id, epoch)
    }
  }, 2000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

async function run(action: () => Promise<void>, success = '') {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await action()
    message.value = success
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function closeDetail() {
  selectionEpoch += 1
  selectedTask.value = null
  selectedFiles.value = []
  selectedPeers.value = []
  ignoreSelectUntil = Date.now() + 400
}

async function loadTasks(showError = true) {
  try {
    const query = new URLSearchParams()
    if (statusFilter.value) query.set('status', statusFilter.value)
    if (search.value) query.set('search', search.value)
    const data = await api<{ tasks: BTTask[] }>(`/api/bt/tasks?${query}`)
    tasks.value = data.tasks || []
    if (selectedTask.value) {
      const currentId = selectedTask.value.id
      selectedTask.value =
        tasks.value.find((task) => task.id === currentId) || selectedTask.value
    }
  } catch (reason) {
    if (showError) error.value = reason instanceof Error ? reason.message : '加载任务失败'
  }
}

async function loadSettings() {
  try {
    const data = await api<{ settings: BTSettings }>('/api/bt/settings')
    settings.value = data.settings
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载设置失败'
  }
}

async function loadStatus() {
  try {
    const data = await api<{ status: BTStatus }>('/api/bt/status')
    status.value = data.status
  } catch {
    // Keep the last known status when a background poll fails.
  }
}

async function openTask(task: BTTask) {
  if (Date.now() < ignoreSelectUntil) return
  const epoch = (selectionEpoch += 1)
  await run(async () => {
    await refreshSelected(true, task.id, epoch)
  })
}

async function refreshSelected(
  showError = true,
  taskId = selectedTask.value?.id,
  epoch = selectionEpoch,
) {
  if (!taskId) return
  try {
    const data = await api<{ task: BTTask; files: BTFile[]; peers: BTPeer[] }>(
      `/api/bt/tasks/${taskId}`,
    )
    if (epoch !== selectionEpoch) return
    selectedTask.value = data.task
    selectedFiles.value = data.files || []
    selectedPeers.value = data.peers || []
  } catch (reason) {
    if (epoch !== selectionEpoch) return
    if (showError) error.value = reason instanceof Error ? reason.message : '加载任务详情失败'
  }
}

async function control(task: BTTask, action: 'pause' | 'resume') {
  await run(async () => {
    await api(`/api/bt/tasks/${task.id}/${action}`, { method: 'POST' })
    await loadTasks()
    if (selectedTask.value?.id === task.id) await refreshSelected(false)
  }, action === 'pause' ? '任务已暂停。' : '任务已恢复。')
}

async function controlSelected(action: 'pause' | 'resume') {
  const targets = selectedTasks.value.filter((task) =>
    action === 'pause' ? task.desiredState !== 'paused' : task.desiredState === 'paused',
  )
  if (!targets.length) return
  await run(async () => {
    for (const task of targets) {
      await api(`/api/bt/tasks/${task.id}/${action}`, { method: 'POST' })
    }
    await loadTasks()
    if (selectedTask.value) await refreshSelected(false)
  }, action === 'pause' ? `已暂停 ${targets.length} 个任务。` : `已恢复 ${targets.length} 个任务。`)
}

async function removeTask(task: BTTask, deleteData: boolean) {
  const label = deleteData ? '移除数据及任务' : '删除任务'
  if (!confirm(`确定对“${task.name || task.infoHash}”执行「${label}」？`)) return
  await run(async () => {
    await api(`/api/bt/tasks/${task.id}?deleteData=${deleteData}`, { method: 'DELETE' })
    selectedIds.value = selectedIds.value.filter((id) => id !== task.id)
    if (selectedTask.value?.id === task.id) closeDetail()
    await loadTasks()
  }, deleteData ? '任务及下载数据已删除。' : '任务已删除，下载数据已保留。')
}

async function removeSelected(deleteData: boolean) {
  const targets = [...selectedTasks.value]
  if (!targets.length) return
  const label = deleteData ? '移除数据及任务' : '删除任务'
  if (!confirm(`确定对选中的 ${targets.length} 个任务执行「${label}」？`)) return
  await run(async () => {
    for (const task of targets) {
      await api(`/api/bt/tasks/${task.id}?deleteData=${deleteData}`, { method: 'DELETE' })
      if (selectedTask.value?.id === task.id) closeDetail()
    }
    selectedIds.value = []
    await loadTasks()
  }, `已处理 ${targets.length} 个任务。`)
}

async function copyMagnet(task: BTTask) {
  await run(async () => {
    const data = await api<{ magnetLink: string }>(`/api/bt/tasks/${task.id}/magnet`)
    if (!data.magnetLink) throw new Error('磁力链接不可用')
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(data.magnetLink)
    } else {
      window.prompt('复制磁力链接', data.magnetLink)
    }
  }, '磁力链接已复制。')
}

async function downloadTorrent(task: BTTask) {
  await run(async () => {
    const response = await fetch(`/api/bt/tasks/${task.id}/torrent`, { credentials: 'same-origin' })
    if (!response.ok) {
      const data = (await response.json().catch(() => ({}))) as { error?: string }
      throw new Error(data.error || `下载失败（${response.status}）`)
    }
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    const disposition = response.headers.get('Content-Disposition') || ''
    const matched = /filename="([^"]+)"/.exec(disposition)
    anchor.href = url
    anchor.download = matched?.[1] || `${task.name || task.infoHash}.magnet`
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    URL.revokeObjectURL(url)
  }, '种子/磁力文件已开始下载。')
}

async function saveFiles(files: Array<{ index: number; priority: number }>) {
  if (!selectedTask.value) return
  await run(async () => {
    const data = await api<{ files: BTFile[] }>(
      `/api/bt/tasks/${selectedTask.value?.id}/files`,
      { method: 'PUT', body: JSON.stringify({ files }) },
    )
    selectedFiles.value = data.files
  }, '文件选择已保存。')
}

async function blockPeer(payload: { type: string; value: string; label: string }) {
  if (!confirm(`${payload.label}？规则将写入配置文件并立即生效。`)) return
  await run(async () => {
    await api('/api/bt/block', {
      method: 'POST',
      body: JSON.stringify({ type: payload.type, value: payload.value }),
    })
    await refreshSelected(false)
  }, '屏蔽规则已添加。')
}

async function saveSettings(payload: {
  downloadLimitBps: number
  uploadLimitBps: number
  seedRatioLimit: number
}) {
  await run(async () => {
    const data = await api<{ settings: BTSettings }>('/api/bt/settings', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
    settings.value = data.settings
  }, 'BT 设置已保存。')
}

function taskAdded(task: BTTask) {
  message.value = '任务已添加，正在获取元数据。'
  activeTab.value = 'tasks'
  tasks.value.unshift(task)
  void loadTasks()
}
</script>

<template>
  <section class="feature-view">
    <nav class="tabs" aria-label="BT 下载导航">
      <button :class="{ active: activeTab === 'tasks' }" @click="activeTab = 'tasks'">任务</button>
      <button :class="{ active: activeTab === 'add' }" @click="activeTab = 'add'">添加</button>
      <button :class="{ active: activeTab === 'settings' }" @click="activeTab = 'settings'">
        设置
      </button>
    </nav>
    <p v-if="error" class="notice error-message" role="alert">{{ error }}</p>
    <p v-if="message" class="notice success-message">{{ message }}</p>

    <template v-if="activeTab === 'tasks'">
      <div class="bt-toolbar">
        <input
          v-model="search"
          type="search"
          placeholder="搜索任务名称"
          @input="loadTasks(false)"
        />
        <select v-model="statusFilter" @change="loadTasks()">
          <option value="">全部状态</option>
          <option value="metadata">获取元数据</option>
          <option value="downloading">下载中</option>
          <option value="paused">已暂停</option>
          <option value="completed">已完成</option>
          <option value="error">错误</option>
        </select>
        <button class="secondary-button small-button" @click="loadTasks()">刷新</button>
      </div>
      <div v-if="selectedIds.length" class="bt-bulk-actions">
        <span>已选 {{ selectedIds.length }} 项</span>
        <button
          type="button"
          class="small-button secondary-button"
          :disabled="busy || !canPauseSelected"
          @click="controlSelected('pause')"
        >
          暂停
        </button>
        <button
          type="button"
          class="small-button"
          :disabled="busy || !canResumeSelected"
          @click="controlSelected('resume')"
        >
          恢复
        </button>
        <button
          type="button"
          class="small-button danger-button"
          :disabled="busy"
          @click="removeSelected(false)"
        >
          删除任务
        </button>
        <button
          type="button"
          class="small-button danger-button"
          :disabled="busy"
          @click="removeSelected(true)"
        >
          移除数据及任务
        </button>
      </div>
      <p v-if="loading" class="empty-state">正在加载任务…</p>
      <TaskList
        v-else
        v-model:selected-ids="selectedIds"
        :tasks="tasks"
        :busy="busy"
        @open="openTask"
        @pause="control($event, 'pause')"
        @remove="removeTask"
        @copy-magnet="copyMagnet"
        @download-torrent="downloadTorrent"
      />
    </template>
    <AddTorrentForm
      v-else-if="activeTab === 'add'"
      :settings="settings"
      @added="taskAdded"
    />
    <BTSettingsView
      v-else
      :settings="settings"
      :busy="busy"
      @save="saveSettings"
    />

    <TaskDetail
      v-if="selectedTask"
      :task="selectedTask"
      :files="selectedFiles"
      :peers="selectedPeers"
      :busy="busy"
      @close="closeDetail"
      @save-files="saveFiles"
      @block-peer="blockPeer"
    />

    <aside class="bt-status-bar" aria-live="polite">
      <span>DHT {{ status?.dhtGoodNodes ?? 0 }}/{{ status?.dhtNodes ?? 0 }}</span>
      <span>↓ {{ formatRate(status?.downloadRate ?? 0) }}</span>
      <span>↑ {{ formatRate(status?.uploadRate ?? 0) }}</span>
    </aside>
  </section>
</template>
