export const safeText = (value, fallback = '') => typeof value === 'string' || typeof value === 'number' ? String(value) : fallback
export const safeObject = (value) => value && typeof value === 'object' && !Array.isArray(value) ? value : {}
export const safeArray = (value) => Array.isArray(value) ? value : []
export const dash = (value, suffix = '') => value === null || value === undefined || value === '' ? '—' : `${value}${suffix}`
export const numeric = (value) => typeof value === 'number' && Number.isFinite(value) ? value : null

export const ratio = (used, total) => {
  const u = numeric(used)
  const t = numeric(total)
  return u !== null && t > 0 ? Math.round((u / t) * 1000) / 10 : null
}

export const formatBytes = (value) => {
  const n = numeric(value)
  if (n === null) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let size = Math.max(0, n)
  let i = 0
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i += 1
  }
  return `${size >= 10 || i === 0 ? Math.round(size) : size.toFixed(1)} ${units[i]}`
}

export const formatRate = (value) => {
  const n = numeric(value)
  return n === null ? '—' : `${formatBytes(Math.max(n, 0))}/s`
}

export const formatPercent = (value) => {
  const n = numeric(value)
  if (n === null) return '—'
  const rounded = Math.round(n * 10) / 10
  return `${Number.isInteger(rounded) ? rounded : rounded.toFixed(1)}%`
}

export const formatLossPercent = (value) => {
  const n = numeric(value)
  if (n === null) return '—'
  const pct = Math.round(n * 1000) / 10
  return `${Number.isInteger(pct) ? pct : pct.toFixed(1)}%`
}

export const formatNumber = (value) => {
  const n = numeric(value)
  return n === null ? '—' : Number.isInteger(n) ? String(n) : n.toFixed(1)
}

export const statusLabel = (status) => status === 'online' ? '在线' : status === 'attention' ? '需关注' : status === 'offline' ? '离线' : '未知'

export const nodeColor = (id = '') => {
  const value = safeText(id)
  return ['mint', 'blue', 'amber', 'rose', 'violet', 'gray'][Array.from(value).reduce((a, c) => a + c.charCodeAt(0), 0) % 6]
}

export const formatAlertTime = (value) => {
  if (!value || (typeof value !== 'string' && typeof value !== 'number')) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12: false })
}

export const formatTimeOfDay = (value) => {
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString('zh-CN', { hour12: false })
}

export const relativeHeartbeat = (value) => {
  if (!value || (typeof value !== 'string' && typeof value !== 'number')) return '—'
  const ms = typeof value === 'number' ? (value < 1e12 ? value * 1000 : value) : new Date(value).getTime()
  if (!Number.isFinite(ms)) return '—'
  const seconds = Math.round((Date.now() - ms) / 1000)
  if (seconds < 15) return '刚刚'
  if (seconds < 60) return `${seconds}秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}小时前`
  return `${Math.floor(seconds / 86400)}天前`
}

export const formatUptime = (startedAt) => {
  const s = numeric(startedAt)
  if (!s || s <= 0) return '—'
  const ms = s < 1e12 ? s * 1000 : s
  const diffSec = Math.max(0, Math.floor((Date.now() - ms) / 1000))
  const days = Math.floor(diffSec / 86400)
  const hours = Math.floor((diffSec % 86400) / 3600)
  const minutes = Math.floor((diffSec % 3600) / 60)
  if (days > 0) return `${days}天 ${hours}时`
  if (hours > 0) return `${hours}时 ${minutes}分`
  return `${Math.max(1, minutes)}分钟`
}

