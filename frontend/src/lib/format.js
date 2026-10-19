export const safeText = (value, fallback = '') => typeof value === 'string' || typeof value === 'number' ? String(value) : fallback
export const safeObject = (value) => value && typeof value === 'object' && !Array.isArray(value) ? value : {}
export const safeArray = (value) => Array.isArray(value) ? value : []
// Normalize labels returned by older seeded targets. Some deployments stored
// a literal "??" suffix as a placeholder; showing it makes otherwise valid
// public checks look corrupted. Keep meaningful operator labels untouched.
export const cleanTargetLabel = (value, fallback = '') => {
  const text = safeText(value, fallback).trim()
  return text.replace(/\s*\?\?\s*$/u, '').trim() || fallback
}
export const dash = (value, suffix = '') => value === null || value === undefined || value === '' ? '—' : `${value}${suffix}`
export const numeric = (value) => typeof value === 'number' && Number.isFinite(value) ? value : null

export const compareVersions = (left, right) => {
  const a = String(left || '').trim().replace(/^v/i, '').split('.')
  const b = String(right || '').trim().replace(/^v/i, '').split('.')
  const length = Math.max(a.length, b.length)
  for (let i = 0; i < length; i += 1) {
    const av = Number.parseInt(a[i] || '0', 10) || 0
    const bv = Number.parseInt(b[i] || '0', 10) || 0
    if (av !== bv) return av > bv ? 1 : -1
  }
  return 0
}

export const isVersionOutdated = (current, latest) => {
  if (!current || !latest) return false
  return compareVersions(current, latest) < 0
}

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

export const formatLatency = (value) => {
  const n = numeric(value)
  if (n === null) return '—'
  return `${Math.round(n)} ms`
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

const COUNTRY_CODE_META = {
  HK: { flag: '🇭🇰', region: '中国香港', tag: '亚太优化' },
  TW: { flag: '🇹🇼', region: '中国台湾', tag: '亚太直连' },
  MO: { flag: '🇲🇴', region: '中国澳门', tag: '亚太优化' },
  CN: { flag: '🇨🇳', region: '中国大陆', tag: '国内BGP' },
  JP: { flag: '🇯🇵', region: '日本', tag: '软银/IIJ' },
  SG: { flag: '🇸🇬', region: '新加坡', tag: '亚太直连' },
  KR: { flag: '🇰🇷', region: '韩国', tag: '亚太直连' },
  US: { flag: '🇺🇸', region: '美国', tag: '9929/4837' },
  CA: { flag: '🇨🇦', region: '加拿大', tag: '北美BGP' },
  DE: { flag: '🇩🇪', region: '德国', tag: '欧洲BGP' },
  GB: { flag: '🇬🇧', region: '英国', tag: '欧洲BGP' },
  UK: { flag: '🇬🇧', region: '英国', tag: '欧洲BGP' },
  NL: { flag: '🇳🇱', region: '荷兰', tag: '欧洲BGP' },
  FR: { flag: '🇫🇷', region: '法国', tag: '欧洲BGP' },
  RU: { flag: '🇷🇺', region: '俄罗斯', tag: '欧亚伯利亚' },
  AU: { flag: '🇦🇺', region: '澳大利亚', tag: '大洋洲BGP' },
  MY: { flag: '🇲🇾', region: '马来西亚', tag: '东南亚BGP' },
  TH: { flag: '🇹🇭', region: '泰国', tag: '东南亚BGP' },
  VN: { flag: '🇻🇳', region: '越南', tag: '东南亚BGP' },
  ID: { flag: '🇮🇩', region: '印度尼西亚', tag: '东南亚BGP' },
  PH: { flag: '🇵🇭', region: '菲律宾', tag: '东南亚BGP' },
  IN: { flag: '🇮🇳', region: '印度', tag: '南亚BGP' },
  TR: { flag: '🇹🇷', region: '土耳其', tag: '欧亚BGP' },
  AE: { flag: '🇦🇪', region: '阿联酋', tag: '中东BGP' },
  CH: { flag: '🇨🇭', region: '瑞士', tag: '欧洲BGP' },
  SE: { flag: '🇸🇪', region: '瑞典', tag: '欧洲BGP' },
  FI: { flag: '🇫🇮', region: '芬兰', tag: '欧洲BGP' },
  NO: { flag: '🇳🇴', region: '挪威', tag: '欧洲BGP' },
  IT: { flag: '🇮🇹', region: '意大利', tag: '欧洲BGP' },
  ES: { flag: '🇪🇸', region: '西班牙', tag: '欧洲BGP' },
  AT: { flag: '🇦🇹', region: '奥地利', tag: '欧洲BGP' },
  PL: { flag: '🇵🇱', region: '波兰', tag: '欧洲BGP' },
  BR: { flag: '🇧🇷', region: '巴西', tag: '南美BGP' },
  ZA: { flag: '🇿🇦', region: '南非', tag: '非洲BGP' },
}

const isoCodeToFlagEmoji = (code) => {
  const clean = String(code || '').trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(clean) || clean === 'XX' || clean === 'ZZ' || clean === 'UN') return ''
  return String.fromCodePoint(
    0x1F1E6 + clean.charCodeAt(0) - 65,
    0x1F1E6 + clean.charCodeAt(1) - 65,
  )
}

