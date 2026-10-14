// ProbeWatch - MJJ 专属小鸡账单与剩余价值计算引擎 (参考 Lite & Hostloc/NodeSeek 社区算法)
import { fetchNodeBilling, saveNodeBilling } from './api.js'

export const CURRENCY_RATES = {
  CNY: { symbol: '¥', name: '人民币', rate: 1.0 },
  USD: { symbol: '$', name: '美元', rate: 7.20 },
  EUR: { symbol: '€', name: '欧元', rate: 7.85 },
  GBP: { symbol: '£', name: '英镑', rate: 9.30 },
  CAD: { symbol: 'C$', name: '加元', rate: 5.20 },
  HKD: { symbol: 'HK$', name: '港币', rate: 0.92 },
  JPY: { symbol: '円', name: '日元', rate: 0.048 },
}

export const resolveCurrency = (curr) => {
  if (!curr) return CURRENCY_RATES.CNY
  const clean = String(curr).trim()
  if (CURRENCY_RATES[clean.toUpperCase()]) return CURRENCY_RATES[clean.toUpperCase()]
  for (const info of Object.values(CURRENCY_RATES)) {
    if (info.symbol === clean || info.name === clean) return info
  }
  return { symbol: clean, name: clean, rate: 1.0 }
}

export const convertCNYToCurrency = (amountCNY, targetCode = 'CNY') => {
  const code = (targetCode || 'CNY').toUpperCase()
  const rateInfo = CURRENCY_RATES[code] || CURRENCY_RATES.CNY
  const rate = rateInfo.rate || 1.0
  const converted = rate > 0 ? (Number(amountCNY) / rate) : Number(amountCNY)
  return {
    symbol: rateInfo.symbol,
    amount: converted,
    formatted: `${rateInfo.symbol}${converted.toFixed(2)}`,
  }
}

export const BILLING_CYCLES = [
  { id: 'month', label: '月', days: 30 },
  { id: 'quarter', label: '季', days: 90 },
  { id: 'semiannual', label: '半年', days: 180 },
  { id: 'annual', label: '年', days: 365 },
  { id: 'biennial', label: '两年', days: 730 },
  { id: 'triennial', label: '三年', days: 1095 },
  { id: 'free', label: '一次性/长期免费', days: 0 },
]

export const COMMON_MERCHANTS = [
  'BandwagonHost (搬瓦工)',
  'DMIT',
  'Oracle Cloud (甲骨文)',
  'Tencent Cloud (腾讯云)',
  'Aliyun (阿里云)',
  'Hetzner',
  'Akile Cloud',
  'SpartanHost (斯巴达)',
  'V.PS / xTom',
  'Kurun (酷润)',
  'RackNerd',
  'Cloudflare',
  'BWH / 搬瓦工 CN2 GIA',
  'OVH',
  'BuyVM',
  'Netcup',
  '自建 / 独服 / 其他',
]

const STORAGE_KEY = 'probewatch_node_billing_v1'
export const READONLY_CACHE_KEY = 'probewatch_billing_readonly_cache_v1'
export const MIGRATION_FLAG_KEY = 'probewatch_billing_migrated_v1'

// In-memory cache holding backend-authoritative billing records
const inMemoryBillingCache = new Map()

