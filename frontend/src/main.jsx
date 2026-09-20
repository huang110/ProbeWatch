import React, { StrictMode, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import {
  ArrowUpRight,
  Bell,
  Broadcast,
  CaretDown,
  Check,
  CircleNotch,
  CloudArrowDown,
  Database,
  DotsThree,
  Gauge,
  GlobeHemisphereWest,
  HardDrives,
  MagnifyingGlass,
  Pulse,
  ChartLineUp,
  ShieldCheck,
  SlidersHorizontal,
  SquaresFour,
  TrendDown,
  TrendUp,
  WifiHigh,
  X,
} from '@phosphor-icons/react'
import './styles.css'

const nodes = [
  { id: 'node-a', name: '新加坡节点', region: '新加坡', ip: '203.0.113.10', status: 'online', latency: 18, cpu: 12, memory: 41, disk: 48, traffic: '1.84 TB', group: '生产节点', color: 'mint' },
  { id: 'node-b', name: '杭州节点', region: '杭州', ip: '198.51.100.20', status: 'online', latency: 29, cpu: 8, memory: 36, disk: 62, traffic: '892 GB', group: '生产节点', color: 'blue' },
  { id: 'node-c', name: '洛杉矶节点', region: '洛杉矶', ip: '192.0.2.30', status: 'online', latency: 162, cpu: 4, memory: 28, disk: 33, traffic: '3.12 TB', group: '跨境线路', color: 'amber' },
  { id: 'node-d', name: '台北节点', region: '台北', ip: '203.0.113.40', status: 'online', latency: 46, cpu: 21, memory: 54, disk: 71, traffic: '1.08 TB', group: '跨境线路', color: 'rose' },
  { id: 'node-e', name: '香港节点', region: '香港', ip: '198.51.100.50', status: 'attention', latency: 83, cpu: 34, memory: 68, disk: 77, traffic: '734 GB', group: '测试节点', color: 'violet' },
  { id: 'node-f', name: '东京节点', region: '东京', ip: '192.0.2.60', status: 'offline', latency: null, cpu: null, memory: null, disk: 44, traffic: '—', group: '测试节点', color: 'gray' },
]

const navItems = [
  { id: 'overview', label: '总览', icon: SquaresFour },
  { id: 'nodes', label: '节点', icon: GlobeHemisphereWest },
  { id: 'network', label: '网络检测', icon: WifiHigh },
  { id: 'mtr', label: 'MTR 路由', icon: Broadcast },
  { id: 'media', label: '流媒体', icon: CloudArrowDown },
  { id: 'alerts', label: '告警事件', icon: Bell, count: 3 },
]

function Sparkline({ tone = 'mint', points = [28, 36, 31, 44, 39, 52, 47, 62, 56, 68, 64, 73] }) {
  const max = Math.max(...points)
  const min = Math.min(...points)
  const path = points.map((point, index) => {
    const x = (index / (points.length - 1)) * 100
    const y = 88 - ((point - min) / Math.max(max - min, 1)) * 64
    return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
  }).join(' ')
  return <svg className={`sparkline sparkline-${tone}`} viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true"><path className="sparkline-fill" d={`${path} L 100 100 L 0 100 Z`} /><path className="sparkline-line" d={path} /></svg>
}

function StatusDot({ status }) {
  return <span className={`status-dot status-${status}`} aria-label={status === 'online' ? '在线' : status === 'attention' ? '需要关注' : '离线'} />
}

function MetricCard({ icon: Icon, label, value, detail, trend, tone = 'mint', children }) {
  return <article className="metric-card"><div className="metric-topline"><span className={`metric-icon metric-icon-${tone}`}><Icon size={17} weight="duotone" /></span><span className="metric-label">{label}</span>{trend && <span className={trend.startsWith('+') ? 'trend trend-up' : 'trend trend-down'}>{trend.startsWith('+') ? <TrendUp size={13} /> : <TrendDown size={13} />}{trend}</span>}</div><div className="metric-value">{value}</div><div className="metric-detail">{detail}</div>{children}</article>
}

function ProgressBar({ value, tone = 'mint' }) {
  return <div className="progress-track"><span className={`progress-fill progress-${tone}`} style={{ width: `${Math.max(0, Math.min(100, value))}%` }} /></div>
}

function NodeList({ data, selectedNode, onSelect }) {
  return <div className="node-list">{data.map((node) => <button key={node.id} className={`node-row ${selectedNode?.id === node.id ? 'node-row-selected' : ''}`} onClick={() => onSelect(node)}><span className={`node-avatar node-avatar-${node.color}`}>{node.name.slice(0, 1)}</span><span className="node-main"><span className="node-name"><StatusDot status={node.status} />{node.name}</span><span className="node-meta">{node.region} <i /> {node.ip}</span></span><span className="node-resource"><span>CPU <b>{node.cpu === null ? '—' : `${node.cpu}%`}</b></span><span>内存 <b>{node.memory === null ? '—' : `${node.memory}%`}</b></span></span><span className={`latency ${node.latency === null ? 'latency-muted' : node.latency > 100 ? 'latency-warn' : ''}`}>{node.latency === null ? '—' : `${node.latency} ms`}</span><ArrowUpRight className="row-arrow" size={16} /></button>)}</div>
}

function NodeDrawer({ node, onClose, onOpenDetails }) {
  const closeButtonRef = useRef(null)
  useEffect(() => {
    const onKeyDown = (event) => event.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKeyDown)
    document.body.style.overflow = 'hidden'
    closeButtonRef.current?.focus()
    return () => { document.removeEventListener('keydown', onKeyDown); document.body.style.overflow = '' }
  }, [onClose])

  return <div className="drawer-backdrop" onClick={onClose}><aside className="node-drawer" role="dialog" aria-modal="true" aria-labelledby="node-drawer-title" tabIndex="-1" onClick={(event) => event.stopPropagation()}><div className="drawer-header"><div><span className="eyebrow">节点详情</span><h2 id="node-drawer-title">{node.name}</h2></div><button ref={closeButtonRef} className="icon-button" onClick={onClose} aria-label="关闭详情"><X size={19} /></button></div><div className="drawer-status"><StatusDot status={node.status} /><strong>{node.status === 'online' ? '在线运行中' : node.status === 'attention' ? '需要关注' : '节点离线'}</strong><span>当前演示数据</span></div><div className="drawer-metrics"><div><span>IPv4</span><strong>{node.ip}</strong></div><div><span>区域</span><strong>{node.region}</strong></div><div><span>延迟</span><strong>{node.latency ? `${node.latency} ms` : '—'}</strong></div></div><div className="drawer-section"><h3>资源使用</h3><div className="drawer-bar"><span>CPU <b>{node.cpu ?? '—'}%</b></span><ProgressBar value={node.cpu ?? 0} /></div><div className="drawer-bar"><span>内存 <b>{node.memory ?? '—'}%</b></span><ProgressBar value={node.memory ?? 0} tone="blue" /></div><div className="drawer-bar"><span>磁盘 <b>{node.disk}%</b></span><ProgressBar value={node.disk} tone={node.disk > 70 ? 'amber' : 'mint'} /></div></div><div className="drawer-section"><h3>检测摘要</h3><div className="drawer-check"><span><WifiHigh size={16} />网络可用性</span><b className="check-pass">通过</b></div><div className="drawer-check"><span><Broadcast size={16} />MTR 路由</span><b className="check-warn">有变化</b></div><div className="drawer-check"><span><CloudArrowDown size={16} />流媒体检测</span><b className="check-pass">18 / 24</b></div></div><button className="button button-primary drawer-button" onClick={() => onOpenDetails(node)}>打开完整详情 <ArrowUpRight size={16} /></button></aside></div>
}

