<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../api/client'
import FileBrowser from './FileBrowser.vue'
import type { StorageBackend } from './types'

type Tab = 'backends' | 'files'
const activeTab = ref<Tab>('backends')
const backends = ref<StorageBackend[]>([])
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

async function testBackend(backend: StorageBackend) {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api(`/api/storage/backends/${encodeURIComponent(backend.name)}/test`, {
      method: 'POST',
    })
    message.value = `后端“${backend.name}”连接测试成功。`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '连接测试失败'
  } finally {
    busy.value = false
  }
}

async function reloadConfig() {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api('/api/system/reload-config', { method: 'POST' })
    await loadBackends()
    message.value = '配置已重新加载。'
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '重新加载配置失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="feature-view">
    <nav class="tabs" aria-label="存储管理导航">
      <button :class="{ active: activeTab === 'backends' }" @click="activeTab = 'backends'">后端</button>
      <button :class="{ active: activeTab === 'files' }" @click="activeTab = 'files'">文件</button>
    </nav>
    <p v-if="error" class="notice error-message" role="alert">{{ error }}</p>
    <p v-if="message" class="notice success-message">{{ message }}</p>

    <section v-if="activeTab === 'backends'" class="panel">
      <div class="panel-heading">
        <h2>存储后端</h2>
        <p>后端定义在 config.yaml 的 storage.backends；此处只读浏览与测试连接。</p>
      </div>
      <div class="file-heading-actions" style="margin-bottom: 1rem">
        <button class="secondary-button small-button" :disabled="busy" @click="reloadConfig">
          重新加载配置
        </button>
      </div>
      <p v-if="!backends.length" class="empty-state">配置文件中还没有存储后端。</p>
      <div v-else class="record-table-wrap">
        <table>
          <thead>
            <tr><th>名称</th><th>类型</th><th>状态</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="backend in backends" :key="backend.name">
              <td>{{ backend.name }}</td>
              <td>{{ backend.type }}</td>
              <td>{{ backend.enabled ? '启用' : '停用' }}</td>
              <td class="task-actions">
                <button
                  class="small-button secondary-button"
                  :disabled="busy || !backend.enabled"
                  @click="testBackend(backend)"
                >
                  测试连接
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <FileBrowser
      v-else
      :backends="backends.filter((item) => item.enabled)"
    />
  </section>
</template>
