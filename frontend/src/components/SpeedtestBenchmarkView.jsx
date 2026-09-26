import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowsClockwise,
  ArrowDown,
  ArrowUp,
  CaretRight,
  CheckCircle,
  Clock,
  Cpu,
  DownloadSimple,
  Gauge,
  Globe,
  Lightning,
  MagnifyingGlass,
  PencilSimple,
  Play,
  Plus,
  RocketLaunch,
  Trash,
  UploadSimple,
  WarningCircle,
  X,
} from '@phosphor-icons/react'
import {
  createSpeedtestTask,
  deleteSpeedtestTask,
  fetchSpeedtestHistory,
  fetchSpeedtestResults,
  fetchSpeedtestTasks,
  runSpeedtestNow,
  updateSpeedtestTask,
} from '../lib/api.js'

const PRESET_SERVERS = [
  {
    name: 'Cloudflare Global Edge',
    url: 'https://speed.cloudflare.com/__down?bytes=10485760',
    dlMB: 10,
    ulMB: 5,
    interval: 3600,
  },
  {
    name: 'Fastly Tokyo Edge CDN',
    url: 'https://jp-speed.fastly.net/10MB.bin',
    dlMB: 10,
    ulMB: 5,
    interval: 7200,
  },
  {
    name: 'AWS CloudFront Global',
    url: 'https://d2908q01vomqb2.cloudfront.net/da4b9237bacccdf19c0760cab7aec4a8359010b0/2021/04/16/speedtest-10mb.bin',
    dlMB: 10,
    ulMB: 5,
    interval: 7200,
  },
  {
    name: 'Hetzner Frankfurt Benchmark',
    url: 'https://fsn1-speed.hetzner.com/100MB.bin',
    dlMB: 20,
    ulMB: 10,
    interval: 14400,
  },
  {
    name: 'Akamai US West Backbone',
    url: 'https://speedtest.sea01.softlayer.com/downloads/test10.zip',
    dlMB: 10,
    ulMB: 5,
    interval: 7200,
  },
]

