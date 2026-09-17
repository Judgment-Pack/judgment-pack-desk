export interface ChatStorageStatus {
  path: string
  recommendedPath: string
  revision: string
  legacy: boolean
  previousPath?: string
  projectCount: number
  bytes: number
  projectBytes: number
  scope: 'personal'
  problem?: string
  maxMoveBytes: number
  maxBackupBytes: number
}
export const chatStorageQueryKey = ['chat-storage']
export function formatStorageBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}
