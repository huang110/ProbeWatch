import { useMemo, useState } from 'react'
import { ArrowUpRight, ChartLineUp, CircleNotch, Cpu, Gauge, GlobeHemisphereWest, HardDrives, Memory, Package, ShieldCheck } from '@phosphor-icons/react'
import { formatBytes, formatPercent, numeric, safeArray } from '../lib/format.js'
import { StatCard } from './Common.jsx'
import { NodeTable } from './NodeList.jsx'
import { RankCard } from './RankList.jsx'
import { RecentAlerts } from './AlertList.jsx'

const trendOf = (series) => {
  const values = safeArray(series).map(numeric).filter((value) => value !== null)
  if (values.length < 2) return null
  const delta = values[values.length - 1] - values[0]
  if (!delta) return null
  const rounded = Math.round(Math.abs(delta) * 10) / 10
  return `${delta > 0 ? '+' : '-'}${rounded}`
}

export function OverviewPage({ data, overview, alerts, lossRates = {}, rates = {}, statHistory = [], onAck, ackingId, selectedNode, onSelectNode, onNavigate, isRefreshing, onRefresh, lastSyncText, apiState }) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('全部')
  const filtered = useMemo(() => data.filter((node) => `${node.name}${node.id}${node.uuid}`.toLowerCase().includes(query.toLowerCase()) && (filter === '全部' || (filter === '在线' && node.status === 'online') || (filter === '需关注' && node.status === 'attention') || (filter === '离线' && node.status === 'offline'))), [data, filter, query])
  const nodes = overview?.nodes || {}
  const checks = overview?.checks || {}
  const resources = overview?.resources || {}
  const online = numeric(nodes.online)
  const total = numeric(nodes.total)
  const avgLatency = numeric(checks.avg_latency_ms)
  const successRate = numeric(checks.success_rate)
  const rx = numeric(resources.network_rx_bytes_delta)
  const tx = numeric(resources.network_tx_bytes_delta)
  const trafficDelta = rx !== null || tx !== null ? (rx || 0) + (tx || 0) : null
  const cpuAvg = numeric(resources.cpu_percent)
  const memoryAvg = (() => { const used = numeric(resources.memory_used_bytes); const totalBytes = numeric(resources.memory_total_bytes); return used !== null && totalBytes > 0 ? Math.round((used / totalBytes) * 1000) / 10 : null })()
  const networkHistory = safeArray(resources.network_history).map(numeric).filter((value) => value !== null)
  const trafficSeries = networkHistory.length >= 2 ? networkHistory : statHistory.map((point) => point.traffic)
  const selectedId = selectedNode ? (selectedNode.uuid || selectedNode.id) : null
  const topBy = (pick, kind) => data.filter((node) => pick(node) !== null).sort((a, b) => pick(b) - pick(a)).slice(0, 5).map((node) => ({ key: node.uuid || node.id, label: node.name, value: pick(node), percent: pick(node), tone: kind === 'loss' ? 'amber' : kind === 'mem' ? 'blue' : 'mint', kind }))
  const cpuTop = topBy((node) => node.cpu, 'cpu')
  const memTop = topBy((node) => node.memory, 'mem')
  const lossTop = data.filter((node) => numeric(lossRates[node.uuid || node.id]) !== null).sort((a, b) => lossRates[b.uuid || b.id] - lossRates[a.uuid || a.id]).slice(0, 5).map((node) => ({ key: node.uuid || node.id, label: node.name, value: lossRates[node.uuid || node.id], percent: Math.min((lossRates[node.uuid || node.id] || 0) * 100, 100), tone: 'amber', kind: 'loss' }))
  return <>
    <section className="page-heading">
      <div><div className="eyebrow">实时监控 · 资源与历史独立刷新</div><h1>节点总览<span className="heading-period">。</span></h1><p>左侧为节点列表，右侧为资源排行与最近告警摘要。</p></div>
      <div className="heading-actions">
        <span className="last-sync">最后同步 <b>{lastSyncText}</b></span>
        <button className="button button-primary" onClick={onRefresh} disabled={isRefreshing}>{isRefreshing ? <CircleNotch size={17} className="spin" /> : <ChartLineUp size={17} />}{isRefreshing ? '正在同步' : '刷新数据'}</button>
      </div>
    </section>
    {apiState.message && <div className={`api-state api-state-${apiState.kind}`} role="status">{apiState.message}{apiState.kind === 'auth' && <button className="button button-primary" onClick={() => { window.location.href = '/auth/github' }}>使用 GitHub 登录</button>}</div>}
    <section className="stat-grid" aria-label="监控统计">
      <StatCard icon={GlobeHemisphereWest} label="节点在线" value={online !== null && total !== null ? `${online} / ${total}` : '—'} detail={`共 ${total ?? '—'} 个节点`} tone="mint" series={statHistory.map((point) => point.online)} trend={trendOf(statHistory.map((point) => point.online))} />
      <StatCard icon={Gauge} label="平均延迟" value={avgLatency !== null ? `${avgLatency} ms` : '—'} detail="overview.checks 平均延迟" tone="blue" series={statHistory.map((point) => point.latency)} trend={trendOf(statHistory.map((point) => point.latency))} />
      <StatCard icon={ShieldCheck} label="检测通过率" value={successRate !== null ? `${successRate}%` : '—'} detail="overview.checks.success_rate" tone="amber" series={statHistory.map((point) => point.success)} trend={trendOf(statHistory.map((point) => point.success))} />
      <StatCard icon={HardDrives} label="今日流量" value={trafficDelta !== null ? formatBytes(trafficDelta) : '—'} detail="overview.resources 网络增量" tone="violet" series={trafficSeries} trend={trendOf(trafficSeries)} />
      <StatCard icon={Cpu} label="CPU 平均" value={formatPercent(cpuAvg)} detail="overview.resources.cpu_percent" tone="rose" series={statHistory.map((point) => point.cpu)} trend={trendOf(statHistory.map((point) => point.cpu))} />
      <StatCard icon={Memory} label="内存平均" value={formatPercent(memoryAvg)} detail="overview.resources 内存占比" tone="mint" series={statHistory.map((point) => point.mem)} trend={trendOf(statHistory.map((point) => point.mem))} />
    </section>
    <section className="overview-columns">
      <div className="panel panel-nodes">
        <div className="panel-header"><div><h2>受控服务器节点</h2><p>哪吒风格卡片与紧凑表格双视图 · 点击查看小鸡体检详情</p></div><button className="text-button" onClick={() => onNavigate('nodes')}>查看全部 <ArrowUpRight size={15} /></button></div>
        <div className="node-toolbar">
          <label className="search-field"><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索节点或 ID" aria-label="搜索节点" /></label>
          <div className="filter-group" role="group" aria-label="节点状态筛选">{['全部', '在线', '需关注', '离线'].map((item) => <button key={item} type="button" className={filter === item ? 'filter-active' : ''} onClick={() => setFilter(item)}>{item}</button>)}</div>
        </div>
        <NodeTable nodes={filtered} rates={rates} lossRates={lossRates} selectedId={selectedId} onSelect={onSelectNode} />
      </div>
      <div className="side-stack">
        <RankCard title="CPU 占用 Top5" hint="resource.cpu_percent 排行" icon={Cpu} tone="mint" items={cpuTop} />
        <RankCard title="内存占用 Top5" hint="内存使用率排行" icon={Memory} tone="blue" items={memTop} />
        <RankCard title="丢包 Top5" hint="checks/summary 聚合丢包" icon={Package} tone="amber" items={lossTop} />
        <section className="panel recent-alerts-panel">
          <div className="panel-header"><div><h2>最近告警</h2><p>{alerts.length ? `共 ${alerts.length} 条 open / acked 告警` : '暂无告警'}</p></div><button className="text-button" onClick={() => onNavigate('alerts')}>查看全部 <ArrowUpRight size={15} /></button></div>
          <RecentAlerts alerts={alerts} onNavigate={onNavigate} />
        </section>
      </div>
    </section>
  </>
}
