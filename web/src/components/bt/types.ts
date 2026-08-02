export interface BTTask {
  id: number
  infoHash: string
  sourceType: string
  name: string
  saveSubdir: string
  desiredState: string
  status: string
  error?: string
  totalBytes: number
  completedBytes: number
  downloadRate: number
  uploadRate: number
  uploadedBytes: number
  peers: number
  ratio: number
  seedingPaused?: boolean
  etaSeconds?: number
  createdAt: string
}

export interface BTFile {
  id: number
  taskId: number
  index: number
  path: string
  length: number
  selected: boolean
  priority: number
  completedBytes: number
}

export interface BTPeer {
  address: string
  peerId: string
  network: string
  source: string
  downloadedBytes: number
  uploadedBytes: number
  downloadRate: number
  uploadRate: number
}

export interface BTSettings {
  enabled: boolean
  downloadRoot: string
  listenPort: number
  running: boolean
  downloadLimitBps: number
  uploadLimitBps: number
  seedRatioLimit: number
}

export interface BTStatus {
  dhtNodes: number
  dhtGoodNodes: number
  downloadRate: number
  uploadRate: number
  downloadedBytes: number
  uploadedBytes: number
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`
}

export function formatRate(value: number) {
  return `${formatBytes(value)}/s`
}

export function formatDuration(seconds?: number) {
  if (seconds === undefined || seconds < 0) return '—'
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.ceil(seconds / 60)} 分钟`
  return `${Math.floor(seconds / 3600)} 小时 ${Math.ceil((seconds % 3600) / 60)} 分钟`
}
