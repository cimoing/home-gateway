export type StorageType = 'local' | 'smb' | 's3'

export interface StorageBackend {
  name: string
  type: StorageType
  config: Record<string, unknown>
  hasSecret: boolean
  enabled: boolean
}

export interface StorageEntry {
  name: string
  path: string
  isDir: boolean
  size: number
  modTime: string
}

export interface SyncJob {
  id: string
  status: string
  error?: string
  sourceBackend: string
  destBackend: string
  totalFiles: number
  copiedFiles: number
  failedFiles: number
  skippedFiles?: number
  totalBytes: number
  copiedBytes: number
  copyRateBps?: number
  currentPath?: string
  createdAt: string
  updatedAt: string
}

export interface SyncCompareRow {
  name: string
  left?: StorageEntry
  right?: StorageEntry
  status: 'left_only' | 'right_only' | 'same' | 'different' | 'both'
}

export interface SyncEndpoint {
  name: string
  path: string
}

export interface SyncSchedule {
  id: number
  interval: string
  enabled: boolean
  src: SyncEndpoint
  dst: SyncEndpoint
  running: boolean
  lastStatus?: string
  lastError?: string
  lastScanned: number
  lastCopied: number
  lastSkipped: number
  lastBytes: number
  lastStartedAt?: string
  lastFinishedAt?: string
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`
}

export type PreviewKind = 'video' | 'image' | 'pdf'

const PREVIEW_VIDEO_EXT = new Set(['mp4', 'webm', 'ogg', 'ogv', 'm4v', 'mov'])
const PREVIEW_IMAGE_EXT = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg'])
const PREVIEW_PDF_EXT = new Set(['pdf'])

export function fileExtension(name: string) {
  const index = name.lastIndexOf('.')
  if (index <= 0 || index === name.length - 1) return ''
  return name.slice(index + 1).toLowerCase()
}

export function previewKindForName(name: string): PreviewKind | null {
  const ext = fileExtension(name)
  if (PREVIEW_VIDEO_EXT.has(ext)) return 'video'
  if (PREVIEW_IMAGE_EXT.has(ext)) return 'image'
  if (PREVIEW_PDF_EXT.has(ext)) return 'pdf'
  return null
}

export function formatEndpoint(endpoint?: SyncEndpoint) {
  if (!endpoint?.name) return '—'
  const path = endpoint.path?.trim()
  return path ? `${endpoint.name}:${path}` : `${endpoint.name}:/`
}

export function scheduleStatusLabel(status?: string) {
  switch (status) {
    case 'running':
      return '进行中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    default:
      return status || '—'
  }
}

export function syncJobLabel(status?: string) {
  switch (status) {
    case 'queued':
      return '排队中'
    case 'running':
      return '进行中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    case 'canceled':
      return '已取消'
    default:
      return status || '—'
  }
}

export function compareStatusLabel(status: SyncCompareRow['status']) {
  switch (status) {
    case 'left_only':
      return '仅左侧'
    case 'right_only':
      return '仅右侧'
    case 'same':
      return '相同'
    case 'different':
      return '不同'
    default:
      return '两侧'
  }
}
