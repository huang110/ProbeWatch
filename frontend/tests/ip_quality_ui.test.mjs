import assert from "node:assert/strict"

// Mock the formatting logic from NodeDetailPage.jsx
function formatIPQualityType(type) {
  if (!type) return '未知 / 未识别'
  const t = String(type).trim().toLowerCase()
  if (t === 'hosting' || t === 'datacenter') return '数据中心 / 机房'
  if (t === 'isp') return '宽带 ISP'
  if (t === 'residential') return '家庭住宅'
  if (t === 'unknown') return '未知 / 未识别'
  return type
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
  if (value === true) return { cls: 'is-risk', text: '是' }
  if (value === false) return { cls: 'is-ok', text: '否' }
  return { cls: 'is-unknown', text: '未知' }
}

console.log("=== Testing IP Quality UI State Transitions ===")

// 1. 无数据 (Empty State)
{
  const { badgeClass, badgeText } = getRiskBadge(null, false, null)
  assert.equal(badgeClass, 'badge-neutral', "Empty state badge must be gray (badge-neutral)")
  assert.notEqual(badgeText, '低风险', "Empty state must not be low risk")
  assert.notEqual(badgeText, '安全', "Empty state must not be marked safe")
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
    sources: { risk_score: 5 }
  }
  const { badgeClass, badgeText } = getRiskBadge(ipQuality, false, null)
  assert.equal(badgeClass, 'badge-mint', "Low risk badge must be mint")
  assert.equal(badgeText, '低风险')
  assert.equal(formatIPQualityType(ipQuality.ip_type), '数据中心 / 机房')
  assert.deepEqual(getFlagStatus(ipQuality.proxy), { cls: 'is-ok', text: '否' })
  assert.equal(getEmptyOrStaleMessage(ipQuality, null), null)
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
  console.log("[PASS] Unknown fields verified")
}

// 6. Threat Flags (True, False, Null/Undefined)
{
  assert.deepEqual(getFlagStatus(true), { cls: 'is-risk', text: '是' })
  assert.deepEqual(getFlagStatus(false), { cls: 'is-ok', text: '否' })
  assert.deepEqual(getFlagStatus(null), { cls: 'is-unknown', text: '未知' })
  assert.deepEqual(getFlagStatus(undefined), { cls: 'is-unknown', text: '未知' })
  console.log("[PASS] Threat flags state mapping verified")
}

console.log("=== ALL IP QUALITY UI TESTS PASSED ===")