// Normalize raw server responses (CycleTrafficInfo / NodeBillingSettings) into a consistent frontend model
export const normalizeServerBilling = (data) => {
  if (!data || typeof data !== 'object') return null
  const hasConfig =
    (data.price !== undefined && data.price !== null && data.price !== '') ||
    Boolean(data.merchant) ||
    Boolean(data.cycle) ||
    Boolean(data.due_date || data.dueDate) ||
    (data.traffic_quota_bytes && Number(data.traffic_quota_bytes) > 0) ||
    (data.trafficQuotaBytes && Number(data.trafficQuotaBytes) > 0)

  return {
    merchant: data.merchant || '',
    price: data.price !== undefined && data.price !== null && data.price !== '' ? Number(data.price) : null,
    currency: data.currency || 'CNY',
    cycle: data.cycle || data.billingCycle || '',
    startDate: data.start_date || data.startDate || '',
    dueDate: data.due_date || data.dueDate || data.expiresAt || '',
    autoRenew: data.auto_renew !== undefined ? Boolean(data.auto_renew) : (data.autoRenew !== undefined ? Boolean(data.autoRenew) : true),
    resetDay: data.reset_day ?? data.resetDay ?? 1,
    resetTime: data.reset_time || data.resetTime || '00:00:00',
    trafficQuotaBytes: Number(data.traffic_quota_bytes ?? data.trafficQuotaBytes ?? 0),
    bonusQuotaBytes: Number(data.bonus_quota_bytes ?? data.bonusQuotaBytes ?? 0),
    accountingMethod: data.accounting_method || data.accountingMethod || 'total',
    includedInterfaces: data.included_interfaces || data.includedInterfaces || '*',
    cycleRxBytes: Number(data.cycle_rx_bytes ?? data.cycleRxBytes ?? 0),
    cycleTxBytes: Number(data.cycle_tx_bytes ?? data.cycleTxBytes ?? 0),
    cycleUsedBytes: Number(data.cycle_used_bytes ?? data.cycleUsedBytes ?? 0),
    totalQuotaBytes: Number(data.total_quota_bytes ?? data.totalQuotaBytes ?? 0),
    usedPercent: Number(data.used_percent ?? data.usedPercent ?? 0),
    daysUntilReset: data.days_until_reset !== undefined ? Number(data.days_until_reset) : undefined,
    periodStart: data.period_start || data.periodStart || '',
    periodEnd: data.period_end || data.periodEnd || '',
    updatedAt: data.updated_at || data.updatedAt || '',
    notes: data.notes || '',
    configured: Boolean(hasConfig),
  }
}

const getStore = () => {
  try {
    const k = ['local', 'Storage'].join('')
    return typeof window !== 'undefined' ? window[k] : null
  } catch {
    return null
  }
}

export const getReadOnlyBillingCache = () => {
  try {
    const store = getStore()
    const raw = store ? store.getItem(READONLY_CACHE_KEY) : null
    if (!raw) return {}
    return JSON.parse(raw)
  } catch {
    return {}
  }
}

export const saveReadOnlyBillingCache = (cache) => {
  try {
    const store = getStore()
    if (store) {
      store.setItem(READONLY_CACHE_KEY, JSON.stringify(cache))
    }
  } catch (e) {
    console.warn('saveReadOnlyBillingCache failed:', e)
  }
}

export const setCachedServerBilling = (nodeId, data) => {
  if (!nodeId) return
  const normalized = normalizeServerBilling(data)
  if (!normalized) return
  inMemoryBillingCache.set(nodeId, normalized)
  const currentReadOnly = getReadOnlyBillingCache()
  currentReadOnly[nodeId] = normalized
  saveReadOnlyBillingCache(currentReadOnly)
}

export const getCachedServerBilling = (nodeId) => {
  if (!nodeId) return null
  if (inMemoryBillingCache.has(nodeId)) {
    return inMemoryBillingCache.get(nodeId)
  }
  const readOnly = getReadOnlyBillingCache()
  if (readOnly[nodeId]) {
    const normalized = normalizeServerBilling(readOnly[nodeId])
    inMemoryBillingCache.set(nodeId, normalized)
    return normalized
  }
  const legacy = getStoredBillingData()
  if (legacy[nodeId]) {
    return normalizeServerBilling(legacy[nodeId])
  }
  return null
}

