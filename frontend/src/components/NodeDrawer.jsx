import { useEffect, useRef, useState } from 'react'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  Broadcast,
  CalendarBlank,
  CircleNotch,
  Coins,
  Cpu,
  FilmStrip,
  FlowArrow,
  GlobeHemisphereWest,
  HardDrive,
  Lightning,
  LinuxLogo,
  Memory,
  Pulse,
  Sparkle,
  WindowsLogo,
  X,
} from '@phosphor-icons/react'
import {
  dash,
  formatBytes,
  formatLoad,
  formatPercent,
  formatRate,
  relativeHeartbeat,
  safeText,
  statusLabel,
} from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling } from '../lib/billing.js'
import { ProgressBar, StatusDot } from './Common.jsx'
import { BillingModal } from './BillingModal.jsx'

const POPULAR_MEDIA = [
  { id: 'youtube', name: 'YouTube', iconBg: '#CC0000', symbol: 'YT' },
  { id: 'netflix', name: 'Netflix', iconBg: '#E50914', symbol: 'NF' },
  { id: 'disney', name: 'Disney+', iconBg: '#113CCF', symbol: 'D+' },
  { id: 'openai', name: 'ChatGPT', iconBg: '#10A37F', symbol: 'AI' },
  { id: 'tiktok', name: 'TikTok', iconBg: '#18181b', symbol: 'TK' },
  { id: 'spotify', name: 'Spotify', iconBg: '#1DB954', symbol: 'SP' },
]

const getMediaStatus = (platformId, mediaList) => {
  const match = (mediaList || []).find((m) => {
    const dId = (m.detector_id || m.target_id || '').toLowerCase()
    const dName = (m.result?.detector || '').toLowerCase()
    return dId.includes(platformId) || dName.includes(platformId)
  })
  if (!match) return { text: '未测试', tone: 'muted', latency: null }
  const res = match.result || {}
  const status = res.status
  const latency = res.latency_ms ?? null
  const region = res.region

  if (status === 'available') {
    return {
      text: region ? `解锁 (${region})` : '原生解锁',
      tone: 'available',
      latency,
    }
  }
  if (status === 'unavailable') {
    return { text: '未解锁', tone: 'unavailable', latency }
  }
  if (status === 'error' || status === 'blocked' || status === 'timeout') {
    return {
      text: res.reason === 'body exceeds limit' ? '仅自制剧' : '超时/异常',
      tone: 'warning',
      latency,
    }
  }
  return { text: status || '未知', tone: 'muted', latency }
}

function DrawerChecksLatencyLine({ checks = [] }) {
  if (!checks || checks.length < 2) return null
  const validLatencies = checks
    .map((c) => (c.latency_avg_ms !== null && c.latency_avg_ms !== undefined ? Number(c.latency_avg_ms) : null))
    .filter((l) => l !== null && l >= 0)
  if (!validLatencies.length) return null

  const maxLat = Math.max(...validLatencies, 20)
  const baselineY = 44
  const topY = 6
  const plotH = baselineY - topY
  const count = checks.length
  const slot = 100 / Math.max(count, 1)

  const pts = checks.map((c, i) => {
    const x = i * slot + slot / 2
    const lat = c.latency_avg_ms !== null && c.latency_avg_ms !== undefined ? Number(c.latency_avg_ms) : 0
    const y = baselineY - Math.min((lat / (maxLat * 1.15)) * plotH, plotH)
    return { x, y }
  })

  // 0.95px 极细平滑贝塞尔曲线
  let path = `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const cx = (p1.x + p2.x) / 2
    path += ` C ${cx.toFixed(2)} ${p1.y.toFixed(2)}, ${cx.toFixed(2)} ${p2.y.toFixed(2)}, ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
  }

  const area = `${path} L ${pts[pts.length - 1].x.toFixed(2)} ${baselineY} L ${pts[0].x.toFixed(2)} ${baselineY} Z`

  return (
    <div className="drawer-mini-latency-card" title="哪吒 2.0 三网探测时延走势">
      <div className="drawer-mini-latency-header mono">
        <span className="text-muted">三网时延流线</span>
        <b className="text-mint">{Math.round(maxLat)} ms 峰值</b>
      </div>
      <svg className="drawer-mini-latency-svg" viewBox="0 0 100 48" preserveAspectRatio="none">
        <defs>
          <linearGradient id="drawer-lat-grad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity="0.12" />
            <stop offset="100%" stopColor="#10b981" stopOpacity="0.0" />
          </linearGradient>
        </defs>
        <line x1="0" y1={baselineY} x2="100" y2={baselineY} stroke="currentColor" strokeOpacity="0.08" strokeWidth="0.8" />
        <path d={area} fill="url(#drawer-lat-grad)" />
        <path d={path} fill="none" stroke="#10b981" strokeWidth="0.95" strokeLinecap="round" />
        {pts.map((p, idx) => (
          <circle key={idx} cx={p.x} cy={p.y} r="1.6" fill="#10b981" stroke="#ffffff" strokeWidth="0.6" />
        ))}
      </svg>
    </div>
  )
}

