<script setup lang="ts">
import type { BTTask } from './types'
import {
  formatBytes,
  formatDuration,
  formatSyncProgress,
  syncStatusLabel,
} from './types'

defineProps<{ tasks: BTTask[]; busy: boolean }>()
const emit = defineEmits<{
  select: [task: BTTask]
  pause: [task: BTTask]
  resume: [task: BTTask]
  sync: [task: BTTask]
  remove: [task: BTTask]
}>()

function progress(task: BTTask) {
  return task.totalBytes ? Math.min(100, (task.completedBytes / task.totalBytes) * 100) : 0
}

function syncLabel(task: BTTask) {
  const status = syncStatusLabel(task.syncStatus)
  if (!status) return ''
  if (task.syncStatus === 'syncing' || task.syncStatus === 'pending') {
    const detail = formatSyncProgress(task.syncedBytes, task.syncTotalBytes)
    return detail ? `${status} ${detail}` : status
  }
  if (task.syncStatus === 'synced') {
    const detail = formatSyncProgress(task.syncedBytes, task.syncTotalBytes)
    return detail || status
  }
  return status
}
</script>

<template>
  <div class="panel bt-task-panel">
    <p v-if="!tasks.length" class="empty-state">没有匹配的 BT 任务。</p>
    <article
      v-for="task in tasks"
      :key="task.id"
      class="bt-task-card"
      tabindex="0"
      @click="emit('select', task)"
      @keydown.enter="emit('select', task)"
    >
      <div class="task-main">
        <div class="task-title-row">
          <strong>{{ task.name || '正在获取元数据…' }}</strong>
          <span class="state-badge" :class="`state-${task.status}`">{{ task.status }}</span>
        </div>
        <div class="progress-track"><span :style="{ width: `${progress(task)}%` }" /></div>
        <div class="task-stats">
          <span>{{ progress(task).toFixed(1) }}%</span>
          <span>{{ formatBytes(task.completedBytes) }} / {{ formatBytes(task.totalBytes) }}</span>
          <span>↓ {{ formatBytes(task.downloadRate) }}/s</span>
          <span>↑ {{ formatBytes(task.uploadRate) }}/s</span>
          <span>{{ task.peers }} peers</span>
          <span>ETA {{ formatDuration(task.etaSeconds) }}</span>
          <span v-if="syncLabel(task)">同步 {{ syncLabel(task) }}</span>
        </div>
        <p v-if="task.error" class="error-message">{{ task.error }}</p>
        <p v-if="task.syncError" class="error-message">{{ task.syncError }}</p>
      </div>
      <div class="task-actions" @click.stop>
        <button
          v-if="task.desiredState !== 'paused'"
          class="small-button secondary-button"
          :disabled="busy"
          @click="emit('pause', task)"
        >
          暂停
        </button>
        <button v-else class="small-button" :disabled="busy" @click="emit('resume', task)">
          恢复
        </button>
        <button
          v-if="task.syncStatus && task.syncStatus !== 'none'"
          class="small-button secondary-button"
          :disabled="busy || task.syncStatus === 'syncing'"
          @click="emit('sync', task)"
        >
          同步
        </button>
        <button class="small-button danger-button" :disabled="busy" @click="emit('remove', task)">
          删除
        </button>
      </div>
    </article>
  </div>
</template>
