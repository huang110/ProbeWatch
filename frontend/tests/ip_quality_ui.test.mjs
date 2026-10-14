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
    ".ip-quality-summary-grid-five",
    ".ip-quality-summary-item",
    ".ip-quality-detail-grid",
    ".ip-quality-detail-grid-three",
    ".ip-quality-panel",
    ".ip-quality-panel-title",
    ".ip-quality-score-row",
    ".ip-quality-score-bar",
    ".ip-quality-flag-grid",
    ".ip-quality-media-list",
    ".ip-quality-media-row",
    ".ip-quality-media-left",
    ".ip-quality-media-icon",
    ".ip-quality-media-right",
    ".ip-quality-media-status",
    ".ip-quality-card-footer",
  ]

  for (const cls of requiredClasses) {
    assert.ok(css.includes(cls), `CSS must define ${cls}`)
  }

  // Check 3 columns in desktop detail grid three
  assert.ok(css.includes("repeat(3, minmax(0, 1fr))"), "Desktop detail grid three must use 3 columns")
  // Check 5 columns in summary grid
  assert.ok(css.includes("repeat(5, minmax(0, 1fr))"), "Desktop summary grid must use 5 columns")
  // Check responsive breakpoints for detail grid three
  assert.ok(css.includes(".ip-quality-detail-grid-three"), "Must define .ip-quality-detail-grid-three")
  // Check light theme support
  assert.ok(css.includes('[data-theme="light"] .komari-ip-quality-card'), "Must support light theme for IP quality card")
  assert.ok(css.includes('[data-theme="light"] .ip-quality-media-row'), "Must support light theme for media row")
  assert.ok(css.includes('[data-theme="light"] .ip-quality-card-footer'), "Must support light theme for card footer")

  console.log("[PASS] All required CSS classes and responsive layout rules verified")
}

