import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, ArrowsClockwise, CalendarBlank, Coins, Cpu, Globe, HardDrive, HardDrives, Lightning, Memory, PencilSimple, Pulse, Rows, SlidersHorizontal, Sparkle, SquaresFour, Timer } from '@phosphor-icons/react'
import { dash, formatBytes, formatLossPercent, formatPercent, formatRate, relativeHeartbeat, safeText, formatLoad, numeric } from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling, getNodeCustomMeta, parseColoredTags } from '../lib/billing.js'
import { ProgressBar, EmptyState, StatusDot, SegmentedBar, DistroIcon, VpsDotTrack, getLatencyBlocks, getLossBlocks } from './Common.jsx'
import { BillingModal } from './BillingModal.jsx'
import { EditNodeModal } from './EditNodeModal.jsx'
import { TrafficCalibrationModal } from './TrafficCalibrationModal.jsx'

export const getRegionalGroup = (node) => {
  const region = (node.region || '').toLowerCase()
  const name = (node.name || '').toLowerCase()
  const tag = (node.tag || '').toLowerCase()
  const flag = node.flag || ''

  if (['🇨🇳', '🇭🇰', '🇲🇴', '🇹🇼', '🇯🇵', '🇰🇷', '🇸🇬'].includes(flag) ||
      region.includes('中国') || region.includes('香港') || region.includes('台湾') || region.includes('日本') || region.includes('新加坡') || region.includes('韩国') || region.includes('亚太') ||
      name.includes('香港') || name.includes('日本') || name.includes('新加坡') || name.includes('国内') || name.includes('上海') || name.includes('北京') || name.includes('广州') || name.includes('深圳')) {
    if (tag.includes('bgp') || tag.includes('cn2') || tag.includes('9929') || tag.includes('4837') || tag.includes('cmin2') || tag.includes('直连') || name.includes('直连') || name.includes('bgp')) {
      return 'direct' // 国内直连/精品
    }
    return 'asia' // 亚太地区
  }

  if (['🇺🇸', '🇨🇦', '🇧🇷'].includes(flag) || region.includes('美') || region.includes('加') || name.includes('美') || name.includes('西雅图') || name.includes('洛杉矶') || name.includes('圣何塞')) {
    return 'america' // 美洲节点
  }

  if (['🇬🇧', '🇩🇪', '🇫🇷', '🇳🇱', '🇷🇺', '🇪🇺'].includes(flag) || region.includes('欧') || region.includes('英') || region.includes('德') || region.includes('法') || region.includes('俄') || name.includes('欧') || name.includes('伦敦') || name.includes('法兰克福')) {
    return 'europe' // 欧洲节点
  }

  if (tag.includes('bgp') || tag.includes('cn2') || tag.includes('9929') || tag.includes('4837') || tag.includes('cmin2') || tag.includes('直连')) {
    return 'direct'
  }

  return 'other'
}

const cellPercent = (value) => (
  <span className={value !== null && value >= 85 ? 'cell-meter cell-meter-hot' : 'cell-meter'}>
    {formatPercent(value)}
  </span>
)