function OverviewPage({ data, selectedNode, setSelectedNode, onNavigate, isRefreshing, onRefresh, lastSync }) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('全部')
  const filteredNodes = useMemo(() => data.filter((node) => `${node.name}${node.region}${node.ip}`.toLowerCase().includes(query.toLowerCase()) && (filter === '全部' || (filter === '在线' && node.status === 'online') || (filter === '需关注' && node.status === 'attention') || (filter === '离线' && node.status === 'offline'))), [data, filter, query])
  const onlineCount = data.filter((node) => node.status === 'online').length
  return <><section className="page-heading"><div><div className="eyebrow">实时监控 · {new Date().toLocaleDateString('zh-CN')}</div><h1>节点状态<span className="heading-period">。</span></h1><p>从资源、网络到出口能力，查看所有节点的实时证据。</p></div><div className="heading-actions"><span className="last-sync">最后同步 <b>{lastSync}</b></span><button className="button button-primary" onClick={onRefresh} disabled={isRefreshing}>{isRefreshing ? <CircleNotch size={17} className="spin" /> : <ChartLineUp size={17} />}{isRefreshing ? '正在同步' : '立即同步'}</button></div></section><section className="metrics-grid" aria-label="监控摘要"><MetricCard icon={GlobeHemisphereWest} label="节点在线" value={`${onlineCount} / ${data.length}`} detail={`${data.length - onlineCount} 个节点需要处理`} trend="+1" /><MetricCard icon={Gauge} label="平均延迟" value="68 ms" detail="较昨日下降 4.6%" trend="-4.6%" tone="blue" /><MetricCard icon={ShieldCheck} label="检测通过率" value="98.7%" detail="过去 24 小时" trend="+0.8%" tone="amber" /><MetricCard icon={HardDrives} label="流量总计" value="7.66 TB" detail={`本月累计 · ${data.length} 个节点`} tone="violet"><Sparkline tone="violet" points={[34, 41, 38, 50, 48, 58, 64, 57, 70, 74, 82, 88]} /></MetricCard></section><section className="content-grid"><div className="panel panel-nodes"><div className="panel-header"><div><h2>节点概览</h2><p>按在线状态和检测延迟排序</p></div><button className="text-button" onClick={() => onNavigate('nodes')}>查看全部 <ArrowUpRight size={15} /></button></div><div className="node-toolbar"><label className="search-field"><MagnifyingGlass size={16} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索节点或 IP" /></label><div className="filter-group" role="group" aria-label="节点状态筛选">{['全部', '在线', '需关注', '离线'].map((item) => <button key={item} className={filter === item ? 'filter-active' : ''} onClick={() => setFilter(item)}>{item}</button>)}</div></div>{filteredNodes.length ? <NodeList data={filteredNodes} selectedNode={selectedNode} onSelect={setSelectedNode} /> : <div className="empty-state"><MagnifyingGlass size={24} /><strong>没有匹配的节点</strong><span>换一个关键词或清除筛选条件。</span></div>}</div><AlertPanel onNavigate={onNavigate} /></section><EvidencePanels onNavigate={onNavigate} /></>
}