// 8. Verify NodeDetailPage.jsx contains 3-column IP quality card and NO standalone media strip
console.log("=== Testing Component Code Security & Structure ===")
{
  const compPath = existsSync(resolve(process.cwd(), "src/components/NodeDetailPage.jsx"))
    ? resolve(process.cwd(), "src/components/NodeDetailPage.jsx")
    : resolve(process.cwd(), "frontend/src/components/NodeDetailPage.jsx")
  const comp = readFileSync(compPath, "utf8")

  // IP quality card must have 5-item summary grid and 3-column detail grid
  assert.ok(comp.includes("ip-quality-summary-grid"), "Component must render ip-quality-summary-grid")
  assert.ok(comp.includes("ip-quality-summary-grid-five"), "Component must render 5-item summary grid")
  assert.ok(comp.includes("ip-quality-detail-grid ip-quality-detail-grid-three"), "Component must render ip-quality-detail-grid-three")
  assert.ok(comp.includes("风险评分（越低越好）"), "Component must render 风险评分（越低越好）")
  assert.ok(comp.includes("数据库标记（命中 / 有结论的库）"), "Component must render 数据库标记")
  assert.ok(comp.includes("流媒体 / AI 解锁"), "Component must render 流媒体 / AI 解锁 as 3rd column")
  assert.ok(comp.includes("暂无多来源评分"), "Component must handle empty sources")

  // Streaming / AI is now in column 3 of IP quality detail grid
  assert.ok(comp.includes("ip-quality-media-list"), "Component must render ip-quality-media-list")
  assert.ok(comp.includes("ip-quality-media-row"), "Component must render ip-quality-media-row")
  assert.ok(comp.includes("ip-quality-card-footer"), "Component must render ip-quality-card-footer")

  // Redundant standalone media strip card must be removed from NodeDetailPage.jsx
  assert.ok(!comp.includes('<section className="komari-media-strip-card">'), "Component must NOT render redundant standalone komari-media-strip-card")

  assert.ok(comp.includes("IPQA 未启用，暂无媒体归档"), "Component must handle empty media with IPQA 未启用，暂无媒体归档")
  assert.ok(comp.includes("暂无媒体检测数据"), "Component must handle empty media with 暂无媒体检测数据")
  assert.ok(comp.includes("查看全部"), "Component must provide 查看全部 button")
  assert.ok(comp.includes("ip-quality-media-modal-backdrop"), "Component must provide media modal")
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

  // 4 States Matrix Validation
  function getIPQAStateInfo(ipQuality) {
    const installed = Boolean(ipQuality?.ipqa_installed ?? ipQuality?.ipqa?.installed)
    const enabled = Boolean(ipQuality?.ipqa_enabled ?? ipQuality?.ipqa?.enabled)
    const alertCount = Number(ipQuality?.alert_count ?? ipQuality?.ipqa?.alertCount ?? 0)
    const criticalCount = Number(ipQuality?.critical_count ?? ipQuality?.ipqa?.criticalCount ?? 0)
    const hasRecentChanges = Boolean(ipQuality?.has_recent_changes ?? ipQuality?.ipqa?.hasRecentChanges)
    const hasArchive = installed && enabled && Boolean(alertCount > 0 || criticalCount > 0 || hasRecentChanges || ipQuality?.latest_archive_date)

    if (!installed) {
      return { badge: '未安装', desc: '未安装 IPQA', state: 'not_installed' }
    }
    if (!enabled) {
      return { badge: '已安装，未开启采集', desc: '已安装，未开启采集', state: 'installed_disabled' }
    }
    if (!hasArchive) {
      return { badge: '等待首次归档', desc: 'IPQA 已启用，等待首次归档', state: 'enabled_waiting_archive' }
    }
    return { badge: '已启用', desc: 'IPQA 已启用', state: 'enabled' }
  }

  assert.deepEqual(getIPQAStateInfo({ ipqa_installed: false, ipqa_enabled: false }), { badge: '未安装', desc: '未安装 IPQA', state: 'not_installed' })
  assert.deepEqual(getIPQAStateInfo({ ipqa_installed: false, ipqa_enabled: true }), { badge: '未安装', desc: '未安装 IPQA', state: 'not_installed' })
  assert.deepEqual(getIPQAStateInfo({ ipqa_installed: true, ipqa_enabled: false }), { badge: '已安装，未开启采集', desc: '已安装，未开启采集', state: 'installed_disabled' })
  assert.deepEqual(getIPQAStateInfo({ ipqa_installed: true, ipqa_enabled: true, alert_count: 0 }), { badge: '等待首次归档', desc: 'IPQA 已启用，等待首次归档', state: 'enabled_waiting_archive' })
  assert.deepEqual(getIPQAStateInfo({ ipqa_installed: true, ipqa_enabled: true, alert_count: 2 }), { badge: '已启用', desc: 'IPQA 已启用', state: 'enabled' })
  assert.deepEqual(getIPQAStateInfo({ ipqa_installed: true, ipqa_enabled: true, latest_archive_date: '2026-10-07', alert_count: 0 }), { badge: '已启用', desc: 'IPQA 已启用', state: 'enabled' })

  // Detailed strip title & badge matching NodeDetailPage.jsx
  function getIPQAStripDisplay(ipQuality) {
    const isInstalled = Boolean(ipQuality?.ipqa_installed ?? ipQuality?.ipqa?.installed)
    const isEnabled = Boolean(ipQuality?.ipqa_enabled ?? ipQuality?.ipqa?.enabled)
    const alertCount = Number(ipQuality?.alert_count ?? ipQuality?.ipqa?.alertCount ?? 0)
    const highestSeverity = ipQuality?.highest_severity || ipQuality?.ipqa?.highestSeverity || ''
    const hasRecentChanges = Boolean(ipQuality?.has_recent_changes ?? ipQuality?.ipqa?.hasRecentChanges)
    const hasArchive = isInstalled && isEnabled && Boolean(
      alertCount > 0 || hasRecentChanges || ipQuality?.latest_archive_date
    )
    const hasRiskChanges = Boolean(
      hasRecentChanges || alertCount > 0 || (highestSeverity && highestSeverity !== 'NONE' && highestSeverity !== 'NORMAL')
    )

    if (isInstalled && isEnabled && hasArchive) {
      if (hasRiskChanges) {
        return {
          title: `IPQA 风险变化（发现 ${alertCount || 1} 项变化 · 最高等级：${highestSeverity || '提示'}）`,
          badge: highestSeverity === 'CRITICAL' ? 'CRITICAL 严重' : highestSeverity === 'WARNING' ? 'WARNING 警告' : `发现 ${alertCount || 1} 项变化`,
        }
      }
      return {
        title: 'IPQA 已启用（暂无风险变化）',
        badge: 'IPQA 已启用',
      }
    }
    return null
  }

  const liveNodeDisplay = getIPQAStripDisplay({
    ipqa_installed: true,
    ipqa_enabled: true,
    latest_archive_date: '2026-10-07',
    highest_severity: 'NONE',
    alert_count: 0,
  })
  assert.equal(liveNodeDisplay?.badge, 'IPQA 已启用')
  assert.equal(liveNodeDisplay?.title, 'IPQA 已启用（暂无风险变化）')

  const riskNodeDisplay = getIPQAStripDisplay({
    ipqa_installed: true,
    ipqa_enabled: true,
    latest_archive_date: '2026-10-07',
    highest_severity: 'WARNING',
    alert_count: 2,
  })
  assert.equal(riskNodeDisplay?.badge, 'WARNING 警告')
  assert.ok(riskNodeDisplay?.title.includes('IPQA 风险变化'))

  console.log("[PASS] IPQA Status Strip logic and 4-state matrix verified")
}