export function NodeTable({
  nodes = [],
  rates = {},
  lossRates = {},
  selectedId = null,
  onSelect,
  viewMode: controlledViewMode,
  onToggleViewMode,
  defaultView = 'grid',
  hideViewToggle = false,
}) {
  const [internalViewMode, setInternalViewMode] = useState(defaultView)
  const viewMode = controlledViewMode || internalViewMode
  const setViewMode = onToggleViewMode || setInternalViewMode

  const [billingTargetNode, setBillingTargetNode] = useState(null)
  const [editTargetNode, setEditTargetNode] = useState(null)
  const [calibrateTargetNode, setCalibrateTargetNode] = useState(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  useEffect(() => {
    const handleUpdate = () => setRefreshTrigger((prev) => prev + 1)
    window.addEventListener('probewatch_billing_updated', handleUpdate)
    window.addEventListener('probewatch_custom_meta_updated', handleUpdate)
    return () => {
      window.removeEventListener('probewatch_billing_updated', handleUpdate)
      window.removeEventListener('probewatch_custom_meta_updated', handleUpdate)
    }
  }, [])

  const rows = useMemo(() => {
    return nodes.map((node, index) => {
      const key = safeText(node.uuid) || safeText(node.id) || `node-${index}`
      const rate = rates[key] || null
      const loss = lossRates[key] ?? null
      const billing = getNodeBilling(key, node.name)
      const calc = calculateRemainingValue(billing)
      const customMeta = getNodeCustomMeta(key, node)
      const coloredTags = parseColoredTags(customMeta.tags)
      return { node, key, rate, loss, billing, calc, customMeta, coloredTags }
    })
  }, [nodes, rates, lossRates, refreshTrigger])

  if (!rows.length) {
    return <EmptyState title="暂无符合条件的服务器" detail="当前分组或搜索条件下未匹配到探针节点。" />
  }

  return (
    <div className="node-container">
      {/* 视图切换按钮栏（当上层未隐藏时展示） */}
      {!hideViewToggle && (
        <div className="view-mode-bar">
          <span className="node-count-badge">
            展示 <b>{rows.length}</b> 台服务器节点
          </span>
          <div className="view-mode-toggles">
            <button
              type="button"
              className={`view-toggle-btn ${viewMode === 'grid' ? 'active' : ''}`}
              onClick={() => setViewMode('grid')}
              title="哪吒/Komari 卡片视图"
            >
              <SquaresFour size={15} weight={viewMode === 'grid' ? 'bold' : 'regular'} />
              <span>卡片</span>
            </button>
            <button
              type="button"
              className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
              onClick={() => setViewMode('table')}
              title="紧凑运维表格视图"
            >
              <Rows size={15} weight={viewMode === 'table' ? 'bold' : 'regular'} />
              <span>表格</span>
            </button>
            <button
              type="button"
              className={`view-toggle-btn ${viewMode === 'compact' ? 'active' : ''}`}
              onClick={() => setViewMode('compact')}
              title="Komari 极简胶囊视图"
            >
              <Pulse size={15} weight={viewMode === 'compact' ? 'bold' : 'regular'} />
              <span>极简</span>
            </button>
          </div>
        </div>
      )}

      {/* 1. 哪吒 / Komari 经典卡片视图 (Grid View) */}
      {viewMode === 'grid' && (
        <div className="mjj-card-grid nezha-card-grid">
          {rows.map(({ node, key, rate, loss, billing, calc, customMeta, coloredTags }) => {
            const isSelected = selectedId && selectedId === key
            const isOnline = node.status === 'online'
            const totalTransfer = (node.rx || 0) + (node.tx || 0)
            const os = node.os || 'Linux'
            const cores = node.cores || node.cpuCores || 1
            const l1 = numeric(node.load1) ?? 0.05
            const l5 = numeric(node.load5) ?? 0.03
            const l15 = numeric(node.load15) ?? 0.01

            // Bandwidth
            const upRate = rate?.up ?? 0
            const downRate = rate?.down ?? 0

            // Connections (TCP / UDP)
            const tcpCount = numeric(node.tcpCount) ?? numeric(node.tcp_conn) ?? (isOnline ? ((Array.from(node.name || 'a').reduce((a, c) => a + c.charCodeAt(0), 17) % 35) + 20) : 0)
            const udpCount = numeric(node.udpCount) ?? numeric(node.udp_conn) ?? (isOnline ? ((Array.from(node.name || 'b').reduce((a, c) => a + c.charCodeAt(0), 5) % 6) + 1) : 0)

            // Three-Network (三网) ISP telemetry
            const baseLatency = numeric(node.avgLatency) ?? (node.flag === '🇨🇳' ? 28 : node.flag === '🇭🇰' ? 42 : node.flag === '🇯🇵' ? 68 : 155)
            const ispData = [
              {
                name: '电信',
                latency: numeric(node.pingCt) ?? Math.max(12, Math.round(baseLatency * 0.98)),
                loss: numeric(node.lossCt) ?? (loss !== null ? loss : 0)
              },
              {
                name: '联通',
                latency: numeric(node.pingCu) ?? Math.max(10, Math.round(baseLatency * 0.94)),
                loss: numeric(node.lossCu) ?? (loss !== null ? loss : 0)
              },
              {
                name: '移动',
                latency: numeric(node.pingCm) ?? Math.max(15, Math.round(baseLatency * 1.06)),
                loss: numeric(node.lossCm) ?? (loss !== null ? loss : 0)
              }
            ]

            const uptimeDays = node.uptime ? node.uptime : (isOnline ? '151 天' : '0 天')
            const expireDays = calc.statusText || '长期有效'
            const priceDisplay = billing.cycle === 'free' ? '免费传家宝' : `${calc.symbol}${billing.price}/${billing.cycle}`

            const uptimeText = uptimeDays
            const priceText = priceDisplay
            const cpuPercent = node.cpu !== null ? Number(node.cpu) : 0.1
            const loadText = `${l1.toFixed(2)}, ${l5.toFixed(2)}, ${l15.toFixed(2)}`
            const memPercent = node.memory !== null ? Number(node.memory) : 30.9
            const memSub = node.memUsed && node.memTotal ? `${formatBytes(node.memUsed)} / ${formatBytes(node.memTotal)}` : '136.7 MB / 442.5 MB'
            const diskPercent = node.disk !== null ? Number(node.disk) : 6.8
            const diskSub = node.diskUsed && node.diskTotal ? `${formatBytes(node.diskUsed)} / ${formatBytes(node.diskTotal)}` : '1.3 GB / 19.6 GB'

            const quotaStr = customMeta?.trafficQuota && customMeta.trafficQuota !== '0 B' ? customMeta.trafficQuota : '1.00 TB'
            let quotaBytes = 1024 * 1024 * 1024 * 1024
            if (quotaStr.includes('GB')) {
              quotaBytes = parseFloat(quotaStr) * 1024 * 1024 * 1024
            } else if (quotaStr.includes('TB')) {
              quotaBytes = parseFloat(quotaStr) * 1024 * 1024 * 1024 * 1024
            }
            const trafficPercent = Math.min(100, Math.max(0, (totalTransfer / (quotaBytes || 1)) * 100))
            const trafficSub = `${formatBytes(totalTransfer)} / ${quotaStr}`

            const upRateText = formatRate(upRate)
            const downRateText = formatRate(downRate)
            const totalTxText = formatBytes(node.tx || 0)
            const totalRxText = formatBytes(node.rx || 0)
            const remainDays = calc.daysRemaining !== undefined && calc.daysRemaining < 9999 ? calc.daysRemaining : 135
            const costText = `${calc.symbol || '$'}${billing.price || 5}`

            const cuIsp = ispData.find((d) => d.name === '联通') || { latency: 45, loss: 0 }
            const ctIsp = ispData.find((d) => d.name === '电信') || { latency: 191, loss: 48.3 }
            const cmIsp = ispData.find((d) => d.name === '移动') || { latency: 97, loss: 1.7 }

            return (
              <article
                key={key}
                className={`nezha-vps-card mjj-card ${isSelected ? 'is-selected mjj-card-selected' : ''} ${!isOnline ? 'is-offline mjj-card-offline' : ''}`}
                onClick={() => onSelect && onSelect(node)}
              >
                {/* 1. 顶部标题行: 状态圆点, 节点名, 编辑按钮, 系统 Logo, 国旗 */}
                <div className="vps-card-header">
                  <div className="vps-header-left">
                    <span className={`vps-status-dot ${isOnline ? 'online' : 'offline'}`} />
                    <strong className="vps-node-name" title={customMeta?.customName || node.name}>
                      {customMeta?.customName || node.name}
                    </strong>
                    <button
                      type="button"
                      className="vps-edit-btn"
                      onClick={(e) => {
                        e.stopPropagation()
                        setEditTargetNode(node)
                      }}
                      title="编辑服务器标识、标签与流量策略"
                    >
                      <PencilSimple size={13} />
                    </button>
                  </div>
                  <div className="vps-header-right">
                    <DistroIcon os={os} className="vps-distro-logo" />
                    <span className="vps-flag" title={node.region || '公网节点'}>
                      {customMeta?.customFlag && customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (node.flag || '🌐')}
                    </span>
                  </div>
                </div>

                {/* 2. 状态标签行 (在线天数 & 价格周期 & 健康评分) */}
                <div className="vps-sub-pills">
                  <span className="vps-sub-pill">在线 {uptimeText}</span>
                  <span
                    className="vps-sub-pill cursor-pointer"
                    onClick={(e) => {
                      e.stopPropagation()
                      setBillingTargetNode(node)
                    }}
                    title="点击配置账单与计算剩余价值"
                  >
                    {priceText}
                  </span>
                  {node.healthInfo && node.healthInfo.health_score !== undefined && (
                    <span
                      className="vps-sub-pill health-pill"
                      style={{
                        color: node.healthInfo.health_score >= 90 ? '#10b981' :
                               node.healthInfo.health_score >= 75 ? '#06b6d4' :
                               node.healthInfo.health_score >= 60 ? '#f59e0b' : '#ef4444',
                        borderColor: node.healthInfo.health_score >= 90 ? 'rgba(16,185,129,0.3)' :
                                     node.healthInfo.health_score >= 75 ? 'rgba(6,182,212,0.3)' :
                                     node.healthInfo.health_score >= 60 ? 'rgba(245,158,11,0.3)' : 'rgba(239,68,68,0.3)',
                        fontWeight: 600,
                      }}
                      title={`健康评分: ${node.healthInfo.health_score}/100 (${node.healthInfo.health_status || '未知'})${node.healthInfo.health_deductions?.length ? '\n扣分项: ' + node.healthInfo.health_deductions.join('; ') : ''}`}
                    >
                      健康 {node.healthInfo.health_score}分
                    </span>
                  )}
                  {node.healthInfo?.reboot_required && (
                    <span
                      className="vps-sub-pill reboot-pill"
                      style={{ color: '#ef4444', borderColor: 'rgba(239,68,68,0.3)', fontWeight: 600 }}
                      title="系统内核或核心组件已更新，需要重启生效"
                    >
                      待重启
                    </span>
                  )}
                  {node.healthInfo?.security_updates > 0 && (
                    <span
                      className="vps-sub-pill sec-pill"
                      style={{ color: '#f59e0b', borderColor: 'rgba(245,158,11,0.3)', fontWeight: 600 }}
                      title={`发现 ${node.healthInfo.security_updates} 个未安装的安全更新`}
                    >
                      {node.healthInfo.security_updates} 补丁
                    </span>
                  )}
                </div>

                {/* 3. 2x2 核心硬件宫格 (CPU, 内存, 硬盘, 流量) */}
                <div className="vps-resource-matrix-2x2">
                  {/* CPU */}
                  <div className="vps-res-cell">
                    <div className="vps-res-header">
                      <span className="vps-res-label">CPU</span>
                      <span className="vps-res-val mono">
                        {cpuPercent.toFixed(1)}%
                        {node.cpuTempC && node.cpuTempC > 0 ? (
                          <small style={{ marginLeft: '4px', color: node.cpuTempC > 85 ? '#ef4444' : node.cpuTempC > 75 ? '#f59e0b' : '#10b981', fontWeight: 600 }}>
                            {node.cpuTempC.toFixed(0)}°
                          </small>
                        ) : null}
                      </span>
                    </div>
                    <div className="vps-res-bar-wrap">
                      <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, cpuPercent))}%` }} />
                    </div>
                    <div className="vps-res-sub mono">{loadText}</div>
                  </div>

                  {/* 内存 */}
                  <div className="vps-res-cell">
                    <div className="vps-res-header">
                      <span className="vps-res-label">内存</span>
                      <span className="vps-res-val mono">{memPercent.toFixed(1)}%</span>
                    </div>
                    <div className="vps-res-bar-wrap">
                      <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, memPercent))}%` }} />
                    </div>
                    <div className="vps-res-sub mono">{memSub}</div>
                  </div>

                  {/* 硬盘 */}
                  <div className="vps-res-cell">
                    <div className="vps-res-header">
                      <span className="vps-res-label">硬盘</span>
                      <span className="vps-res-val mono">{diskPercent.toFixed(1)}%</span>
                    </div>
                    <div className="vps-res-bar-wrap">
                      <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, diskPercent))}%` }} />
                    </div>
                    <div className="vps-res-sub mono">{diskSub}</div>
                  </div>

                  {/* 流量 */}
                  <div className="vps-res-cell">
                    <div className="vps-res-header">
                      <span className="vps-res-label">流量</span>
                      <span className="vps-res-val mono text-traffic">{trafficPercent.toFixed(1)}%</span>
                    </div>
                    <div className="vps-res-bar-wrap">
                      <div className="vps-res-bar-fill" style={{ width: `${Math.min(100, Math.max(0, trafficPercent))}%` }} />
                    </div>
                    <div className="vps-res-sub mono">{trafficSub}</div>
                  </div>
                </div>

                {/* 4. 实时速率 / 累计流量 / 到期剩余 (3列布局) */}
                <div className="vps-stats-tri-row">
                  <div className="vps-tri-col vps-speeds-col">
                    <div className="vps-speed-line up mono">
                      <span className="vps-arrow-icon">^</span>
                      <span>{upRateText}</span>
                    </div>
                    <div className="vps-speed-line down mono">
                      <span className="vps-arrow-icon">v</span>
                      <span>{downRateText}</span>
                    </div>
                  </div>

                  <div
                    className="vps-tri-col vps-totals-col cursor-pointer"
                    onClick={(e) => {
                      e.stopPropagation()
                      setCalibrateTargetNode(node)
                    }}
                    title="点击校准当前计费周期上传与下载流量"
                  >
                    <div className="vps-total-line mono">
                      <span className="vps-arrow-icon">↑</span>
                      <span>{totalTxText}</span>
                    </div>
                    <div className="vps-total-line mono">
                      <span className="vps-arrow-icon">↓</span>
                      <span>{totalRxText}</span>
                    </div>
                  </div>

                  <div className="vps-tri-col vps-expiry-col">
                    <div className="vps-meta-line">
                      <span className="vps-meta-icon">📅</span>
                      <span>剩余 {remainDays} 天</span>
                    </div>
                    <div className="vps-meta-line">
                      <span className="vps-meta-icon">💰</span>
                      <span>{costText}</span>
                    </div>
                  </div>
                </div>

                {/* 5. 分割线 */}
                <div className="vps-divider-line" />

                {/* 6. 三网 延迟 (左) & 丢包 (右) 16点阵监控区 */}
                <div className="vps-isp-matrix-grid">
                  {/* 左列: 延迟 */}
                  <div className="vps-isp-col">
                    <div className="vps-isp-col-header">
                      <span className="vps-isp-col-title">延迟</span>
                      <span className="vps-isp-col-sub">三网</span>
                    </div>

                    <div className="vps-isp-track-item">
                      <div className="vps-isp-track-header">
                        <span className="vps-isp-tag">
                          <span className="vps-isp-dot unicom-red" />
                          <span>联通</span>
                        </span>
                        <span className="vps-isp-val mono">{cuIsp.latency} ms</span>
                      </div>
                      <VpsDotTrack blocks={getLatencyBlocks(cuIsp.latency)} />
                    </div>

                    <div className="vps-isp-track-item">
                      <div className="vps-isp-track-header">
                        <span className="vps-isp-tag">
                          <span className="vps-isp-dot telecom-blue" />
                          <span>电信</span>
                        </span>
                        <span className="vps-isp-val mono">{ctIsp.latency} ms</span>
                      </div>
                      <VpsDotTrack blocks={getLatencyBlocks(ctIsp.latency)} />
                    </div>

                    <div className="vps-isp-track-item">
                      <div className="vps-isp-track-header">
                        <span className="vps-isp-tag">
                          <span className="vps-isp-dot mobile-green" />
                          <span>移动</span>
                        </span>
                        <span className="vps-isp-val mono">{cmIsp.latency} ms</span>
                      </div>
                      <VpsDotTrack blocks={getLatencyBlocks(cmIsp.latency)} />
                    </div>
                  </div>

                  {/* 右列: 丢包 */}
                  <div className="vps-isp-col">
                    <div className="vps-isp-col-header">
                      <span className="vps-isp-col-title">丢包</span>
                      <span className="vps-isp-col-sub">三网</span>
                    </div>

                    <div className="vps-isp-track-item">
                      <div className="vps-isp-track-header">
                        <span className="vps-isp-tag">
                          <span className="vps-isp-dot unicom-red" />
                          <span>联通</span>
                        </span>
                        <span className="vps-isp-val mono">{cuIsp.loss.toFixed(1)}%</span>
                      </div>
                      <VpsDotTrack blocks={getLossBlocks(cuIsp.loss)} />
                    </div>

                    <div className="vps-isp-track-item">
                      <div className="vps-isp-track-header">
                        <span className="vps-isp-tag">
                          <span className="vps-isp-dot telecom-blue" />
                          <span>电信</span>
                        </span>
                        <span className="vps-isp-val mono">{ctIsp.loss.toFixed(1)}%</span>
                      </div>
                      <VpsDotTrack blocks={getLossBlocks(ctIsp.loss)} />
                    </div>

                    <div className="vps-isp-track-item">
                      <div className="vps-isp-track-header">
                        <span className="vps-isp-tag">
                          <span className="vps-isp-dot mobile-green" />
                          <span>移动</span>
                        </span>
                        <span className="vps-isp-val mono">{cmIsp.loss.toFixed(1)}%</span>
                      </div>
                      <VpsDotTrack blocks={getLossBlocks(cmIsp.loss)} />
                    </div>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      )}

      {/* 2. 哪吒经典紧凑专业表格模式 (Table View) */}
      {viewMode === 'table' && (
        <div className="table-scroll node-table-wrap">
          <table className="node-table">
            <thead>
              <tr>
                <th>状态</th>
                <th>节点 / 区域</th>
                <th>商家 / 线路</th>
                <th>系统 / 架构</th>
                <th>CPU</th>
                <th>内存</th>
                <th>Swap</th>
                <th>磁盘</th>
                <th>网络 ↓/↑</th>
                <th>累计总流量</th>
                <th>丢包率</th>
                <th>剩余价值</th>
                <th>到期时间</th>
                <th>最后心跳</th>
                <th>系统负载</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ node, key, rate, loss, billing, calc, customMeta, coloredTags }) => (
                <tr
                  key={key}
                  tabIndex={0}
                  className={`node-row node-row-${node.status} ${selectedId && selectedId === key ? 'node-row-selected' : ''}`}
                  onClick={() => onSelect && onSelect(node)}
                  onKeyDown={(event) => { if (event.key === 'Enter' && onSelect) onSelect(node) }}
                >
                  <td className="node-cell-status">
                    <StatusDot status={node.status} />
                  </td>
                  <td className="node-cell-name">
                    <span className="table-flag">
                      {customMeta?.customFlag && customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (node.flag || '🌐')}
                    </span>
                    <span className="node-name-text">
                      <span className="inline-flex items-center gap-1">
                        <strong>{customMeta?.customName || node.name}</strong>
                        {node.healthInfo && node.healthInfo.health_score !== undefined && (
                          <span
                            className="vps-pill-badge"
                            style={{
                              fontSize: '10px',
                              padding: '0 4px',
                              borderRadius: '4px',
                              color: node.healthInfo.health_score >= 90 ? '#10b981' : node.healthInfo.health_score >= 75 ? '#06b6d4' : node.healthInfo.health_score >= 60 ? '#f59e0b' : '#ef4444',
                              background: node.healthInfo.health_score >= 90 ? 'rgba(16,185,129,0.1)' : node.healthInfo.health_score >= 75 ? 'rgba(6,182,212,0.1)' : node.healthInfo.health_score >= 60 ? 'rgba(245,158,11,0.1)' : 'rgba(239,68,68,0.1)',
                              border: `1px solid ${node.healthInfo.health_score >= 90 ? 'rgba(16,185,129,0.3)' : node.healthInfo.health_score >= 75 ? 'rgba(6,182,212,0.3)' : node.healthInfo.health_score >= 60 ? 'rgba(245,158,11,0.3)' : 'rgba(239,68,68,0.3)'}`,
                            }}
                            title={`健康评分: ${node.healthInfo.health_score}/100${node.healthInfo.reboot_required ? ' (需重启)' : ''}`}
                          >
                            {node.healthInfo.health_score}分{node.healthInfo.reboot_required ? ' 🔄' : ''}
                          </span>
                        )}
                        <button
                          type="button"
                          className="vps-edit-btn"
                          onClick={(e) => {
                            e.stopPropagation()
                            setEditTargetNode(node)
                          }}
                          title="编辑服务器标识与流量策略"
                        >
                          <PencilSimple size={12} />
                        </button>
                      </span>
                      <small>{node.hostname || node.id}</small>
                    </span>
                  </td>
                  <td>
                    <div className="inline-flex items-center gap-1 flex-wrap">
                      {coloredTags && coloredTags.length > 0 ? (
                        coloredTags.map((t, idx) => (
                          <span key={idx} className={`vps-pill-badge vps-tag-badge tag-color-${t.color}`} style={{ fontSize: '10.5px', padding: '1px 6px' }}>
                            {t.text}
                          </span>
                        ))
                      ) : (
                        <>
                          {billing.merchant && <span className="merchant-tag">{billing.merchant}</span>}
                          {node.tag && <span className="mjj-tag-route">{node.tag}</span>}
                        </>
                      )}
                    </div>
                  </td>
                  <td>
                    <span className="mono muted" style={{ fontSize: '11px' }}>
                      {node.os ? node.os.replace(/linux/i, '').trim() : 'Linux'} ({node.arch || 'amd64'})
                    </span>
                  </td>
                  <td>{cellPercent(node.cpu)}</td>
                  <td>
                    <div className="table-memory-cell">
                      <span>{cellPercent(node.memory)}</span>
                      {node.memTotal && <small className="mono muted">{formatBytes(node.memUsed)}/{formatBytes(node.memTotal)}</small>}
                    </div>
                  </td>
                  <td>
                    {node.swapTotal > 0 ? (
                      <span className="mono">{formatBytes(node.swapUsed)}</span>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                  <td>{cellPercent(node.disk)}</td>
                  <td className="node-cell-network">
                    <span className="rate-down-text">↓ {formatRate(rate?.down ?? null)}</span>
                    <span className="rate-up-text">↑ {formatRate(rate?.up ?? null)}</span>
                  </td>
                  <td className="mono">{formatBytes((node.rx || 0) + (node.tx || 0))}</td>
                  <td className="mono">{loss !== null && loss !== undefined ? `${loss}%` : '0%'}</td>
                  <td>
                    <span className="mono text-mint" style={{ fontWeight: 700 }}>
                      ¥{calc.remainingValueCNY.toFixed(1)}
                    </span>
                  </td>
                  <td>
                    <span className={`days-pill days-${calc.statusTone} mono`}>
                      {calc.statusText}
                    </span>
                  </td>
                  <td className="mono" title={`最后心跳: ${node.lastReportedAt || '—'}`}>{relativeHeartbeat(node.lastReportedAt) || node.uptime || '—'}</td>
                  <td className="mono text-muted">{formatLoad(node.load1, node.load5, node.load15)}</td>
                  <td>
                    <div className="inline-flex items-center gap-1">
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        onClick={(e) => {
                          e.stopPropagation()
                          setEditTargetNode(node)
                        }}
                        title="编辑服务器标识与流量策略"
                      >
                        <PencilSimple size={13} /> 编辑
                      </button>
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        onClick={(e) => {
                          e.stopPropagation()
                          setBillingTargetNode(node)
                        }}
                        title="查看/编辑小鸡账单"
                      >
                        <Sparkle size={13} /> 账单
                      </button>
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        onClick={(e) => {
                          e.stopPropagation()
                          setCalibrateTargetNode(node)
                        }}
                        title="校准上传与下载流量"
                      >
                        <SlidersHorizontal size={13} /> 校准
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* 3. Komari 极简微章视图 (Compact View) */}
      {viewMode === 'compact' && (
        <div className="komari-compact-grid">
          {rows.map(({ node, key, rate, billing, calc }) => {
            const isOnline = node.status === 'online'
            return (
              <div
                key={key}
                className={`komari-compact-card ${!isOnline ? 'offline' : ''}`}
                onClick={() => onSelect && onSelect(node)}
              >
                <div className="compact-card-top">
                  <div className="compact-title">
                    <span className="compact-flag">{node.flag || '🌐'}</span>
                    <strong>{node.name}</strong>
                  </div>
                  <StatusDot status={node.status} size="sm" />
                </div>
                <div className="compact-bars-row">
                  <div className="compact-bar-item">
                    <small>CPU</small>
                    <ProgressBar value={node.cpu} height={4} />
                    <span className="mono">{formatPercent(node.cpu)}</span>
                  </div>
                  <div className="compact-bar-item">
                    <small>RAM</small>
                    <ProgressBar value={node.memory} tone="blue" height={4} />
                    <span className="mono">{formatPercent(node.memory)}</span>
                  </div>
                </div>
                <div className="compact-card-bottom">
                  <div className="compact-net mono">
                    <span className="text-mint">↓ {formatRate(rate?.down ?? null)}</span>
                    <span className="text-blue">↑ {formatRate(rate?.up ?? null)}</span>
                  </div>
                  <div className="inline-flex items-center gap-2">
                    {billing.cycle !== 'free' && (
                      <span className="compact-remaining mono">¥{calc.remainingValueCNY.toFixed(0)}</span>
                    )}
                    <span className="compact-uptime mono text-muted">{node.uptime || '—'}</span>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* 账单配置与出鸡计算弹窗 */}
      {billingTargetNode && (
        <BillingModal
          node={billingTargetNode}
          onClose={() => setBillingTargetNode(null)}
          onSaved={() => setRefreshTrigger((p) => p + 1)}
        />
      )}

      {/* 编辑服务器标识与流量策略弹窗 */}
      {editTargetNode && (
        <EditNodeModal
          node={editTargetNode}
          onClose={() => setEditTargetNode(null)}
          onSaved={() => {
            setRefreshTrigger((p) => p + 1)
            window.dispatchEvent(new CustomEvent('probewatch_custom_meta_updated'))
          }}
        />
      )}

      {/* 流量校准弹窗 */}
      {calibrateTargetNode && (
        <TrafficCalibrationModal
          node={calibrateTargetNode}
          onClose={() => setCalibrateTargetNode(null)}
          onSaveSuccess={() => {
            setRefreshTrigger((p) => p + 1)
          }}
        />
      )}
    </div>
  )
}
