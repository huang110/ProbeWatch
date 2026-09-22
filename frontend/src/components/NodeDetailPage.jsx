import { ArrowLeft, Broadcast } from '@phosphor-icons/react'
import { dash, formatBytes, formatEpochSeconds, formatRate, relativeHeartbeat, safeText, statusLabel } from '../lib/format.js'
import { DualLineChart, RingGauge, StatusDot } from './Common.jsx'
import { ChecksSummaryPanel } from './ChecksTable.jsx'
import { TrafficPanel } from './Traffic.jsx'

export function NodeDetailPage({ node, history = [], historyLoading = false, checksSummary = null, checksLoading = false, traffic = null, trafficLoading = false, trafficPeriod = 'day', onTrafficPeriodChange, onBack, rates = {} }) {
  const rate = rates[node.uuid || node.id] || null
  return <section className="subpage node-detail-page">
    <div className="detail-identity panel">
      <span className={`node-avatar node-avatar-lg node-avatar-${node.color}`}>{node.name.slice(0, 1).toUpperCase()}</span>
      <div className="detail-identity-main">
        <div className="detail-identity-title"><h1>{node.name}</h1><span className={`badge badge-${node.status}`}><StatusDot status={node.status} />{statusLabel(node.status)}</span></div>
        <p>UUID：{node.uuid || node.id || '—'}{node.hostname ? ` · 主机名：${node.hostname}` : ''} · 心跳：{relativeHeartbeat(node.lastReportedAt)}</p>
      </div>
      <button className="button button-quiet" onClick={onBack}><ArrowLeft size={15} /> 返回节点列表</button>
    </div>
    <div className="gauge-grid" aria-label="资源仪表">
      <RingGauge label="CPU" value={node.cpu} tone="mint" />
      <RingGauge label="内存" value={node.memory} tone="blue" />
      <RingGauge label="磁盘" value={node.disk} tone="amber" />
    </div>
    <div className="panel">
      <div className="panel-header"><div><h2>资源历史曲线</h2><p>resource history 的 CPU 与内存采样</p></div></div>
      {historyLoading ? <div className="empty-state"><strong>正在加载历史数据…</strong></div> : <DualLineChart points={history} />}
    </div>
    <div className="panel">
      <div className="panel-header"><div><h2>检测目标统计</h2><p>checks/summary 窗口聚合</p></div></div>
      <ChecksSummaryPanel rows={checksSummary} loading={checksLoading} />
    </div>
    <div className="panel">
      <div className="panel-header"><div><h2>窗口流量</h2><p>traffic 窗口增量与序列</p></div></div>
      <TrafficPanel traffic={traffic} loading={trafficLoading} period={trafficPeriod} onPeriodChange={onTrafficPeriodChange} />
    </div>
    <div className="panel">
      <div className="panel-header"><div><h2>节点信息</h2><p>最近一次资源上报的指纹</p></div></div>
      <div className="policy-list detail-list">
        <div><span>操作系统</span><b>{node.os || '—'}</b></div>
        <div><span>内核</span><b>{node.kernel || '—'}</b></div>
        <div><span>架构</span><b>{node.arch || '—'}</b></div>
        <div><span>Agent 版本</span><b>{node.agentVersion || '—'}</b></div>
        <div><span>开始时间</span><b>{formatEpochSeconds(node.startedAt)}</b></div>
        <div><span>内存明细</span><b>{formatBytes(node.resource.memory_used_bytes)} / {formatBytes(node.resource.memory_total_bytes)}</b></div>
        <div><span>磁盘明细</span><b>{formatBytes(node.resource.filesystem_used_bytes)} / {formatBytes(node.resource.filesystem_total_bytes)}</b></div>
        <div><span>网络收发累计</span><b>{formatBytes(node.rx)} / {formatBytes(node.tx)}</b></div>
        <div><span>网络速率估算</span><b>↓ {formatRate(rate?.down ?? null)} · ↑ {formatRate(rate?.up ?? null)}</b></div>
        <div><span>上报时间</span><b>{node.lastReportedAt ? new Date(node.lastReportedAt).toLocaleString('zh-CN') : '—'}</b></div>
      </div>
      {!safeText(node.os) && !safeText(node.kernel) && <p className="detail-hint"><Broadcast size={13} /> 该节点尚未上报系统指纹字段，对应项显示 —。</p>}
    </div>
  </section>
}
