import { useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, ArrowUpRight, Coins, Cpu, HardDrive, HardDrives, Lightning, Memory, Pulse, Rows, Sparkle, SquaresFour, Tag, Timer } from '@phosphor-icons/react'
import { dash, formatBytes, formatLossPercent, formatPercent, formatRate, relativeHeartbeat, safeText, formatLoad } from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling } from '../lib/billing.js'
import { ProgressBar, EmptyState, StatusDot } from './Common.jsx'
import { BillingModal } from './BillingModal.jsx'

const cellPercent = (value) => (
  <span className={value !== null && value >= 85 ? 'cell-meter cell-meter-hot' : 'cell-meter'}>
    {formatPercent(value)}
  </span>
)

export function NodeTable({ nodes, rates = {}, lossRates = {}, selectedId = null, onSelect, defaultView = 'grid' }) {
  const [viewMode, setViewMode] = useState(defaultView) // 'grid' | 'table'
  const [billingTargetNode, setBillingTargetNode] = useState(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  useEffect(() => {
    const handleUpdate = () => setRefreshTrigger((prev) => prev + 1)
    window.addEventListener('probewatch_billing_updated', handleUpdate)
    return () => window.removeEventListener('probewatch_billing_updated', handleUpdate)
  }, [])

  const rows = nodes.map((node, index) => {
    const key = safeText(node.uuid) || safeText(node.id) || `node-${index}`
    const rate = rates[key] || null
    const loss = lossRates[key] ?? null
    const billing = getNodeBilling(key, node.name)
    const calc = calculateRemainingValue(billing)
    return { node, key, rate, loss, billing, calc }
  })

  if (!rows.length) {
    return <EmptyState title="暂无节点数据" detail="当前尚未发现已注册或上报的探针节点。" />
  }

  return (
    <div className="node-container">
      {/* 视图切换按钮栏 */}
      <div className="view-mode-bar">
        <span className="node-count-badge">
          共 <b>{rows.length}</b> 个计算节点
        </span>
        <div className="view-mode-toggles">
          <button
            type="button"
            className={`view-toggle-btn ${viewMode === 'grid' ? 'active' : ''}`}
            onClick={() => setViewMode('grid')}
            title="DStatus 宫格卡片视图"
          >
            <SquaresFour size={16} weight={viewMode === 'grid' ? 'bold' : 'regular'} />
            <span>卡片</span>
          </button>
          <button
            type="button"
            className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
            onClick={() => setViewMode('table')}
            title="紧凑表格视图"
          >
            <Rows size={16} weight={viewMode === 'table' ? 'bold' : 'regular'} />
            <span>表格</span>
          </button>
        </div>
      </div>

      {/* 1. DStatus (demo.vps.mom) + Lite 剩余价值风格独立服务器卡片 */}
      {viewMode === 'grid' && (
        <div className="mjj-card-grid">
          {rows.map(({ node, key, rate, loss, billing, calc }) => {
            const isSelected = selectedId && selectedId === key
            const isOnline = node.status === 'online'
            const totalTransfer = (node.rx || 0) + (node.tx || 0)

            return (
              <article
                key={key}
                className={`mjj-card dstatus-card ${isSelected ? 'mjj-card-selected' : ''} ${!isOnline ? 'mjj-card-offline' : ''}`}
                onClick={() => onSelect(node)}
              >
                {/* 顶部：国旗、商家、节点名、线路与在线徽章 */}
                <div className="mjj-card-header">
                  <div className="mjj-node-title-group">
                    <span className="mjj-flag" title={node.region || '公网节点'}>{node.flag || '🌐'}</span>
                    <div className="mjj-node-name-block">
                      <div className="mjj-node-name-line">
                        <strong className="mjj-node-name">{node.name}</strong>
                        {billing.merchant && (
                          <span className="mjj-merchant-tag">{billing.merchant}</span>
                        )}
                        {node.tag && <span className="mjj-tag-route">{node.tag}</span>}
                      </div>
                      <div className="mjj-node-sub">
                        <span className="mjj-hostname">{node.hostname || node.id || '—'}</span>
                        <span className="mjj-arch-pill">{node.arch || 'amd64'}</span>
                      </div>
                    </div>
                  </div>
                  <div className="mjj-status-badge">
                    <StatusDot status={node.status} />
                    <span className="mjj-status-text">{isOnline ? '在线' : '离线'}</span>
                  </div>
                </div>

                {/* 中部：四大硬件体质进度条 (CPU / 内存 / Swap / 硬盘) */}
                <div className="mjj-metrics-section">
                  {/* CPU */}
                  <div className="mjj-metric-row">
                    <div className="mjj-metric-label">
                      <span className="mjj-metric-name"><Cpu size={13} /> CPU</span>
                      <span className="mjj-metric-val mono">{dash(formatPercent(node.cpu))}</span>
                    </div>
                    <ProgressBar value={node.cpu} tone="dynamic" height={5} />
                  </div>

                  {/* 内存 */}
                  <div className="mjj-metric-row">
                    <div className="mjj-metric-label">
                      <span className="mjj-metric-name"><Memory size={13} /> 内存</span>
                      <span className="mjj-metric-val mono">
                        {node.memUsed !== null && node.memTotal ? `${formatBytes(node.memUsed)} / ${formatBytes(node.memTotal)}` : dash(formatPercent(node.memory))}
                      </span>
                    </div>
                    <ProgressBar value={node.memory} tone="dynamic" height={5} />
                  </div>

                  {/* Swap (MJJ 特供) */}
                  <div className="mjj-metric-row">
                    <div className="mjj-metric-label">
                      <span className="mjj-metric-name"><Lightning size={13} /> Swap</span>
                      <span className="mjj-metric-val mono">
                        {node.swapTotal > 0 ? `${formatBytes(node.swapUsed)} / ${formatBytes(node.swapTotal)}` : '未开启'}
                      </span>
                    </div>
                    <ProgressBar value={node.swap || 0} tone="violet" height={5} />
                  </div>

                  {/* 硬盘 */}
                  <div className="mjj-metric-row">
                    <div className="mjj-metric-label">
                      <span className="mjj-metric-name"><HardDrive size={13} /> 硬盘</span>
                      <span className="mjj-metric-val mono">
                        {node.diskUsed !== null && node.diskTotal ? `${formatBytes(node.diskUsed)} / ${formatBytes(node.diskTotal)}` : dash(formatPercent(node.disk))}
                      </span>
                    </div>
                    <ProgressBar value={node.disk} tone="amber" height={5} />
                  </div>
                </div>

                {/* 网络流速与出入站流量 */}
                <div className="mjj-network-section">
                  <div className="mjj-bandwidth-rates">
                    <div className="mjj-rate-item rate-down">
                      <span className="rate-icon"><ArrowDown size={14} weight="bold" /></span>
                      <div className="rate-info">
                        <small>实时下行</small>
                        <b className="mono">{formatRate(rate?.down ?? null)}</b>
                      </div>
                    </div>
                    <div className="mjj-rate-item rate-up">
                      <span className="rate-icon"><ArrowUp size={14} weight="bold" /></span>
                      <div className="rate-info">
                        <small>实时上行</small>
                        <b className="mono">{formatRate(rate?.up ?? null)}</b>
                      </div>
                    </div>
                  </div>

                  <div className="mjj-traffic-summary">
                    <span className="mjj-traffic-tag" title={`出站: ${formatBytes(node.tx)} | 入站: ${formatBytes(node.rx)}`}>
                      <HardDrives size={13} /> 累计: <b className="mono">{formatBytes(totalTransfer)}</b>
                    </span>
                    {loss !== null && (
                      <span className={`mjj-loss-tag ${loss > 0 ? 'mjj-loss-warn' : ''}`}>
                        丢包: <b className="mono">{formatLossPercent(loss)}</b>
                      </span>
                    )}
                  </div>
                </div>

                {/* 🌟 核心特色：Lite 同款小鸡账单与剩余价值组件 (点击唤起配置与出鸡计算) */}
                <div
                  className="mjj-billing-strip"
                  onClick={(e) => {
                    e.stopPropagation()
                    setBillingTargetNode(node)
                  }}
                  title="点击编辑小鸡账单或计算二手出鸡指导价"
                >
                  <div className="billing-strip-left">
                    <Coins size={14} className="text-amber" />
                    <span className="billing-price-tag mono">
                      {billing.cycle === 'free' ? '免费传家宝' : `${calc.symbol}${billing.price}/${billing.cycle}`}
                    </span>
                    {billing.cycle !== 'free' && (
                      <span className="billing-val-tag mono">
                        剩: <b>¥{calc.remainingValueCNY.toFixed(1)}</b>
                      </span>
                    )}
                  </div>
                  <div className="billing-strip-right">
                    <span className={`billing-days-badge days-${calc.statusTone} mono`}>
                      {calc.statusText}
                    </span>
                    <Sparkle size={13} className="text-mint hover-spin" />
                  </div>
                </div>

                {/* 底部信息栏：在线时长、系统负载与心跳 */}
                <div className="mjj-card-footer">
                  <div className="mjj-uptime-badge" title="小鸡连续在线时长">
                    <Timer size={13} />
                    <span>在线: <b>{node.uptime || '—'}</b></span>
                  </div>
                  <div className="mjj-footer-right">
                    <span className="mjj-load mono" title="系统 Load 1/5/15">
                      Load: {formatLoad(node.load1, node.load5, node.load15)}
                    </span>
                    <span className="mjj-heartbeat" title="探针心跳">
                      {relativeHeartbeat(node.lastReportedAt)}
                    </span>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      )}

      {/* 2. 紧凑专业表格模式 */}
      {viewMode === 'table' && (
        <div className="table-scroll node-table-wrap">
          <table className="node-table">
            <thead>
              <tr>
                <th>状态</th>
                <th>节点 / 地区</th>
                <th>商家</th>
                <th>CPU</th>
                <th>内存 (已用/总)</th>
                <th>Swap</th>
                <th>硬盘</th>
                <th>实时网络 (↓ / ↑)</th>
                <th>累计总流量</th>
                <th>剩余价值</th>
                <th>到期时间</th>
                <th>连续在线</th>
                <th>负载</th>
                <th>管理</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ node, key, rate, loss, billing, calc }) => (
                <tr
                  key={key}
                  tabIndex={0}
                  className={`node-row node-row-${node.status} ${selectedId && selectedId === key ? 'node-row-selected' : ''}`}
                  onClick={() => onSelect(node)}
                  onKeyDown={(event) => { if (event.key === 'Enter') onSelect(node) }}
                >
                  <td className="node-cell-status">
                    <StatusDot status={node.status} />
                  </td>
                  <td className="node-cell-name">
                    <span className="table-flag">{node.flag || '🌐'}</span>
                    <span className="node-name-text">
                      <strong>{node.name}</strong>
                      <small>{node.tag || node.hostname || node.id}</small>
                    </span>
                  </td>
                  <td>
                    <span className="merchant-tag">{billing.merchant}</span>
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
                  <td className="mono">{node.uptime || '—'}</td>
                  <td className="mono text-muted">{formatLoad(node.load1, node.load5, node.load15)}</td>
                  <td>
                    <button
                      type="button"
                      className="button button-quiet btn-sm"
                      onClick={(e) => {
                        e.stopPropagation()
                        setBillingTargetNode(node)
                      }}
                    >
                      <Sparkle size={13} /> 账单
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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
    </div>
  )
}
