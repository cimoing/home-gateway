<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '../../api/client'
import type { StorageBackend, StorageEntry } from './types'
import { formatBytes } from './types'

const props = defineProps<{ backends: StorageBackend[] }>()
const backendName = ref('')
const path = ref('')
const entries = ref<StorageEntry[]>([])
const busy = ref(false)
const error = ref('')
const message = ref('')
const newDir = ref('')

const crumbs = computed(() => {
  if (!path.value) return [] as string[]
  return path.value.split('/').filter(Boolean)
})

watch(
  () => props.backends,
  (items) => {
    if (!backendName.value && items.length) backendName.value = items[0].name
  },
  { immediate: true },
)

watch([backendName, path], () => {
  if (backendName.value) void loadEntries()
})

async function loadEntries() {
  if (!backendName.value) return
  busy.value = true
  error.value = ''
  try {
    const query = new URLSearchParams()
    if (path.value) query.set('path', path.value)
    const data = await api<{ entries: StorageEntry[] }>(
      `/api/storage/backends/${encodeURIComponent(backendName.value)}/entries?${query}`,
    )
    entries.value = data.entries || []
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载目录失败'
  } finally {
    busy.value = false
  }
}

function openEntry(entry: StorageEntry) {
  if (entry.isDir) path.value = entry.path
}

function goCrumb(index: number) {
  path.value = crumbs.value.slice(0, index + 1).join('/')
}

async function mkdir() {
  if (!backendName.value || !newDir.value.trim()) return
  const target = path.value ? `${path.value}/${newDir.value.trim()}` : newDir.value.trim()
  await run(async () => {
    await api(`/api/storage/backends/${encodeURIComponent(backendName.value)}/mkdir`, {
      method: 'POST',
      body: JSON.stringify({ path: target }),
    })
    newDir.value = ''
    await loadEntries()
  }, '目录已创建。')
}

async function removeEntry(entry: StorageEntry) {
  if (!backendName.value) return
  if (!confirm(`确定删除“${entry.name}”？`)) return
  const recursive = entry.isDir ? confirm('目录非空时是否递归删除？') : false
  await run(async () => {
    const query = new URLSearchParams({ path: entry.path, recursive: String(recursive) })
    await api(
      `/api/storage/backends/${encodeURIComponent(backendName.value)}/entries?${query}`,
      { method: 'DELETE' },
    )
    await loadEntries()
  }, '已删除。')
}

async function upload(event: Event) {
  if (!backendName.value) return
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const target = path.value ? `${path.value}/${file.name}` : file.name
  await run(async () => {
    const body = new FormData()
    body.set('file', file)
    body.set('path', target)
    await api(`/api/storage/backends/${encodeURIComponent(backendName.value)}/upload`, {
      method: 'POST',
      body,
    })
    input.value = ''
    await loadEntries()
  }, '上传完成。')
}

function download(entry: StorageEntry) {
  if (!backendName.value || entry.isDir) return
  const query = new URLSearchParams({ path: entry.path })
  window.open(
    `/api/storage/backends/${encodeURIComponent(backendName.value)}/download?${query}`,
    '_blank',
  )
}

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
</script>

<template>
  <section class="panel">
    <div class="panel-heading">
      <h2>文件浏览</h2>
      <p>在已配置的存储后端上浏览、上传与删除文件。</p>
    </div>
    <div class="bt-toolbar">
      <select v-model="backendName">
        <option v-for="backend in backends" :key="backend.name" :value="backend.name">
          {{ backend.name }} ({{ backend.type }})
        </option>
      </select>
      <input v-model="newDir" placeholder="新建目录名" />
      <button class="secondary-button small-button" :disabled="busy || !backendName" @click="mkdir">
        新建目录
      </button>
      <label class="small-button secondary-button upload-label">
        上传
        <input type="file" hidden :disabled="busy || !backendName" @change="upload" />
      </label>
      <button class="secondary-button small-button" :disabled="busy" @click="loadEntries">刷新</button>
    </div>
    <nav class="storage-crumbs" aria-label="路径">
      <button type="button" class="linkish" @click="path = ''">根目录</button>
      <template v-for="(crumb, index) in crumbs" :key="`${crumb}-${index}`">
        <span>/</span>
        <button type="button" class="linkish" @click="goCrumb(index)">{{ crumb }}</button>
      </template>
    </nav>
    <p v-if="error" class="notice error-message">{{ error }}</p>
    <p v-if="message" class="notice success-message">{{ message }}</p>
    <p v-if="!backends.length" class="empty-state">请先在配置文件中声明存储后端。</p>
    <div v-else class="record-table-wrap">
      <table>
        <thead>
          <tr><th>名称</th><th>大小</th><th>修改时间</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="entry in entries" :key="entry.path">
            <td class="content-cell">
              <button v-if="entry.isDir" type="button" class="linkish" @click="openEntry(entry)">
                {{ entry.name }}/
              </button>
              <span v-else>{{ entry.name }}</span>
            </td>
            <td>{{ entry.isDir ? '—' : formatBytes(entry.size) }}</td>
            <td>{{ entry.modTime ? new Date(entry.modTime).toLocaleString() : '—' }}</td>
            <td class="task-actions">
              <button
                v-if="!entry.isDir"
                class="small-button secondary-button"
                :disabled="busy"
                @click="download(entry)"
              >
                下载
              </button>
              <button class="small-button danger-button" :disabled="busy" @click="removeEntry(entry)">
                删除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!entries.length" class="empty-state">当前目录为空。</p>
    </div>
  </section>
</template>