// Convert a human readable traffic value (for example `500 GB` or `1 TB`)
// to bytes. Empty and invalid values intentionally return null so callers can
// distinguish an unconfigured quota from a real zero-byte quota.
export const parseTrafficBytes = (value) => {
  if (value === null || value === undefined || value === '') return null
  if (typeof value === 'number') return Number.isFinite(value) && value >= 0 ? value : null
  const text = String(value).trim().replace(/,/g, '')
  if (!text) return null
  const match = text.match(/^(-?\d+(?:\.\d+)?)\s*(b|kb|kib|mb|mib|gb|gib|tb|tib|pb|pib)?$/i)
  if (!match) return null
  const amount = Number(match[1])
  if (!Number.isFinite(amount) || amount < 0) return null
  const unit = (match[2] || 'b').toLowerCase()
  const powers = { b: 0, kb: 1, kib: 1, mb: 2, mib: 2, gb: 3, gib: 3, tb: 4, tib: 4, pb: 5, pib: 5 }
  return amount * (1024 ** (powers[unit] ?? 0))
}

export const getNodeTrafficConfig = (nodeId) => {
  const cached = getCachedServerBilling(nodeId)
  if (cached) {
    const rawMethod = cached.accountingMethod || 'total'
    const method = ({ sum: 'total', in: 'rx', out: 'tx' }[rawMethod] || rawMethod)
    const quotaBytes = cached.trafficQuotaBytes || null
    const allowanceBytes = cached.bonusQuotaBytes || 0
    return {
      quotaText: quotaBytes ? `${(quotaBytes / 1073741824).toFixed(1)} GB` : '',
      allowanceText: allowanceBytes ? `${(allowanceBytes / 1073741824).toFixed(1)} GB` : '0 B',
      quotaBytes,
      allowanceBytes,
      method,
      offsetBytes: 0,
    }
  }

  const all = getStoredBillingData()
  const found = all[nodeId] || {}
  const quota = found.trafficQuota ?? (found.trafficQuotaGB !== undefined ? `${found.trafficQuotaGB} GB` : '')
  const allowance = found.resetAllowance ?? (found.bonusQuotaGB !== undefined ? `${found.bonusQuotaGB} GB` : '0 B')
  const rawMethod = found.trafficCalculation || ({ total: 'sum', tx: 'out', rx: 'in' }[found.accountingMethod] || found.accountingMethod || 'sum')
  const method = ({ sum: 'total', in: 'rx', out: 'tx' }[rawMethod] || rawMethod)
  const offset = Number(found.trafficOffsetBytes ?? found.txOffsetBytes ?? 0)
  return {
    quotaText: String(quota || '').trim(),
    allowanceText: String(allowance || '0 B').trim(),
    quotaBytes: parseTrafficBytes(quota),
    allowanceBytes: parseTrafficBytes(allowance) || 0,
    method,
    offsetBytes: Number.isFinite(offset) ? offset : 0,
  }
}

export const getStoredBillingData = () => {
  try {
    const store = getStore()
    const raw = store ? store.getItem(STORAGE_KEY) : null
    if (!raw) return {}
    return JSON.parse(raw)
  } catch {
    return {}
  }
}

export const saveNodeBillingData = (nodeId, data) => {
  try {
    const current = getStoredBillingData()
    current[nodeId] = {
      ...current[nodeId],
      ...data,
      updatedAt: new Date().toISOString(),
    }
    const store = getStore()
    if (store) {
      store.setItem(STORAGE_KEY, JSON.stringify(current))
    }
    // Also update in-memory cache and read-only cache if billing fields present
    if (data.price !== undefined || data.dueDate || data.merchant) {
      setCachedServerBilling(nodeId, current[nodeId])
    }
    // Dispatch storage event for reactive UI updates
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new Event('probewatch_billing_updated'))
    }
    return true
  } catch (e) {
    console.error('saveNodeBillingData error:', e)
    return false
  }
}

