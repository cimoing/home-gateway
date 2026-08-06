<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../../api/client'
import type { StorageBackend, StorageEntry, SyncJob, SyncCompareRow } from './types'
import { formatBytes, syncJobLabel, compareStatusLabel } from './types'

const props = defineProps<{ backends: StorageBackend[] }>()

const leftBackend = ref('')
const rightBackend = ref('')
const leftPath = ref('')
const rightPath = ref('')
const leftEntries = ref<StorageEntry[]>([])
const rightEntries = ref<StorageEntry[]>([])
const selectedLeft = ref<Set<string>>(new Set())
const selectedRight = ref<Set<string>>(new Set())
const overwrite = ref(true)
const busy = ref(false)
const error = ref('')
const message = ref('')
const job = ref<SyncJob | null>(null)
let pollTimer: number | undefined

const enabledBackends = computed(() => props.backends.filter((item) => item.enabled))

const compareRows = computed((): SyncCompareRow[] => {
  const leftMap = new Map(leftEntries.value.map((entry) => [entry.name, entry]))
  const rightMap = new Map(rightEntries.value.map((entry) => [entry.name, entry]))
  const names = new Set([...leftMap.keys(), ...rightMap.keys()])
  const rows: SyncCompareRow[] = []
  for (const name of [...names].sort((a, b) => a.localeCompare(b))) {
    const left = leftMap.get(name)
    const right = rightMap.get(name)
    let status: SyncCompareRow['status'] = 'both'
    if (left && !right) status = 'left_only'
    else if (!left && right) status = 'right_only'
    else if (left && right) {
      if (left.isDir !== right.isDir) status = 'different'
      else if (!left.isDir && (left.size !== right.size)) status = 'different'
      else status = 'same'
    }
    rows.push({ name, left, right, status })
  }
  return rows
})

const leftCrumbs = computed(() => (leftPath.value ? leftPath.value.split('/').filter(Boolean) : []))
const rightCrumbs = computed(() => (rightPath.value ? rightPath.value.split('/').filter(Boolean) : []))

watch(
  () => enabledBackends.value,
  (items) => {
    if (!leftBackend.value && items[0]) leftBackend.value = items[0].name
    if (!rightBackend.value && items[1]) rightBackend.value = items[1].name
    else if (!rightBackend.value && items[0]) rightBackend.value = items[0].name
  },
  { immediate: true },
)

watch([leftBackend, leftPath], () => {
  if (leftBackend.value) void loadSide('left')
})
watch([rightBackend, rightPath], () => {
  if (rightBackend.value) void loadSide('right')
})

onBeforeUnmount(() => {
  if (pollTimer) window.clearInterval(pollTimer)
})