function AlertPanel({ onNavigate }) {
  return <div className="panel panel-alerts"><div className="panel-header"><div><h2>需要关注</h2><p>按优先级排列的最新事件</p></div><button className="icon-button icon-button-small" aria-label="更多告警" onClick={() => onNavigate('alerts')}><DotsThree size={19} /></button></div><div className="alert-list"><article className="alert-item alert-item-critical"><div className="alert-icon"><WifiHigh size={16} /></div><div><strong>喵云 · 节点离线</strong><p>最后心跳在 26 分钟前</p><small>26 分钟前</small></div></article><article className="alert-item alert-item-warning"><div className="alert-icon"><TrendUp size={16} /></div><div><strong>六六云 · 磁盘使用率 77%</strong><p>接近设定阈值 80%</p><small>42 分钟前</small></div></article><article className="alert-item alert-item-info"><div className="alert-icon"><Broadcast size={16} /></div><div><strong>甲骨文 · MTR 路径变化</strong><p>到 Cloudflare 的路径出现新跳点</p><small>1 小时前</small></div></article></div><button className="button button-quiet alert-button" onClick={() => onNavigate('alerts')}>查看全部事件 <ArrowUpRight size={15} /></button></div>
}

function EvidencePanels({ onNavigate }) {
  return <section className="lower-grid"><div className="panel evidence-panel"><div className="panel-header"><div><h2>MTR 路由证据</h2><p>过去 24 小时的路径指纹变化</p></div><button className="text-button" onClick={() => onNavigate('mtr')}>打开 MTR <ArrowUpRight size={15} /></button></div><div className="evidence-content"><div className="route-summary"><div className="route-stat"><span className="route-stat-value">24</span><span>已完成检测</span></div><div className="route-stat"><span className="route-stat-value route-stat-warn">2</span><span>路径发生变化</span></div><div className="route-stat"><span className="route-stat-value">0.3%</span><span>末端丢包率</span></div></div><div className="route-visual"><div className="route-line"><span className="route-node route-node-start" /><span className="route-segment" /><span className="route-node" /><span className="route-segment" /><span className="route-node route-node-warn" /><span className="route-segment" /><span className="route-node" /><span className="route-segment" /><span className="route-node route-node-end" /></div><div className="route-labels"><span>本机</span><span>运营商</span><span className="route-label-warn">新跳点</span><span>骨干网</span><span>Cloudflare</span></div></div></div></div><div className="panel media-panel"><div className="panel-header"><div><h2>出口能力</h2><p>最近一次检测 · 2 小时前</p></div><button className="text-button" onClick={() => onNavigate('media')}>查看详情 <ArrowUpRight size={15} /></button></div><div className="media-summary"><div className="media-score"><span>18</span><small>/ 24 项通过</small></div><ProgressBar value={75} tone="mint" /></div><div className="media-grid"><div className="media-cell"><span className="media-name">Netflix</span><span className="media-region">SG</span><Check size={15} className="media-ok" /></div><div className="media-cell"><span className="media-name">YouTube</span><span className="media-region">SG</span><Check size={15} className="media-ok" /></div><div className="media-cell"><span className="media-name">Disney+</span><span className="media-region">—</span><X size={15} className="media-fail" /></div><div className="media-cell"><span className="media-name">TikTok</span><span className="media-region">SG</span><Check size={15} className="media-ok" /></div></div></div></section>
}