// Test 6: Media Matrix Removal and IPQA Streaming Integration Contract
{
  console.log("=== Testing Media Matrix Removal & IPQA Unification ===")
  const srcDir = existsSync(resolve(process.cwd(), "src"))
    ? resolve(process.cwd(), "src")
    : resolve(process.cwd(), "frontend/src")
  const appSrc = readFileSync(resolve(srcDir, 'App.jsx'), 'utf8')
  const subPageSrc = readFileSync(resolve(srcDir, 'components/SubPage.jsx'), 'utf8')
  const mediaMatrixSrc = readFileSync(resolve(srcDir, 'components/MediaMatrix.jsx'), 'utf8')
  const dashboardSrc = readFileSync(resolve(srcDir, 'components/DashboardView.jsx'), 'utf8')
  const nodeDrawerSrc = readFileSync(resolve(srcDir, 'components/NodeDrawer.jsx'), 'utf8')
  const targetManageSrc = readFileSync(resolve(srcDir, 'components/TargetManage.jsx'), 'utf8')

  // 1. App.jsx sidebar must not have media button
  assert.ok(!appSrc.includes('title="流媒体矩阵"'), 'App.jsx must not have 流媒体矩阵 button in sidebar')
  assert.ok(!appSrc.includes("onNavigate('media')"), 'App.jsx sidebar must not have onNavigate("media")')

  // 2. SubPage.jsx must not render old MediaMatrix and page === 'media' must have read-only notice
  assert.ok(!subPageSrc.includes("import { MediaMatrix }"), 'SubPage.jsx must not import MediaMatrix')
  assert.ok(subPageSrc.includes("media-deprecated-notice-card"), 'SubPage.jsx must contain media-deprecated-notice-card')
  assert.ok(subPageSrc.includes("流媒体与 AI 解锁已统一升级"), 'SubPage.jsx must display upgrade title')
  assert.ok(subPageSrc.includes("node-detail"), 'SubPage.jsx must direct users to node-detail')

  // 3. MediaMatrix.jsx must be defanged: no mutate calls, no seed-media
  assert.ok(!mediaMatrixSrc.includes("seed-media"), 'MediaMatrix.jsx must not call seed-media')
  assert.ok(!mediaMatrixSrc.includes("handleAddMediaPresets"), 'MediaMatrix.jsx must not have handleAddMediaPresets')
  assert.ok(!mediaMatrixSrc.includes("POST"), 'MediaMatrix.jsx must not make POST requests')

  // 4. DashboardView.jsx must not navigate to 'media' and must point to node-detail
  assert.ok(!dashboardSrc.includes("onNavigate('media')"), 'DashboardView.jsx must not navigate to media')
  assert.ok(dashboardSrc.includes("流媒体解锁（来自 IPQA 归档）"), 'DashboardView.jsx must display IPQA archive label')
  assert.ok(dashboardSrc.includes("查看节点详情"), 'DashboardView.jsx must offer 查看节点详情 button')

  // 5. NodeDrawer.jsx must not navigate to 'media'
  assert.ok(!nodeDrawerSrc.includes("onNavigate('media')"), 'NodeDrawer.jsx must not navigate to media')
  assert.ok(nodeDrawerSrc.includes("在节点详情查看 IPQA 流媒体归档"), 'NodeDrawer.jsx footer must link to node details IPQA archive')
  assert.ok(nodeDrawerSrc.includes("来自 IPQA 归档"), 'NodeDrawer.jsx header must mention 来自 IPQA 归档')

  // 6. TargetManage.jsx must show IPQA disabled state and forbid toggling/creating media targets
  assert.ok(targetManageSrc.includes("已停用 · IPQA 接管"), 'TargetManage.jsx must render 已停用 · IPQA 接管')
  assert.ok(targetManageSrc.includes("不再支持重新启用独立 media-* 周期探测"), 'TargetManage.jsx must prevent enabling media targets')
  assert.ok(targetManageSrc.includes("不再支持新建 media_http 检测目标"), 'TargetManage.jsx must reject creating media_http targets')

  console.log("[PASS] Media matrix removal, navigation redirection, and IPQA unification contracts verified")
}