async function loadSide(side: 'left' | 'right') {
  const backend = side === 'left' ? leftBackend.value : rightBackend.value
  const path = side === 'left' ? leftPath.value : rightPath.value
  if (!backend) return
  busy.value = true
  error.value = ''
  try {
    const query = new URLSearchParams()
    if (path) query.set('path', path)
    const data = await api<{ entries: StorageEntry[] }>(
      `/api/storage/backends/${encodeURIComponent(backend)}/entries?${query}`,
    )
    if (side === 'left') {
      leftEntries.value = data.entries || []
      selectedLeft.value = new Set()
    } else {
      rightEntries.value = data.entries || []
      selectedRight.value = new Set()
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载目录失败'
  } finally {
    busy.value = false
  }
}

async function refreshBoth() {
  await Promise.all([loadSide('left'), loadSide('right')])
}

function openLeft(entry: StorageEntry) {
  if (entry.isDir) leftPath.value = entry.path
}

function openRight(entry: StorageEntry) {
  if (entry.isDir) rightPath.value = entry.path
}

function goLeftCrumb(index: number) {
  leftPath.value = leftCrumbs.value.slice(0, index + 1).join('/')
}

function goRightCrumb(index: number) {
  rightPath.value = rightCrumbs.value.slice(0, index + 1).join('/')
}

function toggleLeft(name: string) {
  const next = new Set(selectedLeft.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  selectedLeft.value = next
}

function toggleRight(name: string) {
  const next = new Set(selectedRight.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  selectedRight.value = next
}

function joinPath(base: string, name: string) {
  return base ? `${base}/${name}` : name
}

async function copySelection(direction: 'ltr' | 'rtl') {
  const sourceBackend = direction === 'ltr' ? leftBackend.value : rightBackend.value
  const destBackend = direction === 'ltr' ? rightBackend.value : leftBackend.value
  const sourceBase = direction === 'ltr' ? leftPath.value : rightPath.value
  const destBase = direction === 'ltr' ? rightPath.value : leftPath.value
  const selected = direction === 'ltr' ? selectedLeft.value : selectedRight.value
  const entries = direction === 'ltr' ? leftEntries.value : rightEntries.value
  if (!sourceBackend || !destBackend) {
    error.value = '请先选择两侧存储后端。'
    return
  }
  if (!selected.size) {
    error.value = '请先勾选要复制的条目。'
    return
  }
  const items = entries
    .filter((entry) => selected.has(entry.name))
    .map((entry) => ({
      sourcePath: entry.path || joinPath(sourceBase, entry.name),
      destPath: joinPath(destBase, entry.name),
    }))
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const data = await api<{ job: SyncJob }>('/api/storage/sync/jobs', {
      method: 'POST',
      body: JSON.stringify({
        sourceBackend,
        destBackend,
        items,
        overwrite: overwrite.value,
      }),
    })
    job.value = data.job
    message.value = '同步任务已启动。'
    startPolling(data.job.id)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '启动同步失败'
  } finally {
    busy.value = false
  }
}

function startPolling(jobID: string) {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = window.setInterval(async () => {
    try {
      const data = await api<{ job: SyncJob }>(
        `/api/storage/sync/jobs/${encodeURIComponent(jobID)}`,
      )
      job.value = data.job
      if (['completed', 'failed', 'canceled'].includes(data.job.status)) {
        if (pollTimer) window.clearInterval(pollTimer)
        pollTimer = undefined
        if (data.job.status === 'completed') {
          message.value = '同步完成。'
          await refreshBoth()
        } else if (data.job.status === 'failed') {
          error.value = data.job.error || '同步失败'
        }
      }
    } catch {
      // keep last known job state
    }
  }, 1000)
}

async function cancelJob() {
  if (!job.value) return
  await api(`/api/storage/sync/jobs/${encodeURIComponent(job.value.id)}/cancel`, {
    method: 'POST',
  })
}
</script>

<template>
  <section class="panel sync-panel">
    <div class="panel-heading">
      <h2>存储同步</h2>
      <p>左右两侧可选择不同后端与目录，对比后将勾选条目复制到另一侧。</p>
    </div>

    <div class="sync-toolbar">
      <label class="checkbox-label">
        <input v-model="overwrite" type="checkbox" />
        覆盖已存在目标
      </label>
      <button
        class="secondary-button small-button"
        :disabled="busy || !selectedLeft.size"
        @click="copySelection('ltr')"
      >
        复制到右侧 →
      </button>
      <button
        class="secondary-button small-button"
        :disabled="busy || !selectedRight.size"
        @click="copySelection('rtl')"
      >
        ← 复制到左侧
      </button>
      <button class="secondary-button small-button" :disabled="busy" @click="refreshBoth">
        刷新两侧
      </button>
    </div>

    <p v-if="error" class="notice error-message">{{ error }}</p>
    <p v-if="message" class="notice success-message">{{ message }}</p>
    <p v-if="job" class="sync-job-status">
      任务 {{ syncJobLabel(job.status) }}：
      {{ job.copiedFiles }}/{{ job.totalFiles }} 文件，
      {{ formatBytes(job.copiedBytes) }}
      <template v-if="job.totalBytes"> / {{ formatBytes(job.totalBytes) }}</template>
      <template v-if="job.currentPath"> · {{ job.currentPath }}</template>
      <button
        v-if="job.status === 'queued' || job.status === 'running'"
        class="small-button secondary-button"
        type="button"
        @click="cancelJob"
      >
        取消
      </button>
    </p>

    <p v-if="enabledBackends.length < 1" class="empty-state">
      请先在配置文件中声明至少一个存储后端。
    </p>

    <div v-else class="sync-dual">
      <section class="sync-pane">
        <div class="sync-pane-header">
          <select v-model="leftBackend">
            <option
              v-for="backend in enabledBackends"
              :key="`l-${backend.name}`"
              :value="backend.name"
            >
              {{ backend.name }} ({{ backend.type }})
            </option>
          </select>
        </div>
        <nav class="storage-crumbs" aria-label="左侧路径">
          <button type="button" class="linkish" @click="leftPath = ''">根目录</button>
          <template v-for="(crumb, index) in leftCrumbs" :key="`lc-${index}`">
            <span>/</span>
            <button type="button" class="linkish" @click="goLeftCrumb(index)">{{ crumb }}</button>
          </template>
        </nav>
        <div class="record-table-wrap">
          <table>
            <thead>
              <tr><th></th><th>名称</th><th>大小</th><th>对比</th></tr>
            </thead>
            <tbody>
              <tr
                v-for="row in compareRows.filter((item) => item.left)"
                :key="`l-${row.name}`"
                :class="`compare-${row.status}`"
              >
                <td>
                  <input
                    type="checkbox"
                    :checked="selectedLeft.has(row.name)"
                    @change="toggleLeft(row.name)"
                  />
                </td>
                <td class="content-cell">
                  <button
                    v-if="row.left?.isDir"
                    type="button"
                    class="linkish"
                    @click="openLeft(row.left)"
                  >
                    {{ row.name }}/
                  </button>
                  <span v-else>{{ row.name }}</span>
                </td>
                <td>{{ row.left?.isDir ? '—' : formatBytes(row.left?.size || 0) }}</td>
                <td>{{ compareStatusLabel(row.status) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!leftEntries.length" class="empty-state">左侧目录为空。</p>
        </div>
      </section>

      <section class="sync-pane">
        <div class="sync-pane-header">
          <select v-model="rightBackend">
            <option
              v-for="backend in enabledBackends"
              :key="`r-${backend.name}`"
              :value="backend.name"
            >
              {{ backend.name }} ({{ backend.type }})
            </option>
          </select>
        </div>
        <nav class="storage-crumbs" aria-label="右侧路径">
          <button type="button" class="linkish" @click="rightPath = ''">根目录</button>
          <template v-for="(crumb, index) in rightCrumbs" :key="`rc-${index}`">
            <span>/</span>
            <button type="button" class="linkish" @click="goRightCrumb(index)">{{ crumb }}</button>
          </template>
        </nav>
        <div class="record-table-wrap">
          <table>
            <thead>
              <tr><th></th><th>名称</th><th>大小</th><th>对比</th></tr>
            </thead>
            <tbody>
              <tr
                v-for="row in compareRows.filter((item) => item.right)"
                :key="`r-${row.name}`"
                :class="`compare-${row.status}`"
              >
                <td>
                  <input
                    type="checkbox"
                    :checked="selectedRight.has(row.name)"
                    @change="toggleRight(row.name)"
                  />
                </td>
                <td class="content-cell">
                  <button
                    v-if="row.right?.isDir"
                    type="button"
                    class="linkish"
                    @click="openRight(row.right)"
                  >
                    {{ row.name }}/
                  </button>
                  <span v-else>{{ row.name }}</span>
                </td>
                <td>{{ row.right?.isDir ? '—' : formatBytes(row.right?.size || 0) }}</td>
                <td>{{ compareStatusLabel(row.status) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!rightEntries.length" class="empty-state">右侧目录为空。</p>
        </div>
      </section>
    </div>
  </section>
</template>