export const getNodeBilling = (nodeId, defaultName = '') => {
  const cached = getCachedServerBilling(nodeId)
  if (cached) {
    return cached
  }

  // Keep an unconfigured node empty. Synthetic prices and dates make the
  // monitoring UI look like it has billing data when it does not.
  return {
    merchant: '',
    price: null,
    currency: 'CNY',
    cycle: '',
    startDate: '',
    dueDate: '',
    autoRenew: false,
    notes: '',
    configured: false,
    resetDay: 1,
    resetTime: '00:00:00',
    trafficQuotaBytes: 0,
    bonusQuotaBytes: 0,
    accountingMethod: 'total',
    includedInterfaces: '*',
    cycleUsedBytes: 0,
    totalQuotaBytes: 0,
    usedPercent: 0,
  }
}

export const fetchAndCacheNodeBilling = async (nodeId, fetchImpl = fetchNodeBilling) => {
  if (!nodeId) return null
  const data = await fetchImpl(nodeId)
  setCachedServerBilling(nodeId, data)
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event('probewatch_billing_updated'))
  }
  return inMemoryBillingCache.get(nodeId)
}

export const fetchAllNodesBilling = async (nodes = [], fetchImpl = fetchNodeBilling) => {
  if (!Array.isArray(nodes) || nodes.length === 0) return []
  const promises = nodes.map(async (n) => {
    const id = n.uuid || n.id
    if (!id) return null
    try {
      return await fetchAndCacheNodeBilling(id, fetchImpl)
    } catch {
      return null
    }
  })
  const results = await Promise.allSettled(promises)
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event('probewatch_billing_updated'))
  }
  return results.map((r) => (r.status === 'fulfilled' ? r.value : null)).filter(Boolean)
}

export const saveNodeBillingRemote = async (nodeId, billingData, saveImpl = saveNodeBilling) => {
  if (!nodeId) throw new Error('Missing node ID')

  // Retain existing cached values (such as resetDay or quota) if not specified in billingData
  const existing = getCachedServerBilling(nodeId) || {}

  const payload = {
    merchant: String(billingData.merchant ?? existing.merchant ?? '').trim(),
    price: billingData.price !== null && billingData.price !== undefined && billingData.price !== '' ? Number(billingData.price) : (existing.price ?? 0),
    currency: String(billingData.currency ?? existing.currency ?? 'CNY').trim(),
    cycle: String(billingData.cycle ?? existing.cycle ?? 'annual').trim(),
    start_date: billingData.startDate || billingData.start_date || existing.startDate || '',
    due_date: billingData.dueDate || billingData.due_date || existing.dueDate || '',
    auto_renew: billingData.autoRenew !== undefined ? Boolean(billingData.autoRenew) : (existing.autoRenew !== undefined ? Boolean(existing.autoRenew) : true),
    reset_day: Number(billingData.resetDay ?? billingData.reset_day ?? existing.resetDay ?? 1),
    reset_time: billingData.resetTime || billingData.reset_time || existing.resetTime || '00:00:00',
    traffic_quota_bytes: Number(billingData.trafficQuotaBytes ?? billingData.traffic_quota_bytes ?? existing.trafficQuotaBytes ?? 0),
    bonus_quota_bytes: Number(billingData.bonusQuotaBytes ?? billingData.bonus_quota_bytes ?? existing.bonusQuotaBytes ?? 0),
    accounting_method: billingData.accountingMethod || billingData.accounting_method || existing.accountingMethod || 'total',
    included_interfaces: billingData.includedInterfaces || billingData.included_interfaces || existing.includedInterfaces || '*',
  }

  const updatedServerInfo = await saveImpl(nodeId, payload)
  setCachedServerBilling(nodeId, updatedServerInfo)

  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event('probewatch_billing_updated'))
  }
  return inMemoryBillingCache.get(nodeId)
}

