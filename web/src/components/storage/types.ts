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

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`
}
