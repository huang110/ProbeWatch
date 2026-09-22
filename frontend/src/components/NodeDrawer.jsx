import { useEffect, useRef } from 'react'
import { ArrowUpRight, X } from '@phosphor-icons/react'
import { dash, formatRate, relativeHeartbeat, statusLabel } from '../lib/format.js'
import { ProgressBar, StatusDot } from './Common.jsx'

export function NodeDrawer({ node, rates = {}, onClose, onOpenDetails }) {
  const closeButtonRef = useRef(null)
  useEffect(() => {
    const key = (event) => event.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    document.body.style.overflow = 'hidden'
    closeButtonRef.current?.focus()
    return () => { document.removeEventListener('keydown', key); document.body.style.overflow = '' }
  }, [onClose])
  const key = node.uuid || node.id
  const rate = rates[key] || null
  return <div className="drawer-backdrop" onClick={onClose}>
    <aside className="node-drawer" role="dialog" aria-modal="true" aria-labelledby="node-drawer-title" tabIndex="-1" onClick={(event) => event.stopPropagation()}>
      <div className="drawer-header">
        <div><span className="eyebrow">节点摘要</span><h2 id="node-drawer-title">{node.name}</h2></div>
        <button ref={closeButtonRef} className="icon-button" onClick={onClose} aria-label="关闭详情"><X size={19} /></button>
      </div>
      <div className="drawer-status"><StatusDot status={node.status} /><strong>{statusLabel(node.status)}</strong><span>心跳 {relativeHeartbeat(node.lastReportedAt)}</span></div>
      <div className="drawer-section">
        <h3>资源摘要</h3>
        <div className="drawer-bar"><span>CPU <b>{dash(node.cpu, '%')}</b></span><ProgressBar value={node.cpu} /></div>
        <div className="drawer-bar"><span>内存 <b>{dash(node.memory, '%')}</b></span><ProgressBar value={node.memory} tone="blue" /></div>
        <div className="drawer-bar"><span>磁盘 <b>{dash(node.disk, '%')}</b></span><ProgressBar value={node.disk} tone="amber" /></div>
        <div className="drawer-bar drawer-bar-plain"><span>网络 ↓</span><b>{formatRate(rate?.down ?? null)}</b></div>
        <div className="drawer-bar drawer-bar-plain"><span>网络 ↑</span><b>{formatRate(rate?.up ?? null)}</b></div>
      </div>
      <div className="drawer-actions">
        <button className="button button-primary drawer-button" onClick={() => onOpenDetails(node)}>打开完整详情 <ArrowUpRight size={16} /></button>
        <button className="button button-quiet" onClick={onClose}>关闭</button>
      </div>
    </aside>
  </div>
}
