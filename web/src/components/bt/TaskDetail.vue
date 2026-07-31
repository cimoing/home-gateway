<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { BTFile, BTTask } from './types'
import { formatBytes } from './types'

const props = defineProps<{ task: BTTask; files: BTFile[]; busy: boolean }>()
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

function save() {
  emit(
    'saveFiles',
    props.files.map((file) => ({
      index: file.index,
      priority: Number(priorities.value[file.index] || 0),
    })),
  )
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
        <div><dt>分享率</dt><dd>{{ task.ratio.toFixed(2) }}</dd></div>
      </dl>
      <div class="file-heading">
        <div><h3>文件选择</h3><p>已选择 {{ formatBytes(selectedBytes) }}</p></div>
        <button :disabled="busy || !files.length" @click="save">保存选择</button>
      </div>
      <p v-if="!files.length" class="empty-state">元数据尚未就绪。</p>
      <div v-else class="record-table-wrap file-table">
        <table>
          <thead><tr><th>文件</th><th>大小</th><th>进度</th><th>优先级</th></tr></thead>
          <tbody>
            <tr v-for="file in files" :key="file.id">
              <td class="content-cell">{{ file.path }}</td>
              <td>{{ formatBytes(file.length) }}</td>
              <td>{{ file.length ? ((file.completedBytes / file.length) * 100).toFixed(1) : 100 }}%</td>
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
