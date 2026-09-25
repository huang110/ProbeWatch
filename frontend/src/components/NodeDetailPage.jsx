import { useEffect, useState } from 'react'
import { ArrowLeft, Broadcast, Check, Copy } from '@phosphor-icons/react'
import { dash, formatBytes, formatEpochSeconds, formatRate, relativeHeartbeat, safeText, statusLabel, formatLoad } from '../lib/format.js'
import { DualLineChart, RingGauge, StatusDot } from './Common.jsx'
import { ChecksSummaryPanel } from './ChecksTable.jsx'
import { TrafficPanel } from './Traffic.jsx'

export function NodeDetailPage({
  node,
  loading = false,
  history = [],
  historyLoading = false,
  checksSummary = null,
  checksLoading = false,
  traffic = null,
  trafficLoading = false,
  trafficPeriod = 'day',
  onTrafficPeriodChange,
  onBack,
  rates = {},
}) {
  const [copied, setCopied] = useState(false)
  const [networkHistory, setNetworkHistory] = useState([])

  const nodeUuid = node?.uuid || node?.id || ''

  useEffect(() => {
    if (!nodeUuid) {
      setNetworkHistory([])
      return undefined
    }
    const controller = new AbortController()
    fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/network/history?limit=60`, {
      credentials: 'same-origin',
      signal: controller.signal,
    })
      .then((res) => (res.ok ? res.json() : []))
      .then((json) => {
        if (!controller.signal.aborted && Array.isArray(json)) {
          setNetworkHistory(json)
        }
      })
      .catch(() => {})
    return () => controller.abort()
  }, [nodeUuid])
  if (!node) {
    return (
      <section className="subpage node-detail-page">
        <div className="panel" style={{ textAlign: 'center', padding: '40px 20px' }}>
          <p style={{ color: 'var(--text-3)', marginBottom: '16px' }}>
            {loading ? '正在同步节点清单与详情…' : '未找到指定节点的信息或该节点已被移除。'}
          </p>
          <button type="button" className="button button-primary" onClick={onBack}>
            <ArrowLeft size={15} /> 返回节点列表
          </button>
        </div>
      </section>
    )
  }

  const handleCopyUuid = () => {
    const text = node.uuid || node.id
    if (text && navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const nodeKey = node.uuid || node.id || ''
  const rate = rates[nodeKey] || null
  const nodeName = node.name || '未命名节点'
  const firstChar = (nodeName.slice(0, 1) || 'N').toUpperCase()
  const resource = node.resource || {}

  const memUsed = node.memUsed ?? resource.memory_used_bytes ?? null
  const memTotal = node.memTotal ?? resource.memory_total_bytes ?? null
  const diskUsed = node.diskUsed ?? resource.filesystem_used_bytes ?? null
  const diskTotal = node.diskTotal ?? resource.filesystem_total_bytes ?? null

  return (
    <section className="subpage node-detail-page">
      <div className="detail-identity panel">
        <span className={`node-avatar node-avatar-lg node-avatar-${node.color || 'blue'}`}>
          {firstChar}
        </span>
        <div className="detail-identity-main">
          <div className="detail-identity-title">
            <h1>{nodeName}</h1>
            <span className={`badge badge-${node.status || 'unknown'}`}>
              <StatusDot status={node.status} />
              {statusLabel(node.status)}
            </span>
            {node.flag && (
              <span className="badge badge-subtle">
                <span>{node.flag}</span>
                <span>{node.region || '公网节点'}</span>
              </span>
            )}
            {node.uptime && (
              <span className="badge badge-subtle mono">
                <span>运行时长: {node.uptime}</span>
              </span>
            )}
          </div>
          <p>
            UUID：{node.uuid || node.id || '—'}
            {node.hostname ? ` · 主机名：${node.hostname}` : ''}
            {' · '}心跳：{relativeHeartbeat(node.lastReportedAt)}
            {(node.uuid || node.id) && (
              <button
                type="button"
                className="copy-chip-btn"
                onClick={handleCopyUuid}
                title="复制节点 UUID"
                aria-label="复制节点 UUID"
              >
                {copied ? <Check size={11} className="text-mint" /> : <Copy size={11} />}
                <span>{copied ? '已复制' : '复制 UUID'}</span>
              </button>
            )}
          </p>
        </div>
        <button type="button" className="button button-quiet" onClick={onBack}>
          <ArrowLeft size={15} /> 返回节点列表
        </button>
      </div>

      <div className="gauge-grid" aria-label="资源仪表">
        <RingGauge
          label="CPU"
          value={node.cpu}
          tone="mint"
          detail={formatLoad(node.load1 ?? resource.load1, node.load5 ?? resource.load5, node.load15 ?? resource.load15)}
          series={Array.isArray(history) && history.length ? history.map((h) => (h && typeof h === 'object' ? h.cpu : h)).filter((v) => v !== null && v !== undefined) : [node.cpu]}
        />
        <RingGauge
          label="内存"
          value={node.memory}
          tone="blue"
          detail={memUsed !== null && memTotal ? `${formatBytes(memUsed)} / ${formatBytes(memTotal)}` : null}
          series={Array.isArray(history) && history.length ? history.map((h) => (h && typeof h === 'object' ? h.mem : h)).filter((v) => v !== null && v !== undefined) : [node.memory]}
        />
        <RingGauge
          label="磁盘"
          value={node.disk}
          tone="amber"
          detail={diskUsed !== null && diskTotal ? `${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}` : null}
          series={Array.isArray(history) && history.length ? history.map((h) => (h && typeof h === 'object' ? h.disk : h)).filter((v) => v !== null && v !== undefined) : [node.disk]}
        />
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>资源历史曲线</h2>
            <p>resource history 的 CPU 与内存采样走势</p>
          </div>
        </div>
        {historyLoading ? (
          <div className="empty-state">
            <strong>正在加载历史数据…</strong>
          </div>
        ) : (
          <DualLineChart points={Array.isArray(history) ? history : []} />
        )}
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>检测目标统计</h2>
            <p>checks/summary 窗口聚合质量与丢包</p>
          </div>
        </div>
        <ChecksSummaryPanel rows={checksSummary} loading={checksLoading} networkHistory={networkHistory} />
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>窗口流量</h2>
            <p>traffic 窗口增量与序列分析</p>
          </div>
        </div>
        <TrafficPanel
          traffic={traffic}
          loading={trafficLoading}
          period={trafficPeriod}
          onPeriodChange={onTrafficPeriodChange}
        />
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>节点系统指纹与规格</h2>
            <p>最近一次 Agent 资源上报的系统指纹与环境信息</p>
          </div>
        </div>
        <div className="policy-list detail-list">
          <div>
            <span>操作系统</span>
            <b>{node.os || resource.os || '—'}</b>
          </div>
          <div>
            <span>内核</span>
            <b>{node.kernel || resource.kernel || '—'}</b>
          </div>
          <div>
            <span>架构</span>
            <b>{node.arch || resource.arch || '—'}</b>
          </div>
          <div>
            <span>Agent 版本</span>
            <b>{node.agentVersion || resource.agent_version || '—'}</b>
          </div>
          <div>
            <span>开始时间</span>
            <b>{formatEpochSeconds(node.startedAt || resource.started_at)}</b>
          </div>
          <div>
            <span>内存明细</span>
            <b>
              {formatBytes(memUsed)} / {formatBytes(memTotal)}
            </b>
          </div>
          <div>
            <span>磁盘明细</span>
            <b>
              {formatBytes(diskUsed)} / {formatBytes(diskTotal)}
            </b>
          </div>
          <div>
            <span>网络收发累计</span>
            <b>
              ↓ {formatBytes(node.rx)} · ↑ {formatBytes(node.tx)}
            </b>
          </div>
          <div>
            <span>网络速率估算</span>
            <b>
              ↓ {formatRate(rate?.down ?? null)} · ↑ {formatRate(rate?.up ?? null)}
            </b>
          </div>
          <div>
            <span>最新上报时间</span>
            <b>
              {node.lastReportedAt
                ? new Date(node.lastReportedAt).toLocaleString('zh-CN')
                : '—'}
            </b>
          </div>
        </div>
        {!safeText(node.os) && !safeText(node.kernel) && (
          <p className="detail-hint">
            <Broadcast size={13} /> 该节点尚未上报系统指纹字段，对应项显示 —。
          </p>
        )}
      </div>
    </section>
  )
}
