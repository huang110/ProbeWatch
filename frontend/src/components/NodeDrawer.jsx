import { useEffect, useRef, useState } from 'react'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  CalendarBlank,
  Coins,
  Cpu,
  HardDrive,
  Lightning,
  LinuxLogo,
  Memory,
  Pulse,
  Sparkle,
  WindowsLogo,
  X,
} from '@phosphor-icons/react'
import { dash, formatBytes, formatLoad, formatRate, relativeHeartbeat, safeText, statusLabel } from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling } from '../lib/billing.js'
import { ProgressBar, StatusDot } from './Common.jsx'
import { BillingModal } from './BillingModal.jsx'

export function NodeDrawer({ node, rates = {}, onClose, onOpenDetails }) {
  const closeButtonRef = useRef(null)
  const [showBillingModal, setShowBillingModal] = useState(false)
  const [billingVersion, setBillingVersion] = useState(0)

  useEffect(() => {
    const key = (event) => event.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    document.body.style.overflow = 'hidden'
    closeButtonRef.current?.focus()
    return () => {
      document.removeEventListener('keydown', key)
      document.body.style.overflow = ''
    }
  }, [onClose])

  if (!node) return null

  const key = safeText(node.uuid) || safeText(node.id) || 'node'
  const rate = rates[key] || null
  const isOnline = node.status === 'online'
  const billing = getNodeBilling(key, node.name)
  const calc = calculateRemainingValue(billing)
  const totalTransfer = (node.rx || 0) + (node.tx || 0)
  const isWindows = (node.os || '').toLowerCase().includes('windows')

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
              <h2 id="node-drawer-title" className="drawer-name">{node.name}</h2>
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
          <button
            ref={closeButtonRef}
            className="icon-button drawer-close-btn"
            onClick={onClose}
            aria-label="关闭详情"
          >
            <X size={18} />
          </button>
        </div>

        <div className="drawer-scroll-content">
          {/* 实时流速大看板 */}
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

          {/* 硬件与系统概览卡片 */}
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
                  <span>{node.os || 'Linux'} · {node.arch || 'x86_64'}</span>
                </div>
              </div>

              <div className="spec-item">
                <span className="spec-label">连续运行时间</span>
                <div className="spec-value mono">
                  {node.uptime || '—'}
                </div>
              </div>

              <div className="spec-item">
                <span className="spec-label">平均负载 (1/5/15)</span>
                <div className="spec-value mono">
                  {formatLoad(node.load1, node.load5, node.load15)}
                </div>
              </div>

              <div className="spec-item">
                <span className="spec-label">探针版本</span>
                <div className="spec-value mono text-mint">
                  {node.version || 'ProbeWatch Agent v1.0'}
                </div>
              </div>
            </div>
          </div>

          {/* 核心资源占用条 */}
          <div className="drawer-section-card">
            <div className="section-card-title">
              <Pulse size={15} className="text-blue" />
              <span>核心资源负载</span>
            </div>

            {/* CPU */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name"><Cpu size={13} /> CPU 占用</span>
                <span className="meter-val mono">{dash(node.cpu, '%')}</span>
              </div>
              <ProgressBar value={node.cpu} tone="mint" height={6} />
            </div>

            {/* 内存 */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name"><Memory size={13} /> 物理内存</span>
                <span className="meter-val mono">
                  {node.memoryUsed !== null && node.memoryTotal
                    ? `${formatBytes(node.memoryUsed)} / ${formatBytes(node.memoryTotal)}`
                    : dash(node.memory, '%')}
                </span>
              </div>
              <ProgressBar value={node.memory} tone="blue" height={6} />
            </div>

            {/* Swap */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name"><Lightning size={13} /> 虚拟内存 (Swap)</span>
                <span className="meter-val mono">
                  {node.swapUsed !== null && node.swapTotal
                    ? `${formatBytes(node.swapUsed)} / ${formatBytes(node.swapTotal)}`
                    : dash(node.swap, '%')}
                </span>
              </div>
              <ProgressBar value={node.swap || 0} tone="violet" height={6} />
            </div>

            {/* 硬盘 */}
            <div className="drawer-meter-item">
              <div className="meter-label-row">
                <span className="meter-name"><HardDrive size={13} /> 根磁盘空间</span>
                <span className="meter-val mono">
                  {node.diskUsed !== null && node.diskTotal
                    ? `${formatBytes(node.diskUsed)} / ${formatBytes(node.diskTotal)}`
                    : dash(node.disk, '%')}
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
                <b className="transfer-val mono text-mint">{formatBytes(totalTransfer)}</b>
              </div>
            </div>
          </div>

          {/* MJJ 小鸡账单与剩余价值卡片 */}
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
                    <span>{billing.expireDate || '未配置'}</span>
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
        </div>

        {/* 底部操作栏 */}
        <div className="drawer-actions modern-drawer-actions">
          <button
            className="button button-primary drawer-button"
            onClick={() => onOpenDetails && onOpenDetails(node)}
          >
            <span>打开小鸡体检详情</span>
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
