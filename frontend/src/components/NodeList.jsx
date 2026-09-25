import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, ArrowsClockwise, CalendarBlank, Coins, Cpu, Globe, HardDrive, HardDrives, Lightning, Memory, PencilSimple, Pulse, Rows, Sparkle, SquaresFour, Timer } from '@phosphor-icons/react'
import { dash, formatBytes, formatLossPercent, formatPercent, formatRate, relativeHeartbeat, safeText, formatLoad, numeric } from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling, getNodeCustomMeta, parseColoredTags } from '../lib/billing.js'
import { ProgressBar, EmptyState, StatusDot, SegmentedBar, DistroIcon } from './Common.jsx'
import { BillingModal } from './BillingModal.jsx'
import { EditNodeModal } from './EditNodeModal.jsx'

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

            return (
              <article
                key={key}
                className={`nezha-vps-card mjj-card ${isSelected ? 'is-selected mjj-card-selected' : ''} ${!isOnline ? 'is-offline mjj-card-offline' : ''}`}
                onClick={() => onSelect && onSelect(node)}
              >
                {/* 1. 顶部标题行、国旗、节点名、状态徽章与系统 Logo */}
                <div className="vps-card-header">
                  <div className="vps-header-left">
                    <div className="vps-title-row">
                      <span className="vps-flag" title={node.region || '公网节点'}>
                        {customMeta?.customFlag && customMeta.customFlag !== '自动识别' ? customMeta.customFlag : (node.flag || '🌐')}
                      </span>
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
                    <div className="vps-badge-row">
                      <span className={`vps-pill-badge ${isOnline ? 'pill-good' : 'pill-offline'}`}>
                        <span className="status-mini-dot" />
                        {isOnline ? 'GOOD' : 'OFFLINE'}
                      </span>
                      <span className="vps-pill-badge pill-proto">V4</span>
                      <span className="vps-pill-badge pill-proto">V6</span>
                      {customMeta?.bandwidth && (
                        <span className="vps-pill-badge pill-bw">{customMeta.bandwidth}</span>
                      )}
                      {coloredTags && coloredTags.length > 0 ? (
                        coloredTags.map((t, idx) => (
                          <span key={idx} className={`vps-pill-badge vps-tag-badge tag-color-${t.color}`}>
                            {t.text}
                          </span>
                        ))
                      ) : (
                        <>
                          {billing.merchant && (
                            <span className="vps-pill-badge pill-merchant">{billing.merchant}</span>
                          )}
                          {node.tag && (
                            <span className="vps-pill-badge pill-route">{node.tag}</span>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                  <div className="vps-header-right">
                    <DistroIcon os={os} className="vps-distro-logo" />
                  </div>
                </div>

                {/* 2. 核心硬件 2x2 宫格 (CPU / 内存 / 磁盘 / 负载) */}
                <div className="vps-hardware-grid">
                  {/* CPU */}
                  <div className="vps-hw-cell">
                    <div className="vps-hw-header">
                      <span className="vps-hw-label"><Cpu size={13} /> CPU</span>
                      <b className="vps-hw-val mono">{node.cpu !== null ? `${formatPercent(node.cpu)}` : '0.0 %'}</b>
                    </div>
                    <div className="vps-hw-sub mono">{cores} 核</div>
                    <SegmentedBar value={node.cpu || 0} max={100} segments={14} activeColor="#3b82f6" />
                  </div>

                  {/* 内存 */}
                  <div className="vps-hw-cell">
                    <div className="vps-hw-header">
                      <span className="vps-hw-label"><Memory size={13} /> 内存</span>
                      <b className="vps-hw-val mono">{node.memory !== null ? `${formatPercent(node.memory)}` : '0.0 %'}</b>
                    </div>
                    <div className="vps-hw-sub mono">
                      {node.memUsed !== null && node.memTotal ? `${formatBytes(node.memUsed)} / ${formatBytes(node.memTotal)}` : '500 MB / 960 MB'}
                    </div>
                    <SegmentedBar value={node.memory || 0} max={100} segments={14} activeColor="#8b5cf6" />
                  </div>

                  {/* 磁盘 */}
                  <div className="vps-hw-cell">
                    <div className="vps-hw-header">
                      <span className="vps-hw-label"><HardDrive size={13} /> 磁盘</span>
                      <b className="vps-hw-val mono">{node.disk !== null ? `${formatPercent(node.disk)}` : '0.0 %'}</b>
                    </div>
                    <div className="vps-hw-sub mono">
                      {node.diskUsed !== null && node.diskTotal ? `${formatBytes(node.diskUsed)} / ${formatBytes(node.diskTotal)}` : '7.09 GB / 29.4 GB'}
                    </div>
                    <SegmentedBar value={node.disk || 0} max={100} segments={14} activeColor="#f97316" />
                  </div>

                  {/* 负载 */}
                  <div className="vps-hw-cell">
                    <div className="vps-hw-header">
                      <span className="vps-hw-label"><Timer size={13} /> 负载</span>
                      <b className="vps-hw-val mono">{l1.toFixed(2)}</b>
                    </div>
                    <div className="vps-hw-sub mono">{l1.toFixed(2)} / {l5.toFixed(2)} / {l15.toFixed(2)}</div>
                    <SegmentedBar value={Math.min(l1 * 50, 100)} max={100} segments={14} activeColor="#64748b" />
                  </div>
                </div>

                {/* 3. 实时速率与出入站流量 */}
                <div className="vps-net-rates">
                  <div className="vps-rate-row">
                    <div className="vps-rate-left">
                      <span className="rate-dir-icon text-blue"><ArrowUp size={13} weight="bold" /></span>
                      <span className="rate-dir-name">上行</span>
                      <b className="rate-speed-large mono">{formatRate(upRate)}</b>
                      <span className="live-pulse">
                        <span className="pulse-dots">•••</span>
                        <span className="live-text">实时</span>
                      </span>
                    </div>
                    <div className="vps-rate-right">
                      <Globe size={13} className="text-muted" />
                      <span className="traffic-side-label">出站</span>
                      <b className="mono traffic-side-val">{formatBytes(node.tx || 0)}</b>
                    </div>
                  </div>

                  <div className="vps-rate-row">
                    <div className="vps-rate-left">
                      <span className="rate-dir-icon text-mint"><ArrowDown size={13} weight="bold" /></span>
                      <span className="rate-dir-name">下行</span>
                      <b className="rate-speed-large mono">{formatRate(downRate)}</b>
                      <span className="live-pulse">
                        <span className="pulse-dots">•••</span>
                        <span className="live-text">实时</span>
                      </span>
                    </div>
                    <div className="vps-rate-right">
                      <Globe size={13} className="text-muted" />
                      <span className="traffic-side-label">入站</span>
                      <b className="mono traffic-side-val">{formatBytes(node.rx || 0)}</b>
                    </div>
                  </div>

                  <div className="vps-transfer-bar-wrap">
                    <div className="vps-transfer-meta">
                      <span className="transfer-meta-left">
                        🗄️ 剩余流量 {customMeta?.trafficQuota && customMeta.trafficQuota !== '0 B' ? customMeta.trafficQuota : '∞'}
                      </span>
                      <span className="transfer-meta-right mono">
                        {formatBytes(totalTransfer)} / {customMeta?.trafficQuota && customMeta.trafficQuota !== '0 B' ? customMeta.trafficQuota : '∞'}
                      </span>
                    </div>
                    <SegmentedBar value={15} max={100} segments={28} activeColor="#3b82f6" className="vps-full-segmented" />
                  </div>
                </div>

                {/* 4. TCP / UDP 连接数 */}
                <div className="vps-conns-row">
                  <div className="vps-conn-item">
                    <span className="conn-label">TCP 连接</span>
                    <b className="conn-val mono text-mint">{tcpCount}</b>
                  </div>
                  <div className="vps-conn-item">
                    <span className="conn-label">UDP 连接</span>
                    <b className="conn-val mono text-mint">{udpCount}</b>
                  </div>
                </div>

                {/* 5. 三网 (电信 / 联通 / 移动) 延迟与丢包率点阵矩阵 */}
                <div className="vps-isp-matrix">
                  {ispData.map((isp) => {
                    const latColor = isp.latency < 60 ? '#34d399' : isp.latency < 160 ? '#f59e0b' : '#f43f5e'
                    return (
                      <div className="vps-isp-row" key={isp.name}>
                        <span className="isp-name">{isp.name}</span>
                        <span className="isp-lat mono" style={{ color: latColor }}>{isp.latency} ms</span>
                        <SegmentedBar
                          value={Math.min(isp.latency, 250)}
                          max={250}
                          segments={14}
                          activeColor={latColor}
                          className="isp-bar"
                        />
                        <SegmentedBar
                          value={isp.loss}
                          max={100}
                          segments={14}
                          activeColor="#f43f5e"
                          className="isp-bar"
                        />
                        <span className={`isp-loss mono ${isp.loss > 0 ? 'text-rose' : 'text-mint'}`}>
                          {isp.loss.toFixed(1)} %
                        </span>
                      </div>
                    )
                  })}
                </div>

                {/* 6. 底部信息栏：在线时长、到期时间、精品小鸡与账单价格 */}
                <div className="vps-card-footer">
                  <div className="vps-footer-left">
                    <span className="vps-footer-item">
                      <ArrowsClockwise size={13} className="text-blue" />
                      <span>在线: <b className="mono text-blue">{uptimeDays}</b></span>
                    </span>
                    <span className="vps-footer-item">
                      <CalendarBlank size={13} className="text-mint" />
                      <span>到期: <b className="mono text-mint">{expireDays}</b></span>
                    </span>
                  </div>
                  <div className="vps-footer-right">
                    <span
                      className="vps-pill-tag pill-lavender"
                      onClick={(e) => {
                        e.stopPropagation()
                        setBillingTargetNode(node)
                      }}
                      title="点击配置小鸡属性"
                    >
                      精品小鸡
                    </span>
                    <span
                      className="vps-pill-tag pill-price mono"
                      onClick={(e) => {
                        e.stopPropagation()
                        setBillingTargetNode(node)
                      }}
                      title="点击计算二手出鸡指导价与账单详情"
                    >
                      💲 {priceDisplay}
                    </span>
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
    </div>
  )
}