function SubPage({ page, data, onBack, onSelectNode }) {
  const pages = { nodes: ['节点', '查看所有已注册节点和探针状态。'], network: ['网络检测', '管理 TCP、HTTP、HTTPS 和 DNS 检测目标。'], mtr: ['MTR 路由', '查看各节点到公共目标的路径指纹和丢包证据。'], media: ['流媒体', '查看出口能力检测器和区域识别结果。'], alerts: ['告警事件', '查看离线、资源阈值和检测变化事件。'], targets: ['检测目标', '统一维护网络与 MTR 检测模板。'], settings: ['系统设置', '配置数据保留、通知和私密状态页。'] }
  const [title, description] = pages[page] || pages.nodes
  return <section className="subpage"><div className="subpage-heading"><div><div className="eyebrow">监控模块</div><h1>{title}<span className="heading-period">。</span></h1><p>{description}</p></div><button className="button button-quiet" onClick={onBack}>返回总览</button></div>{page === 'nodes' ? <div className="panel"><div className="panel-header"><div><h2>节点清单</h2><p>共 {data.length} 个节点</p></div></div><NodeList data={data} onSelect={onSelectNode} /></div> : page === 'mtr' ? <div className="panel"><div className="panel-header"><div><h2>路径指纹</h2><p>检测周期：每 1 小时</p></div><span className="pill pill-warning">2 条路径有变化</span></div><div className="route-table"><div className="route-table-row route-table-head"><span>节点</span><span>目标</span><span>末端丢包</span><span>状态</span></div>{data.slice(0, 5).map((node) => <div className="route-table-row" key={node.id}><span>{node.name}</span><span>Cloudflare</span><span>{node.status === 'offline' ? '—' : '0.3%'}</span><span className={node.id === 'oracle' ? 'check-warn' : 'check-pass'}>{node.id === 'oracle' ? '路径变化' : '稳定'}</span></div>)}</div></div> : <div className="subpage-grid"><div className="panel placeholder-panel"><div className="placeholder-icon"><Pulse size={22} /></div><h2>{title}数据面板</h2><p>此模块已完成中文页面和导航骨架，等待 Go 主控 API 接入真实数据。</p><button className="button button-quiet" onClick={onBack}>返回总览</button></div><div className="panel"><h2>当前策略</h2><div className="policy-list"><div><span>默认频率</span><b>{page === 'media' ? '每 12 小时' : page === 'mtr' ? '每 1 小时' : '每 5 分钟'}</b></div><div><span>数据来源</span><b>节点本地 Agent</b></div><div><span>远程执行</span><b className="check-pass">已禁用</b></div></div></div></div>}</section>
}