const matchKeywordRegion = (rawText) => {
  const text = String(rawText || '').toLowerCase()
  if (!text.trim()) return null
  if (/香港|hong\s*kong|\bhk\b|\bhkg\b/.test(text)) {
    return { ...COUNTRY_CODE_META.HK }
  }
  if (/台湾|台北|台中|台南|高雄|彰化|新北|桃园|新竹|\btw\b|\btpe\b|taiwan|taipei|taichung|changhua|kaohsiung|hinet/.test(text)) {
    return { ...COUNTRY_CODE_META.TW }
  }
  if (/澳门|macau|macao|\bmo\b/.test(text)) {
    return { ...COUNTRY_CODE_META.MO }
  }
  if (/日本|东京|大阪|千叶|福冈|名古屋|横滨|\bjp\b|\bnrt\b|\bkix\b|\btyo\b|japan|tokyo|osaka|chiba/.test(text)) {
    return { ...COUNTRY_CODE_META.JP }
  }
  if (/美国|美西|美东|硅谷|圣何塞|洛杉矶|西雅图|达拉斯|纽约|芝加哥|凤凰城|弗吉尼亚|阿什本|波特兰|亚特兰大|迈阿密|休斯顿|盐湖城|加州|\bus\b|\busa\b|\bsjc\b|\blax\b|\bsea\b|\bdfw\b|\bord\b|\biad\b|\bewr\b|united\s*states|california|silicon\s*valley|san\s*jose|los\s*angeles|seattle|dallas|new\s*york|chicago|phoenix|ashburn|virginia/.test(text)) {
    return { ...COUNTRY_CODE_META.US }
  }
  if (/新加坡|\bsg\b|\bsin\b|singapore/.test(text)) {
    return { ...COUNTRY_CODE_META.SG }
  }
  if (/德国|法兰克福|黑森|慕尼黑|柏林|纽伦堡|杜塞尔多夫|\bde\b|\bfra\b|germany|frankfurt|hessen|munich|berlin|nuremberg/.test(text)) {
    return { ...COUNTRY_CODE_META.DE }
  }
  if (/英国|伦敦|英格兰|曼彻斯特|伯明翰|考文垂|\buk\b|\bgb\b|\blhr\b|\blon\b|united\s*kingdom|england|britain|london|manchester/.test(text)) {
    return { ...COUNTRY_CODE_META.GB }
  }
  if (/韩国|首尔|釜山|仁川|\bkr\b|\bicn\b|korea|seoul|busan/.test(text)) {
    return { ...COUNTRY_CODE_META.KR }
  }
  if (/俄罗斯|莫斯科|圣彼得堡|新西伯利亚|海参崴|伯力|\bru\b|russia|moscow/.test(text)) {
    return { ...COUNTRY_CODE_META.RU }
  }
  if (/法国|巴黎|马赛|\bfr\b|\bcdg\b|france|paris/.test(text)) {
    return { ...COUNTRY_CODE_META.FR }
  }
  if (/荷兰|阿姆斯特丹|\bnl\b|\bams\b|netherlands|amsterdam/.test(text)) {
    return { ...COUNTRY_CODE_META.NL }
  }
  if (/加拿大|多伦多|温哥华|蒙特利尔|\bca\b|\byyz\b|\byvr\b|canada|toronto|vancouver|montreal/.test(text)) {
    return { ...COUNTRY_CODE_META.CA }
  }
  if (/澳大利亚|澳洲|悉尼|墨尔本|\bau\b|\bsyd\b|australia|sydney|melbourne/.test(text)) {
    return { ...COUNTRY_CODE_META.AU }
  }
  if (/马来西亚|吉隆坡|\bmy\b|\bkul\b|malaysia|kuala\s*lumpur/.test(text)) {
    return { ...COUNTRY_CODE_META.MY }
  }
  if (/泰国|曼谷|\bth\b|\bbkk\b|thailand|bangkok/.test(text)) {
    return { ...COUNTRY_CODE_META.TH }
  }
  if (/越南|河内|胡志明|\bvn\b|vietnam|hanoi/.test(text)) {
    return { ...COUNTRY_CODE_META.VN }
  }
  if (/印尼|印度尼西亚|雅加达|\bid\b|indonesia|jakarta/.test(text)) {
    return { ...COUNTRY_CODE_META.ID }
  }
  if (/菲律宾|马尼拉|\bph\b|philippines|manila/.test(text)) {
    return { ...COUNTRY_CODE_META.PH }
  }
  if (/印度|孟买|新德里|\bin\b|india|mumbai/.test(text)) {
    return { ...COUNTRY_CODE_META.IN }
  }
  if (/土耳其|伊斯坦布尔|\btr\b|turkey|istanbul/.test(text)) {
    return { ...COUNTRY_CODE_META.TR }
  }
  if (/筋斗[雲云]|国内|中国|大陆|广州|北京|上海|深圳|杭州|成都|南京|武汉|广东|湖北|浙江|江苏|四川|福建|山东|河南|湖南|天津|重庆|西安|苏州|东莞|佛山|宁波|无锡|青岛|郑州|长沙|福州|厦门|合肥|南昌|济南|沈阳|大连|哈尔滨|长春|石家庄|太原|呼和浩特|南宁|海口|贵阳|昆明|拉萨|兰州|西宁|银川|乌鲁木齐|电信|联通|移动|\bcn\b|china|guangdong|hubei|zhejiang|jiangsu|beijing|shanghai|shenzhen|guangzhou/.test(text)) {
    return { ...COUNTRY_CODE_META.CN }
  }
  return null
}

