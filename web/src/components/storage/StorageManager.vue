<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../api/client'
import BackendForm from './BackendForm.vue'
import FileBrowser from './FileBrowser.vue'
import type { StorageBackend } from './types'

type Tab = 'backends' | 'files' | 'form'
const activeTab = ref<Tab>('backends')
const backends = ref<StorageBackend[]>([])
const editing = ref<StorageBackend | null>(null)
const busy = ref(false)
const error = ref('')
const message = ref('')

onMounted(() => {
  void loadBackends()
})

async function loadBackends() {
  try {
    const data = await api<{ backends: StorageBackend[] }>('/api/storage/backends')
    backends.value = data.backends || []
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载存储后端失败'
  }
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

function openCreate() {
  editing.value = null
  activeTab.value = 'form'
}

function openEdit(backend: StorageBackend) {
  editing.value = backend
  activeTab.value = 'form'
}

async function saveBackend(payload: Record<string, unknown>) {
  await run(async () => {
    if (editing.value) {
      await api(`/api/storage/backends/${editing.value.id}`, {
        method: 'PUT',
        body: JSON.stringify(payload),
      })
    } else {
      await api('/api/storage/backends', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
    }
    await loadBackends()
    activeTab.value = 'backends'
    editing.value = null
  }, '存储后端已保存。')
}

async function testBackend(payload: Record<string, unknown>) {
  await run(async () => {
    if (editing.value && !payload.secret) {
      await api(`/api/storage/backends/${editing.value.id}/test`, { method: 'POST' })
    } else {
      await api('/api/storage/backends/test', {
        method: 'POST',
        body: JSON.stringify(payload),
      })
    }
  }, '连接测试成功。')
}

async function removeBackend(backend: StorageBackend) {
  if (!confirm(`确定删除存储后端“${backend.name}”？`)) return
  await run(async () => {
    await api(`/api/storage/backends/${backend.id}`, { method: 'DELETE' })
    await loadBackends()
  }, '存储后端已删除。')
}
</script>

<template>
  <section class="feature-view">
    <nav class="tabs" aria-label="存储管理导航">
      <button :class="{ active: activeTab === 'backends' }" @click="activeTab = 'backends'">后端</button>
      <button :class="{ active: activeTab === 'files' }" @click="activeTab = 'files'">文件</button>
      <button :class="{ active: activeTab === 'form' }" @click="openCreate">添加</button>
    </nav>
    <p v-if="error" class="notice error-message" role="alert">{{ error }}</p>
    <p v-if="message" class="notice success-message">{{ message }}</p>

    <section v-if="activeTab === 'backends'" class="panel">
      <div class="panel-heading">
        <h2>存储后端</h2>
        <p>管理本地、Samba 与 S3 存储目标。</p>
      </div>
      <p v-if="!backends.length" class="empty-state">还没有存储后端。</p>
      <div v-else class="record-table-wrap">
        <table>
          <thead>
            <tr><th>名称</th><th>类型</th><th>状态</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="backend in backends" :key="backend.id">
              <td>{{ backend.name }}</td>
              <td>{{ backend.type }}</td>
              <td>{{ backend.enabled ? '启用' : '停用' }}</td>
              <td class="task-actions">
                <button class="small-button secondary-button" :disabled="busy" @click="openEdit(backend)">
                  编辑
                </button>
                <button class="small-button danger-button" :disabled="busy" @click="removeBackend(backend)">
                  删除
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <FileBrowser v-else-if="activeTab === 'files'" :backends="backends.filter((item) => item.enabled)" />
    <BackendForm
      v-else
      :initial="editing"
      :busy="busy"
      @save="saveBackend"
      @test="testBackend"
      @cancel="activeTab = 'backends'; editing = null"
    />
  </section>
</template>
