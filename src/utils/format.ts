// 网络速率格式化（字节/秒 -> 人类可读）
export function formatNet(bytesPerSec: number | null | undefined): string {
  if (bytesPerSec == null || isNaN(bytesPerSec)) return '-'
  const kbps = bytesPerSec / 1024
  if (kbps < 1024) return `${kbps.toFixed(1)} KB/s`
  return `${(kbps / 1024).toFixed(2)} MB/s`
}

// 相对时间
export function timeAgo(iso: string | undefined): string {
  if (!iso) return '-'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return '-'
  const diff = Date.now() - t
  if (diff < 60000) return '刚刚'
  if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`
  return `${Math.floor(diff / 86400000)} 天前`
}

// 从 ping 数组计算有效延迟的 min / avg / max（>1000ms 或 null 视为无效）
export function pingStats(pings: number[] | null | undefined): {
  min: number | null
  avg: number | null
  max: number | null
  valid: number
} {
  if (!pings || pings.length === 0) return { min: null, avg: null, max: null, valid: 0 }
  const valid = pings.filter((v) => v != null && v > 0 && v <= 1000) // -1/0 视为无效
  if (valid.length === 0) return { min: null, avg: null, max: null, valid: 0 }
  const min = Math.min(...valid)
  const max = Math.max(...valid)
  const avg = valid.reduce((a, b) => a + b, 0) / valid.length
  return { min, avg, max, valid: valid.length }
}
