<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../../api/client'
import type { SyncSchedule } from './types'
import { formatBytes, scheduleStatusLabel, formatEndpoint } from './types'

const schedules = ref<SyncSchedule[]>([])
const busyID = ref<number | null>(null)
const error = ref('')
const message = ref('')
let timer: number | undefined

onMounted(() => {
  void loadSchedules()
  timer = window.setInterval(() => {
    if (document.hidden) return
    if (schedules.value.some((item) => item.running)) void loadSchedules(false)
  }, 2000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

async function loadSchedules(showError = true) {
  try {
    const data = await api<{ schedules: SyncSchedule[] }>('/api/storage/sync/schedules')
    schedules.value = data.schedules || []
  } catch (reason) {
    if (showError) error.value = reason instanceof Error ? reason.message : '加载定时同步失败'
  }
}

async function runSchedule(schedule: SyncSchedule) {
  busyID.value = schedule.id
  error.value = ''
  message.value = ''
  try {
    const data = await api<{ schedule: SyncSchedule }>(
      `/api/storage/sync/schedules/${schedule.id}/run`,
      { method: 'POST' },
    )
    const next = data.schedule
    schedules.value = schedules.value.map((item) => (item.id === next.id ? next : item))
    message.value = `已触发同步 #${schedule.id + 1}。`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '触发同步失败'
  } finally {
    busyID.value = null
    void loadSchedules(false)
  }
}

function lastSummary(schedule: SyncSchedule) {
  if (!schedule.lastStatus) return '尚未运行'
  const parts = [
    scheduleStatusLabel(schedule.lastStatus),
    `扫描 ${schedule.lastScanned}`,
    `复制 ${schedule.lastCopied}`,
    `跳过 ${schedule.lastSkipped}`,
  ]
  if (schedule.lastBytes > 0) parts.push(formatBytes(schedule.lastBytes))
  return parts.join(' · ')
}
</script>

<template>
  <section class="panel">
    <div class="panel-heading">
      <h2>定时同步</h2>
      <p>规则来自 config.yaml 的 <code>storage.sync</code>；可手动立即执行增量同步。</p>
    </div>
    <div class="file-heading-actions" style="margin-bottom: 1rem">
      <button class="secondary-button small-button" @click="loadSchedules()">刷新</button>
    </div>
    <p v-if="error" class="notice error-message">{{ error }}</p>
    <p v-if="message" class="notice success-message">{{ message }}</p>
    <p v-if="!schedules.length" class="empty-state">尚未配置定时同步规则。</p>
    <div v-else class="record-table-wrap">
      <table>
        <thead>
          <tr>
            <th>#</th>
            <th>计划</th>
            <th>源</th>
            <th>目标</th>
            <th>启用</th>
            <th>最近一次</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="schedule in schedules" :key="schedule.id">
            <td>{{ schedule.id + 1 }}</td>
            <td><code>{{ schedule.interval }}</code></td>
            <td class="content-cell">{{ formatEndpoint(schedule.src) }}</td>
            <td class="content-cell">{{ formatEndpoint(schedule.dst) }}</td>
            <td>{{ schedule.enabled ? '是' : '否' }}</td>
            <td class="content-cell">
              <div>{{ lastSummary(schedule) }}</div>
              <div v-if="schedule.lastError" class="error-message">{{ schedule.lastError }}</div>
              <div v-if="schedule.lastFinishedAt" class="muted-note">
                {{ new Date(schedule.lastFinishedAt).toLocaleString() }}
              </div>
            </td>
            <td class="task-actions">
              <button
                class="small-button"
                :disabled="busyID === schedule.id || schedule.running"
                @click="runSchedule(schedule)"
              >
                {{ schedule.running ? '同步中…' : '立即同步' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
