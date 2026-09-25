// ProbeWatch - MJJ 专属小鸡账单与剩余价值计算引擎 (参考 Lite & Hostloc/NodeSeek 社区算法)

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

const getStore = () => {
  try {
    const k = ['local', 'Storage'].join('')
    return typeof window !== 'undefined' ? window[k] : null
  } catch {
    return null
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
    // Dispatch storage event for reactive UI updates
    window.dispatchEvent(new Event('probewatch_billing_updated'))
    return true
  } catch (e) {
    console.error('saveNodeBillingData error:', e)
    return false
  }
}

export const getNodeBilling = (nodeId, defaultName = '') => {
  const all = getStoredBillingData()
  const found = all[nodeId] || {}
  
  // Default values if not yet customized
  return {
    merchant: found.merchant || '云服务器',
    price: found.price !== undefined ? Number(found.price) : 99,
    currency: found.currency || 'CNY',
    cycle: found.cycle || 'annual',
    startDate: found.startDate || '2026-01-01',
    dueDate: found.dueDate || '2027-01-01',
    autoRenew: found.autoRenew !== undefined ? Boolean(found.autoRenew) : true,
    notes: found.notes || '',
  }
}

export const getNodeCustomMeta = (nodeId, defaultNode = {}) => {
  const all = getStoredBillingData()
  const found = all[nodeId] || {}
  return {
    customName: found.customName || defaultNode?.name || '',
    customFlag: found.customFlag || defaultNode?.flag || '自动识别',
    tags: found.tags || (defaultNode?.tag ? `${defaultNode.tag}<blue>;` : '电信CN2GIA<Red>;联通9929<blue>;移动CMIN2<Green>;'),
    bandwidth: found.bandwidth || '500 Mbps',
    group: found.group || '',
    privateNote: found.privateNote || '',
    publicNote: found.publicNote || '',
    hidden: found.hidden || false,
    timezone: found.timezone || 'Asia/Shanghai (UTC+8)',
    resetDay: found.resetDay !== undefined ? found.resetDay : 22,
    resetTime: found.resetTime || '00:00:00',
    trafficCalculation: found.trafficCalculation || 'sum',
    trafficQuota: found.trafficQuota || '500.00 GB',
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
  if (!billing || billing.cycle === 'free') {
    return {
      daysRemaining: 9999,
      isExpired: false,
      remainingValueOriginal: 0,
      remainingValueCNY: 0,
      dailyCostCNY: 0,
      annualCostCNY: 0,
      statusTone: 'free',
      statusText: '永久免费',
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

// 一键生成 Hostloc / NodeSeek 社区标准的出鸡发帖文案
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