export const detectRegionAndFlag = (name = '', hostname = '', extra = null) => {
  let nodeName = ''
  let nodeHostname = ''
  let hint = extra

  if (typeof name === 'object' && name !== null) {
    hint = name
    nodeName = safeText(name.name)
    nodeHostname = safeText(name.hostname || name.resource?.hostname)
  } else {
    nodeName = safeText(name)
    nodeHostname = safeText(hostname)
  }

  // 1. Check explicit region keywords in the node name first
  const byName = matchKeywordRegion(nodeName)
  if (byName) return byName

  // 2. Check structured GeoIP / IPQA country codes and location fields
  if (hint && typeof hint === 'object') {
    if (hint.flag && hint.flag !== '🌐' && hint.region && hint.region !== '公网节点') {
      return { flag: hint.flag, region: hint.region, tag: hint.tag || 'BGP网络' }
    }
    const res = safeObject(hint.resource)
    const ipq = safeObject(hint.ip_quality || hint.ipQuality || res.ip_quality || res.ipQuality)
    const ipqa = safeObject(hint.ipqa || res.ipqa)
    const ipqaV4 = safeObject(ipqa.ipv4)
    const ipqaV6 = safeObject(ipqa.ipv6)

    const codeCandidates = [
      hint.country_code,
      hint.countryCode,
      ipq.country,
      ipqaV4.country,
      ipqaV6.country,
      hint.country,
      res.country,
    ]
    for (const rawCode of codeCandidates) {
      const code = String(rawCode || '').trim().toUpperCase()
      if (!code) continue
      if (COUNTRY_CODE_META[code]) {
        return { ...COUNTRY_CODE_META[code] }
      }
      const byCodeKeyword = matchKeywordRegion(code)
      if (byCodeKeyword) return byCodeKeyword
      if (/^[A-Z]{2}$/.test(code)) {
        const emoji = isoCodeToFlagEmoji(code)
        if (emoji) {
          const regionLabel = safeText(ipqaV4.region || ipq.region || ipq.city || hint.region, code)
          return { flag: emoji, region: regionLabel, tag: 'BGP网络' }
        }
      }
    }

    const geoText = [
      ipqaV4.region,
      ipqaV4.city,
      ipqaV6.region,
      ipqaV6.city,
      ipq.region,
      ipq.city,
      hint.region !== '公网节点' ? hint.region : '',
    ].filter(Boolean).join(' ')
    const byGeoText = matchKeywordRegion(geoText)
    if (byGeoText) return byGeoText
  } else if (typeof hint === 'string' && hint.trim()) {
    const code = hint.trim().toUpperCase()
    if (COUNTRY_CODE_META[code]) return { ...COUNTRY_CODE_META[code] }
    const byHint = matchKeywordRegion(hint)
    if (byHint) return byHint
  }

  // 3. Check combined nodeName + nodeHostname
  const byCombined = matchKeywordRegion(`${nodeName} ${nodeHostname}`)
  if (byCombined) return byCombined

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
  const meta = detectRegionAndFlag(name, hostname, source)

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
    cpuModel: safeText(resource.cpu_model || resource.cpu_name, '—'),
    cpuMhz: numeric(resource.cpu_mhz),
    cpuCores: numeric(resource.cpu_cores) || 1,
    cpuTempC: numeric(resource.cpu_temp_c),
    sensors: Array.isArray(resource.sensors) ? resource.sensors : [],
    disks: Array.isArray(resource.disks) ? resource.disks : [],
    mounts: Array.isArray(resource.mounts) ? resource.mounts : [],
    socketStats: safeObject(resource.socket_stats),
    listeningPorts: Array.isArray(resource.listening_ports) ? resource.listening_ports : [],
    healthInfo: safeObject(resource.health_info),
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

// 运营商目标与探针任务识别函数 (支持电信/联通/移动、ct/cu/cm、cn2/9929/cmin2 及 isp/operator 显式字段)
export const identifyTargetCarrier = (item) => {
  if (!item) return '未知'

  const isCT = (s) => /电信|telecom|chinanet|cn2|4809|4134/i.test(s) || /(?:^|[-_.\s/])ct(?:[-_.\s/0-9]|$)/i.test(s)
  const isCU = (s) => /联通|unicom|9929|4837|10099|cug/i.test(s) || /(?:^|[-_.\s/])cu(?:[-_.\s/0-9]|$)/i.test(s)
  const isCM = (s) => /移动|mobile|cmcc|cmin2|58807|58453|9808/i.test(s) || /(?:^|[-_.\s/])cm(?:[-_.\s/0-9]|$)/i.test(s)

  // 1. 优先使用显式 isp / operator / carrier 字段
  if (typeof item === 'object') {
    const explicit = String(item.isp || item.operator || item.carrier || '').trim()
    if (explicit) {
      if (isCT(explicit)) return '电信'
      if (isCU(explicit)) return '联通'
      if (isCM(explicit)) return '移动'
    }
  }

  // 2. 依次匹配 target_id / name / host / id / target 或纯字符串
  const fields = typeof item === 'string' ? [item] : [item.target_id, item.name, item.host, item.id, item.target]
  for (const f of fields) {
    if (!f) continue
    const s = String(f).trim()
    if (isCT(s)) return '电信'
    if (isCU(s)) return '联通'
    if (isCM(s)) return '移动'
  }

  return '未知'
}