export function SpeedtestBenchmarkView() {
  const [activeTab, setActiveTab] = useState('rankings') // 'rankings' | 'tasks' | 'history'
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState(null)
  const [successToast, setSuccessToast] = useState(null)

  // Data states
  const [resultsData, setResultsData] = useState({
    stats: {
      max_download_mbps: 0,
      max_upload_mbps: 0,
      avg_download_mbps: 0,
      avg_upload_mbps: 0,
      avg_latency_ms: 0,
      active_benchmark_nodes: 0,
      total_benchmarks_count: 0,
    },
    rankings: [],
    results: [],
  })
  const [tasks, setTasks] = useState([])
  const [history, setHistory] = useState([])
  const [historyLoading, setHistoryLoading] = useState(false)

  // Filter & Search states
  const [searchQuery, setSearchQuery] = useState('')
  const [historyFilterNode, setHistoryFilterNode] = useState('all')

  // Run Now state
  const [triggering, setTriggering] = useState(false)

  // Task Edit / Create Modal state
  const [isTaskModalOpen, setIsTaskModalOpen] = useState(false)
  const [editingTask, setEditingTask] = useState(null)
  const [taskForm, setTaskForm] = useState({
    name: '',
    server_url: '',
    dl_mb: 10,
    ul_mb: 5,
    interval_seconds: 3600,
    node_tags: '',
    node_ids: '',
    enabled: true,
  })
  const [savingTask, setSavingTask] = useState(false)
  const [taskModalError, setTaskModalError] = useState(null)

  const showToast = (msg) => {
    setSuccessToast(msg)
    setTimeout(() => setSuccessToast(null), 4000)
  }

  // Load results and tasks
  const loadData = useCallback(async (isManual = false) => {
    if (isManual) setRefreshing(true)
    setError(null)
    try {
      const [resData, tasksData] = await Promise.all([
        fetchSpeedtestResults().catch(() => null),
        fetchSpeedtestTasks().catch(() => []),
      ])
      if (resData) {
        setResultsData(resData)
      }
      if (Array.isArray(tasksData)) {
        setTasks(tasksData)
      }
    } catch (err) {
      setError(err.message || '加载测速与基准数据失败')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  // Load history
  const loadHistory = useCallback(async (nodeId = '') => {
    setHistoryLoading(true)
    try {
      const hist = await fetchSpeedtestHistory(nodeId === 'all' ? '' : nodeId, '', 100)
      setHistory(Array.isArray(hist) ? hist : [])
    } catch {
      // Ignore background history errors
    } finally {
      setHistoryLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    const timer = setInterval(() => loadData(false), 30000)
    return () => clearInterval(timer)
  }, [loadData])

  useEffect(() => {
    if (activeTab === 'history') {
      loadHistory(historyFilterNode)
    }
  }, [activeTab, historyFilterNode, loadHistory])

  // Trigger immediate speedtest benchmark
  const handleTriggerRun = async (taskId = '', nodeId = '') => {
    setTriggering(true)
    try {
      const resp = await runSpeedtestNow({ task_id: taskId, node_id: nodeId })
      showToast(resp.message || '测速任务已下发，边缘节点将在下个探测周期开始测速')
      setTimeout(() => loadData(false), 2000)
    } catch (err) {
      setError(err.message || '触发测速失败')
    } finally {
      setTriggering(false)
    }
  }

  // Open modal to create a new task
  const handleOpenCreateTask = () => {
    setEditingTask(null)
    setTaskForm({
      name: 'Cloudflare Global Edge',
      server_url: 'https://speed.cloudflare.com/__down?bytes=10485760',
      dl_mb: 10,
      ul_mb: 5,
      interval_seconds: 3600,
      node_tags: '',
      node_ids: '',
      enabled: true,
    })
    setTaskModalError(null)
    setIsTaskModalOpen(true)
  }

  // Open modal to edit existing task
  const handleOpenEditTask = (task) => {
    setEditingTask(task)
    setTaskForm({
      name: task.name,
      server_url: task.server_url,
      dl_mb: Math.round((task.download_bytes || 0) / 1024 / 1024) || 10,
      ul_mb: Math.round((task.upload_bytes || 0) / 1024 / 1024) || 5,
      interval_seconds: task.interval_seconds || 3600,
      node_tags: (task.node_tags || []).join(', '),
      node_ids: (task.node_ids || []).join(', '),
      enabled: task.enabled,
    })
    setTaskModalError(null)
    setIsTaskModalOpen(true)
  }

  // Apply a preset server configuration
  const handleApplyPreset = (preset) => {
    setTaskForm((prev) => ({
      ...prev,
      name: preset.name,
      server_url: preset.url,
      dl_mb: preset.dlMB,
      ul_mb: preset.ulMB,
      interval_seconds: preset.interval,
    }))
  }

  // Save Task
  const handleSaveTask = async (e) => {
    e.preventDefault()
    if (!taskForm.name.trim() || !taskForm.server_url.trim()) {
      setTaskModalError('请填写任务名称和测速服务器 URL')
      return
    }
    setSavingTask(true)
    setTaskModalError(null)

    const payload = {
      name: taskForm.name.trim(),
      server_url: taskForm.server_url.trim(),
      download_bytes: Math.max(1, Number(taskForm.dl_mb)) * 1024 * 1024,
      upload_bytes: Math.max(0, Number(taskForm.ul_mb)) * 1024 * 1024,
      interval_seconds: Math.max(60, Number(taskForm.interval_seconds)),
      node_tags: taskForm.node_tags
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      node_ids: taskForm.node_ids
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      enabled: taskForm.enabled,
    }

    try {
      if (editingTask) {
        await updateSpeedtestTask(editingTask.id, payload)
        showToast(`已更新测速任务「${payload.name}」`)
      } else {
        await createSpeedtestTask(payload)
        showToast(`已新建测速任务「${payload.name}」`)
      }
      setIsTaskModalOpen(false)
      loadData(false)
    } catch (err) {
      setTaskModalError(err.message || '保存任务失败')
    } finally {
      setSavingTask(false)
    }
  }

  // Toggle Task Enabled
  const handleToggleTask = async (task) => {
    try {
      await updateSpeedtestTask(task.id, { enabled: !task.enabled })
      showToast(`已${!task.enabled ? '启用' : '暂停'}任务「${task.name}」`)
      loadData(false)
    } catch (err) {
      setError(err.message || '更新任务状态失败')
    }
  }

  // Delete Task
  const handleDeleteTask = async (task) => {
    if (!window.confirm(`确定要删除测速任务「${task.name}」吗？`)) return
    try {
      await deleteSpeedtestTask(task.id)
      showToast(`已删除测速任务「${task.name}」`)
      loadData(false)
    } catch (err) {
      setError(err.message || '删除任务失败')
    }
  }

  // Filtered rankings
  const filteredRankings = useMemo(() => {
    const list = resultsData.rankings || []
    if (!searchQuery.trim()) return list
    const q = searchQuery.toLowerCase().trim()
    return list.filter(
      (item) =>
        (item.node_name || '').toLowerCase().includes(q) ||
        (item.node_id || '').toLowerCase().includes(q) ||
        (item.server_name || '').toLowerCase().includes(q)
    )
  }, [resultsData.rankings, searchQuery])

  // Max download speed in current filtered list for bar percentage calculation
  const maxDlValue = useMemo(() => {
    const max = Math.max(
      ...filteredRankings.map((r) => r.download_speed_mbps || 0),
      resultsData.stats?.max_download_mbps || 0,
      1
    )
    return max
  }, [filteredRankings, resultsData.stats])

  const maxUlValue = useMemo(() => {
    const max = Math.max(
      ...filteredRankings.map((r) => r.upload_speed_mbps || 0),
      resultsData.stats?.max_upload_mbps || 0,
      1
    )
    return max
  }, [filteredRankings, resultsData.stats])

  // Unique node list from rankings & history for filter
  const uniqueNodeList = useMemo(() => {
    const map = new Map()
    for (const r of resultsData.rankings || []) {
      if (r.node_id) map.set(r.node_id, r.node_name || r.node_id)
    }
    for (const h of history) {
      if (h.node_id) map.set(h.node_id, h.node_name || h.node_id)
    }
    return Array.from(map.entries()).map(([id, name]) => ({ id, name }))
  }, [resultsData.rankings, history])

  const formatDateTime = (unixOrStr) => {
    if (!unixOrStr) return '—'
    const d = typeof unixOrStr === 'number' ? new Date(unixOrStr * 1000) : new Date(unixOrStr)
    if (isNaN(d.getTime())) return '—'
    return d.toLocaleString('zh-CN', { hour12: false })
  }

  return (
    <div className="speedtest-benchmark-view" style={{ padding: '0 0 40px' }}>
      {/* Toast Notification */}
      {successToast && (
        <div
          style={{
            position: 'fixed',
            top: '20px',
            right: '20px',
            zIndex: 9999,
            background: 'var(--brand-primary, #2563eb)',
            color: '#fff',
            padding: '12px 20px',
            borderRadius: '8px',
            boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            fontSize: '14px',
            fontWeight: 500,
          }}
        >
          <CheckCircle size={20} weight="fill" />
          <span>{successToast}</span>
        </div>
      )}

      {/* Top Banner and Navigation Tabs */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: '20px',
          flexWrap: 'wrap',
          gap: '12px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
          <button
            type="button"
            className={`button ${activeTab === 'rankings' ? 'button-primary' : 'button-quiet'}`}
            onClick={() => setActiveTab('rankings')}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Gauge size={18} />
            <span>测速排行榜与基准</span>
            {filteredRankings.length > 0 && (
              <span
                style={{
                  background: 'rgba(255,255,255,0.2)',
                  fontSize: '11px',
                  borderRadius: '10px',
                  padding: '1px 6px',
                  fontWeight: 600,
                }}
              >
                {filteredRankings.length}
              </span>
            )}
          </button>

          <button
            type="button"
            className={`button ${activeTab === 'tasks' ? 'button-primary' : 'button-quiet'}`}
            onClick={() => setActiveTab('tasks')}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Clock size={18} />
            <span>测速调度任务</span>
            <span
              style={{
                background: tasks.some((t) => t.enabled) ? '#10b981' : 'var(--border-subtle)',
                color: '#fff',
                fontSize: '11px',
                borderRadius: '10px',
                padding: '1px 6px',
                fontWeight: 600,
              }}
            >
              {tasks.length}
            </span>
          </button>

          <button
            type="button"
            className={`button ${activeTab === 'history' ? 'button-primary' : 'button-quiet'}`}
            onClick={() => setActiveTab('history')}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Lightning size={18} />
            <span>历史测速流水</span>
          </button>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <button
            type="button"
            className="button button-primary"
            onClick={() => handleTriggerRun()}
            disabled={triggering}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              background: 'linear-gradient(135deg, #2563eb, #3b82f6)',
              borderColor: '#2563eb',
            }}
            title="立即触发边缘节点执行测速"
          >
            <RocketLaunch size={16} className={triggering ? 'spin' : ''} />
            <span>{triggering ? '指令下发中...' : '立即测速'}</span>
          </button>

          <button
            type="button"
            className="button button-secondary"
            onClick={handleOpenCreateTask}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Plus size={16} />
            <span>新建测速任务</span>
          </button>

          <button
            type="button"
            className="button button-secondary"
            onClick={() => loadData(true)}
            disabled={refreshing}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <ArrowsClockwise size={16} className={refreshing ? 'spin' : ''} />
            <span>{refreshing ? '刷新中...' : '刷新'}</span>
          </button>
        </div>
      </div>

      {error && (
        <div
          style={{
            padding: '12px 16px',
            background: 'var(--color-danger-bg, #fef2f2)',
            border: '1px solid var(--color-danger-border, #fee2e2)',
            borderRadius: '8px',
            color: 'var(--color-danger, #b91c1c)',
            marginBottom: '20px',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          <WarningCircle size={20} />
          <span>{error}</span>
        </div>
      )}

      {/* 4 Metric Summary Cards */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
          gap: '16px',
          marginBottom: '24px',
        }}
      >
        <div className="panel" style={{ padding: '16px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              color: 'var(--text-muted)',
              fontSize: '13px',
            }}
          >
            <span>全网最高下行吞吐</span>
            <div
              style={{
                width: '32px',
                height: '32px',
                borderRadius: '8px',
                background: 'rgba(37, 99, 235, 0.1)',
                color: '#2563eb',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <ArrowDown size={18} weight="bold" />
            </div>
          </div>
          <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#2563eb' }}>
            {resultsData.stats?.max_download_mbps || 0}{' '}
            <span style={{ fontSize: '14px', fontWeight: 500, color: 'var(--text-muted)' }}>Mbps</span>
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            平均下行 {resultsData.stats?.avg_download_mbps || 0} Mbps
          </div>
        </div>

        <div className="panel" style={{ padding: '16px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              color: 'var(--text-muted)',
              fontSize: '13px',
            }}
          >
            <span>全网最高上行吞吐</span>
            <div
              style={{
                width: '32px',
                height: '32px',
                borderRadius: '8px',
                background: 'rgba(16, 185, 129, 0.1)',
                color: '#10b981',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <ArrowUp size={18} weight="bold" />
            </div>
          </div>
          <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#10b981' }}>
            {resultsData.stats?.max_upload_mbps || 0}{' '}
            <span style={{ fontSize: '14px', fontWeight: 500, color: 'var(--text-muted)' }}>Mbps</span>
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            平均上行 {resultsData.stats?.avg_upload_mbps || 0} Mbps
          </div>
        </div>

        <div className="panel" style={{ padding: '16px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              color: 'var(--text-muted)',
              fontSize: '13px',
            }}
          >
            <span>平均往返延迟</span>
            <div
              style={{
                width: '32px',
                height: '32px',
                borderRadius: '8px',
                background: 'rgba(245, 158, 11, 0.1)',
                color: '#f59e0b',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <Gauge size={18} weight="bold" />
            </div>
          </div>
          <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#f59e0b' }}>
            {resultsData.stats?.avg_latency_ms || 0}{' '}
            <span style={{ fontSize: '14px', fontWeight: 500, color: 'var(--text-muted)' }}>ms</span>
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            3-Probe ICMP/TCP RTT 均值
          </div>
        </div>

        <div className="panel" style={{ padding: '16px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              color: 'var(--text-muted)',
              fontSize: '13px',
            }}
          >
            <span>参与测速节点</span>
            <div
              style={{
                width: '32px',
                height: '32px',
                borderRadius: '8px',
                background: 'rgba(139, 92, 246, 0.1)',
                color: '#8b5cf6',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <Globe size={18} weight="bold" />
            </div>
          </div>
          <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#8b5cf6' }}>
            {resultsData.stats?.active_benchmark_nodes || 0}{' '}
            <span style={{ fontSize: '14px', fontWeight: 500, color: 'var(--text-muted)' }}>台</span>
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            累计完成 {resultsData.stats?.total_benchmarks_count || 0} 次基准压测
          </div>
        </div>
      </div>

      {/* TAB 1: RANKINGS LEADERBOARD */}
      {activeTab === 'rankings' && (
        <div className="panel" style={{ padding: '20px' }}>
          {/* Header & Search */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              marginBottom: '20px',
              flexWrap: 'wrap',
              gap: '12px',
            }}
          >
            <div>
              <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>全网节点带宽排行榜</h2>
              <p style={{ margin: '4px 0 0', fontSize: '13px', color: 'var(--text-muted)' }}>
                根据各边缘节点最近一次基准测速下行吞吐实时排序
              </p>
            </div>
            <div style={{ position: 'relative', width: '280px' }}>
              <MagnifyingGlass
                size={16}
                style={{
                  position: 'absolute',
                  left: '12px',
                  top: '50%',
                  transform: 'translateY(-50%)',
                  color: 'var(--text-muted)',
                }}
              />
              <input
                type="text"
                placeholder="搜索节点或测速目标..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                style={{
                  width: '100%',
                  padding: '8px 12px 8px 36px',
                  borderRadius: '6px',
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-subtle)',
                  color: 'inherit',
                  fontSize: '13px',
                }}
              />
            </div>
          </div>

          {filteredRankings.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '48px 0', color: 'var(--text-muted)' }}>
              <Gauge size={40} style={{ opacity: 0.5, marginBottom: '12px' }} />
              <div style={{ fontSize: '15px', fontWeight: 500 }}>暂无测速基准数据</div>
              <p style={{ fontSize: '13px', marginTop: '6px' }}>
                点击右上角「立即测速」或配置调度任务，边缘节点将自动开始吞吐基准评估。
              </p>
              <button
                type="button"
                className="button button-primary"
                onClick={() => handleTriggerRun()}
                disabled={triggering}
                style={{ marginTop: '16px' }}
              >
                立即开始首次测速
              </button>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
              {filteredRankings.map((item) => {
                const dlPct = Math.min(100, Math.round(((item.download_speed_mbps || 0) / maxDlValue) * 100))
                const ulPct = Math.min(100, Math.round(((item.upload_speed_mbps || 0) / maxUlValue) * 100))

                const rankBadgeColor =
                  item.rank === 1
                    ? 'linear-gradient(135deg, #f59e0b, #fbbf24)'
                    : item.rank === 2
                    ? 'linear-gradient(135deg, #94a3b8, #cbd5e1)'
                    : item.rank === 3
                    ? 'linear-gradient(135deg, #b45309, #d97706)'
                    : 'var(--bg-subtle)'

                const rankTextColor = item.rank <= 3 ? '#fff' : 'var(--text-muted)'

                return (
                  <div
                    key={`${item.node_id}-${item.rank}`}
                    style={{
                      padding: '16px',
                      borderRadius: '8px',
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--bg-subtle)',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: '12px',
                    }}
                  >
                    {/* Top Row: Rank, Node, Metadata & Single Node Run */}
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        flexWrap: 'wrap',
                        gap: '12px',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                        <div
                          style={{
                            width: '28px',
                            height: '28px',
                            borderRadius: '50%',
                            background: rankBadgeColor,
                            color: rankTextColor,
                            fontWeight: 700,
                            fontSize: '13px',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            boxShadow: item.rank <= 3 ? '0 2px 6px rgba(0,0,0,0.15)' : 'none',
                          }}
                        >
                          {item.rank}
                        </div>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: '15px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                            <span>{item.node_name || item.node_id}</span>
                            <span
                              style={{
                                fontSize: '11px',
                                padding: '2px 8px',
                                borderRadius: '4px',
                                background: 'var(--border-subtle)',
                                color: 'var(--text-muted)',
                                fontWeight: 400,
                              }}
                            >
                              {item.server_name || '基准测速目标'}
                            </span>
                          </div>
                          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '2px' }}>
                            节点 ID: {item.node_id} · 最近测速: {formatDateTime(item.tested_at)}
                          </div>
                        </div>
                      </div>

                      <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
                        <div style={{ textAlign: 'right' }}>
                          <span
                            style={{
                              fontSize: '12px',
                              padding: '3px 8px',
                              borderRadius: '4px',
                              background: 'rgba(245, 158, 11, 0.1)',
                              color: '#f59e0b',
                              fontWeight: 600,
                            }}
                          >
                            延迟 {item.latency_ms} ms · 抖动 {item.jitter_ms} ms
                          </span>
                        </div>
                        <button
                          type="button"
                          className="button button-quiet"
                          onClick={() => handleTriggerRun('', item.node_id)}
                          disabled={triggering}
                          style={{ fontSize: '12px', padding: '4px 8px' }}
                          title="仅为该节点下发立即测速"
                        >
                          <Play size={14} />
                          <span>重测</span>
                        </button>
                      </div>
                    </div>

                    {/* Bandwidth Progress Bars */}
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '16px' }}>
                      {/* Download Bar */}
                      <div>
                        <div
                          style={{
                            display: 'flex',
                            justifyContent: 'space-between',
                            fontSize: '12px',
                            marginBottom: '4px',
                          }}
                        >
                          <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: '#2563eb', fontWeight: 600 }}>
                            <DownloadSimple size={14} /> 下行吞吐 (Download)
                          </span>
                          <span style={{ fontWeight: 700, color: '#2563eb' }}>
                            {item.download_speed_mbps} <span style={{ fontWeight: 400, fontSize: '11px' }}>Mbps</span>
                          </span>
                        </div>
                        <div
                          style={{
                            width: '100%',
                            height: '8px',
                            background: 'rgba(37, 99, 235, 0.1)',
                            borderRadius: '4px',
                            overflow: 'hidden',
                          }}
                        >
                          <div
                            style={{
                              width: `${Math.max(2, dlPct)}%`,
                              height: '100%',
                              background: 'linear-gradient(90deg, #3b82f6, #2563eb)',
                              borderRadius: '4px',
                              transition: 'width 0.4s ease',
                            }}
                          />
                        </div>
                      </div>

                      {/* Upload Bar */}
                      <div>
                        <div
                          style={{
                            display: 'flex',
                            justifyContent: 'space-between',
                            fontSize: '12px',
                            marginBottom: '4px',
                          }}
                        >
                          <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: '#10b981', fontWeight: 600 }}>
                            <UploadSimple size={14} /> 上行吞吐 (Upload)
                          </span>
                          <span style={{ fontWeight: 700, color: '#10b981' }}>
                            {item.upload_speed_mbps} <span style={{ fontWeight: 400, fontSize: '11px' }}>Mbps</span>
                          </span>
                        </div>
                        <div
                          style={{
                            width: '100%',
                            height: '8px',
                            background: 'rgba(16, 185, 129, 0.1)',
                            borderRadius: '4px',
                            overflow: 'hidden',
                          }}
                        >
                          <div
                            style={{
                              width: `${Math.max(2, ulPct)}%`,
                              height: '100%',
                              background: 'linear-gradient(90deg, #34d399, #10b981)',
                              borderRadius: '4px',
                              transition: 'width 0.4s ease',
                            }}
                          />
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* TAB 2: SCHEDULED TASKS CONFIG */}
      {activeTab === 'tasks' && (
        <div className="panel" style={{ padding: '20px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              marginBottom: '20px',
              flexWrap: 'wrap',
              gap: '12px',
            }}
          >
            <div>
              <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>测速调度任务管理</h2>
              <p style={{ margin: '4px 0 0', fontSize: '13px', color: 'var(--text-muted)' }}>
                下发给边缘节点的周期性吞吐基准测速目标，支持按节点标签/指定节点按需过滤
              </p>
            </div>
            <button
              type="button"
              className="button button-primary"
              onClick={handleOpenCreateTask}
              style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
            >
              <Plus size={16} />
              <span>新建测速任务</span>
            </button>
          </div>

          {tasks.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '48px 0', color: 'var(--text-muted)' }}>
              <Clock size={40} style={{ opacity: 0.5, marginBottom: '12px' }} />
              <div style={{ fontSize: '15px', fontWeight: 500 }}>暂无调度测速任务</div>
              <p style={{ fontSize: '13px', marginTop: '6px' }}>
                创建定时任务后，探针将在后台静默压测，记录历史带宽指标。
              </p>
              <button
                type="button"
                className="button button-primary"
                onClick={handleOpenCreateTask}
                style={{ marginTop: '16px' }}
              >
                新建首个测速任务
              </button>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border-subtle)', color: 'var(--text-muted)' }}>
                    <th style={{ padding: '12px 8px' }}>状态</th>
                    <th style={{ padding: '12px 8px' }}>任务名称</th>
                    <th style={{ padding: '12px 8px' }}>测速目标 URL</th>
                    <th style={{ padding: '12px 8px' }}>下载限额</th>
                    <th style={{ padding: '12px 8px' }}>上传限额</th>
                    <th style={{ padding: '12px 8px' }}>执行周期</th>
                    <th style={{ padding: '12px 8px' }}>目标节点范围</th>
                    <th style={{ padding: '12px 8px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.map((task) => (
                    <tr
                      key={task.id}
                      style={{
                        borderBottom: '1px solid var(--border-subtle)',
                        opacity: task.enabled ? 1 : 0.6,
                      }}
                    >
                      <td style={{ padding: '12px 8px' }}>
                        <span
                          style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: '4px',
                            padding: '2px 8px',
                            borderRadius: '10px',
                            fontSize: '11px',
                            fontWeight: 600,
                            background: task.enabled ? 'rgba(16, 185, 129, 0.15)' : 'var(--border-subtle)',
                            color: task.enabled ? '#10b981' : 'var(--text-muted)',
                          }}
                        >
                          {task.enabled ? '运行中' : '已暂停'}
                        </span>
                      </td>
                      <td style={{ padding: '12px 8px', fontWeight: 600 }}>{task.name}</td>
                      <td style={{ padding: '12px 8px', maxWidth: '240px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        <span title={task.server_url} style={{ fontFamily: 'monospace', fontSize: '12px' }}>
                          {task.server_url}
                        </span>
                      </td>
                      <td style={{ padding: '12px 8px' }}>
                        {task.download_bytes ? `${Math.round(task.download_bytes / 1024 / 1024)} MB` : '10 MB'}
                      </td>
                      <td style={{ padding: '12px 8px' }}>
                        {task.upload_bytes ? `${Math.round(task.upload_bytes / 1024 / 1024)} MB` : '5 MB'}
                      </td>
                      <td style={{ padding: '12px 8px' }}>
                        {task.interval_seconds ? `${Math.round(task.interval_seconds / 60)} 分钟` : '60 分钟'}
                      </td>
                      <td style={{ padding: '12px 8px' }}>
                        {task.node_tags && task.node_tags.length > 0 ? (
                          <span style={{ fontSize: '11px', background: 'rgba(37,99,235,0.1)', color: '#2563eb', padding: '2px 6px', borderRadius: '4px' }}>
                            标签: {task.node_tags.join(', ')}
                          </span>
                        ) : task.node_ids && task.node_ids.length > 0 ? (
                          <span style={{ fontSize: '11px', background: 'rgba(139,92,246,0.1)', color: '#8b5cf6', padding: '2px 6px', borderRadius: '4px' }}>
                            节点: {task.node_ids.join(', ')}
                          </span>
                        ) : (
                          <span style={{ color: 'var(--text-muted)', fontSize: '12px' }}>全网所有节点</span>
                        )}
                      </td>
                      <td style={{ padding: '12px 8px', textAlign: 'right' }}>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '6px' }}>
                          <button
                            type="button"
                            className="button button-quiet button-small"
                            onClick={() => handleToggleTask(task)}
                            title={task.enabled ? '暂停此任务' : '启用此任务'}
                          >
                            {task.enabled ? '暂停' : '启用'}
                          </button>
                          <button
                            type="button"
                            className="button button-quiet button-small"
                            onClick={() => handleOpenEditTask(task)}
                            title="编辑任务"
                          >
                            <PencilSimple size={14} />
                          </button>
                          <button
                            type="button"
                            className="button button-quiet button-small"
                            onClick={() => handleDeleteTask(task)}
                            style={{ color: 'var(--color-danger, #ef4444)' }}
                            title="删除任务"
                          >
                            <Trash size={14} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* TAB 3: BENCHMARK HISTORY */}
      {activeTab === 'history' && (
        <div className="panel" style={{ padding: '20px' }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              marginBottom: '20px',
              flexWrap: 'wrap',
              gap: '12px',
            }}
          >
            <div>
              <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>基准测速历史流水</h2>
              <p style={{ margin: '4px 0 0', fontSize: '13px', color: 'var(--text-muted)' }}>
                记录各节点往次压测的上下行速率、网络时延与抖动审计明细
              </p>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <select
                value={historyFilterNode}
                onChange={(e) => setHistoryFilterNode(e.target.value)}
                style={{
                  padding: '6px 12px',
                  borderRadius: '6px',
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-subtle)',
                  color: 'inherit',
                  fontSize: '13px',
                }}
              >
                <option value="all">全部边缘节点</option>
                {uniqueNodeList.map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.name}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="button button-secondary button-small"
                onClick={() => loadHistory(historyFilterNode)}
                disabled={historyLoading}
              >
                <ArrowsClockwise size={14} className={historyLoading ? 'spin' : ''} />
                <span>刷新流水</span>
              </button>
            </div>
          </div>

          {history.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '48px 0', color: 'var(--text-muted)' }}>
              <Lightning size={40} style={{ opacity: 0.5, marginBottom: '12px' }} />
              <div style={{ fontSize: '15px', fontWeight: 500 }}>
                {historyLoading ? '正在加载历史流水...' : '暂无测速历史记录'}
              </div>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border-subtle)', color: 'var(--text-muted)' }}>
                    <th style={{ padding: '12px 8px' }}>测速时间</th>
                    <th style={{ padding: '12px 8px' }}>节点名称</th>
                    <th style={{ padding: '12px 8px' }}>测速目标</th>
                    <th style={{ padding: '12px 8px' }}>下行速率 (Mbps)</th>
                    <th style={{ padding: '12px 8px' }}>上行速率 (Mbps)</th>
                    <th style={{ padding: '12px 8px' }}>往返延迟</th>
                    <th style={{ padding: '12px 8px' }}>时延抖动</th>
                    <th style={{ padding: '12px 8px' }}>状态</th>
                  </tr>
                </thead>
                <tbody>
                  {history.map((h) => (
                    <tr key={h.id || `${h.node_id}-${h.tested_at}`} style={{ borderBottom: '1px solid var(--border-subtle)' }}>
                      <td style={{ padding: '12px 8px', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                        {formatDateTime(h.tested_at)}
                      </td>
                      <td style={{ padding: '12px 8px', fontWeight: 600 }}>{h.node_name || h.node_id}</td>
                      <td style={{ padding: '12px 8px' }}>{h.task_name || '默认测速'}</td>
                      <td style={{ padding: '12px 8px', fontWeight: 600, color: '#2563eb' }}>
                        {h.download_speed_mbps || 0} Mbps
                      </td>
                      <td style={{ padding: '12px 8px', fontWeight: 600, color: '#10b981' }}>
                        {h.upload_speed_mbps || 0} Mbps
                      </td>
                      <td style={{ padding: '12px 8px' }}>{h.latency_ms || 0} ms</td>
                      <td style={{ padding: '12px 8px' }}>{h.jitter_ms || 0} ms</td>
                      <td style={{ padding: '12px 8px' }}>
                        {h.status === 'ok' ? (
                          <span
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: '4px',
                              padding: '2px 8px',
                              borderRadius: '10px',
                              fontSize: '11px',
                              fontWeight: 600,
                              background: 'rgba(16, 185, 129, 0.15)',
                              color: '#10b981',
                            }}
                          >
                            <CheckCircle size={12} weight="fill" /> 成功
                          </span>
                        ) : (
                          <span
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: '4px',
                              padding: '2px 8px',
                              borderRadius: '10px',
                              fontSize: '11px',
                              fontWeight: 600,
                              background: 'rgba(239, 68, 68, 0.15)',
                              color: '#ef4444',
                            }}
                            title={h.error_message}
                          >
                            <WarningCircle size={12} weight="fill" /> 异常
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Task Edit / Create Modal */}
      {isTaskModalOpen && (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: 'rgba(0, 0, 0, 0.6)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
            padding: '20px',
          }}
          onClick={() => !savingTask && setIsTaskModalOpen(false)}
        >
          <div
            style={{
              background: 'var(--bg-panel, #ffffff)',
              borderRadius: '12px',
              width: '100%',
              maxWidth: '560px',
              padding: '24px',
              boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.2)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '16px',
              }}
            >
              <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>
                {editingTask ? '编辑测速任务' : '新建测速任务'}
              </h3>
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setIsTaskModalOpen(false)}
                disabled={savingTask}
              >
                <X size={18} />
              </button>
            </div>

            {taskModalError && (
              <div
                style={{
                  padding: '10px 14px',
                  background: 'var(--color-danger-bg, #fef2f2)',
                  border: '1px solid var(--color-danger-border, #fee2e2)',
                  borderRadius: '6px',
                  color: 'var(--color-danger, #b91c1c)',
                  marginBottom: '16px',
                  fontSize: '13px',
                }}
              >
                {taskModalError}
              </div>
            )}

            {/* Presets Quick Selector */}
            <div style={{ marginBottom: '16px' }}>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '8px' }}>
                选择全球测速节点预设 (快速填充):
              </div>
              <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
                {PRESET_SERVERS.map((preset) => (
                  <button
                    key={preset.name}
                    type="button"
                    className="button button-quiet button-small"
                    onClick={() => handleApplyPreset(preset)}
                    style={{ fontSize: '11px', border: '1px solid var(--border-subtle)' }}
                  >
                    {preset.name}
                  </button>
                ))}
              </div>
            </div>

            <form onSubmit={handleSaveTask} style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 500, marginBottom: '4px' }}>
                  任务名称 *
                </label>
                <input
                  type="text"
                  required
                  placeholder="例如: Cloudflare Global Edge"
                  value={taskForm.name}
                  onChange={(e) => setTaskForm({ ...taskForm, name: e.target.value })}
                  style={{
                    width: '100%',
                    padding: '8px 12px',
                    borderRadius: '6px',
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-subtle)',
                    color: 'inherit',
                    fontSize: '13px',
                  }}
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '13px', fontWeight: 500, marginBottom: '4px' }}>
                  测速目标服务器 URL *
                </label>
                <input
                  type="url"
                  required
                  placeholder="https://speed.cloudflare.com/__down?bytes=10485760"
                  value={taskForm.server_url}
                  onChange={(e) => setTaskForm({ ...taskForm, server_url: e.target.value })}
                  style={{
                    width: '100%',
                    padding: '8px 12px',
                    borderRadius: '6px',
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-subtle)',
                    color: 'inherit',
                    fontSize: '13px',
                    fontFamily: 'monospace',
                  }}
                />
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px' }}>
                  支持 HTTP/HTTPS 测试文件下载与 POST 上传回传接口
                </div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 500, marginBottom: '4px' }}>
                    下行流量上限 (MB)
                  </label>
                  <input
                    type="number"
                    min="1"
                    max="50"
                    value={taskForm.dl_mb}
                    onChange={(e) => setTaskForm({ ...taskForm, dl_mb: e.target.value })}
                    style={{
                      width: '100%',
                      padding: '8px 12px',
                      borderRadius: '6px',
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--bg-subtle)',
                      color: 'inherit',
                      fontSize: '13px',
                    }}
                  />
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '2px' }}>最大 50MB</div>
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 500, marginBottom: '4px' }}>
                    上行流量上限 (MB)
                  </label>
                  <input
                    type="number"
                    min="0"
                    max="20"
                    value={taskForm.ul_mb}
                    onChange={(e) => setTaskForm({ ...taskForm, ul_mb: e.target.value })}
                    style={{
                      width: '100%',
                      padding: '8px 12px',
                      borderRadius: '6px',
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--bg-subtle)',
                      color: 'inherit',
                      fontSize: '13px',
                    }}
                  />
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '2px' }}>最大 20MB</div>
                </div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 500, marginBottom: '4px' }}>
                    调度执行周期 (秒)
                  </label>
                  <select
                    value={taskForm.interval_seconds}
                    onChange={(e) => setTaskForm({ ...taskForm, interval_seconds: Number(e.target.value) })}
                    style={{
                      width: '100%',
                      padding: '8px 12px',
                      borderRadius: '6px',
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--bg-subtle)',
                      color: 'inherit',
                      fontSize: '13px',
                    }}
                  >
                    <option value={1800}>每 30 分钟</option>
                    <option value={3600}>每 1 小时 (推荐)</option>
                    <option value={7200}>每 2 小时</option>
                    <option value={14400}>每 4 小时</option>
                    <option value={28800}>每 8 小时</option>
                    <option value={86400}>每 24 小时</option>
                  </select>
                </div>

                <div>
                  <label style={{ display: 'block', fontSize: '13px', fontWeight: 500, marginBottom: '4px' }}>
                    节点标签过滤 (选填)
                  </label>
                  <input
                    type="text"
                    placeholder="例如: hk, jp, prod"
                    value={taskForm.node_tags}
                    onChange={(e) => setTaskForm({ ...taskForm, node_tags: e.target.value })}
                    style={{
                      width: '100%',
                      padding: '8px 12px',
                      borderRadius: '6px',
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--bg-subtle)',
                      color: 'inherit',
                      fontSize: '13px',
                    }}
                  />
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '2px' }}>
                    逗号分隔，留空适用于全量节点
                  </div>
                </div>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginTop: '6px' }}>
                <input
                  type="checkbox"
                  id="task-enabled-check"
                  checked={taskForm.enabled}
                  onChange={(e) => setTaskForm({ ...taskForm, enabled: e.target.checked })}
                  style={{ width: '16px', height: '16px', cursor: 'pointer' }}
                />
                <label htmlFor="task-enabled-check" style={{ fontSize: '13px', cursor: 'pointer', userSelect: 'none' }}>
                  启用此测速调度任务
                </label>
              </div>

              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'flex-end',
                  gap: '8px',
                  marginTop: '12px',
                }}
              >
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={() => setIsTaskModalOpen(false)}
                  disabled={savingTask}
                >
                  取消
                </button>
                <button type="submit" className="button button-primary" disabled={savingTask}>
                  {savingTask ? '保存中...' : editingTask ? '更新任务' : '创建任务'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