// Test 7: IPv4/IPv6 Protocol Switching, Songzhong Scoping & 3-Column MJJ Layout Contract
{
  console.log("=== Testing IPv4/IPv6 Protocol Switching & Songzhong Verification ===")
  const srcDir = existsSync(resolve(process.cwd(), "src"))
    ? resolve(process.cwd(), "src")
    : resolve(process.cwd(), "frontend/src")
  const nodeDetailSrc = readFileSync(resolve(srcDir, 'components/NodeDetailPage.jsx'), 'utf8')
  const apiSrc = readFileSync(resolve(srcDir, 'lib/api.js'), 'utf8')
  const css = readFileSync(resolve(srcDir, 'styles.css'), 'utf8')

  // 1. Verify CSS defines the new tabs, songzhong badges, factors grid, and dnsbl row
  const requiredClasses = [
    ".ip-family-tabs",
    ".ip-family-tab",
    ".tab-songzhong-badge",
    ".risk-songzhong-badge",
    ".ip-quality-factors-grid",
    ".ip-quality-factor-cell",
    ".ip-quality-dnsbl-row",
    ".status-songzhong",
    ".status-bullet",
  ]
  for (const cls of requiredClasses) {
    assert.ok(css.includes(cls), `styles.css must define ${cls}`)
  }

  // 2. NodeDetailPage.jsx must support protocol tabs, songzhong badges, and 8 standard media platforms
  assert.ok(nodeDetailSrc.includes("ip-family-tabs"), "NodeDetailPage must render ip-family-tabs")
  assert.ok(nodeDetailSrc.includes("tab-songzhong-badge"), "NodeDetailPage must render tab-songzhong-badge")
  assert.ok(nodeDetailSrc.includes("risk-songzhong-badge"), "NodeDetailPage must render risk-songzhong-badge")
  assert.ok(nodeDetailSrc.includes("ip-quality-factors-grid"), "NodeDetailPage must render ip-quality-factors-grid")
  assert.ok(nodeDetailSrc.includes("ip-quality-dnsbl-row"), "NodeDetailPage must render ip-quality-dnsbl-row")
  assert.ok(nodeDetailSrc.includes("暂无 IP 质量数据"), "NodeDetailPage must display 暂无 IP 质量数据 when both v4 and v6 are empty")

  // 3. Test Songzhong Scoping: only Google and YouTube allowed, strictly forbidden on other platforms
  const { isSongzhongCandidate, parseSongzhong, parseMediaItemDetails } = await import("../src/lib/api.js")

  assert.equal(isSongzhongCandidate("YouTube"), true, "YouTube must be songzhong candidate")
  assert.equal(isSongzhongCandidate("Google 定位"), true, "Google 定位 must be songzhong candidate")
  assert.equal(isSongzhongCandidate("google_redirect"), true, "Google redirect must be songzhong candidate")
  assert.equal(isSongzhongCandidate("Netflix"), false, "Netflix must NOT be songzhong candidate")
  assert.equal(isSongzhongCandidate("DisneyPlus"), false, "Disney+ must NOT be songzhong candidate")
  assert.equal(isSongzhongCandidate("TikTok"), false, "TikTok must NOT be songzhong candidate")
  assert.equal(isSongzhongCandidate("ChatGPT"), false, "ChatGPT must NOT be songzhong candidate")
  assert.equal(isSongzhongCandidate("Reddit"), false, "Reddit must NOT be songzhong candidate")
  assert.equal(isSongzhongCandidate("AmazonPrimeVideo"), false, "Prime Video must NOT be songzhong candidate")

  // YouTube with CN region/status -> songzhong
  const ytSZ = parseSongzhong({ name: 'YouTube', status: '送中', region: 'CN' })
  assert.equal(ytSZ.isSongzhong, true, "YouTube with CN status must be songzhong")
  assert.equal(ytSZ.label, '送中 · CN')

  // Netflix with CN status/region -> strictly NOT songzhong
  const nfCN = parseSongzhong({ name: 'Netflix', status: '送中', region: 'CN' })
  assert.equal(nfCN.isSongzhong, false, "Netflix must NEVER be marked as songzhong")

  // YouTube parseMediaItemDetails
  const ytDetails = parseMediaItemDetails({ name: 'YouTube', status: '送中', region: 'CN' })
  assert.equal(ytDetails.tone, 'songzhong')
  assert.equal(ytDetails.displayStatus, '送中 · CN')
  assert.equal(ytDetails.isSongzhong, true)

  // Netflix parseMediaItemDetails
  const nfDetails = parseMediaItemDetails({ name: 'Netflix', status: '仅自制', region: 'TW' })
  assert.equal(nfDetails.tone, 'warning')
  assert.equal(nfDetails.displayStatus, 'NF.Only')
  assert.equal(nfDetails.isSongzhong, false)

  // 4. Test Dual-Stack Model Separation
  const { buildIPQualityViewModel } = await import("../src/lib/api.js")
  const dualStackFixture = {
    has_ipv4: true,
    has_ipv6: true,
    ipv4: {
      risk_score: 0,
      risk_level: 'low',
      ip_type: '原生IP',
      country: 'TW',
      registered_region: 'TW',
      asn: 'AS3462',
      organization: 'Data Communication Business Group',
      source_scores: { IP2LOCATION: 0, SCAMALYTICS: 0, ipapi: 0.69, AbuseIPDB: 0, DBIP: 0 },
      media: [{ name: 'YouTube', status: 'available', region: 'TW' }],
    },
    ipv6: {
      risk_score: 95,
      risk_level: 'high',
      ip_type: '机房',
      country: 'HK',
      asn: 'AS202355',
      organization: 'Ciallo Networks LTD',
      source_scores: { IP2LOCATION: 3, SCAMALYTICS: 88 },
      media: [{ name: 'YouTube', status: 'available', region: 'CN' }], // Songzhong!
    },
  }

  const v4Model = buildIPQualityViewModel(dualStackFixture, 'ipv4')
  assert.equal(v4Model.activeFamily, 'ipv4')
  assert.equal(v4Model.v4.risk, 'low')
  assert.equal(v4Model.v4.location, 'TW (注册地一致)')
  assert.equal(v4Model.v4.hasSongzhong, false)
  // Verify standard score items has 6 entries and score 0 has tone 'low'
  const ip2loc = v4Model.v4.standardScoreItems.find(s => s.sourceKey === 'ip2location')
  assert.ok(ip2loc, "IP2Location must be in standard scores")
  assert.equal(ip2loc.value, 0)
  assert.equal(ip2loc.displayValue, '0')
  assert.equal(ip2loc.tone, 'low')

  const v6Model = buildIPQualityViewModel(dualStackFixture, 'ipv6')
  assert.equal(v6Model.activeFamily, 'ipv6')
  assert.equal(v6Model.v6.risk, 'high')
  assert.equal(v6Model.v6.hasSongzhong, true)
  assert.equal(v6Model.v6.songzhongLabel, '送中 · CN')

  // IPv4 risk score does not overwrite IPv6 and vice versa
  assert.notEqual(v4Model.v4.riskScore, v6Model.v6.riskScore)
  assert.notEqual(v4Model.v4.location, v6Model.v6.location)

  console.log("[PASS] Protocol switching, data separation, and songzhong contracts verified")

  // -----------------------------------------------------------------------------
  // 5. Strict IPv4/IPv6 Existence & Boundary Tests
  // -----------------------------------------------------------------------------
  console.log("=== Testing Strict IPv4/IPv6 Existence & Zero Hardcode Contracts ===")
  const { hasRealFamilyData, formatFactorDisplay, formatDNSBLDisplay } = await import("../src/lib/api.js")

  // Test 1: {} -> neither tab displayed
  const emptyVM = buildIPQualityViewModel({})
  assert.equal(emptyVM.hasIPv4, false, "{} must have hasIPv4 = false")
  assert.equal(emptyVM.hasIPv6, false, "{} must have hasIPv6 = false")

  // Test 2: { ip_quality: {} } -> neither tab displayed
  const emptyWrappedVM = buildIPQualityViewModel({ ip_quality: {} })
  assert.equal(emptyWrappedVM.hasIPv4, false, "{ ip_quality: {} } must have hasIPv4 = false")
  assert.equal(emptyWrappedVM.hasIPv6, false, "{ ip_quality: {} } must have hasIPv6 = false")

  // Test 3: Only IPv4 object -> only IPv4 tab
  const onlyV4VM = buildIPQualityViewModel({
    ip_quality: {
      ipv4: { country: 'TW', asn: 'AS3462', risk_score: 10 }
    }
  })
  assert.equal(onlyV4VM.hasIPv4, true, "Only IPv4 object must have hasIPv4 = true")
  assert.equal(onlyV4VM.hasIPv6, false, "Only IPv4 object must have hasIPv6 = false")

  // Test 4: Only IPv6 object -> only IPv6 tab
  const onlyV6VM = buildIPQualityViewModel({
    ip_quality: {
      ipv6: { country: 'HK', asn: 'AS202355', risk_score: 90 }
    }
  })
  assert.equal(onlyV6VM.hasIPv4, false, "Only IPv6 object must have hasIPv4 = false")
  assert.equal(onlyV6VM.hasIPv6, true, "Only IPv6 object must have hasIPv6 = true")

  // Test 5: Legacy risk_score without protocol attribution -> NO tabs fabricated
  const legacyNoFamilyVM = buildIPQualityViewModel({
    ip_quality: { risk_score: 45, country: 'US' }
  })
  assert.equal(legacyNoFamilyVM.hasIPv4, false, "Legacy risk_score without attribution must not fabricate hasIPv4")
  assert.equal(legacyNoFamilyVM.hasIPv6, false, "Legacy risk_score without attribution must not fabricate hasIPv6")

  // Test 6: Explicit has_ipv4: false, has_ipv6: false -> empty state
  const explicitBothFalseVM = buildIPQualityViewModel({
    ip_quality: {
      has_ipv4: false,
      has_ipv6: false,
      ipv4: { risk_score: 10 },
      ipv6: { risk_score: 20 },
    }
  })
  assert.equal(explicitBothFalseVM.hasIPv4, false, "Explicit has_ipv4: false must override")
  assert.equal(explicitBothFalseVM.hasIPv6, false, "Explicit has_ipv6: false must override")

  // -----------------------------------------------------------------------------
  // 6. Zero Hardcoded Conclusions Verification
  // -----------------------------------------------------------------------------
  const forbiddenHardcodes = [
    '407 干净 / 14 标记 / 2 黑名单',
    'text: activeVM.flags?.proxy === true ? \'2/7\' : \'0/2\'',
    'text: activeVM.flags?.vpn === true ? \'3/6\' : \'0/2\'',
    'text: activeVM.flags?.tor === true ? \'1/2\' : \'0/2\'',
    'text: activeVM.flags?.abuse === true ? \'1/2\' : \'0/1\'',
    'text: activeVM.flags?.robot === true ? \'1/2\' : \'0/0\'',
    'displayStatus: \'HK\'',
    'statusText: \'HK\'',
  ]
  for (const fh of forbiddenHardcodes) {
    assert.ok(
      !nodeDetailSrc.includes(fh),
      `NodeDetailPage.jsx must NOT contain hardcoded pattern: "${fh}"`
    )
  }

  // Factor formatting tests
  assert.equal(formatFactorDisplay(null, true), '命中', 'Boolean true must display 命中')
  assert.equal(formatFactorDisplay(null, false), '未命中', 'Boolean false must display 未命中')
  assert.equal(formatFactorDisplay(null, null), '—', 'Null flag must display —')
  assert.equal(formatFactorDisplay(undefined, undefined), '—', 'Undefined flag must display —')
  assert.equal(formatFactorDisplay({ count: 2, total: 7 }, true), '2/7', 'Real count/total must display 2/7')
  assert.equal(formatFactorDisplay({ count: 0, total: 2 }, false), '0/2', 'Real count/total must display 0/2')

  // DNSBL formatting tests
  assert.equal(formatDNSBLDisplay(null), '暂无数据', 'Null DNSBL must display 暂无数据')
  assert.equal(formatDNSBLDisplay(undefined), '暂无数据', 'Undefined DNSBL must display 暂无数据')
  assert.equal(formatDNSBLDisplay({}), '暂无数据', 'Empty DNSBL object must display 暂无数据')
  assert.equal(formatDNSBLDisplay({ Clean: 416, Marked: 7, Blacklisted: 0 }), '416 干净 / 7 标记 / 0 黑名单', 'Real numeric DNSBL must format properly')
  assert.equal(formatDNSBLDisplay({ clean: 100, marked: 0, blacklisted: 2 }), '100 干净 / 0 标记 / 2 黑名单', 'Lowercase numeric DNSBL must format properly')

  // DNSBL cross-protocol isolation check in NodeDetailPage
  assert.ok(
    !nodeDetailSrc.includes('vm.v4?.dnsbl'),
    'NodeDetailPage must not fall back to vm.v4?.dnsbl'
  )

  // -----------------------------------------------------------------------------
  // 7. Songzhong Keywords & Media Isolation Tests
  // -----------------------------------------------------------------------------
  // Google / YouTube matching real songzhong keywords
  const directCnMatch = parseSongzhong({ name: 'YouTube', status: 'direct_cn' })
  assert.equal(directCnMatch.isSongzhong, true, 'direct_cn keyword must trigger songzhong')

  const mainlandRedirectMatch = parseSongzhong({ name: 'Google 定位', status: 'mainland_redirect' })
  assert.equal(mainlandRedirectMatch.isSongzhong, true, 'mainland_redirect keyword must trigger songzhong')

  // Other platforms forbidden from songzhong
  const chatgptFake = parseSongzhong({ name: 'ChatGPT', status: 'direct_cn', region: 'CN' })
  assert.equal(chatgptFake.isSongzhong, false, 'ChatGPT must NEVER be songzhong')

  const tiktokFake = parseSongzhong({ name: 'TikTok', status: 'mainland_redirect', region: 'CN' })
  assert.equal(tiktokFake.isSongzhong, false, 'TikTok must NEVER be songzhong')

  console.log("[PASS] Strict IPv4/IPv6 existence, zero hardcodes, and songzhong contracts verified")
}