function NodeDetailPage({ node, onBack }) {
  return <section className="subpage node-detail-page"><div className="subpage-heading"><div><div className="eyebrow">节点详情 · {node.group}</div><h1>{node.name}<span className="heading-period">。</span></h1><p>{node.region} · {node.ip} · 探针状态：{node.status === 'online' ? '在线' : node.status === 'attention' ? '需要关注' : '离线'}</p></div><button className="button button-quiet" onClick={onBack}>返回节点列表</button></div><div className="metrics-grid"><MetricCard icon={Gauge} label="当前延迟" value={node.latency ? `${node.latency} ms` : '—'} detail="最近一次网络检测" tone="blue" /><MetricCard icon={ShieldCheck} label="资源健康度" value={node.status === 'offline' ? '—' : '良好'} detail="根据 CPU、内存和磁盘计算" /><MetricCard icon={HardDrives} label="本月流量" value={node.traffic} detail="来自节点本地探针" tone="violet" /><MetricCard icon={Broadcast} label="MTR 状态" value="有变化" detail="Cloudflare 路径指纹" tone="amber" /></div><div className="subpage-grid"><div className="panel"><div className="panel-header"><div><h2>资源时间线</h2><p>最近 12 个采样点 · 30 秒间隔</p></div><span className="pill pill-success">采集正常</span></div><div className="detail-chart"><div className="chart-grid"><span>100%</span><span>75%</span><span>50%</span><span>25%</span><span>0%</span></div><div className="chart-area"><Sparkline tone="mint" points={[22, 29, 26, 35, 31, 40, 34, 47, 43, 51, 48, node.cpu ?? 0]} /><div className="chart-caption"><span>12 个采样点</span><span>当前 CPU {node.cpu ?? '—'}%</span></div></div></div></div><div className="panel"><div className="panel-header"><div><h2>资源明细</h2><p>节点最近一次上报</p></div></div><div className="policy-list detail-list"><div><span>CPU 使用率</span><b>{node.cpu ?? '—'}%</b></div><div><span>内存使用率</span><b>{node.memory ?? '—'}%</b></div><div><span>磁盘使用率</span><b>{node.disk}%</b></div><div><span>检测频率</span><b>30 秒</b></div></div></div></div><div className="panel detail-checks"><div className="panel-header"><div><h2>检测摘要</h2><p>来自网络、MTR 和流媒体检测器</p></div></div><div className="detail-check-grid"><div><span><WifiHigh size={17} />网络可用性</span><b className="check-pass">通过</b></div><div><span><Broadcast size={17} />MTR 路由</span><b className="check-warn">有变化</b></div><div><span><CloudArrowDown size={17} />流媒体出口</span><b className="check-pass">18 / 24</b></div></div></div></section>
}