export function NodeDrawer({ node, rates = {}, onClose, onOpenDetails, onNavigate }) {
  const closeButtonRef = useRef(null)
  const [showBillingModal, setShowBillingModal] = useState(false)
  const [billingVersion, setBillingVersion] = useState(0)

  // 扩展诊断数据状态：网络检测、MTR 路由、流媒体
  const [checksData, setChecksData] = useState([])
  const [loadingChecks, setLoadingChecks] = useState(false)
  const [mtrData, setMtrData] = useState([])
  const [loadingMtr, setLoadingMtr] = useState(false)
  const [mediaData, setMediaData] = useState([])
  const [loadingMedia, setLoadingMedia] = useState(false)

  const key = safeText(node?.uuid) || safeText(node?.id) || ''

  // 监听 Escape 键与焦点管理
  useEffect(() => {
    const keyHandler = (event) => event.key === 'Escape' && onClose()
    document.addEventListener('keydown', keyHandler)
    document.body.style.overflow = 'hidden'
    closeButtonRef.current?.focus()
    return () => {
      document.removeEventListener('keydown', keyHandler)
      document.body.style.overflow = ''
    }
  }, [onClose])

  // 异步获取当前小鸡的实时网络检测、MTR 路由与流媒体解锁数据
  useEffect(() => {
    if (!key) return
    const controller = new AbortController()

    const buildNodePath = (action) => ['/api', 'nodes', encodeURIComponent(key), action].join('/')

    // 1. 网络检测概览
    setLoadingChecks(true)
    fetch(buildNodePath(['checks', 'summary'].join('/')), {
      credentials: 'same-origin',
      signal: controller.signal,
    })
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => {
        if (Array.isArray(list)) setChecksData(list)
      })
      .catch(() => {})
      .finally(() => setLoadingChecks(false))

    // 2. MTR 骨干路由追踪
    setLoadingMtr(true)
    fetch(buildNodePath('mtr'), {
      credentials: 'same-origin',
      signal: controller.signal,
    })
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => {
        if (Array.isArray(list)) setMtrData(list)
      })
      .catch(() => {})
      .finally(() => setLoadingMtr(false))

    // 3. 全球流媒体解锁
    setLoadingMedia(true)
    fetch(buildNodePath('media'), {
      credentials: 'same-origin',
      signal: controller.signal,
    })
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => {
        if (Array.isArray(list)) setMediaData(list)
      })
      .catch(() => {})
      .finally(() => setLoadingMedia(false))

    return () => controller.abort()
  }, [key])

  if (!node) return null

  const rate = rates[key] || null
  const billing = getNodeBilling(key, node.name)
  const calc = calculateRemainingValue(billing)
  const totalTransfer = (node.rx || 0) + (node.tx || 0)
  const isWindows = (node.os || '').toLowerCase().includes('windows')

  // MTR 活跃报告解析
  const activeMtrReport = mtrData.length > 0 ? mtrData[0] : null
  const mtrResult = activeMtrReport?.result || {}
  const mtrHops = Array.isArray(mtrResult?.hops) ? mtrResult.hops : []
  const isMtrReached = mtrResult?.reached === true

  // 流媒体解锁统计
  const unlockedMediaCount = POPULAR_MEDIA.filter(
    (p) => getMediaStatus(p.id, mediaData).tone === 'available'
  ).length

  return (
    <>
      <div className="drawer-backdrop" onClick={onClose} aria-hidden="true" />
      <aside
        className="node-drawer modern-node-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="node-drawer-title"
        tabIndex="-1"
        onClick={(event) => event.stopPropagation()}
      >
        {/* 顶部标题栏 */}
        <div className="drawer-header modern-drawer-header">
          <div className="drawer-title-group">
            <div className="drawer-title-row">
              <span className="drawer-flag">{node.flag || '🌐'}</span>
              <h2 id="node-drawer-title" className="drawer-name">
                {node.name}
              </h2>
              {node.tag && <span className="mjj-tag-badge">{node.tag}</span>}
            </div>
            <div className="drawer-subtitle">
              <StatusDot status={node.status} size="sm" />
              <span className={`drawer-status-text ${node.status}`}>
                {statusLabel(node.status)}
              </span>
              <span className="drawer-sep">·</span>
              <span className="drawer-heartbeat">
                心跳 {relativeHeartbeat(node.lastReportedAt)}
              </span>
            </div>
          </div>
          <div className="drawer-header-actions inline-flex items-center gap-2">
            <kbd className="linear-kbd">ESC</kbd>
            <button
              ref={closeButtonRef}
              className="icon-button drawer-close-btn"
              onClick={onClose}
              aria-label="关闭详情"
            >
              <X size={16} />
            </button>
          </div>
        </div>

        <div className="drawer-scroll-content">
          {/* 1. 实时流速大看板 */}
          <div className="drawer-rates-card">
            <div className="drawer-rate-box rate-down">
              <div className="rate-icon-badge">
                <ArrowDown size={16} weight="bold" />
              </div>
              <div className="rate-content">
                <span className="rate-label">实时下行流速</span>
                <b className="rate-val mono text-mint">
                  {formatRate(rate?.down ?? null)}
                </b>
              </div>
            </div>
            <div className="drawer-rate-box rate-up">
              <div className="rate-icon-badge">
                <ArrowUp size={16} weight="bold" />
              </div>
              <div className="rate-content">
                <span className="rate-label">实时上行流速</span>
                <b className="rate-val mono text-blue">
                  {formatRate(rate?.up ?? null)}
                </b>
              </div>
            </div>
          </div>

          {/* 2. 硬件与系统概览卡片 */}
          <div className="drawer-section-card">
            <div className="section-card-title">
              <Cpu size={15} className="text-mint" />
              <span>系统与运行规格</span>
            </div>
            <div className="drawer-specs-grid">
              <div className="spec-item">
                <span className="spec-label">操作系统</span>
                <div className="spec-value inline-flex items-center gap-1.5">
                  {isWindows ? <WindowsLogo size={14} /> : <LinuxLogo size={14} />}
                  <span>
                    {node.os || 'Linux'} · {node.arch || 'amd64'}
                  </span>
                </div>
              </div>

              <div className="spec-item">
                <span className="spec-label">连续运行时间</span>
                <div className="spec-value mono">{node.uptime || '—'}</div>
              </div>

              <div className="spec-item">
                <span className="spec-label">平均负载 (1/5/15)</span>
                <div className="spec-value mono">
                  {formatLoad(
                    node.load1 ?? node.resource?.load1,
                    node.load5 ?? node.resource?.load5,
                    node.load15 ?? node.resource?.load15
                  )}
                </div>
              </div>

              <div className="spec-item">
                <span className="spec-label">探针版本</span>
                <div className="spec-value mono text-mint">
                  {node.agentVersion || node.version || 'ProbeWatch Agent v0.2.2'}
                </div>
              </div>
            </div>
          </div>

          {/* 3. 核心资源占用条 */}
          <div className="drawer-section-card">
            <div className="section-card-title">
              <Pulse size={15} className="text-blue" />
              <span>资源摘要</span>
            </div>

            {/* CPU */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name">
                  <Cpu size={13} /> CPU 占用
                </span>
                <span className="meter-val mono">{formatPercent(node.cpu)}</span>
              </div>
              <ProgressBar value={node.cpu} tone="mint" height={6} />
            </div>

            {/* 内存 */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name">
                  <Memory size={13} /> 物理内存
                </span>
                <span className="meter-val mono">
                  {node.memUsed !== null && node.memTotal
                    ? `${formatBytes(node.memUsed)} / ${formatBytes(node.memTotal)}`
                    : formatPercent(node.memory)}
                </span>
              </div>
              <ProgressBar value={node.memory} tone="blue" height={6} />
            </div>

            {/* Swap */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name">
                  <Lightning size={13} /> 虚拟内存 (Swap)
                </span>
                <span className="meter-val mono">
                  {node.swapUsed !== null && node.swapTotal
                    ? `${formatBytes(node.swapUsed)} / ${formatBytes(node.swapTotal)}`
                    : formatPercent(node.swap)}
                </span>
              </div>
              <ProgressBar value={node.swap || 0} tone="violet" height={6} />
            </div>

            {/* 硬盘 */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name">
                  <HardDrive size={13} /> 根磁盘空间
                </span>
                <span className="meter-val mono">
                  {node.diskUsed !== null && node.diskTotal
                    ? `${formatBytes(node.diskUsed)} / ${formatBytes(node.diskTotal)}`
                    : formatPercent(node.disk)}
                </span>
              </div>
              <ProgressBar value={node.disk} tone="amber" height={6} />
            </div>

            {/* 流量统计 */}
            <div className="drawer-transfer-row">
              <div className="transfer-stat">
                <span className="transfer-label">累计入站 (Rx)</span>
                <b className="transfer-val mono">{formatBytes(node.rx || 0)}</b>
              </div>
              <div className="transfer-divider" />
              <div className="transfer-stat">
                <span className="transfer-label">累计出站 (Tx)</span>
                <b className="transfer-val mono">{formatBytes(node.tx || 0)}</b>
              </div>
              <div className="transfer-divider" />
              <div className="transfer-stat">
                <span className="transfer-label">双向总计</span>
                <b className="transfer-val mono text-mint">
                  {formatBytes(totalTransfer)}
                </b>
              </div>
            </div>
          </div>

          {/* 4. MJJ 小鸡账单与剩余价值卡片 */}
          <div className="drawer-section-card drawer-billing-card">
            <div className="section-card-title justify-between">
              <div className="inline-flex items-center gap-1.5">
                <Coins size={15} className="text-amber" />
                <span>小鸡账单与剩余价值 (MJJ 模式)</span>
              </div>
              <button
                type="button"
                className="button button-quiet btn-sm"
                onClick={() => setShowBillingModal(true)}
              >
                <Sparkle size={13} className="text-mint" />
                <span>编辑账单</span>
              </button>
            </div>

            <div className="drawer-billing-body">
              <div className="billing-meta-row">
                <div className="billing-meta-item">
                  <span className="meta-label">付费周期 & 续费价格</span>
                  <strong className="meta-val mono">
                    {billing.cycle === 'free'
                      ? '永久免费传家宝'
                      : `${calc.symbol}${billing.price} / ${billing.cycle === 'monthly' ? '月' : billing.cycle === 'quarterly' ? '季' : billing.cycle === 'semi_annual' ? '半年' : billing.cycle === 'annual' ? '年' : `${billing.cycle}年`}`}
                  </strong>
                </div>

                <div className="billing-meta-item">
                  <span className="meta-label">到期时间</span>
                  <div className="inline-flex items-center gap-1.5 meta-val mono">
                    <CalendarBlank size={13} className="text-3" />
                    <span>{billing.dueDate || billing.expireDate || '未配置'}</span>
                  </div>
                </div>
              </div>

              <div className="billing-calc-box">
                <div className="calc-left">
                  <span className="calc-label">当前折合剩余价值 (CNY)</span>
                  <div className="calc-amount mono">
                    ¥<b>{calc.remainingValueCNY.toFixed(2)}</b>
                  </div>
                </div>
                <div className="calc-right">
                  <span className={`days-pill days-${calc.statusTone} mono`}>
                    {calc.statusText}
                  </span>
                  {calc.daysRemaining > 0 && (
                    <small className="mono text-muted">
                      剩余 {calc.daysRemaining} 天
                    </small>
                  )}
                </div>
              </div>
            </div>
          </div>

          {/* 5. 🌐 网络检测与链路质量卡片 */}
          <div className="drawer-section-card drawer-checks-card">
            <div className="section-card-title justify-between">
              <div className="inline-flex items-center gap-1.5">
                <GlobeHemisphereWest size={15} className="text-mint" />
                <span>三网质量与延迟检测</span>
              </div>
              {checksData.length > 0 && (
                <span className="drawer-count-badge mono">{checksData.length} 监控目标</span>
              )}
            </div>

            {loadingChecks ? (
              <div className="drawer-loading-box">
                <CircleNotch size={16} className="spin text-mint" />
                <span>正在检测三网与链路数据…</span>
              </div>
            ) : checksData.length > 0 ? (
              <>
                <DrawerChecksLatencyLine checks={checksData} />
                <div className="drawer-checks-list">
                {checksData.map((item, idx) => {
                  const latency =
                    item.latency_avg_ms !== null && item.latency_avg_ms !== undefined
                      ? Number(item.latency_avg_ms).toFixed(1)
                      : null
                  const loss =
                    item.loss_rate !== null && item.loss_rate !== undefined
                      ? Number(item.loss_rate * 100).toFixed(1)
                      : '0.0'
                  const jitter =
                    item.jitter_ms !== null && item.jitter_ms !== undefined
                      ? Number(item.jitter_ms).toFixed(0)
                      : null
                  const latencyTone =
                    latency === null
                      ? 'muted'
                      : latency < 35
                        ? 'mint'
                        : latency < 90
                          ? 'blue'
                          : latency < 180
                            ? 'amber'
                            : 'rose'

                  return (
                    <div key={item.target_id || idx} className="drawer-check-row">
                      <div className="check-target-info">
                        <span className="check-target-name">
                          {item.name || item.target_id}
                        </span>
                        <div className="check-sub-meta">
                          <span className="check-kind-tag mono">
                            {(item.kind || 'tcp').toUpperCase()}
                          </span>
                          {item.host && (
                            <span className="check-host mono text-muted">{item.host}</span>
                          )}
                        </div>
                      </div>
                      <div className="check-metrics-right">
                        <div className="metric-col">
                          <span className="metric-label">平均延迟</span>
                          <b className={`mono text-${latencyTone}`}>
                            {latency !== null ? `${latency} ms` : '—'}
                          </b>
                        </div>
                        <div className="metric-col">
                          <span className="metric-label">丢包率</span>
                          <span
                            className={`mono ${Number(loss) > 0 ? 'text-rose font-bold' : 'text-muted'}`}
                          >
                            {loss}%
                          </span>
                        </div>
                        {jitter !== null && (
                          <div className="metric-col">
                            <span className="metric-label">抖动</span>
                            <span className="mono text-muted">±{jitter}ms</span>
                          </div>
                        )}
                      </div>
                    </div>
                  )
                })}
                </div>
              </>
            ) : (
              <div className="drawer-empty-sub">
                当前尚未配置常规网络探测目标，可前往「网络检测」添加针对 Cloudflare、国内三网等端点的连通性监控。
              </div>
            )}

            {onNavigate && (
              <button
                type="button"
                className="drawer-card-footer-link"
                onClick={() => {
                  onClose()
                  onNavigate('network')
                }}
              >
                <span>前往网络检测管理与时延走势</span>
                <ArrowUpRight size={13} />
              </button>
            )}
          </div>

          {/* 6. 🔀 MTR 骨干路由追踪卡片 */}
          <div className="drawer-section-card drawer-mtr-card">
            <div className="section-card-title justify-between">
              <div className="inline-flex items-center gap-1.5">
                <FlowArrow size={15} className="text-blue" />
                <span>MTR 骨干路由追踪</span>
              </div>
              {activeMtrReport && (
                <span
                  className={`target-chip-status ${isMtrReached ? 'status-reached' : ''}`}
                >
                  {isMtrReached ? '已抵达成环' : '路由追踪中'}
                </span>
              )}
            </div>

            {loadingMtr ? (
              <div className="drawer-loading-box">
                <CircleNotch size={16} className="spin text-blue" />
                <span>正在追踪骨干网路由跳数…</span>
              </div>
            ) : activeMtrReport ? (
              <div className="drawer-mtr-body">
                <div className="drawer-mtr-meta-strip">
                  <div className="mtr-dest-badge">
                    <Broadcast size={14} className="text-blue" />
                    <span>目标: {mtrResult.host || activeMtrReport.target_id}</span>
                    {mtrResult.destination_ip && (
                      <span className="destination-ip mono text-muted">
                        ({mtrResult.destination_ip})
                      </span>
                    )}
                  </div>
                  <span className="mono text-muted" style={{ fontSize: '11px' }}>
                    共 {mtrHops.length} 跳
                  </span>
                </div>

                <div className="drawer-mtr-hops-wrap">
                  <table className="drawer-mtr-table">
                    <thead>
                      <tr>
                        <th style={{ width: '42px' }}>跳数</th>
                        <th>路由中继 IP</th>
                        <th style={{ width: '65px' }}>单跳延迟</th>
                        <th style={{ width: '55px' }}>状态</th>
                      </tr>
                    </thead>
                    <tbody>
                      {mtrHops.slice(0, 8).map((hop, idx) => {
                        const ttl = hop.ttl ?? idx + 1
                        const isTimedOut = hop.timed_out === true || !hop.ip
                        const lat =
                          hop.latency_ms !== null && hop.latency_ms !== undefined
                            ? Number(hop.latency_ms).toFixed(1)
                            : null
                        const latTone =
                          lat === null
                            ? 'muted'
                            : lat < 35
                              ? 'mint'
                              : lat < 90
                                ? 'blue'
                                : lat < 180
                                  ? 'amber'
                                  : 'rose'
                        return (
                          <tr key={ttl} className={isTimedOut ? 'hop-timeout' : ''}>
                            <td className="mono text-muted">#{ttl}</td>
                            <td className="mono hop-ip-col">
                              {isTimedOut ? '* * * (超时无回显)' : hop.ip}
                            </td>
                            <td className={`mono text-${latTone}`}>
                              {lat !== null ? `${lat}ms` : '—'}
                            </td>
                            <td>
                              {ttl === mtrHops.length && isMtrReached ? (
                                <span className="hop-pill-dest">抵达</span>
                              ) : isTimedOut ? (
                                <span className="hop-pill-timeout">超时</span>
                              ) : (
                                <span className="hop-pill-relay">中继</span>
                              )}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                  {mtrHops.length > 8 && (
                    <div className="drawer-mtr-more-hint">
                      还有 {mtrHops.length - 8} 个中继路由节点，可在完整视图查看…
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <div className="drawer-empty-sub">
                当前节点暂未采集到 MTR 路由报告。支持快捷追踪中国电信 CN2、联通 9929、移动 CMIN2
                等国内骨干网路由链路。
              </div>
            )}

            {onNavigate && (
              <button
                type="button"
                className="drawer-card-footer-link"
                onClick={() => {
                  onClose()
                  onNavigate('mtr')
                }}
              >
                <span>查看完整 MTR 路由拓扑与网络指纹</span>
                <ArrowUpRight size={13} />
              </button>
            )}
          </div>

          {/* 7. 🎬 全球流媒体与 AI 服务解锁 */}
          <div className="drawer-section-card drawer-media-card">
            <div className="section-card-title justify-between">
              <div className="inline-flex items-center gap-1.5">
                <FilmStrip size={15} className="text-amber" />
                <span>全球流媒体与 AI 解锁能力</span>
              </div>
              <span className="drawer-count-badge mono">
                {unlockedMediaCount}/{POPULAR_MEDIA.length} 解锁
              </span>
            </div>

            {loadingMedia ? (
              <div className="drawer-loading-box">
                <CircleNotch size={16} className="spin text-amber" />
                <span>正在检测流媒体与 AI 解锁状态…</span>
              </div>
            ) : (
              <div className="drawer-media-grid">
                {POPULAR_MEDIA.map((p) => {
                  const status = getMediaStatus(p.id, mediaData)
                  return (
                    <div key={p.id} className="drawer-media-pill">
                      <div className="drawer-media-platform">
                        <span
                          className="drawer-media-icon"
                          style={{ backgroundColor: p.iconBg, color: '#fff' }}
                        >
                          {p.symbol}
                        </span>
                        <span>{p.name}</span>
                      </div>
                      <div className="inline-flex items-center gap-1.5">
                        <span className={`media-tag-${status.tone}`}>{status.text}</span>
                        {status.latency !== null && (
                          <small
                            className="mono text-muted"
                            style={{ fontSize: '10px' }}
                          >
                            {status.latency}ms
                          </small>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            )}

            {onNavigate && (
              <button
                type="button"
                className="drawer-card-footer-link"
                onClick={() => {
                  onClose()
                  onNavigate('media')
                }}
              >
                <span>查看全网流媒体矩阵大屏</span>
                <ArrowUpRight size={13} />
              </button>
            )}
          </div>
        </div>

        {/* 底部操作栏 */}
        <div className="drawer-actions modern-drawer-actions">
          <button
            className="button button-primary drawer-button"
            onClick={() => onOpenDetails && onOpenDetails(node)}
          >
            <span>打开完整详情</span>
            <ArrowUpRight size={16} />
          </button>
          <button className="button button-quiet" onClick={onClose}>
            关闭
          </button>
        </div>
      </aside>

      {/* 账单配置与出鸡计算弹窗 */}
      {showBillingModal && (
        <BillingModal
          node={node}
          onClose={() => setShowBillingModal(false)}
          onSaved={() => {
            setShowBillingModal(false)
            setBillingVersion((v) => v + 1)
          }}
        />
      )}
    </>
  )
}
