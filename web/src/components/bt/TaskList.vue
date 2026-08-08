<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { BTTask } from './types'
import { formatBytes, formatDuration, formatPercent } from './types'

const props = defineProps<{
  tasks: BTTask[]
  busy: boolean
  selectedIds: number[]
}>()

const emit = defineEmits<{
  'update:selectedIds': [ids: number[]]
  open: [task: BTTask]
  pause: [task: BTTask]
  remove: [task: BTTask, deleteData: boolean]
  copyMagnet: [task: BTTask]
}>()

const menu = ref<{
  x: number
  y: number
  task: BTTask
} | null>(null)
let longPressTimer: number | null = null

const selectedSet = computed(() => new Set(props.selectedIds))
const allSelected = computed(
  () => props.tasks.length > 0 && props.tasks.every((task) => selectedSet.value.has(task.id)),
)
const someSelected = computed(
  () => props.tasks.some((task) => selectedSet.value.has(task.id)) && !allSelected.value,
)

watch(
  () => props.tasks.map((task) => task.id).join(','),
  () => {
    const valid = new Set(props.tasks.map((task) => task.id))
    const next = props.selectedIds.filter((id) => valid.has(id))
    if (next.length !== props.selectedIds.length) emit('update:selectedIds', next)
  },
)

function progress(task: BTTask) {
  if (task.sizeWhenDone && task.sizeWhenDone > 0) {
    return Math.min(100, (task.completedBytes / task.sizeWhenDone) * 100)
  }
  return task.totalBytes ? Math.min(100, (task.completedBytes / task.totalBytes) * 100) : 0
}

function toggleAll(checked: boolean) {
  emit('update:selectedIds', checked ? props.tasks.map((task) => task.id) : [])
}

function toggleOne(task: BTTask, checked: boolean) {
  const next = new Set(props.selectedIds)
  if (checked) next.add(task.id)
  else next.delete(task.id)
  emit('update:selectedIds', [...next])
}

function bindIndeterminate(el: Element | null) {
  if (el instanceof HTMLInputElement) el.indeterminate = someSelected.value
}

function clearLongPress() {
  if (longPressTimer !== null) {
    window.clearTimeout(longPressTimer)
    longPressTimer = null
  }
}

function placeMenu(x: number, y: number, task: BTTask) {
  const width = 240
  const height = 260
  menu.value = {
    x: Math.max(8, Math.min(x, window.innerWidth - width - 8)),
    y: Math.max(8, Math.min(y, window.innerHeight - height - 8)),
    task,
  }
}

function openMenu(event: MouseEvent, task: BTTask) {
  event.preventDefault()
  placeMenu(event.clientX, event.clientY, task)
}

function onPointerDown(event: PointerEvent, task: BTTask) {
  if (event.pointerType !== 'touch') return
  clearLongPress()
  longPressTimer = window.setTimeout(() => {
    longPressTimer = null
    placeMenu(event.clientX, event.clientY, task)
  }, 480)
}

function closeMenu() {
  menu.value = null
}

function choose(action: 'pause' | 'remove' | 'removeData' | 'copy') {
  const task = menu.value?.task
  menu.value = null
  if (!task) return
  if (action === 'pause') emit('pause', task)
  else if (action === 'remove') emit('remove', task, false)
  else if (action === 'removeData') emit('remove', task, true)
  else emit('copyMagnet', task)
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') closeMenu()
}

onMounted(() => {
  window.addEventListener('click', closeMenu)
  window.addEventListener('keydown', onWindowKeydown)
})
onBeforeUnmount(() => {
  clearLongPress()
  window.removeEventListener('click', closeMenu)
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<template>
  <div class="panel bt-task-panel">
    <p v-if="!tasks.length" class="empty-state">没有匹配的 BT 任务。</p>
    <template v-else>
      <label class="bt-select-all">
        <input
          type="checkbox"
          :checked="allSelected"
          :disabled="busy"
          :ref="(el) => bindIndeterminate(el as Element | null)"
          @change="toggleAll(($event.target as HTMLInputElement).checked)"
        />
        <span>全选（{{ selectedIds.length }}/{{ tasks.length }}）</span>
      </label>
      <article
        v-for="task in tasks"
        :key="task.id"
        class="bt-task-card"
        :class="{ selected: selectedSet.has(task.id) }"
        tabindex="0"
        @dblclick="emit('open', task)"
        @keydown.enter="emit('open', task)"
        @contextmenu="openMenu($event, task)"
        @pointerdown="onPointerDown($event, task)"
        @pointerup="clearLongPress"
        @pointercancel="clearLongPress"
        @pointerleave="clearLongPress"
      >
        <label class="bt-task-check" @click.stop @pointerdown.stop>
          <input
            type="checkbox"
            :checked="selectedSet.has(task.id)"
            :disabled="busy"
            :aria-label="`选择 ${task.name || task.infoHash}`"
            @change="toggleOne(task, ($event.target as HTMLInputElement).checked)"
          />
        </label>
        <div class="task-main" @click="emit('open', task)">
          <div class="task-title-row">
            <strong>{{ task.name || '正在获取元数据…' }}</strong>
            <span class="state-badge" :class="`state-${task.status}`">{{ task.status }}</span>
          </div>
          <div class="progress-track"><span :style="{ width: `${progress(task)}%` }" /></div>
          <div class="task-stats">
            <span>{{ progress(task).toFixed(1) }}%</span>
            <span>可用 {{ formatPercent(task.availablePercent) }}</span>
            <span>{{ formatBytes(task.completedBytes) }} / {{ formatBytes(task.totalBytes) }}</span>
            <span>↓ {{ formatBytes(task.downloadRate) }}/s</span>
            <span>↑ {{ formatBytes(task.uploadRate) }}/s</span>
            <span>{{ task.peers }} peers</span>
            <span>ETA {{ formatDuration(task.etaSeconds) }}</span>
          </div>
          <p v-if="task.error" class="error-message">{{ task.error }}</p>
        </div>
      </article>
    </template>

    <div
      v-if="menu"
      class="peer-context-menu task-context-menu"
      :style="{ left: `${menu.x}px`, top: `${menu.y}px` }"
      role="menu"
      @click.stop
      @pointerdown.stop
    >
      <button type="button" role="menuitem" :disabled="busy" @click="choose('pause')">
        暂停任务
      </button>
      <button type="button" role="menuitem" :disabled="busy" @click="choose('remove')">
        删除任务
      </button>
      <button type="button" role="menuitem" :disabled="busy" @click="choose('removeData')">
        移除数据及任务
      </button>
      <button type="button" role="menuitem" :disabled="busy" @click="choose('copy')">
        复制磁力链接
      </button>
    </div>
  </div>
</template>
