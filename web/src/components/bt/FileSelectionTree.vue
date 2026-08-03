<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { BTFile } from './types'
import { formatBytes, syncStatusLabel } from './types'
import {
  buildFileTree,
  collectFileIndexes,
  defaultExpandedPaths,
  type FileTreeNode,
} from './fileTree'

const props = defineProps<{
  files: BTFile[]
  priorities: Record<number, number>
  busy: boolean
}>()

const emit = defineEmits<{
  'update:priorities': [value: Record<number, number>]
}>()

const tree = computed(() => buildFileTree(props.files))
const expanded = ref<Set<string>>(new Set())
const structureKey = computed(() => props.files.map((file) => file.path).join('\0'))

watch(
  structureKey,
  (key, previous) => {
    if (key === previous) return
    expanded.value = defaultExpandedPaths(tree.value)
  },
  { immediate: true },
)

function isSelected(index: number) {
  return Number(props.priorities[index] || 0) > 0
}

function folderState(node: FileTreeNode): 'all' | 'none' | 'some' {
  const indexes = collectFileIndexes(node)
  if (!indexes.length) return 'none'
  let selected = 0
  for (const index of indexes) {
    if (isSelected(index)) selected += 1
  }
  if (selected === 0) return 'none'
  if (selected === indexes.length) return 'all'
  return 'some'
}

function setIndexes(indexes: number[], selected: boolean) {
  const next = { ...props.priorities }
  for (const index of indexes) {
    if (!selected) {
      next[index] = 0
      continue
    }
    const current = Number(next[index] || 0)
    next[index] = current > 0 ? current : 1
  }
  emit('update:priorities', next)
}

function toggleFolder(node: FileTreeNode) {
  const indexes = collectFileIndexes(node)
  const select = folderState(node) !== 'all'
  setIndexes(indexes, select)
}

function toggleFile(file: BTFile) {
  setIndexes([file.index], !isSelected(file.index))
}

function setPriority(index: number, priority: number) {
  emit('update:priorities', { ...props.priorities, [index]: priority })
}

function toggleExpand(path: string) {
  const next = new Set(expanded.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  expanded.value = next
}

function bindIndeterminate(el: Element | null, indeterminate: boolean) {
  if (el instanceof HTMLInputElement) el.indeterminate = indeterminate
}

function progressLabel(file: BTFile) {
  if (!file.length) return '100%'
  return `${((file.completedBytes / file.length) * 100).toFixed(1)}%`
}

type FlatRow =
  | { kind: 'dir'; node: FileTreeNode; depth: number; state: 'all' | 'none' | 'some' }
  | { kind: 'file'; node: FileTreeNode; depth: number; file: BTFile }

const rows = computed(() => {
  const result: FlatRow[] = []
  function walk(node: FileTreeNode, depth: number) {
    for (const child of node.children) {
      if (child.isDir) {
        result.push({ kind: 'dir', node: child, depth, state: folderState(child) })
        if (expanded.value.has(child.path)) walk(child, depth + 1)
      } else if (child.file) {
        result.push({ kind: 'file', node: child, depth, file: child.file })
      }
    }
  }
  walk(tree.value, 0)
  return result
})
</script>

<template>
  <div class="file-tree-wrap record-table-wrap">
    <table class="file-tree-table">
      <thead>
        <tr>
          <th class="file-tree-check">选择</th>
          <th>文件</th>
          <th>大小</th>
          <th>进度</th>
          <th>同步</th>
          <th>优先级</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="`${row.kind}:${row.node.path}`">
          <td class="file-tree-check">
            <input
              v-if="row.kind === 'dir'"
              type="checkbox"
              :checked="row.state === 'all'"
              :disabled="busy"
              :ref="(el) => bindIndeterminate(el as Element | null, row.state === 'some')"
              :aria-label="`选择目录 ${row.node.name}`"
              @change="toggleFolder(row.node)"
            />
            <input
              v-else
              type="checkbox"
              :checked="isSelected(row.file.index)"
              :disabled="busy"
              :aria-label="`选择文件 ${row.node.name}`"
              @change="toggleFile(row.file)"
            />
          </td>
          <td class="content-cell file-tree-name">
            <div class="file-tree-label" :style="{ paddingLeft: `${row.depth * 18}px` }">
              <button
                v-if="row.kind === 'dir'"
                type="button"
                class="file-tree-toggle"
                :aria-expanded="expanded.has(row.node.path)"
                @click="toggleExpand(row.node.path)"
              >
                {{ expanded.has(row.node.path) ? '▼' : '▶' }}
              </button>
              <span v-else class="file-tree-spacer" aria-hidden="true" />
              <span :class="row.kind === 'dir' ? 'file-tree-dir' : 'file-tree-file'">
                {{ row.node.name }}
              </span>
            </div>
          </td>
          <td>
            <template v-if="row.kind === 'file'">{{ formatBytes(row.file.length) }}</template>
            <template v-else>—</template>
          </td>
          <td>
            <template v-if="row.kind === 'file'">{{ progressLabel(row.file) }}</template>
            <template v-else>—</template>
          </td>
          <td>
            <template v-if="row.kind === 'file'">
              <span v-if="syncStatusLabel(row.file.syncStatus)">
                {{ syncStatusLabel(row.file.syncStatus) }}
              </span>
              <span v-else>—</span>
            </template>
            <template v-else>—</template>
          </td>
          <td>
            <select
              v-if="row.kind === 'file'"
              :value="Number(priorities[row.file.index] || 0)"
              :disabled="busy"
              @change="
                setPriority(
                  row.file.index,
                  Number(($event.target as HTMLSelectElement).value),
                )
              "
            >
              <option :value="0">不下载</option>
              <option :value="1">普通</option>
              <option :value="2">高</option>
            </select>
            <span v-else>—</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