export const detectRegionAndFlag = (name = '', hostname = '') => {
  const text = `${name} ${hostname}`.toLowerCase()
  if (/香港|hong kong|\bhk\b/.test(text)) {
    return { flag: '🇭🇰', region: '中国香港', tag: '亚太优化' }
  }
  if (/台湾|台北|\btw\b|taiwan/.test(text)) {
    return { flag: '🇹🇼', region: '中国台湾', tag: '亚太直连' }
  }
  if (/日本|东京|大阪|\bjp\b|japan|tokyo|osaka/.test(text)) {
    return { flag: '🇯🇵', region: '日本', tag: '软银/IIJ' }
  }
  if (/美国|美西|美东|圣何塞|洛杉矶|西雅图|达拉斯|纽约|\bus\b|\busa\b|\bsjc\b|\blax\b|\bsea\b/.test(text)) {
    return { flag: '🇺🇸', region: '美国', tag: '9929/4837' }
  }
  if (/新加坡|\bsg\b|singapore/.test(text)) {
    return { flag: '🇸🇬', region: '新加坡', tag: '亚太直连' }
  }
  if (/德国|法兰克福|\bde\b|germany|\bfra\b/.test(text)) {
    return { flag: '🇩🇪', region: '德国', tag: '欧洲BGP' }
  }
  if (/英国|伦敦|\buk\b|\bgb\b|london/.test(text)) {
    return { flag: '🇬🇧', region: '英国', tag: '欧洲BGP' }
  }
  if (/韩国|首尔|\bkr\b|korea|seoul/.test(text)) {
    return { flag: '🇰🇷', region: '韩国', tag: '亚太直连' }
  }
  if (/俄罗斯|莫斯科|\bru\b|russia|moscow/.test(text)) {
    return { flag: '🇷🇺', region: '俄罗斯', tag: '欧亚伯利亚' }
  }
  if (/筋斗[雲云]|国内|中国|大陆|广州|北京|上海|深圳|杭州|成都|南京|武汉|电信|联通|移动|\bcn\b|china/.test(text)) {
    return { flag: '🇨🇳', region: '中国大陆', tag: '国内BGP' }
  }
  return { flag: '🌐', region: '公网节点', tag: 'BGP网络' }
}

export const formatLoad = (l1, l5, l15) => {
  const f = (v) => v !== null && v !== undefined ? Number(v).toFixed(2) : '—'
  if (l1 === null || l1 === undefined) return '—'
  return `${f(l1)} ${f(l5)} ${f(l15)}`
}

export const formatEpochSeconds = (value) => {
  const n = numeric(value)
  if (n === null || n <= 0) return '—'
  const date = new Date(n < 1e12 ? n * 1000 : n)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN')
}

export const normalizeNode = (node) => {
  const source = safeObject(node)
  const resource = safeObject(source.resource)
  const id = safeText(source.id ?? source.uuid)
  const uuid = safeText(source.uuid)
  const name = safeText(source.name, '未命名节点')
  const hostname = safeText(resource.hostname)
  const meta = detectRegionAndFlag(name, hostname)

  const memUsed = numeric(resource.memory_used_bytes)
  const memTotal = numeric(resource.memory_total_bytes)
  const swapUsed = numeric(resource.swap_used_bytes)
  const swapTotal = numeric(resource.swap_total_bytes)
  const diskUsed = numeric(resource.filesystem_used_bytes)
  const diskTotal = numeric(resource.filesystem_total_bytes)
  const startedAt = numeric(resource.started_at)
  const rx = numeric(resource.network_rx_bytes)
  const tx = numeric(resource.network_tx_bytes)

  return {
    id,
    uuid,
    name,
    status: safeText(source.status, 'unknown'),
    lastReportedAt: source.last_reported_at ?? null,
    resource,
    color: nodeColor(id),
    hostname,
    os: safeText(resource.os, 'Linux'),
    kernel: safeText(resource.kernel, '—'),
    arch: safeText(resource.arch, 'amd64'),
    agentVersion: safeText(resource.agent_version, '—'),
    startedAt,
    uptime: formatUptime(startedAt),
    cpu: numeric(resource.cpu_percent),
    memUsed,
    memTotal,
    memory: ratio(memUsed, memTotal),
    swapUsed,
    swapTotal,
    swap: ratio(swapUsed, swapTotal),
    diskUsed,
    diskTotal,
    disk: ratio(diskUsed, diskTotal),
    load1: numeric(resource.load_1),
    load5: numeric(resource.load_5),
    load15: numeric(resource.load_15),
    rx,
    tx,
    totalTraffic: (rx !== null || tx !== null) ? (rx || 0) + (tx || 0) : null,
    flag: meta.flag,
    region: meta.region,
    tag: meta.tag,
  }
}

export const normalizeAlert = (alert) => {
  const source = safeObject(alert)
  return {
    id: safeText(source.id),
    severity: safeText(source.severity, 'info'),
    title: safeText(source.title ?? source.category, '告警事件'),
    message: safeText(source.message ?? source.reason, '暂无告警详情'),
    status: safeText(source.status, 'open'),
    lastSeen: source.last_seen ?? source.last_seen_at ?? source.last_seenAt ?? null,
    occurrenceCount: numeric(source.occurrence_count) ?? 0,
  }
}

export const alertSeverity = (severity) => ['critical', 'warning', 'info'].includes(severity) ? severity : 'info'
