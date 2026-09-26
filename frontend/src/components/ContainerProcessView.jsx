import { useCallback, useEffect, useState, useMemo } from 'react'
import {
  ArrowsClockwise,
  Cpu,
  HardDrives,
  MagnifyingGlass,
  Cube,
  Play,
  Stop,
  Warning,
  CheckCircle,
  XCircle,
  Clock,
  Copy,
  Check,
  Rows,
} from '@phosphor-icons/react'
import {
  fetchFleetContainerOverview,
  fetchNodeContainers,
  fetchNodeProcesses,
  fetchNodes,
} from '../lib/api.js'

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i]
}

export function ContainerProcessView({ initialNodeId = '' }) {
  const [activeTab, setActiveTab] = useState('containers') // 'containers' | 'processes'
  const [nodes, setNodes] = useState([])
  const [selectedNode, setSelectedNode] = useState(initialNodeId) // '' = all fleet
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState(null)
  const [copiedId, setCopiedId] = useState('')

  // Fleet overview data
  const [overview, setOverview] = useState({
    total_nodes: 0,
    nodes_with_docker: 0,
    total_containers: 0,
    running_containers: 0,
    stopped_containers: 0,
    unhealthy_containers: 0,
    top_cpu_containers: [],
    top_memory_containers: [],
  })

  // Selected node containers
  const [nodeContainersData, setNodeContainersData] = useState({
    node_id: '',
    node_name: '',
    docker_available: false,
    docker_version: '',
    containers_total: 0,
    containers_running: 0,
    containers_stopped: 0,
    containers: [],
  })

  // Selected node processes
  const [nodeProcessesData, setNodeProcessesData] = useState({
    node_id: '',
    node_name: '',
    top_processes: [],
    reported_at: 0,
  })

  // Filters
  const [searchTerm, setSearchTerm] = useState('')
  const [stateFilter, setStateFilter] = useState('all') // 'all' | 'running' | 'stopped' | 'unhealthy'
  const [processSortBy, setProcessSortBy] = useState('memory') // 'memory' | 'cpu'

  // Load nodes list once
  useEffect(() => {
    fetchNodes()
      .then((data) => {
        const list = Array.isArray(data) ? data : data.nodes || []
        setNodes(list)
      })
      .catch(() => {})
  }, [])

  // Main data loader
  const loadData = useCallback(async (isManual = false) => {
    if (isManual) setRefreshing(true)
    setError(null)

    try {
      if (!selectedNode) {
        // Fleet overview mode
        const overData = await fetchFleetContainerOverview()
        setOverview(overData || {})
      } else {
        // Specific node mode
        const [cData, pData] = await Promise.all([
          fetchNodeContainers(selectedNode).catch(() => ({
            node_id: selectedNode,
            containers: [],
            docker_available: false,
          })),
          fetchNodeProcesses(selectedNode).catch(() => ({
            node_id: selectedNode,
            top_processes: [],
          })),
        ])
        setNodeContainersData(cData || {})
        setNodeProcessesData(pData || {})
      }
    } catch (err) {
      setError(err.message || '加载工作负载数据失败')
    } finally {
      setLoading(false)
      if (isManual) setRefreshing(false)
    }
  }, [selectedNode])

  useEffect(() => {
    loadData()
    const timer = setInterval(() => loadData(), 15000)
    return () => clearInterval(timer)
  }, [loadData])

  const copyToClipboard = (text, id) => {
    navigator.clipboard.writeText(text)
    setCopiedId(id)
    setTimeout(() => setCopiedId(''), 2000)
  }

  // Active containers list depending on fleet or single node
  const activeContainersList = useMemo(() => {
    if (!selectedNode) {
      // In fleet mode, show Top CPU containers or all available
      const combined = [...(overview.top_cpu_containers || [])]
      return combined
    }
    return nodeContainersData.containers || []
  }, [selectedNode, overview, nodeContainersData])

  // Filtered containers
  const filteredContainers = useMemo(() => {
    return activeContainersList.filter((c) => {
      const name = (c.name || '').toLowerCase()
      const image = (c.image || '').toLowerCase()
      const term = searchTerm.toLowerCase()
      const matchesSearch = !term || name.includes(term) || image.includes(term) || (c.ports || []).some(p => p.toLowerCase().includes(term))

      if (!matchesSearch) return false

      if (stateFilter === 'running') return c.state === 'running'
      if (stateFilter === 'stopped') return c.state !== 'running'
      if (stateFilter === 'unhealthy') return c.health === 'unhealthy'
      return true
    })
  }, [activeContainersList, searchTerm, stateFilter])

  // Filtered & sorted processes
  const sortedProcesses = useMemo(() => {
    const list = [...(nodeProcessesData.top_processes || [])]
    return list
      .filter((p) => {
        const name = (p.name || '').toLowerCase()
        const user = (p.user || '').toLowerCase()
        const cmd = (p.command_line || '').toLowerCase()
        const term = searchTerm.toLowerCase()
        return !term || name.includes(term) || user.includes(term) || cmd.includes(term) || String(p.pid).includes(term)
      })
      .sort((a, b) => {
        if (processSortBy === 'cpu') return (b.cpu_percent || 0) - (a.cpu_percent || 0)
        return (b.memory_rss_bytes || 0) - (a.memory_rss_bytes || 0)
      })
  }, [nodeProcessesData.top_processes, searchTerm, processSortBy])

  return (
    <div className="subpage-container" style={{ padding: '24px 32px', maxWidth: '1440px', margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '24px', flexWrap: 'wrap', gap: '16px' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            <div style={{ width: '32px', height: '32px', borderRadius: '8px', background: 'rgba(94, 106, 210, 0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#5e6ad2' }}>
              <Cube size={20} weight="duotone" />
            </div>
            <h1 style={{ fontSize: '20px', fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>
              容器与微服务观测 (Containers & Processes)
            </h1>
          </div>
          <p style={{ fontSize: '13px', color: 'var(--text-muted)', margin: '4px 0 0 42px' }}>
            分布式 Docker 容器生命周期度量、宿主资源杀手排查与 Top 进程深度剖析
          </p>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          {/* Node Selector */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', background: 'var(--panel-bg, #0e1012)', border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: '6px', padding: '0 10px', height: '36px' }}>
            <HardDrives size={16} color="var(--text-muted)" />
            <select
              value={selectedNode}
              onChange={(e) => setSelectedNode(e.target.value)}
              style={{ background: 'transparent', border: 'none', color: 'var(--text-primary)', fontSize: '13px', outline: 'none', cursor: 'pointer' }}
            >
              <option value="" style={{ background: '#141618' }}>🌐 全机群概览 (All Fleet)</option>
              {nodes.map((n) => (
                <option key={n.uuid || n.id} value={n.uuid || n.id} style={{ background: '#141618' }}>
                  {n.name} ({n.uuid?.slice(0, 8)})
                </option>
              ))}
            </select>
          </div>

          {/* Refresh Button */}
          <button
            onClick={() => loadData(true)}
            disabled={refreshing}
            className="btn btn-secondary"
            style={{ display: 'flex', alignItems: 'center', gap: '6px', height: '36px', padding: '0 14px', fontSize: '13px' }}
          >
            <ArrowsClockwise size={16} className={refreshing ? 'spin-animation' : ''} />
            刷新
          </button>
        </div>
      </div>

      {/* KPI Cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginBottom: '24px' }}>
        <div className="panel" style={{ padding: '16px', border: '1px solid rgba(255, 255, 255, 0.07)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: 'var(--text-muted)', fontSize: '12px' }}>
            <span>总纳管容器</span>
            <Cube size={16} color="#5e6ad2" />
          </div>
          <div style={{ fontSize: '24px', fontWeight: 700, color: 'var(--text-primary)', marginTop: '8px' }}>
            {selectedNode ? nodeContainersData.containers_total : overview.total_containers}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            {selectedNode
              ? (nodeContainersData.docker_available ? `Docker v${nodeContainersData.docker_version || 'active'}` : '未安装或无 Docker Socket')
              : `${overview.nodes_with_docker} / ${overview.total_nodes} 节点启用 Docker`}
          </div>
        </div>

        <div className="panel" style={{ padding: '16px', border: '1px solid rgba(255, 255, 255, 0.07)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: '#10b981', fontSize: '12px' }}>
            <span>运行中 (Running)</span>
            <Play size={16} />
          </div>
          <div style={{ fontSize: '24px', fontWeight: 700, color: '#10b981', marginTop: '8px' }}>
            {selectedNode ? nodeContainersData.containers_running : overview.running_containers}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            正常提供服务
          </div>
        </div>

        <div className="panel" style={{ padding: '16px', border: '1px solid rgba(255, 255, 255, 0.07)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: '#94a3b8', fontSize: '12px' }}>
            <span>已退出 / 停止</span>
            <Stop size={16} />
          </div>
          <div style={{ fontSize: '24px', fontWeight: 700, color: '#94a3b8', marginTop: '8px' }}>
            {selectedNode ? nodeContainersData.containers_stopped : overview.stopped_containers}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            休眠或历史任务
          </div>
        </div>

        <div className="panel" style={{ padding: '16px', border: '1px solid rgba(255, 255, 255, 0.07)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: overview.unhealthy_containers > 0 ? '#ef4444' : '#10b981', fontSize: '12px' }}>
            <span>健康检查异常</span>
            <Warning size={16} />
          </div>
          <div style={{ fontSize: '24px', fontWeight: 700, color: overview.unhealthy_containers > 0 ? '#ef4444' : '#10b981', marginTop: '8px' }}>
            {overview.unhealthy_containers}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            {overview.unhealthy_containers > 0 ? '检测到容器不健康' : '全网容器运行正常'}
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', gap: '8px', borderBottom: '1px solid rgba(255, 255, 255, 0.08)', marginBottom: '20px' }}>
        <button
          onClick={() => setActiveTab('containers')}
          style={{
            padding: '10px 16px',
            background: 'none',
            border: 'none',
            borderBottom: activeTab === 'containers' ? '2px solid #5e6ad2' : '2px solid transparent',
            color: activeTab === 'containers' ? '#5e6ad2' : 'var(--text-muted)',
            fontWeight: activeTab === 'containers' ? 600 : 400,
            fontSize: '14px',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          <Cube size={18} />
          Docker 容器微服务
        </button>
        <button
          onClick={() => setActiveTab('processes')}
          style={{
            padding: '10px 16px',
            background: 'none',
            border: 'none',
            borderBottom: activeTab === 'processes' ? '2px solid #5e6ad2' : '2px solid transparent',
            color: activeTab === 'processes' ? '#5e6ad2' : 'var(--text-muted)',
            fontWeight: activeTab === 'processes' ? 600 : 400,
            fontSize: '14px',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          <Rows size={18} />
          宿主 Top 进程剖析
        </button>
      </div>

      {/* Search & Filter Bar */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px', gap: '12px', flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', background: 'var(--panel-bg, #0e1012)', border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: '6px', padding: '0 12px', height: '36px', width: '320px' }}>
          <MagnifyingGlass size={16} color="var(--text-muted)" />
          <input
            type="text"
            placeholder={activeTab === 'containers' ? '搜索容器名、镜像、端口...' : '搜索 PID、进程名、执行用户...'}
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            style={{ background: 'transparent', border: 'none', color: 'var(--text-primary)', fontSize: '13px', outline: 'none', width: '100%' }}
          />
        </div>

        {activeTab === 'containers' && (
          <div style={{ display: 'flex', gap: '6px' }}>
            {[
              { id: 'all', label: '全部' },
              { id: 'running', label: '仅运行中' },
              { id: 'stopped', label: '仅已停止' },
              { id: 'unhealthy', label: '异常不健康' },
            ].map((f) => (
              <button
                key={f.id}
                onClick={() => setStateFilter(f.id)}
                style={{
                  padding: '6px 12px',
                  borderRadius: '6px',
                  fontSize: '12px',
                  border: '1px solid',
                  borderColor: stateFilter === f.id ? '#5e6ad2' : 'rgba(255, 255, 255, 0.08)',
                  background: stateFilter === f.id ? 'rgba(94, 106, 210, 0.15)' : 'transparent',
                  color: stateFilter === f.id ? '#5e6ad2' : 'var(--text-muted)',
                  cursor: 'pointer',
                }}
              >
                {f.label}
              </button>
            ))}
          </div>
        )}

        {activeTab === 'processes' && (
          <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
            <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>排序指标:</span>
            {[
              { id: 'memory', label: '常驻物理内存 (RSS)' },
              { id: 'cpu', label: 'CPU 负载 (CPU%)' },
            ].map((s) => (
              <button
                key={s.id}
                onClick={() => setProcessSortBy(s.id)}
                style={{
                  padding: '6px 12px',
                  borderRadius: '6px',
                  fontSize: '12px',
                  border: '1px solid',
                  borderColor: processSortBy === s.id ? '#5e6ad2' : 'rgba(255, 255, 255, 0.08)',
                  background: processSortBy === s.id ? 'rgba(94, 106, 210, 0.15)' : 'transparent',
                  color: processSortBy === s.id ? '#5e6ad2' : 'var(--text-muted)',
                  cursor: 'pointer',
                }}
              >
                {s.label}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Main Tab Content */}
      {error && (
        <div style={{ padding: '12px 16px', background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.2)', borderRadius: '8px', color: '#ef4444', fontSize: '13px', marginBottom: '20px' }}>
          {error}
        </div>
      )}

      {activeTab === 'containers' ? (
        filteredContainers.length === 0 ? (
          <div className="panel" style={{ padding: '48px 24px', textAlign: 'center', color: 'var(--text-muted)' }}>
            <Cube size={48} style={{ opacity: 0.3, marginBottom: '12px' }} />
            <div style={{ fontSize: '15px', color: 'var(--text-primary)', fontWeight: 500 }}>未发现匹配的容器</div>
            <div style={{ fontSize: '13px', marginTop: '6px' }}>
              {selectedNode
                ? (!nodeContainersData.docker_available ? '该节点尚未安装 Docker 或探针无 Docker Socket 权限。' : '当前节点暂无运行的容器。')
                : '请检查节点探针是否已成功接入并在服务器上运行 Docker 容器。'}
            </div>
          </div>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(360px, 1fr))', gap: '16px' }}>
            {filteredContainers.map((c) => {
              const isRunning = c.state === 'running'
              const memPercent = c.mem_percent || 0
              const cpuPercent = c.cpu_percent || 0

              let memBarColor = '#10b981'
              if (memPercent > 80) memBarColor = '#f59e0b'
              if (memPercent > 90) memBarColor = '#ef4444'

              let cpuBarColor = '#5e6ad2'
              if (cpuPercent > 50) cpuBarColor = '#f59e0b'
              if (cpuPercent > 80) cpuBarColor = '#ef4444'

              return (
                <div
                  key={c.container_id}
                  className="panel"
                  style={{
                    padding: '18px',
                    border: '1px solid rgba(255, 255, 255, 0.08)',
                    borderRadius: '8px',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '14px',
                    background: 'var(--card-bg, #0e1012)',
                  }}
                >
                  {/* Top: Name & State */}
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '8px' }}>
                    <div style={{ overflow: 'hidden' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <span
                          style={{
                            width: '8px',
                            height: '8px',
                            borderRadius: '50%',
                            background: isRunning ? '#10b981' : '#64748b',
                            boxShadow: isRunning ? '0 0 8px rgba(16, 185, 129, 0.5)' : 'none',
                          }}
                        />
                        <span style={{ fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                          {c.name}
                        </span>
                      </div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '2px', display: 'flex', alignItems: 'center', gap: '6px' }}>
                        <span style={{ maxWidth: '240px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {c.image}
                        </span>
                        <button
                          onClick={() => copyToClipboard(c.image, 'img-' + c.container_id)}
                          style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', padding: 0 }}
                          title="复制镜像名称"
                        >
                          {copiedId === 'img-' + c.container_id ? <Check size={12} color="#10b981" /> : <Copy size={12} />}
                        </button>
                      </div>
                    </div>

                    <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                      {c.health && (
                        <span
                          style={{
                            fontSize: '11px',
                            padding: '2px 6px',
                            borderRadius: '4px',
                            background: c.health === 'healthy' ? 'rgba(16, 185, 129, 0.15)' : 'rgba(239, 68, 68, 0.15)',
                            color: c.health === 'healthy' ? '#10b981' : '#ef4444',
                            fontWeight: 500,
                          }}
                        >
                          {c.health}
                        </span>
                      )}
                      <span
                        style={{
                          fontSize: '11px',
                          padding: '2px 8px',
                          borderRadius: '4px',
                          background: isRunning ? 'rgba(16, 185, 129, 0.15)' : 'rgba(148, 163, 184, 0.15)',
                          color: isRunning ? '#10b981' : '#94a3b8',
                          fontWeight: 500,
                        }}
                      >
                        {c.state}
                      </span>
                    </div>
                  </div>

                  {/* Middle: Gauges */}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', background: 'rgba(255, 255, 255, 0.02)', padding: '10px', borderRadius: '6px' }}>
                    {/* CPU */}
                    <div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                        <span>CPU 负载</span>
                        <span style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{cpuPercent.toFixed(1)}%</span>
                      </div>
                      <div style={{ height: '4px', width: '100%', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '2px', overflow: 'hidden' }}>
                        <div style={{ height: '100%', width: `${Math.min(cpuPercent, 100)}%`, background: cpuBarColor, transition: 'width 0.3s' }} />
                      </div>
                    </div>

                    {/* Memory */}
                    <div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                        <span>内存占用</span>
                        <span style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{formatBytes(c.mem_usage_bytes)}</span>
                      </div>
                      <div style={{ height: '4px', width: '100%', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '2px', overflow: 'hidden' }}>
                        <div style={{ height: '100%', width: `${Math.min(memPercent, 100)}%`, background: memBarColor, transition: 'width 0.3s' }} />
                      </div>
                    </div>
                  </div>

                  {/* Footer: Ports & Node */}
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '11px', color: 'var(--text-muted)', flexWrap: 'wrap', gap: '6px' }}>
                    <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap', maxWidth: '240px' }}>
                      {(c.ports || []).slice(0, 3).map((port, idx) => (
                        <span key={idx} style={{ background: 'rgba(255, 255, 255, 0.06)', padding: '1px 5px', borderRadius: '3px' }}>
                          {port}
                        </span>
                      ))}
                      {(c.ports || []).length > 3 && (
                        <span style={{ background: 'rgba(255, 255, 255, 0.06)', padding: '1px 5px', borderRadius: '3px' }}>
                          +{c.ports.length - 3}
                        </span>
                      )}
                    </div>

                    {c.node_name && (
                      <span style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                        <HardDrives size={12} />
                        {c.node_name}
                      </span>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )
      ) : (
        /* Host Processes Table */
        <div className="panel" style={{ overflowX: 'auto', border: '1px solid rgba(255, 255, 255, 0.08)' }}>
          <table className="table" style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.08)', color: 'var(--text-muted)', height: '40px' }}>
                <th style={{ padding: '0 16px', width: '80px' }}>PID</th>
                <th style={{ padding: '0 16px' }}>进程名称 (Comm)</th>
                <th style={{ padding: '0 16px', width: '100px' }}>执行用户</th>
                <th style={{ padding: '0 16px', width: '80px' }}>状态</th>
                <th style={{ padding: '0 16px', width: '120px' }}>CPU 占比</th>
                <th style={{ padding: '0 16px', width: '160px' }}>常驻内存 (RSS)</th>
                <th style={{ padding: '0 16px', width: '80px' }}>线程数</th>
                <th style={{ padding: '0 16px' }}>命令行预览</th>
              </tr>
            </thead>
            <tbody>
              {sortedProcesses.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ padding: '36px', textAlign: 'center', color: 'var(--text-muted)' }}>
                    暂无进程快照数据或当前节点不支持
                  </td>
                </tr>
              ) : (
                sortedProcesses.map((p) => (
                  <tr key={p.pid} style={{ borderBottom: '1px solid rgba(255, 255, 255, 0.04)', height: '42px' }}>
                    <td style={{ padding: '0 16px', fontFamily: 'monospace', color: 'var(--text-muted)' }}>{p.pid}</td>
                    <td style={{ padding: '0 16px', fontWeight: 600, color: 'var(--text-primary)' }}>{p.name}</td>
                    <td style={{ padding: '0 16px', color: 'var(--text-muted)' }}>{p.user || 'root'}</td>
                    <td style={{ padding: '0 16px' }}>
                      <span
                        style={{
                          fontSize: '11px',
                          padding: '1px 6px',
                          borderRadius: '3px',
                          background: p.state === 'R' ? 'rgba(16, 185, 129, 0.15)' : 'rgba(255, 255, 255, 0.06)',
                          color: p.state === 'R' ? '#10b981' : 'var(--text-muted)',
                        }}
                      >
                        {p.state === 'R' ? 'R (运行)' : p.state === 'S' ? 'S (休眠)' : p.state === 'Z' ? 'Z (僵尸)' : p.state}
                      </span>
                    </td>
                    <td style={{ padding: '0 16px', color: p.cpu_percent > 20 ? '#f59e0b' : 'var(--text-primary)' }}>
                      {(p.cpu_percent || 0).toFixed(1)}%
                    </td>
                    <td style={{ padding: '0 16px', color: 'var(--text-primary)' }}>
                      {formatBytes(p.memory_rss_bytes)}
                      {p.memory_percent > 0 && (
                        <span style={{ fontSize: '11px', color: 'var(--text-muted)', marginLeft: '6px' }}>
                          ({p.memory_percent.toFixed(1)}%)
                        </span>
                      )}
                    </td>
                    <td style={{ padding: '0 16px', color: 'var(--text-muted)' }}>{p.threads || 1}</td>
                    <td style={{ padding: '0 16px', color: 'var(--text-muted)', fontFamily: 'monospace', fontSize: '11px', maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {p.command_line || p.name}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