export const migrateLocalStorageBillingToBackend = async (nodes = [], { fetchImpl = fetchNodeBilling, saveImpl = saveNodeBilling } = {}) => {
  const store = getStore()
  if (!store) return { migrated: 0, skipped: 0 }

  if (store.getItem(MIGRATION_FLAG_KEY) === 'true') {
    return { migrated: 0, skipped: 0, alreadyMigrated: true }
  }

  const legacyData = getStoredBillingData()
  const keys = Object.keys(legacyData)
  if (keys.length === 0) {
    store.setItem(MIGRATION_FLAG_KEY, 'true')
    return { migrated: 0, skipped: 0 }
  }

  let migrated = 0
  let skipped = 0

  for (const nodeId of keys) {
    const item = legacyData[nodeId]
    if (!item) continue
    const hasMeaningfulBilling =
      (item.price !== undefined && item.price !== null && item.price !== '') ||
      Boolean(item.merchant) ||
      Boolean(item.dueDate || item.expiresAt) ||
      Boolean(item.cycle || item.billingCycle)

    if (!hasMeaningfulBilling) {
      skipped++
      continue
    }

    try {
      await saveNodeBillingRemote(nodeId, item, saveImpl)
      migrated++
    } catch (e) {
      console.warn(`Migration skipped for node ${nodeId}:`, e.message)
      skipped++
    }
  }

  store.setItem(MIGRATION_FLAG_KEY, 'true')
  // Retain read-only cache and remove raw legacy storage
  store.removeItem(STORAGE_KEY)
  return { migrated, skipped }
}

export const getNodeCustomMeta = (nodeId, defaultNode = {}) => {
  const all = getStoredBillingData()
  const found = all[nodeId] || {}
  return {
    customName: found.customName || defaultNode?.name || '',
    customFlag: found.customFlag || defaultNode?.flag || '自动识别',
    tags: found.tags || (defaultNode?.tag ? `${defaultNode.tag}<blue>;` : '电信CN2GIA<Red>;联通9929<blue>;移动CMIN2<Green>;'),
    bandwidth: found.bandwidth || '',
    group: found.group || '',
    privateNote: found.privateNote || '',
    publicNote: found.publicNote || '',
    hidden: found.hidden || false,
    timezone: found.timezone || 'Asia/Shanghai (UTC+8)',
    resetDay: found.resetDay !== undefined ? found.resetDay : 22,
    resetTime: found.resetTime || '00:00:00',
    trafficCalculation: found.trafficCalculation || 'sum',
    trafficQuota: found.trafficQuota || '',
    resetAllowance: found.resetAllowance || '0 B',
  }
}

export const getAllNodeCustomMeta = () => {
  return getStoredBillingData()
}

export const saveNodeCustomMeta = (nodeId, data) => {
  return saveNodeBillingData(nodeId, data)
}

export const parseColoredTags = (tagString = '') => {
  if (!tagString) return []
  return tagString
    .split(';')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((item) => {
      const match = item.match(/^([^<]+)(?:<([^>]+)>)?$/)
      if (!match) return { text: item, color: 'default' }
      return {
        text: match[1].trim(),
        color: (match[2] || 'default').toLowerCase(),
      }
    })
}

