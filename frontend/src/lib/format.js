export const safeText = (value, fallback = '') => typeof value === 'string' || typeof value === 'number' ? String(value) : fallback
export const safeObject = (value) => value && typeof value === 'object' && !Array.isArray(value) ? value : {}
export const safeArray = (value) => Array.isArray(value) ? value : []
export const dash = (value, suffix = '') => value === null || value === undefined || value === '' ? '—' : `${value}${suffix}`
export const numeric = (value) => typeof value === 'number' && Number.isFinite(value) ? value : null
export const ratio = (used, total) => { const u = numeric(used); const t = numeric(total); return u !== null && t > 0 ? Math.round((u / t) * 100) : null }
export const formatBytes = (value) => { const n = numeric(value); if (n === null) return '—'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; let size = n; let i = 0; while (size >= 1024 && i < units.length - 1) { size /= 1024; i += 1 } return `${size >= 10 || i === 0 ? Math.round(size) : size.toFixed(1)} ${units[i]}` }
export const formatRate = (value) => { const n = numeric(value); return n === null ? '—' : `${formatBytes(Math.max(n, 0))}/s` }
export const formatPercent = (value) => { const n = numeric(value); if (n === null) return '—'; const rounded = Math.round(n * 10) / 10; return `${Number.isInteger(rounded) ? rounded : rounded.toFixed(1)}%` }
export const formatLossPercent = (value) => { const n = numeric(value); if (n === null) return '—'; const pct = Math.round(n * 1000) / 10; return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%` }
export const formatNumber = (value) => { const n = numeric(value); return n === null ? '—' : Number.isInteger(n) ? String(n) : n.toFixed(1) }
export const statusLabel = (status) => status === 'online' ? '在线' : status === 'attention' ? '需要关注' : status === 'offline' ? '离线' : '未知'
export const nodeColor = (id = '') => { const value = safeText(id); return ['mint', 'blue', 'amber', 'rose', 'violet', 'gray'][Array.from(value).reduce((a, c) => a + c.charCodeAt(0), 0) % 6] }
export const formatAlertTime = (value) => { if (!value || (typeof value !== 'string' && typeof value !== 'number')) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN') }
export const formatTimeOfDay = (value) => { const date = value instanceof Date ? value : new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString('zh-CN', { hour12: false }) }
export const relativeHeartbeat = (value) => {
  if (!value || (typeof value !== 'string' && typeof value !== 'number')) return '—'
  const ms = typeof value === 'number' ? (value < 1e12 ? value * 1000 : value) : new Date(value).getTime()
  if (!Number.isFinite(ms)) return '—'
  const seconds = Math.round((Date.now() - ms) / 1000)
  if (seconds < 0) return '刚刚'
  if (seconds < 60) return '刚刚'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  return `${Math.floor(seconds / 86400)} 天前`
}
export const formatEpochSeconds = (value) => { const n = numeric(value); if (n === null || n <= 0) return '—'; const date = new Date(n < 1e12 ? n * 1000 : n); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN') }
export const normalizeNode = (node) => { const source = safeObject(node); const resource = safeObject(source.resource); const id = safeText(source.id ?? source.uuid); const uuid = safeText(source.uuid); return { id, uuid, name: safeText(source.name, '未命名节点'), status: safeText(source.status, 'unknown'), lastReportedAt: source.last_reported_at ?? null, resource, color: nodeColor(id), hostname: safeText(resource.hostname), os: safeText(resource.os), kernel: safeText(resource.kernel), arch: safeText(resource.arch), agentVersion: safeText(resource.agent_version), startedAt: numeric(resource.started_at), cpu: numeric(resource.cpu_percent), memory: ratio(resource.memory_used_bytes, resource.memory_total_bytes), disk: ratio(resource.filesystem_used_bytes, resource.filesystem_total_bytes), rx: numeric(resource.network_rx_bytes), tx: numeric(resource.network_tx_bytes) } }
export const normalizeAlert = (alert) => { const source = safeObject(alert); return { id: safeText(source.id), severity: safeText(source.severity, 'info'), title: safeText(source.title ?? source.category, '告警事件'), message: safeText(source.message ?? source.reason, '暂无告警详情'), status: safeText(source.status, 'open'), lastSeen: source.last_seen ?? source.last_seen_at ?? source.last_seenAt ?? null, occurrenceCount: numeric(source.occurrence_count) ?? 0 } }
export const alertSeverity = (severity) => ['critical', 'warning', 'info'].includes(severity) ? severity : 'info'
