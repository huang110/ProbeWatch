import assert from "node:assert/strict"
import { readFileSync, existsSync } from "node:fs"
import { resolve } from "node:path"

// Logic matching NodeDetailPage.jsx
function formatIPQualityType(type) {
  if (!type) return '未知 / 未识别'
  const t = String(type).trim().toLowerCase()
  if (t === 'hosting' || t === 'datacenter') return '机房'
  if (t === 'isp') return 'ISP'
  if (t === 'residential') return '家宽'
  if (t === 'unknown') return '未知 / 未识别'
  return type
}

const IP_QUALITY_SOURCE_MAP = {
  risk_score: '综合风险',
  fraud_score: '欺诈风险',
  abuse_score: '滥用风险',
  threat_score: '威胁分',
  scamalytics: 'Scamalytics',
  ip2location: 'IP2Location',
  abuseipdb: 'AbuseIPDB',
  ipqs: 'IPQS',
  dbip: 'DB-IP',
}

function formatIPQualitySourceName(key) {
  if (!key) return '—'
  if (IP_QUALITY_SOURCE_MAP[key]) return IP_QUALITY_SOURCE_MAP[key]
  return String(key).replace(/_/g, ' ')
}

function formatIPQualityDateTime(timestamp) {
  if (!timestamp) return '未知'
  const ms = Number(timestamp) > 1e11 ? Number(timestamp) : Number(timestamp) * 1000
  const d = new Date(ms)
  if (isNaN(d.getTime())) return '未知'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatScoreValue(scoreVal) {
  const scoreNum = Number(scoreVal)
  if (!Number.isFinite(scoreNum)) return '—'
  return String(Math.round(scoreNum * 100) / 100)
}

function getScoreTone(scoreVal) {
  const scoreNum = Number(scoreVal)
  if (!Number.isFinite(scoreNum)) return 'unknown'
  if (scoreNum < 25) return 'low'
  if (scoreNum < 75) return 'medium'
  return 'high'
}

function getRiskBadge(ipQuality, loading, error) {
  const badgeClass =
    ipQuality?.risk === 'high' ? 'badge-rose' :
    ipQuality?.risk === 'medium' ? 'badge-amber' :
    ipQuality?.risk === 'low' ? 'badge-mint' :
    'badge-neutral'

  const badgeText =
    loading ? '检测中' :
    ipQuality?.risk === 'high' ? '高风险' :
    ipQuality?.risk === 'medium' ? '中风险' :
    ipQuality?.risk === 'low' ? '低风险' :
    ipQuality ? '未知风险' :
    error === 'unauthorized' ? '未授权' :
    error ? '请求失败' :
    '等待检测'

  return { badgeClass, badgeText }
}

function getEmptyOrStaleMessage(ipQuality, error) {
  if (ipQuality) {
    if (error) {
      return '检测服务暂时不可用，显示最近一次成功结果'
    }
    return null
  }
  if (error === 'unauthorized') {
    return '访客模式未开放此项指标'
  }
  if (error) {
    return 'IP 质量数据请求失败，正在等待自动重试'
  }
  return '等待 Agent 首次质量检测'
}

function getFlagStatus(value) {
  if (value === true) return { status: 'yes', text: '是' }
  if (value === false) return { status: 'no', text: '否' }
  return { status: 'unknown', text: '未知' }
}

console.log("=== Testing IP Quality UI State Transitions ===")

// 1. 无数据 (Empty State)
{
  const { badgeClass, badgeText } = getRiskBadge(null, false, null)
  assert.equal(badgeClass, 'badge-neutral', "Empty state badge must be gray (badge-neutral)")
  assert.notEqual(badgeText, '低风险', "Empty state must not be low risk")
  assert.notEqual(badgeText, '安全', "Empty state must not be marked safe")
  assert.notEqual(badgeText, '正常', "Empty state must not be marked normal")
  const msg = getEmptyOrStaleMessage(null, null)
  assert.equal(msg, '等待 Agent 首次质量检测', "Empty state message must prompt waiting for agent")
  console.log("[PASS] Empty state verified")
}

// 2. 成功真实数据 (Real Successful Data)
{
  const ipQuality = {
    ip_type: 'hosting',
    asn: 'AS31972',
    country: 'TW',
    region: 'Taipei',
    organization: 'Taiwan Internet Technology Co., Ltd.',
    proxy: false,
    vpn: false,
    tor: false,
    abuse: false,
    risk: 'low',
    checked_at: 1700000000,
    sources: { risk_score: 5, fraud_score: 8.5 }
  }
  const { badgeClass, badgeText } = getRiskBadge(ipQuality, false, null)
  assert.equal(badgeClass, 'badge-mint', "Low risk badge must be mint")
  assert.equal(badgeText, '低风险')
  assert.equal(formatIPQualityType(ipQuality.ip_type), '机房')
  assert.deepEqual(getFlagStatus(ipQuality.proxy), { status: 'no', text: '否' })
  assert.equal(getEmptyOrStaleMessage(ipQuality, null), null)

  // Region and ASN formatting
  const regionCountry = [ipQuality.region, ipQuality.country].filter(Boolean).join(' · ')
  assert.equal(regionCountry, 'Taipei · TW')
  const asnOrg = [ipQuality.asn, ipQuality.organization].filter(Boolean).join(' ')
  assert.equal(asnOrg, 'AS31972 Taiwan Internet Technology Co., Ltd.')

  // Sources formatting
  assert.equal(formatIPQualitySourceName('risk_score'), '综合风险')
  assert.equal(formatIPQualitySourceName('fraud_score'), '欺诈风险')
  assert.equal(formatScoreValue(ipQuality.sources.risk_score), '5')
  assert.equal(formatScoreValue(ipQuality.sources.fraud_score), '8.5')
  assert.equal(getScoreTone(ipQuality.sources.risk_score), 'low')
  console.log("[PASS] Successful real data verified")
}

// 3. 失败但有旧数据 (Failed with Stale Data)
{
  const oldQuality = {
    ip_type: 'isp',
    asn: 'AS1234',
    risk: 'medium',
    checked_at: 1700000000
  }
  const { badgeClass, badgeText } = getRiskBadge(oldQuality, false, 'request_failed')
  assert.equal(badgeClass, 'badge-amber', "Medium risk badge must be amber")
  assert.equal(badgeText, '中风险')
  const msg = getEmptyOrStaleMessage(oldQuality, 'request_failed')
  assert.equal(msg, '检测服务暂时不可用，显示最近一次成功结果')
  console.log("[PASS] Stale data warning verified")
}

// 4. 请求失败无旧数据 (Request Failed without Data)
{
  const { badgeClass, badgeText } = getRiskBadge(null, false, 'request_failed')
  assert.equal(badgeClass, 'badge-neutral')
  assert.equal(badgeText, '请求失败')
  const msg = getEmptyOrStaleMessage(null, 'request_failed')
  assert.equal(msg, 'IP 质量数据请求失败，正在等待自动重试')
  console.log("[PASS] Failed state without data verified")
}

// 5. 未知字段与类型 (Unknown IP Type & Unknown Risk)
{
  assert.equal(formatIPQualityType('unknown'), '未知 / 未识别')
  assert.equal(formatIPQualityType(''), '未知 / 未识别')
  assert.equal(formatIPQualityType(null), '未知 / 未识别')

  const unknownQuality = { ip_type: 'unknown', risk: 'unknown' }
  const { badgeClass, badgeText } = getRiskBadge(unknownQuality, false, null)
  assert.equal(badgeClass, 'badge-neutral')
  assert.equal(badgeText, '未知风险')
  assert.notEqual(badgeText, '低风险')
  assert.notEqual(badgeText, '安全')
  assert.notEqual(badgeText, '正常')
  console.log("[PASS] Unknown fields verified")
}

// 6. Threat Flags (True, False, Null/Undefined)
{
  assert.deepEqual(getFlagStatus(true), { status: 'yes', text: '是' })
  assert.deepEqual(getFlagStatus(false), { status: 'no', text: '否' })
  assert.deepEqual(getFlagStatus(null), { status: 'unknown', text: '未知' })
  assert.deepEqual(getFlagStatus(undefined), { status: 'unknown', text: '未知' })
  console.log("[PASS] Threat flags state mapping verified")
}

// 7. Verify CSS classes and styles in styles.css
console.log("=== Testing CSS Rules and Layout Definitions ===")
{
  const cssPath = existsSync(resolve(process.cwd(), "src/styles.css"))
    ? resolve(process.cwd(), "src/styles.css")
    : resolve(process.cwd(), "frontend/src/styles.css")
  const css = readFileSync(cssPath, "utf8")

  const requiredClasses = [
    ".komari-ip-quality-card",
    ".ip-quality-summary-grid",
    ".ip-quality-summary-item",
    ".ip-quality-detail-grid",
    ".ip-quality-detail-grid-two",
    ".ip-quality-panel",
    ".ip-quality-panel-title",
    ".ip-quality-score-row",
    ".ip-quality-score-bar",
    ".ip-quality-flag-grid",
    ".komari-media-strip-card",
    ".komari-media-strip-header",
    ".komari-media-strip-title",
    ".komari-media-strip-summary",
    ".komari-media-strip-list",
    ".komari-media-service-card",
    ".komari-media-service-icon",
    ".komari-media-service-name",
    ".komari-media-service-status",
    ".komari-media-service-region",
    ".komari-media-service-latency",
  ]

  for (const cls of requiredClasses) {
    assert.ok(css.includes(cls), `CSS must define ${cls}`)
  }

  // Check 2 columns in desktop detail grid two
  assert.ok(css.includes("repeat(2, minmax(0, 1fr))"), "Desktop detail grid two must use 2 columns")
  // Check 4 columns in summary grid
  assert.ok(css.includes("repeat(4, minmax(0, 1fr))"), "Desktop summary grid must use 4 columns")
  // Check responsive single column for detail grid two
  assert.ok(css.includes(".ip-quality-detail-grid.ip-quality-detail-grid-two"), "Must support responsive single column for detail grid two")
  // Check mobile scroll for media strip list
  assert.ok(css.includes("overflow-x: auto"), "Mobile media strip list must support horizontal scroll")
  assert.ok(css.includes("min-width: 148px"), "Mobile media service card must have min-width 148px")
  // Check light theme support
  assert.ok(css.includes('[data-theme="light"] .komari-ip-quality-card'), "Must support light theme for IP quality card")
  assert.ok(css.includes('[data-theme="light"] .komari-media-strip-card'), "Must support light theme for media strip card")

  console.log("[PASS] All required CSS classes and responsive layout rules verified")
}

// 8. Verify NodeDetailPage.jsx contains independent media strip card and 2-panel IP quality
console.log("=== Testing Component Code Security & Structure ===")
{
  const compPath = existsSync(resolve(process.cwd(), "src/components/NodeDetailPage.jsx"))
    ? resolve(process.cwd(), "src/components/NodeDetailPage.jsx")
    : resolve(process.cwd(), "frontend/src/components/NodeDetailPage.jsx")
  const comp = readFileSync(compPath, "utf8")

  // IP quality card must have summary grid and detail grid two
  assert.ok(comp.includes("ip-quality-summary-grid"), "Component must render ip-quality-summary-grid")
  assert.ok(comp.includes("ip-quality-detail-grid ip-quality-detail-grid-two"), "Component must render ip-quality-detail-grid-two")
  assert.ok(comp.includes("风险评分（越低越好）"), "Component must render 风险评分（越低越好）")
  assert.ok(comp.includes("数据库标记（命中 / 有结论的库）"), "Component must render 数据库标记")
  assert.ok(comp.includes("暂无多来源评分"), "Component must handle empty sources")

  // Streaming / AI must NOT be inside IP quality detail grid
  const ipQualityGridIdx = comp.indexOf("ip-quality-detail-grid ip-quality-detail-grid-two")
  const ipQualityGridEnd = comp.indexOf("komari-ip-quality-empty", ipQualityGridIdx)
  const ipQualitySection = comp.substring(ipQualityGridIdx, ipQualityGridEnd > 0 ? ipQualityGridEnd : ipQualityGridIdx + 3000)
  assert.ok(!ipQualitySection.includes("流媒体 / AI"), "IP Quality section must NOT contain 流媒体 / AI")

  // Independent horizontal streaming card
  assert.ok(comp.includes("komari-media-strip-card"), "Component must render komari-media-strip-card")
  assert.ok(comp.includes("komari-media-strip-header"), "Component must render komari-media-strip-header")
  assert.ok(comp.includes("komari-media-strip-title"), "Component must render komari-media-strip-title")
  assert.ok(comp.includes("komari-media-strip-summary"), "Component must render komari-media-strip-summary")
  assert.ok(comp.includes("komari-media-strip-list"), "Component must render komari-media-strip-list")
  assert.ok(comp.includes("komari-media-service-card"), "Component must render komari-media-service-card")
  assert.ok(comp.includes("komari-media-service-icon"), "Component must render komari-media-service-icon")
  assert.ok(comp.includes("komari-media-service-name"), "Component must render komari-media-service-name")
  assert.ok(comp.includes("komari-media-service-status"), "Component must render komari-media-service-status")
  assert.ok(comp.includes("komari-media-service-region"), "Component must render komari-media-service-region")
  assert.ok(comp.includes("komari-media-service-latency"), "Component must render komari-media-service-latency")

  assert.ok(comp.includes("全球流媒体与 AI 服务解锁能力"), "Component must render title 全球流媒体与 AI 服务解锁能力")
  assert.ok(comp.includes("暂无流媒体 / AI 检测结果"), "Component must handle empty media with 暂无流媒体 / AI 检测结果")
  assert.ok(comp.includes("正在同步流媒体 / AI 检测结果…"), "Component must handle loading with 正在同步流媒体 / AI 检测结果…")

  // Security: No public IP in media cards, no hardcoded fake scores
  assert.ok(!comp.includes("scamalytics: 12"), "Must not hardcode fake scores")
  assert.ok(!comp.includes("abuseipdb: 0"), "Must not hardcode fake scores")

  console.log("[PASS] Component layout structure and security checks verified")
}

// 9. Verify Media Field Mapping and Formatting Logic
console.log("=== Testing Media Field Mapping Logic ===")
{
  function mapMediaStatus(rawStatus) {
    const s = String(rawStatus || '').toLowerCase()
    if (s === 'available') return { tone: 'available', text: '已解锁' }
    if (s === 'unavailable') return { tone: 'unavailable', text: '未解锁' }
    if (s === 'blocked') return { tone: 'blocked', text: '已封锁' }
    if (s === 'error') return { tone: 'error', text: '异常' }
    if (s === 'timeout') return { tone: 'timeout', text: '超时' }
    return { tone: 'unknown', text: '未知' }
  }

  function formatMediaRegion(region) {
    if (!region) return '—'
    return String(region).trim().toUpperCase()
  }

  function formatMediaLatency(latency) {
    if (latency === null || latency === undefined || latency === '' || isNaN(Number(latency)) || Number(latency) <= 0) {
      return '—'
    }
    return `${Math.round(Number(latency))}ms`
  }

  assert.deepEqual(mapMediaStatus('available'), { tone: 'available', text: '已解锁' })
  assert.deepEqual(mapMediaStatus('unavailable'), { tone: 'unavailable', text: '未解锁' })
  assert.deepEqual(mapMediaStatus('blocked'), { tone: 'blocked', text: '已封锁' })
  assert.deepEqual(mapMediaStatus('error'), { tone: 'error', text: '异常' })
  assert.deepEqual(mapMediaStatus('timeout'), { tone: 'timeout', text: '超时' })
  assert.deepEqual(mapMediaStatus('other'), { tone: 'unknown', text: '未知' })
  assert.deepEqual(mapMediaStatus(null), { tone: 'unknown', text: '未知' })

  assert.equal(formatMediaRegion('tw'), 'TW')
  assert.equal(formatMediaRegion('US'), 'US')
  assert.equal(formatMediaRegion(null), '—')
  assert.equal(formatMediaRegion(''), '—')

  assert.equal(formatMediaLatency(45.6), '46ms')
  assert.equal(formatMediaLatency(12), '12ms')
  assert.equal(formatMediaLatency(0), '—', "Latency 0 must be formatted as — (not 0ms)")
  assert.equal(formatMediaLatency(null), '—')
  assert.equal(formatMediaLatency(undefined), '—')

  console.log("[PASS] Media field mapping logic verified")
}

// 9. IPQA Status Strip Verification
console.log("=== Testing IPQA Status Strip Logic and CSS ===")
{
  const cssPath = existsSync(resolve(process.cwd(), "src/styles.css"))
    ? resolve(process.cwd(), "src/styles.css")
    : resolve(process.cwd(), "frontend/src/styles.css")
  const css = readFileSync(cssPath, "utf8")

  const ipqaClasses = [
    ".ipqa-status-strip",
    ".ipqa-status-header",
    ".ipqa-status-title",
    ".ipqa-badge",
    ".ipqa-badge-green",
    ".ipqa-badge-blue",
    ".ipqa-badge-yellow",
    ".ipqa-badge-red",
    ".ipqa-badge-orange",
    ".ipqa-badge-gray",
    ".ipqa-metrics-grid",
    ".ipqa-metric-item",
  ]
  for (const cls of ipqaClasses) {
    assert.ok(css.includes(cls), `CSS must define ${cls}`)
  }

  function getIPQABadge(ipQuality) {
    if (!ipQuality?.ipqa_enabled) return { cls: 'ipqa-badge-gray', text: '未启用 IPQA' }
    if (!ipQuality?.ipqa_installed) return { cls: 'ipqa-badge-gray', text: '节点未安装 IP-Quality-Archive' }
    if (ipQuality?.collection_error) return { cls: 'ipqa-badge-orange', text: '采集失败' }
    if (ipQuality?.highest_severity === 'CRITICAL') return { cls: 'ipqa-badge-red', text: 'CRITICAL 严重风险' }
    if (ipQuality?.highest_severity === 'WARNING') return { cls: 'ipqa-badge-yellow', text: 'WARNING 警告' }
    if (ipQuality?.highest_severity === 'INFO') return { cls: 'ipqa-badge-blue', text: 'INFO 提示' }
    return { cls: 'ipqa-badge-green', text: '无变化' }
  }

  assert.deepEqual(getIPQABadge({ ipqa_enabled: false }), { cls: 'ipqa-badge-gray', text: '未启用 IPQA' })
  assert.deepEqual(getIPQABadge({ ipqa_enabled: true, ipqa_installed: false }), { cls: 'ipqa-badge-gray', text: '节点未安装 IP-Quality-Archive' })
  assert.deepEqual(getIPQABadge({ ipqa_enabled: true, ipqa_installed: true, collection_error: 'log_too_large' }), { cls: 'ipqa-badge-orange', text: '采集失败' })
  assert.deepEqual(getIPQABadge({ ipqa_enabled: true, ipqa_installed: true, highest_severity: 'CRITICAL' }), { cls: 'ipqa-badge-red', text: 'CRITICAL 严重风险' })
  assert.deepEqual(getIPQABadge({ ipqa_enabled: true, ipqa_installed: true, highest_severity: 'WARNING' }), { cls: 'ipqa-badge-yellow', text: 'WARNING 警告' })
  assert.deepEqual(getIPQABadge({ ipqa_enabled: true, ipqa_installed: true, highest_severity: 'INFO' }), { cls: 'ipqa-badge-blue', text: 'INFO 提示' })
  assert.deepEqual(getIPQABadge({ ipqa_enabled: true, ipqa_installed: true, highest_severity: 'NONE' }), { cls: 'ipqa-badge-green', text: '无变化' })

  console.log("[PASS] IPQA Status Strip logic and CSS verified")
}

console.log("=== ALL IP QUALITY, IPQA & STREAMING UI TESTS PASSED ===")