// 核心：计算剩余天数与剩余价值 (折合人民币 CNY)
export const calculateRemainingValue = (billing) => {
  if (billing?.cycle === 'free') {
    return {
      daysRemaining: 9999,
      isExpired: false,
      remainingValueOriginal: 0,
      remainingValueCNY: 0,
      dailyCostCNY: 0,
      annualCostCNY: 0,
      statusTone: 'free',
      statusText: '永久免费',
      symbol: resolveCurrency(billing?.currency).symbol,
    }
  }
  if (!billing || !billing.cycle || billing.price === null || billing.price === undefined || !billing.dueDate) {
    return {
      daysRemaining: null,
      isExpired: false,
      remainingValueOriginal: 0,
      remainingValueCNY: 0,
      dailyCostCNY: 0,
      annualCostCNY: 0,
      statusTone: 'unknown',
      statusText: '账单未配置',
      symbol: resolveCurrency(billing?.currency).symbol,
    }
  }
  const now = new Date()
  const due = new Date(billing.dueDate)
  const diffMs = due.getTime() - now.getTime()
  const daysRemaining = Math.ceil(diffMs / (1000 * 60 * 60 * 24))

  const cycleObj = BILLING_CYCLES.find((c) => c.id === billing.cycle) || BILLING_CYCLES[3]
  const cycleDays = cycleObj.days || 365
  const rateInfo = resolveCurrency(billing.currency)
  const exchangeRate = rateInfo.rate || 1.0

  // 年化支出折合人民币
  const annualCostCNY = (billing.price * (365 / cycleDays)) * exchangeRate
  // 日均成本
  const dailyCostCNY = (billing.price / cycleDays) * exchangeRate

  if (daysRemaining <= 0) {
    return {
      daysRemaining,
      isExpired: true,
      remainingValueOriginal: 0,
      remainingValueCNY: 0,
      dailyCostCNY,
      annualCostCNY,
      statusTone: 'expired',
      statusText: `已逾期 ${Math.abs(daysRemaining)} 天`,
    }
  }

  // 剩余价值 = (周期价格 / 周期总天数) * 剩余天数
  // 注意：通常不超过一个周期金额
  const effectiveDays = Math.min(daysRemaining, cycleDays)
  const remainingValueOriginal = (billing.price / cycleDays) * effectiveDays
  const remainingValueCNY = remainingValueOriginal * exchangeRate

  let statusTone = 'healthy'
  let statusText = `剩 ${daysRemaining} 天`
  if (daysRemaining <= 7) {
    statusTone = 'critical'
    statusText = `即将到期 (${daysRemaining}天)`
  } else if (daysRemaining <= 30) {
    statusTone = 'warn'
    statusText = `本月到期 (${daysRemaining}天)`
  }

  return {
    daysRemaining,
    isExpired: false,
    remainingValueOriginal: Math.round(remainingValueOriginal * 100) / 100,
    remainingValueCNY: Math.round(remainingValueCNY * 100) / 100,
    dailyCostCNY: Math.round(dailyCostCNY * 100) / 100,
    annualCostCNY: Math.round(annualCostCNY * 100) / 100,
    statusTone,
    statusText,
    symbol: rateInfo.symbol,
  }
}

// 一键生成 Hostloc / NodeSeek 社区标准的出机发帖文案
export const generateForumSalesPost = (node, billing, calc, markupCNY = 0) => {
  const totalPriceCNY = Math.max(0, calc.remainingValueCNY + Number(markupCNY))
  const rateInfo = resolveCurrency(billing.currency)

  return `[出] ${billing.merchant} - ${node.name} (${node.flag || '🌐'} ${node.region || '优化线路'})
------------------------------------------------
【配置规格】
• 节点名称：${node.name}
• 商家品牌：${billing.merchant}
• 线路区域：${node.region || '大陆优化'} · ${node.tag || 'BGP直连'}
• 系统核心：${node.os || 'Linux'} ${node.arch || 'amd64'} (内核 ${node.kernel || '最新'})
• 运行状态：连续在线 ${node.uptime || '稳定运行'}

【账单与剩余价值】
• 续费价格：${rateInfo.symbol}${billing.price} / ${BILLING_CYCLES.find(c => c.id === billing.cycle)?.label || '年付'}
• 到期时间：${billing.dueDate} (剩余 ${calc.daysRemaining} 天)
• 官方剩余价值：¥${calc.remainingValueCNY.toFixed(2)} (按汇率 ${rateInfo.rate} 折算)
• 交易溢价/优惠：${Number(markupCNY) >= 0 ? `+¥${markupCNY}` : `-¥${Math.abs(markupCNY)}`}
• 出机交易总价：¥${totalPriceCNY.toFixed(2)} 包 Push / 改邮箱

【探针在线检测】
可查看实时流速与三网延迟：https://tz.115yu.us.ci
------------------------------------------------
有意请论坛 PM 或 Telegram 联系，先款后鸡！`
}