function App() {
  const [activeNav, setActiveNav] = useState('overview')
  const [selectedNode, setSelectedNode] = useState(null)
  const [detailNode, setDetailNode] = useState(null)
  const [data, setData] = useState(nodes)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [lastSync, setLastSync] = useState('刚刚')

  const navigate = (page) => setActiveNav(page)
  const refresh = () => {
    setIsRefreshing(true)
    window.setTimeout(() => { setData((current) => current.map((node) => node.status === 'online' ? { ...node, latency: Math.max(8, node.latency + (node.id.length % 3) - 1) } : node)); setLastSync('刚刚'); setIsRefreshing(false) }, 650)
  }

  const pageTitle = activeNav === 'node-detail' ? '节点详情' : navItems.find((item) => item.id === activeNav)?.label || ({ targets: '检测目标', settings: '系统设置' }[activeNav] || '总览')
  return <div className="app-shell"><aside className="sidebar"><div className="brand-lockup"><div className="brand-mark"><Pulse size={21} weight="bold" /></div><div><strong>ProbeWatch</strong><span>纯监控控制台</span></div></div><div className="workspace-switcher"><div className="workspace-avatar">L</div><div><span>工作区</span><strong>演示节点组</strong></div><CaretDown size={15} className="muted" /></div><nav className="side-nav" aria-label="主导航"><span className="nav-section-label">监控</span>{navItems.map(({ id, label, icon: Icon, count }) => <button key={id} className={`nav-item ${activeNav === id ? 'nav-item-active' : ''}`} onClick={() => navigate(id)}><Icon size={18} weight={activeNav === id ? 'fill' : 'regular'} /><span>{label}</span>{count && <small>{count}</small>}</button>)}<span className="nav-section-label nav-section-spaced">配置</span><button className={`nav-item ${activeNav === 'targets' ? 'nav-item-active' : ''}`} onClick={() => navigate('targets')}><SlidersHorizontal size={18} /><span>检测目标</span></button><button className={`nav-item ${activeNav === 'settings' ? 'nav-item-active' : ''}`} onClick={() => navigate('settings')}><Database size={18} /><span>系统设置</span></button></nav><div className="sidebar-footer"><div className="health-chip"><span className="status-dot status-attention" /><span>演示数据模式</span><span className="mono">v0.1.0</span></div><div className="profile-row"><div className="profile-avatar">DE</div><div><strong>演示管理员</strong><span>未连接主控</span></div><DotsThree size={20} className="muted" /></div></div></aside><main className="main-content"><header className="topbar"><div className="mobile-brand"><div className="brand-mark"><Pulse size={18} weight="bold" /></div><strong>ProbeWatch</strong></div><div className="breadcrumb"><span>监控</span><span className="breadcrumb-slash">/</span><strong>{pageTitle}</strong></div><div className="top-actions"><span className="sync-state"><span className="status-dot status-attention" />演示数据</span><button className="icon-button" aria-label="查看告警" onClick={() => navigate('alerts')}><Bell size={19} /><span className="notification-badge">3</span></button><button className="profile-avatar profile-avatar-top" aria-label="演示管理员菜单">DE</button></div></header><div className="content-wrap">{activeNav === 'overview' ? <OverviewPage data={data} selectedNode={selectedNode} setSelectedNode={setSelectedNode} onNavigate={navigate} isRefreshing={isRefreshing} onRefresh={refresh} lastSync={lastSync} /> : activeNav === 'node-detail' && detailNode ? <NodeDetailPage node={detailNode} onBack={() => navigate('nodes')} /> : <SubPage page={activeNav} data={data} onBack={() => navigate('overview')} onSelectNode={setSelectedNode} />}<footer className="content-footer"><span><span className="status-dot status-attention" />当前为本地演示数据，未连接主控</span><span className="footer-divider" /><span>资源 30 秒 · 网络 5 分钟 · MTR 1 小时 · 流媒体 12 小时</span></footer></div></main>{selectedNode && <NodeDrawer node={selectedNode} onClose={() => setSelectedNode(null)} onOpenDetails={(node) => { setSelectedNode(null); setDetailNode(node); navigate('node-detail') }} />}</div>
}

createRoot(document.getElementById('root')).render(<StrictMode><App /></StrictMode>)
