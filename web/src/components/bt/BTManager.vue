<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../../api/client'
import AddTorrentForm from './AddTorrentForm.vue'
import BTSettingsView from './BTSettings.vue'
import TaskDetail from './TaskDetail.vue'
import TaskList from './TaskList.vue'
import type { BTFile, BTSettings, BTTask } from './types'

type Tab = 'tasks' | 'add' | 'settings'
const activeTab = ref<Tab>('tasks')
const tasks = ref<BTTask[]>([])
const settings = ref<BTSettings | null>(null)
const selectedTask = ref<BTTask | null>(null)
const selectedFiles = ref<BTFile[]>([])
const busy = ref(false)
const loading = ref(true)
const error = ref('')
const message = ref('')
const statusFilter = ref('')
const search = ref('')
let timer: number | undefined

onMounted(async () => {
  await Promise.all([loadTasks(), loadSettings()])
  loading.value = false
  timer = window.setInterval(() => {
    if (!document.hidden) void loadTasks(false)
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

async function loadTasks(showError = true) {
  try {
    const query = new URLSearchParams()
    if (statusFilter.value) query.set('status', statusFilter.value)
    if (search.value) query.set('search', search.value)
    const data = await api<{ tasks: BTTask[] }>(`/api/bt/tasks?${query}`)
    tasks.value = data.tasks || []
    if (selectedTask.value) {
      selectedTask.value =
        tasks.value.find((task) => task.id === selectedTask.value?.id) || selectedTask.value
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

async function selectTask(task: BTTask) {
  await run(async () => {
    const data = await api<{ task: BTTask; files: BTFile[] }>(`/api/bt/tasks/${task.id}`)
    selectedTask.value = data.task
    selectedFiles.value = data.files || []
  })
}

async function control(task: BTTask, action: 'pause' | 'resume') {
  await run(async () => {
    await api(`/api/bt/tasks/${task.id}/${action}`, { method: 'POST' })
    await loadTasks()
  }, action === 'pause' ? '任务已暂停。' : '任务已恢复。')
}

async function removeTask(task: BTTask) {
  if (!confirm(`确定从任务列表删除“${task.name || task.infoHash}”？`)) return
  const deleteData = confirm('是否同时删除已下载的数据？取消将保留文件。')
  await run(async () => {
    await api(`/api/bt/tasks/${task.id}?deleteData=${deleteData}`, { method: 'DELETE' })
    if (selectedTask.value?.id === task.id) selectedTask.value = null
    await loadTasks()
  }, deleteData ? '任务及下载数据已删除。' : '任务已删除，下载数据已保留。')
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
      <p v-if="loading" class="empty-state">正在加载任务…</p>
      <TaskList
        v-else
        :tasks="tasks"
        :busy="busy"
        @select="selectTask"
        @pause="control($event, 'pause')"
        @resume="control($event, 'resume')"
        @remove="removeTask"
      />
    </template>
    <AddTorrentForm v-else-if="activeTab === 'add'" @added="taskAdded" />
    <BTSettingsView v-else :settings="settings" />

    <TaskDetail
      v-if="selectedTask"
      :task="selectedTask"
      :files="selectedFiles"
      :busy="busy"
      @close="selectedTask = null"
      @save-files="saveFiles"
    />
  </section>
</template>