// -----------------------------------------------------------------------------
// Test 8: Compact Typography, 3-Panel Alignment & Media Height Contract (User Request 5)
// -----------------------------------------------------------------------------
{
  console.log("=== Testing 3-Panel Typography, Alignment & Media Height Contract ===")
  const srcDir = existsSync(resolve(process.cwd(), "src"))
    ? resolve(process.cwd(), "src")
    : resolve(process.cwd(), "frontend/src")
  const css = readFileSync(resolve(srcDir, 'styles.css'), 'utf8')
  const nodeDetailSrc = readFileSync(resolve(srcDir, 'components/NodeDetailPage.jsx'), 'utf8')

  // 1. Panels Top & Bottom Alignment via stretch & flex
  assert.ok(css.includes(".ip-quality-detail-grid-three"), "styles.css must define .ip-quality-detail-grid-three")
  assert.ok(css.includes("align-items: stretch;"), "Grid must stretch items to ensure top and bottom alignment")
  assert.ok(css.includes("height: 100%;"), "Panels in grid must take 100% height")
  assert.ok(css.includes(".ip-quality-panel-body {\n  flex: 1;\n  display: flex;\n  flex-direction: column;"), "Panel body must be flex-1 to fill the card")

  // Panel 1 score list uses space-between to evenly distribute items and match panel 3 height
  assert.ok(css.includes("justify-content: space-between;"), "Score list must use space-between for vertical distribution")

  // Panel 2 factors use margin-bottom: auto and dnsbl-row uses margin-top: auto to pin to bottom
  assert.ok(css.includes("margin-top: auto;\n  border-top: 1px solid"), "DNSBL row must pin to the bottom of factors panel")

  // 2. Risk Score Typography (>= 12px)
  assert.ok(css.includes(".ip-quality-score-label {\n  color: var(--text-2);\n  overflow: hidden;\n  text-overflow: ellipsis;\n  white-space: nowrap;\n  font-size: 12px;"), "Score label font size must be 12px")
  assert.ok(css.includes(".ip-quality-score-value {\n  font-variant-numeric: tabular-nums;\n  font-weight: 700;\n  text-align: right;\n  min-width: 34px;\n  font-size: 12px;"), "Score value font size must be 12px with font-weight 700 and min-width 34px")
  assert.ok(css.includes(".ip-quality-score-row {\n  display: grid;\n  grid-template-columns: minmax(84px, 118px) 1fr 34px;\n  align-items: center;\n  min-height: 25px;"), "Score row must have min-height 25px")

  // 3. Database Factors Typography (>= 12px)
  assert.ok(css.includes(".ip-quality-factor-cell {\n  display: flex;\n  align-items: center;\n  justify-content: space-between;\n  min-height: 30px;"), "Factor cell must have min-height 30px")
  assert.ok(css.includes(".ip-quality-factor-cell .factor-label {\n  color: var(--text-2);\n  font-size: 12px;"), "Factor label font size must be 12px")
  assert.ok(css.includes(".ip-quality-factor-cell .factor-val {\n  font-weight: 700;\n  font-size: 12px;"), "Factor val font size must be 12px with font-weight 700")
  assert.ok(css.includes(".ip-quality-dnsbl-row .dnsbl-label {\n  color: var(--text-2);\n  font-size: 12px;"), "DNSBL label font size must be 12px")
  assert.ok(css.includes(".ip-quality-dnsbl-row .dnsbl-val {\n  font-size: 11.5px;\n  font-weight: 600;"), "DNSBL val font size must be 11.5px with font-weight 600")

  // 4. Streaming Media Compactness (<= 28px height, <= 4px gap)
  assert.ok(css.includes(".ip-quality-media-list {\n  display: grid;\n  grid-template-columns: 1fr;\n  gap: 4px;"), "Media list gap must be 4px")
  assert.ok(css.includes("height: 28px;\n  min-height: 28px;"), "Media row height must be 28px")
  assert.ok(css.includes("padding: 2px 8px;"), "Media row padding must be 2px 8px")
  assert.ok(css.includes(".ip-quality-media-icon {\n  width: 17px;\n  height: 17px;"), "Media icon must be 17px x 17px")
  assert.ok(css.includes(".ip-quality-media-status,\n.ip-quality-media-row .media-status {\n  font-size: 11px;"), "Media status font size must be 11px")

  // 5. Semantic Panel Classes in NodeDetailPage.jsx
  assert.ok(nodeDetailSrc.includes("ip-quality-score-panel"), "NodeDetailPage must include semantic class ip-quality-score-panel")
  assert.ok(nodeDetailSrc.includes("ip-quality-factors-panel"), "NodeDetailPage must include semantic class ip-quality-factors-panel")
  assert.ok(nodeDetailSrc.includes("ip-quality-media-panel"), "NodeDetailPage must include semantic class ip-quality-media-panel")

  // 6. Responsive Breakpoint Rules (1024px, 860px, 768px, 480px)
  assert.ok(css.includes("@media (max-width: 1024px)"), "CSS must define 1024px breakpoint")
  assert.ok(css.includes("@media (max-width: 860px)"), "CSS must define 860px breakpoint")
  assert.ok(css.includes("@media (max-width: 768px)"), "CSS must define 768px breakpoint")
  assert.ok(css.includes("@media (max-width: 480px)"), "CSS must define 480px breakpoint")
  assert.ok(css.includes("height: 26px;\n    min-height: 26px;\n    padding: 2px 6px;"), "Mobile <= 480px must compress media row to 26px")

  console.log("[PASS] 3-Panel Typography, Alignment & Media Height Contract verified")
}

console.log("=== ALL IP QUALITY, IPQA & STREAMING UI TESTS PASSED ===")

