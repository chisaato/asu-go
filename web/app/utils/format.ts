export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes)) return '—'
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  const idx = Math.min(i, sizes.length - 1)
  const val = bytes / Math.pow(k, idx)
  return `${val.toFixed(val >= 100 || idx === 0 ? 0 : 1)} ${sizes[idx]}`
}

export function parseClient(ua: string | null | undefined): string {
  if (!ua) return '未知客户端'
  // 匹配常见如 owut/1.2, luci-app-attendedsysupgrade/24.10, curl/8.5.0
  const match = ua.match(/^([a-zA-Z0-9_-]+)\/([a-zA-Z0-9._-]+)/)
  if (match) {
    return `${match[1]}/${match[2]}`
  }
  return ua.length > 20 ? `${ua.slice(0, 20)}…` : ua
}

export function truncateHash(hash: string | null | undefined, len = 8): string {
  if (!hash) return '—'
  if (hash.length <= len) return hash
  return hash.slice(0, len)
}
