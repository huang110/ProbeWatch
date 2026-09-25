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
  let ms = NaN
  if (typeof value === 'number') {
    if (value > 1e14) ms = Math.floor(value / 1e6)
    else if (value < 1e11) ms = value * 1000
    else ms = value
  } else if (typeof value === 'string') {
    const trimmed = value.trim()
    if (/^\d+$/.test(trimmed)) {
      const num = Number(trimmed)
      if (num > 1e14) ms = Math.floor(num / 1e6)
      else if (num < 1e11) ms = num * 1000
      else ms = num
    } else {
      ms = new Date(trimmed).getTime()
    }
  }
  if (!Number.isFinite(ms) || Number.isNaN(ms)) return '—'
  const date = new Date(ms)
  return date.toLocaleString('zh-CN', { hour12: false })
}

export const formatTimeOfDay = (value) => {
  if (!value) return '—'
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
    ipv4: safeText(resource.ipv4),
    ipv6: safeText(resource.ipv6),
    interfaces: Array.isArray(resource.interfaces) ? resource.interfaces : [],
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

// 5 种流量统计口径 (参考 Lite admin/traffic 与 admin/servers)
export const TRAFFIC_ACCOUNTING_METHODS = [
  { id: 'total', label: '总和 (上传 + 下载)', description: '按上传与下载之和计算用量' },
  { id: 'tx', label: '仅计流出 (上传)', description: '只计流出流量 (适合 AWS / 腾讯云等)' },
  { id: 'rx', label: '仅计流入 (下载)', description: '只计流入流量' },
  { id: 'max', label: '较大值 (Max)', description: '取上传与下载中数值较大的一方' },
  { id: 'min', label: '较小值 (Min)', description: '取上传与下载中数值较小的一方' },
]

export const calcEffectiveTraffic = (rx, tx, method = 'total', offsetBytes = 0) => {
  const inBytes = numeric(rx) || 0
  const outBytes = numeric(tx) || 0
  let base = inBytes + outBytes
  if (method === 'tx') base = outBytes
  else if (method === 'rx') base = inBytes
  else if (method === 'max') base = Math.max(inBytes, outBytes)
  else if (method === 'min') base = Math.min(inBytes, outBytes)

  const effective = Math.max(0, base + (Number(offsetBytes) || 0))
  return {
    rawIn: inBytes,
    rawOut: outBytes,
    rawBase: base,
    offset: Number(offsetBytes) || 0,
    effective,
  }
}

// 三大运营商精品骨干线路指纹库 (参考 Lite admin/monitoring)
export const identifyCarrierRoute = (hops = [], targetCarrier = 'telecom') => {
  const text = (Array.isArray(hops) ? hops.map((h) => `${h.ip || ''} ${h.as || ''} ${h.name || ''}`).join(' ') : String(hops)).toUpperCase()
  const isCU = targetCarrier === 'unicom' || /10099|联通|UNICOM|9929|4837/.test(text)
  const isCT = targetCarrier === 'telecom' || /4809|4134|电信|TELECOM|CN2/.test(text)
  const isCM = targetCarrier === 'mobile' || /58807|58453|9808|移动|CMIN2|CMI/.test(text)

  if (isCT) {
    if (text.includes('4809') || text.includes('CN2')) {
      if (text.includes('4134') || text.includes('163')) {
        return { carrier: '中国电信', line: 'CN2 GT (半程优化)', badge: 'cn2-gt', quality: 'high', verified: true }
      }
      return { carrier: '中国电信', line: 'CN2 GIA (全程极速)', badge: 'cn2-gia', quality: 'elite', verified: true }
    }
    return { carrier: '中国电信', line: '163 骨干直连 (AS4134)', badge: '163', quality: 'standard', verified: true }
  }

  if (isCU) {
    if (text.includes('10099')) {
      if (text.includes('9929')) {
        return { carrier: '中国联通', line: 'CUG VIP (10099 + 9929)', badge: 'cug-vip', quality: 'elite', verified: true }
      }
      if (text.includes('4837')) {
        return { carrier: '中国联通', line: 'CUG 优化 (10099 + 4837)', badge: 'cug-opt', quality: 'high', verified: true }
      }
    }
    if (text.includes('9929')) {
      return { carrier: '中国联通', line: '联通 9929 A网精品', badge: 'cu-9929', quality: 'elite', verified: true }
    }
    return { carrier: '中国联通', line: '联通 4837 普通网', badge: 'cu-4837', quality: 'standard', verified: true }
  }

  if (isCM) {
    if (text.includes('58807') || text.includes('CMIN2')) {
      return { carrier: '中国移动', line: 'CMIN2 精品二期 (AS58807)', badge: 'cmin2', quality: 'elite', verified: true }
    }
    if (text.includes('58453') || text.includes('CMI')) {
      return { carrier: '中国移动', line: 'CMI 国际骨干 (AS58453)', badge: 'cmi', quality: 'high', verified: true }
    }
    return { carrier: '中国移动', line: 'CMNET 普通骨干 (AS9808)', badge: 'cmnet', quality: 'standard', verified: true }
  }

  return { carrier: '国际 BGP', line: '标准公网 BGP 互联', badge: 'bgp', quality: 'standard', verified: false }
}

