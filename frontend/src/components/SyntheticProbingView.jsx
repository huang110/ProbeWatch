import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowsClockwise,
  CheckCircle,
  Clock,
  Globe,
  Lightning,
  MagnifyingGlass,
  PencilSimple,
  Play,
  Plus,
  ShieldCheck,
  Trash,
  WarningCircle,
  X,
  Cpu,
  Pulse,
  Terminal,
  Info,
  Sliders,
} from '@phosphor-icons/react'
import {
  fetchSyntheticTargets,
  createSyntheticTarget,
  updateSyntheticTarget,
  deleteSyntheticTarget,
  fetchSyntheticResults,
  fetchSyntheticHistory,
  testSyntheticTarget,
} from '../lib/api.js'

const PROTOCOL_CONFIG = {
  http: { label: 'HTTP', color: 'bg-blue-500/10 text-blue-400 border-blue-500/20' },
  https: { label: 'HTTPS', color: 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20' },
  grpc: { label: 'gRPC', color: 'bg-purple-500/10 text-purple-400 border-purple-500/20' },
  websocket: { label: 'WebSocket', color: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' },
  doh: { label: 'DoH (DNS)', color: 'bg-amber-500/10 text-amber-400 border-amber-500/20' },
}

const PRESET_TARGETS = [
  {
    name: 'Cloudflare Trace (HTTP/HTTPS)',
    protocol: 'https',
    target_url: 'https://1.1.1.1/cdn-cgi/trace',
    method: 'GET',
    interval_seconds: 30,
    timeout_ms: 3000,
    consensus_nodes: 1,
    assertions: [
      { source: 'status_code', operator: 'equals', target: '200' },
      { source: 'body_regex', operator: 'contains', target: 'h=1.1.1.1' },
      { source: 'max_latency_ms', operator: 'less_than', target: '1000' },
    ],
  },
  {
    name: 'Google DNS-over-HTTPS (DoH)',
    protocol: 'doh',
    target_url: 'https://dns.google/dns-query?name=google.com&type=A',
    method: 'GET',
    headers: { 'Accept': 'application/dns-message' },
    interval_seconds: 60,
    timeout_ms: 4000,
    consensus_nodes: 2,
    assertions: [
      { source: 'status_code', operator: 'equals', target: '200' },
      { source: 'max_latency_ms', operator: 'less_than', target: '500' },
    ],
  },
  {
    name: 'Postman Echo WebSocket',
    protocol: 'websocket',
    target_url: 'wss://ws.postman-echo.com/raw',
    method: 'GET',
    interval_seconds: 60,
    timeout_ms: 5000,
    consensus_nodes: 1,
    assertions: [
      { source: 'status_code', operator: 'equals', target: '101' },
      { source: 'max_latency_ms', operator: 'less_than', target: '1500' },
    ],
  },
  {
    name: 'gRPC Health Check Endpoint',
    protocol: 'grpc',
    target_url: 'https://grpcb.in:9001',
    grpc_service: 'grpc.health.v1.Health',
    interval_seconds: 60,
    timeout_ms: 5000,
    consensus_nodes: 1,
    assertions: [
      { source: 'status_code', operator: 'equals', target: '200' },
    ],
  },
]

export default function SyntheticProbingView() {
  const [overview, setOverview] = useState(null)
  const [targets, setTargets] = useState([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [activeProtocolTab, setActiveProtocolTab] = useState('all')

  // Modals state
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingTarget, setEditingTarget] = useState(null)
  const [isTestModalOpen, setIsTestModalOpen] = useState(false)
  const [testTarget, setTestTarget] = useState(null)
  const [historyTarget, setHistoryTarget] = useState(null)

  const loadData = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true)
    try {
      const [overviewData, targetsData] = await Promise.all([
        fetchSyntheticResults().catch(() => null),
        fetchSyntheticTargets().catch(() => []),
      ])
      if (overviewData) setOverview(overviewData)
      if (Array.isArray(targetsData)) setTargets(targetsData)
      setError(null)
    } catch (err) {
      console.error('Failed to load synthetic monitoring data:', err)
      setError(err.message || '加载合成监控数据失败')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    const timer = setInterval(() => loadData(true), 15000)
    return () => clearInterval(timer)
  }, [loadData])

  // Filter targets
  const filteredTargets = useMemo(() => {
    const list = overview?.targets || []
    return list.filter((item) => {
      const t = item.target || {}
      const matchesSearch =
        (t.name || '').toLowerCase().includes(searchQuery.toLowerCase()) ||
        (t.target_url || '').toLowerCase().includes(searchQuery.toLowerCase()) ||
        (t.protocol || '').toLowerCase().includes(searchQuery.toLowerCase())
      const matchesProto =
        activeProtocolTab === 'all' || (t.protocol || '').toLowerCase() === activeProtocolTab
      return matchesSearch && matchesProto
    })
  }, [overview, searchQuery, activeProtocolTab])

  // Summary Metrics
  const metrics = useMemo(() => {
    const total = overview?.total_targets || targets.length || 0
    const passing = overview?.passing_targets || 0
    const degraded = overview?.degraded_targets || 0
    const failing = overview?.failing_targets || 0
    const passRate = total > 0 ? Math.round((passing / total) * 100) : 100

    let totalTTFB = 0
    let ttfbCount = 0
    if (overview?.targets) {
      overview.targets.forEach((t) => {
        if (t.avg_ttfb_ms > 0) {
          totalTTFB += t.avg_ttfb_ms
          ttfbCount++
        }
      })
    }
    const fleetAvgTTFB = ttfbCount > 0 ? Math.round(totalTTFB / ttfbCount) : 0

    return { total, passing, degraded, failing, passRate, fleetAvgTTFB }
  }, [overview, targets])

  const handleDelete = async (id) => {
    if (!window.confirm('确认删除此合成监控目标？相关拨测历史将被同时移除。')) return
    try {
      await deleteSyntheticTarget(id)
      loadData(true)
    } catch (err) {
      alert('删除失败: ' + err.message)
    }
  }

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 bg-slate-900/60 backdrop-blur-md p-6 rounded-2xl border border-slate-800 shadow-xl">
        <div>
          <div className="flex items-center gap-3">
            <div className="p-2.5 bg-gradient-to-br from-indigo-500/20 to-purple-500/20 rounded-xl border border-indigo-500/30 text-indigo-400">
              <Pulse size={26} weight="duotone" />
            </div>
            <div>
              <h1 className="text-2xl font-bold text-white tracking-tight flex items-center gap-2">
                全景合成监控与 SLA 契约引擎
                <span className="text-xs px-2.5 py-0.5 rounded-full bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 font-medium">
                  v0.8.2
                </span>
              </h1>
              <p className="text-sm text-slate-400 mt-0.5">
                支持 HTTP/S、gRPC Health、WebSocket 与 DoH 多协议多节点共识主动拨测与全链路时延瀑布流
              </p>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={() => {
              setTestTarget(null)
              setIsTestModalOpen(true)
            }}
            className="flex items-center gap-2 px-4 py-2.5 rounded-xl text-sm font-medium bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 hover:border-slate-600 transition"
          >
            <Play size={16} weight="bold" className="text-emerald-400" />
            实时模拟拨测
          </button>
          <button
            onClick={() => {
              setEditingTarget(null)
              setIsModalOpen(true)
            }}
            className="flex items-center gap-2 px-4 py-2.5 rounded-xl text-sm font-medium bg-indigo-600 hover:bg-indigo-500 text-white shadow-lg shadow-indigo-600/25 transition"
          >
            <Plus size={16} weight="bold" />
            新建监控契约
          </button>
          <button
            onClick={() => loadData(true)}
            disabled={refreshing}
            className="p-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-400 hover:text-white border border-slate-700 transition"
            title="刷新数据"
          >
            <ArrowsClockwise size={18} className={refreshing ? 'animate-spin text-indigo-400' : ''} />
          </button>
        </div>
      </div>

      {/* Metric Stat Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="bg-slate-900/60 p-5 rounded-2xl border border-slate-800 shadow-sm relative overflow-hidden">
          <div className="flex items-center justify-between text-slate-400 mb-2">
            <span className="text-xs font-semibold uppercase tracking-wider">监测契约总数</span>
            <Globe size={18} className="text-indigo-400" />
          </div>
          <div className="text-3xl font-bold text-white tracking-tight">{metrics.total}</div>
          <div className="text-xs text-slate-500 mt-1">跨边缘探针节点分布式运行</div>
        </div>

        <div className="bg-slate-900/60 p-5 rounded-2xl border border-slate-800 shadow-sm relative overflow-hidden">
          <div className="flex items-center justify-between text-slate-400 mb-2">
            <span className="text-xs font-semibold uppercase tracking-wider">SLA 达标率</span>
            <ShieldCheck size={18} className="text-emerald-400" />
          </div>
          <div className="text-3xl font-bold text-emerald-400 tracking-tight">{metrics.passRate}%</div>
          <div className="text-xs text-slate-500 mt-1">
            {metrics.passing} 契约正常 · {metrics.failing} 失败 · {metrics.degraded} 降级
          </div>
        </div>

        <div className="bg-slate-900/60 p-5 rounded-2xl border border-slate-800 shadow-sm relative overflow-hidden">
          <div className="flex items-center justify-between text-slate-400 mb-2">
            <span className="text-xs font-semibold uppercase tracking-wider">多节点共识告警</span>
            <WarningCircle size={18} className={metrics.failing > 0 ? 'text-rose-400' : 'text-slate-400'} />
          </div>
          <div className={`text-3xl font-bold tracking-tight ${metrics.failing > 0 ? 'text-rose-400' : 'text-slate-200'}`}>
            {metrics.failing > 0 ? `${metrics.failing} 失活` : '全网稳态'}
          </div>
          <div className="text-xs text-slate-500 mt-1">多 ISP 交叉校验过滤偶发抖动</div>
        </div>

        <div className="bg-slate-900/60 p-5 rounded-2xl border border-slate-800 shadow-sm relative overflow-hidden">
          <div className="flex items-center justify-between text-slate-400 mb-2">
            <span className="text-xs font-semibold uppercase tracking-wider">全网平均首包 (TTFB)</span>
            <Lightning size={18} className="text-amber-400" />
          </div>
          <div className="text-3xl font-bold text-amber-400 tracking-tight">{metrics.fleetAvgTTFB} ms</div>
          <div className="text-xs text-slate-500 mt-1">DNS+TCP+TLS 全链路网络瀑布流</div>
        </div>
      </div>

      {/* Protocol Tabs & Search */}
      <div className="flex flex-col sm:flex-row items-center justify-between gap-4">
        <div className="flex items-center gap-1.5 p-1 bg-slate-900/80 rounded-xl border border-slate-800 overflow-x-auto w-full sm:w-auto">
          {[
            { id: 'all', label: '全部协议' },
            { id: 'https', label: 'HTTP / HTTPS' },
            { id: 'grpc', label: 'gRPC Health' },
            { id: 'websocket', label: 'WebSocket' },
            { id: 'doh', label: 'DoH (DNS)' },
          ].map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveProtocolTab(tab.id)}
              className={`px-3.5 py-1.5 rounded-lg text-xs font-medium transition whitespace-nowrap ${
                activeProtocolTab === tab.id
                  ? 'bg-indigo-600 text-white shadow-sm'
                  : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72">
          <MagnifyingGlass size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="搜索契约名称、URL 或协议..."
            className="w-full pl-9 pr-4 py-2 bg-slate-900/80 border border-slate-800 rounded-xl text-xs text-slate-200 placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition"
          />
        </div>
      </div>

      {/* Targets List */}
      {loading ? (
        <div className="p-16 text-center text-slate-400 bg-slate-900/40 rounded-2xl border border-slate-800">
          <ArrowsClockwise size={28} className="animate-spin mx-auto text-indigo-400 mb-3" />
          正在加载合成拨测契约与节点回传时延...
        </div>
      ) : filteredTargets.length === 0 ? (
        <div className="p-16 text-center text-slate-400 bg-slate-900/40 rounded-2xl border border-slate-800">
          <Globe size={40} className="mx-auto text-slate-600 mb-3" />
          <h3 className="text-base font-semibold text-slate-300">暂无匹配的合成拨测目标</h3>
          <p className="text-xs text-slate-500 mt-1 max-w-md mx-auto">
            您可以点击右上角“新建监控契约”或使用预设模版快速创建多协议合成探针。
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4">
          {filteredTargets.map((item) => (
            <SyntheticTargetCard
              key={item.target?.id}
              item={item}
              onEdit={() => {
                setEditingTarget(item.target)
                setIsModalOpen(true)
              }}
              onDelete={() => handleDelete(item.target?.id)}
              onTest={() => {
                setTestTarget(item.target)
                setIsTestModalOpen(true)
              }}
              onViewHistory={() => setHistoryTarget(item.target)}
            />
          ))}
        </div>
      )}

      {/* Create / Edit Modal */}
      {isModalOpen && (
        <TargetModal
          target={editingTarget}
          onClose={() => {
            setIsModalOpen(false)
            setEditingTarget(null)
          }}
          onSaved={() => {
            setIsModalOpen(false)
            setEditingTarget(null)
            loadData(true)
          }}
        />
      )}

      {/* Live Simulation Test Modal */}
      {isTestModalOpen && (
        <LiveTestModal
          initialTarget={testTarget}
          onClose={() => {
            setIsTestModalOpen(false)
            setTestTarget(null)
          }}
        />
      )}

      {/* History Modal */}
      {historyTarget && (
        <HistoryModal
          target={historyTarget}
          onClose={() => setHistoryTarget(null)}
        />
      )}
    </div>
  )
}

function SyntheticTargetCard({ item, onEdit, onDelete, onTest, onViewHistory }) {
  const t = item.target || {}
  const proto = (t.protocol || 'https').toLowerCase()
  const protoStyle = PROTOCOL_CONFIG[proto] || PROTOCOL_CONFIG.https
  const results = item.latest_results || []

  let consensusBadge = {
    label: '等待回传',
    color: 'bg-slate-800 text-slate-400 border-slate-700',
  }
  if (item.consensus_status === 'healthy') {
    consensusBadge = {
      label: `共识达标 (${item.passing_nodes}/${item.total_nodes} 节点正常)`,
      color: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
    }
  } else if (item.consensus_status === 'degraded') {
    consensusBadge = {
      label: `单点降级 (${item.failing_nodes}/${item.total_nodes} 节点异常)`,
      color: 'bg-amber-500/10 text-amber-400 border-amber-500/20',
    }
  } else if (item.consensus_status === 'failing') {
    consensusBadge = {
      label: `共识失活 (>= ${t.consensus_nodes || 1} 节点共识报警)`,
      color: 'bg-rose-500/10 text-rose-400 border-rose-500/20',
    }
  }

  // Parse assertion rules
  let assertions = []
  try {
    if (t.assertions) assertions = JSON.parse(t.assertions)
  } catch {}

  // Worst or average timing
  const avgTiming = results.length > 0 ? results[0] : null

  return (
    <div className="bg-slate-900/60 rounded-2xl border border-slate-800 hover:border-slate-700/80 p-5 shadow-sm transition">
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4 pb-4 border-b border-slate-800/80">
        <div className="flex items-start sm:items-center gap-3">
          <span className={`px-2.5 py-1 rounded-lg text-xs font-semibold border ${protoStyle.color} uppercase tracking-wider`}>
            {protoStyle.label}
          </span>
          <div>
            <div className="flex items-center gap-2 flex-wrap">
              <h3 className="text-base font-semibold text-white">{t.name}</h3>
              <span className={`px-2 py-0.5 rounded-full text-xs font-medium border ${consensusBadge.color}`}>
                {consensusBadge.label}
              </span>
              {!t.enabled && (
                <span className="px-2 py-0.5 rounded-full text-xs bg-slate-800 text-slate-400 border border-slate-700">
                  已暂停
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 text-xs text-slate-400 font-mono mt-1 break-all">
              <span className="text-slate-500 font-semibold">{t.method || 'GET'}</span>
              <span>{t.target_url}</span>
              {t.grpc_service && (
                <span className="text-purple-400">· service: {t.grpc_service}</span>
              )}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2 self-end lg:self-center">
          <button
            onClick={onTest}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-800 hover:bg-slate-700 text-emerald-400 border border-slate-700 transition"
            title="实时向此目标发送模拟探测"
          >
            <Play size={13} weight="bold" />
            即时调试
          </button>
          <button
            onClick={onViewHistory}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 transition"
          >
            <Clock size={13} />
            历史
          </button>
          <button
            onClick={onEdit}
            className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-slate-800 transition"
            title="编辑"
          >
            <PencilSimple size={15} />
          </button>
          <button
            onClick={onDelete}
            className="p-1.5 rounded-lg text-rose-400/70 hover:text-rose-400 hover:bg-rose-500/10 transition"
            title="删除"
          >
            <Trash size={15} />
          </button>
        </div>
      </div>

      {/* Timing Waterfall & Assertions */}
      <div className="grid grid-cols-1 md:grid-cols-12 gap-5 pt-4">
        {/* Timing Waterfall Chart */}
        <div className="md:col-span-7 space-y-2">
          <div className="flex items-center justify-between text-xs text-slate-400">
            <span className="font-medium text-slate-300 flex items-center gap-1.5">
              <Pulse size={14} className="text-indigo-400" />
              全链路网络时延瀑布流 (Waterfall)
            </span>
            <span className="font-mono text-slate-400">
              平均总时延: <strong className="text-white">{Math.round(item.avg_latency_ms)} ms</strong>
            </span>
          </div>

          {avgTiming ? (
            <WaterfallBar timing={avgTiming} />
          ) : (
            <div className="h-8 rounded-lg bg-slate-800/40 border border-slate-800/80 flex items-center justify-center text-xs text-slate-500">
              暂无探针节点上报时延瀑布数据
            </div>
          )}

          {/* Node Results Pills */}
          <div className="flex items-center gap-2 pt-1 flex-wrap">
            <span className="text-[11px] text-slate-500">参与拨测节点:</span>
            {results.length === 0 ? (
              <span className="text-[11px] text-slate-500">等待调度执行...</span>
            ) : (
              results.map((r, idx) => (
                <div
                  key={idx}
                  className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[11px] font-mono border ${
                    r.passed
                      ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
                      : 'bg-rose-500/10 text-rose-400 border-rose-500/20'
                  }`}
                  title={`状态码: ${r.status_code}, TTFB: ${r.ttfb_ms}ms, 总时延: ${r.total_ms}ms`}
                >
                  <span className={`w-1.5 h-1.5 rounded-full ${r.passed ? 'bg-emerald-400' : 'bg-rose-400'}`} />
                  <span>{r.node_name || r.node_id}</span>
                  <span className="text-slate-400">({r.total_ms}ms)</span>
                </div>
              ))
            )}
          </div>
        </div>

        {/* Assertions & SLA Rules */}
        <div className="md:col-span-5 space-y-2 border-t md:border-t-0 md:border-l border-slate-800/80 md:pl-5 pt-3 md:pt-0">
          <div className="text-xs font-medium text-slate-300 flex items-center gap-1.5">
            <Sliders size={14} className="text-purple-400" />
            SLA 契约断言 ({assertions.length})
          </div>
          <div className="flex flex-wrap gap-1.5">
            {assertions.length === 0 ? (
              <span className="text-xs text-slate-500">未配置自定义断言（默认检查 2xx/连接成功）</span>
            ) : (
              assertions.map((a, i) => (
                <span
                  key={i}
                  className="px-2 py-1 rounded bg-slate-800 text-[11px] font-mono text-slate-300 border border-slate-700/60"
                >
                  <span className="text-indigo-400">{a.source}</span>
                  {a.property && <span className="text-slate-400">.{a.property}</span>}{' '}
                  <span className="text-amber-400">{a.operator}</span>{' '}
                  <span className="text-emerald-400">{a.target}</span>
                </span>
              ))
            )}
          </div>
          <div className="text-[11px] text-slate-500 pt-1">
            探测周期: {t.interval_seconds || 60}s · 超时: {t.timeout_ms || 5000}ms · 共识阈值: {t.consensus_nodes || 1} 节点
          </div>
        </div>
      </div>
    </div>
  )
}

function WaterfallBar({ timing }) {
  const dns = Math.max(0, timing.dns_ms || 0)
  const connect = Math.max(0, timing.connect_ms || 0)
  const tls = Math.max(0, timing.tls_ms || 0)
  const ttfb = Math.max(0, timing.ttfb_ms || 0)
  const total = Math.max(1, timing.total_ms || dns + connect + tls + ttfb)
  const transfer = Math.max(0, total - (dns + connect + tls + ttfb))

  const pDNS = (dns / total) * 100
  const pConnect = (connect / total) * 100
  const pTLS = (tls / total) * 100
  const pTTFB = (ttfb / total) * 100
  const pTransfer = (transfer / total) * 100

  return (
    <div className="space-y-1.5">
      <div className="h-6 w-full rounded-lg bg-slate-800/80 overflow-hidden flex border border-slate-700/60">
        {pDNS > 0 && (
          <div
            style={{ width: `${pDNS}%` }}
            className="bg-purple-500/80 hover:bg-purple-400 transition flex items-center justify-center text-[10px] text-white font-mono"
            title={`DNS 解析: ${dns} ms`}
          >
            {pDNS > 12 && `${dns}ms`}
          </div>
        )}
        {pConnect > 0 && (
          <div
            style={{ width: `${pConnect}%` }}
            className="bg-blue-500/80 hover:bg-blue-400 transition flex items-center justify-center text-[10px] text-white font-mono"
            title={`TCP 连接: ${connect} ms`}
          >
            {pConnect > 12 && `${connect}ms`}
          </div>
        )}
        {pTLS > 0 && (
          <div
            style={{ width: `${pTLS}%` }}
            className="bg-amber-500/80 hover:bg-amber-400 transition flex items-center justify-center text-[10px] text-white font-mono"
            title={`TLS 握手: ${tls} ms`}
          >
            {pTLS > 12 && `${tls}ms`}
          </div>
        )}
        {pTTFB > 0 && (
          <div
            style={{ width: `${pTTFB}%` }}
            className="bg-emerald-500/80 hover:bg-emerald-400 transition flex items-center justify-center text-[10px] text-white font-mono"
            title={`首包等待 (TTFB): ${ttfb} ms`}
          >
            {pTTFB > 12 && `${ttfb}ms`}
          </div>
        )}
        {pTransfer > 0 && (
          <div
            style={{ width: `${pTransfer}%` }}
            className="bg-indigo-500/80 hover:bg-indigo-400 transition flex items-center justify-center text-[10px] text-white font-mono"
            title={`数据传输: ${transfer} ms`}
          >
            {pTransfer > 12 && `${transfer}ms`}
          </div>
        )}
      </div>

      <div className="flex items-center gap-3 text-[10px] text-slate-400 font-mono">
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded bg-purple-500 inline-block" /> DNS {dns}ms
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded bg-blue-500 inline-block" /> TCP {connect}ms
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded bg-amber-500 inline-block" /> TLS {tls}ms
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded bg-emerald-500 inline-block" /> TTFB {ttfb}ms
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded bg-indigo-500 inline-block" /> 传输 {transfer}ms
        </span>
      </div>
    </div>
  )
}

function TargetModal({ target, onClose, onSaved }) {
  const isEdit = Boolean(target?.id)
  const [name, setName] = useState(target?.name || '')
  const [protocol, setProtocol] = useState(target?.protocol || 'https')
  const [targetURL, setTargetURL] = useState(target?.target_url || '')
  const [method, setMethod] = useState(target?.method || 'GET')
  const [grpcService, setGrpcService] = useState(target?.grpc_service || '')
  const [interval, setIntervalVal] = useState(target?.interval_seconds || 60)
  const [timeout, setTimeoutVal] = useState(target?.timeout_ms || 5000)
  const [consensusNodes, setConsensusNodes] = useState(target?.consensus_nodes || 1)
  const [enabled, setEnabled] = useState(target?.enabled ?? true)

  // Assertions state
  const [assertions, setAssertions] = useState(() => {
    try {
      return target?.assertions ? JSON.parse(target.assertions) : [{ source: 'status_code', operator: 'equals', target: '200' }]
    } catch {
      return [{ source: 'status_code', operator: 'equals', target: '200' }]
    }
  })

  // Headers string
  const [headersText, setHeadersText] = useState(() => {
    try {
      return target?.headers ? JSON.stringify(JSON.parse(target.headers), null, 2) : ''
    } catch {
      return ''
    }
  })

  const [saving, setSaving] = useState(false)
  const [formErr, setFormErr] = useState('')

  const handleAddAssertion = () => {
    setAssertions([...assertions, { source: 'status_code', operator: 'equals', target: '200' }])
  }

  const handleRemoveAssertion = (index) => {
    setAssertions(assertions.filter((_, i) => i !== index))
  }

  const handleAssertionChange = (index, field, value) => {
    const next = [...assertions]
    next[index] = { ...next[index], [field]: value }
    setAssertions(next)
  }

  const handleApplyPreset = (preset) => {
    setName(preset.name)
    setProtocol(preset.protocol)
    setTargetURL(preset.target_url)
    setMethod(preset.method || 'GET')
    setGrpcService(preset.grpc_service || '')
    setIntervalVal(preset.interval_seconds || 60)
    setTimeoutVal(preset.timeout_ms || 5000)
    setConsensusNodes(preset.consensus_nodes || 1)
    setAssertions(preset.assertions || [])
    if (preset.headers) {
      setHeadersText(JSON.stringify(preset.headers, null, 2))
    } else {
      setHeadersText('')
    }
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    if (!name.trim()) return setFormErr('请输入契约名称')
    if (!targetURL.trim()) return setFormErr('请输入监控目标 URL')

    let headersObj = {}
    if (headersText.trim()) {
      try {
        headersObj = JSON.parse(headersText)
      } catch {
        return setFormErr('Headers 必须为有效的 JSON 对象格式')
      }
    }

    setSaving(true)
    setFormErr('')

    const payload = {
      name: name.trim(),
      protocol,
      target_url: targetURL.trim(),
      method: protocol === 'grpc' || protocol === 'websocket' ? 'GET' : method,
      grpc_service: grpcService.trim(),
      headers: headersObj,
      assertions,
      interval_seconds: Number(interval) || 60,
      timeout_ms: Number(timeout) || 5000,
      consensus_nodes: Number(consensusNodes) || 1,
      enabled,
    }

    try {
      if (isEdit) {
        await updateSyntheticTarget(target.id, payload)
      } else {
        await createSyntheticTarget(payload)
      }
      onSaved()
    } catch (err) {
      setFormErr(err.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/80 backdrop-blur-sm">
      <div className="bg-slate-900 border border-slate-800 rounded-2xl w-full max-w-2xl max-h-[90vh] overflow-y-auto shadow-2xl">
        <div className="sticky top-0 bg-slate-900/95 backdrop-blur-md px-6 py-4 border-b border-slate-800 flex items-center justify-between z-10">
          <h2 className="text-lg font-bold text-white flex items-center gap-2">
            <Pulse size={20} className="text-indigo-400" />
            {isEdit ? '编辑合成拨测契约' : '新建合成拨测契约'}
          </h2>
          <button onClick={onClose} className="p-1.5 text-slate-400 hover:text-white rounded-lg hover:bg-slate-800">
            <X size={18} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-6 space-y-5">
          {formErr && (
            <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-center gap-2">
              <WarningCircle size={16} />
              {formErr}
            </div>
          )}

          {/* Preset Buttons */}
          {!isEdit && (
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-2">快速填充预设模版</label>
              <div className="flex flex-wrap gap-2">
                {PRESET_TARGETS.map((p, idx) => (
                  <button
                    key={idx}
                    type="button"
                    onClick={() => handleApplyPreset(p)}
                    className="px-2.5 py-1 text-xs rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 transition"
                  >
                    + {p.name}
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1.5">契约名称 *</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如: 核心网关 API 健康"
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500"
                required
              />
            </div>

            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1.5">拨测协议 *</label>
              <select
                value={protocol}
                onChange={(e) => setProtocol(e.target.value)}
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500"
              >
                <option value="https">HTTPS</option>
                <option value="http">HTTP</option>
                <option value="grpc">gRPC Health Check</option>
                <option value="websocket">WebSocket (RFC 6455)</option>
                <option value="doh">DNS-over-HTTPS (DoH)</option>
              </select>
            </div>
          </div>

          <div>
            <label className="text-xs text-slate-400 font-medium block mb-1.5">目标 URL / 地址 *</label>
            <div className="flex gap-2">
              {(protocol === 'http' || protocol === 'https' || protocol === 'doh') && (
                <select
                  value={method}
                  onChange={(e) => setMethod(e.target.value)}
                  className="px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500 w-28"
                >
                  <option value="GET">GET</option>
                  <option value="POST">POST</option>
                  <option value="PUT">PUT</option>
                  <option value="DELETE">DELETE</option>
                  <option value="HEAD">HEAD</option>
                </select>
              )}
              <input
                type="text"
                value={targetURL}
                onChange={(e) => setTargetURL(e.target.value)}
                placeholder={
                  protocol === 'websocket'
                    ? 'wss://echo.websocket.events'
                    : protocol === 'grpc'
                    ? 'https://grpcb.in:9001'
                    : 'https://api.example.com/health'
                }
                className="flex-1 px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500 font-mono"
                required
              />
            </div>
          </div>

          {protocol === 'grpc' && (
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1.5">gRPC 服务名称 (可选)</label>
              <input
                type="text"
                value={grpcService}
                onChange={(e) => setGrpcService(e.target.value)}
                placeholder="例如: grpc.health.v1.Health"
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 font-mono focus:outline-none focus:border-indigo-500"
              />
            </div>
          )}

          {/* Assertions Builder */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="text-xs text-slate-300 font-semibold flex items-center gap-1.5">
                <ShieldCheck size={16} className="text-indigo-400" />
                SLA 契约断言规则
              </label>
              <button
                type="button"
                onClick={handleAddAssertion}
                className="text-xs text-indigo-400 hover:text-indigo-300 flex items-center gap-1 font-medium"
              >
                <Plus size={14} /> 添加断言
              </button>
            </div>

            <div className="space-y-2">
              {assertions.map((a, i) => (
                <div key={i} className="flex items-center gap-2 bg-slate-950 p-2.5 rounded-xl border border-slate-800">
                  <select
                    value={a.source}
                    onChange={(e) => handleAssertionChange(i, 'source', e.target.value)}
                    className="px-2 py-1.5 bg-slate-900 border border-slate-700/80 rounded-lg text-xs text-slate-200"
                  >
                    <option value="status_code">状态码 (status_code)</option>
                    <option value="jsonpath">JSONPath 字段 (jsonpath)</option>
                    <option value="body_regex">响应体匹配 (body_regex)</option>
                    <option value="header">响应头 (header)</option>
                    <option value="max_latency_ms">时延上限 (max_latency_ms)</option>
                    <option value="cert_days_left">证书剩余天数 (cert_days_left)</option>
                  </select>

                  {(a.source === 'jsonpath' || a.source === 'header') && (
                    <input
                      type="text"
                      value={a.property || ''}
                      onChange={(e) => handleAssertionChange(i, 'property', e.target.value)}
                      placeholder={a.source === 'jsonpath' ? '$.status' : 'content-type'}
                      className="w-28 px-2 py-1.5 bg-slate-900 border border-slate-700/80 rounded-lg text-xs text-slate-200 font-mono"
                    />
                  )}

                  <select
                    value={a.operator}
                    onChange={(e) => handleAssertionChange(i, 'operator', e.target.value)}
                    className="px-2 py-1.5 bg-slate-900 border border-slate-700/80 rounded-lg text-xs text-slate-200"
                  >
                    <option value="equals">等于 (equals)</option>
                    <option value="not_equals">不等于 (not_equals)</option>
                    <option value="contains">包含 (contains)</option>
                    <option value="regex_match">正则 (regex_match)</option>
                    <option value="less_than">小于 (less_than)</option>
                    <option value="greater_than">大于 (greater_than)</option>
                  </select>

                  <input
                    type="text"
                    value={a.target || ''}
                    onChange={(e) => handleAssertionChange(i, 'target', e.target.value)}
                    placeholder="预期值, 例如: 200"
                    className="flex-1 px-2 py-1.5 bg-slate-900 border border-slate-700/80 rounded-lg text-xs text-slate-200 font-mono"
                  />

                  {assertions.length > 1 && (
                    <button
                      type="button"
                      onClick={() => handleRemoveAssertion(i)}
                      className="p-1.5 text-slate-500 hover:text-rose-400"
                    >
                      <Trash size={14} />
                    </button>
                  )}
                </div>
              ))}
            </div>
          </div>

          {/* Headers JSON */}
          <div>
            <label className="text-xs text-slate-400 font-medium block mb-1.5">自定义 Headers (JSON 格式，可选)</label>
            <textarea
              rows={2}
              value={headersText}
              onChange={(e) => setHeadersText(e.target.value)}
              placeholder='{ "Authorization": "Bearer token", "X-Custom-Header": "value" }'
              className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-xs font-mono text-slate-200 focus:outline-none focus:border-indigo-500"
            />
          </div>

          {/* Tuning params */}
          <div className="grid grid-cols-3 gap-3">
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1">拨测间隔 (秒)</label>
              <input
                type="number"
                min="10"
                max="86400"
                value={interval}
                onChange={(e) => setIntervalVal(e.target.value)}
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500 font-mono"
              />
            </div>
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1">超时时间 (ms)</label>
              <input
                type="number"
                min="500"
                max="30000"
                value={timeout}
                onChange={(e) => setTimeoutVal(e.target.value)}
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500 font-mono"
              />
            </div>
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1">多节点共识阈值</label>
              <input
                type="number"
                min="1"
                max="10"
                value={consensusNodes}
                onChange={(e) => setConsensusNodes(e.target.value)}
                title="需至少几个节点同时探测失败才触发告警（防止单 ISP 误报）"
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-sm text-slate-200 focus:outline-none focus:border-indigo-500 font-mono"
              />
            </div>
          </div>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="synEnabled"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              className="rounded bg-slate-950 border-slate-800 text-indigo-600 focus:ring-0"
            />
            <label htmlFor="synEnabled" className="text-xs text-slate-300 font-medium cursor-pointer">
              立即启用该监控契约 (由全网节点同步拉取执行)
            </label>
          </div>

          <div className="flex items-center justify-end gap-3 pt-3 border-t border-slate-800">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-xl text-sm text-slate-400 hover:text-white hover:bg-slate-800"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={saving}
              className="px-5 py-2 rounded-xl text-sm font-medium bg-indigo-600 hover:bg-indigo-500 text-white shadow-lg shadow-indigo-600/20 disabled:opacity-50 transition"
            >
              {saving ? '保存中...' : isEdit ? '更新契约' : '立即创建'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function LiveTestModal({ initialTarget, onClose }) {
  const [protocol, setProtocol] = useState(initialTarget?.protocol || 'https')
  const [targetURL, setTargetURL] = useState(initialTarget?.target_url || 'https://1.1.1.1/cdn-cgi/trace')
  const [method, setMethod] = useState(initialTarget?.method || 'GET')
  const [assertionsText, setAssertionsText] = useState(() => {
    if (initialTarget?.assertions) {
      try {
        return JSON.stringify(JSON.parse(initialTarget.assertions), null, 2)
      } catch {}
    }
    return JSON.stringify([{ source: 'status_code', operator: 'equals', target: '200' }], null, 2)
  })
  const [headersText, setHeadersText] = useState(() => {
    if (initialTarget?.headers) {
      try {
        return JSON.stringify(JSON.parse(initialTarget.headers), null, 2)
      } catch {}
    }
    return ''
  })
  const [grpcService, setGrpcService] = useState(initialTarget?.grpc_service || '')

  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState(null)
  const [testErr, setTestErr] = useState('')

  const handleRunTest = async () => {
    if (!targetURL.trim()) {
      setTestErr('请输入目标 URL')
      return
    }

    let parsedAssertions = []
    if (assertionsText.trim()) {
      try {
        parsedAssertions = JSON.parse(assertionsText)
      } catch {
        setTestErr('断言规则必须是有效的 JSON 数组')
        return
      }
    }

    let parsedHeaders = {}
    if (headersText.trim()) {
      try {
        parsedHeaders = JSON.parse(headersText)
      } catch {
        setTestErr('Headers 必须是有效的 JSON 对象')
        return
      }
    }

    setTesting(true)
    setTestErr('')
    setResult(null)

    try {
      const res = await testSyntheticTarget({
        protocol,
        target_url: targetURL.trim(),
        method,
        headers: parsedHeaders,
        assertions: parsedAssertions,
        grpc_service: grpcService.trim(),
        timeout_ms: 5000,
      })
      setResult(res)
    } catch (err) {
      setTestErr(err.message || '模拟拨测请求失败')
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/80 backdrop-blur-sm">
      <div className="bg-slate-900 border border-slate-800 rounded-2xl w-full max-w-3xl max-h-[90vh] overflow-y-auto shadow-2xl">
        <div className="sticky top-0 bg-slate-900/95 backdrop-blur-md px-6 py-4 border-b border-slate-800 flex items-center justify-between z-10">
          <h2 className="text-lg font-bold text-white flex items-center gap-2">
            <Play size={20} className="text-emerald-400" />
            实时合成模拟拨测与断言调试器
          </h2>
          <button onClick={onClose} className="p-1.5 text-slate-400 hover:text-white rounded-lg hover:bg-slate-800">
            <X size={18} />
          </button>
        </div>

        <div className="p-6 space-y-5">
          {testErr && (
            <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-center gap-2">
              <WarningCircle size={16} />
              {testErr}
            </div>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1">拨测协议</label>
              <select
                value={protocol}
                onChange={(e) => setProtocol(e.target.value)}
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-xs text-slate-200"
              >
                <option value="https">HTTPS</option>
                <option value="http">HTTP</option>
                <option value="grpc">gRPC</option>
                <option value="websocket">WebSocket</option>
                <option value="doh">DoH</option>
              </select>
            </div>
            <div className="sm:col-span-3">
              <label className="text-xs text-slate-400 font-medium block mb-1">目标 URL</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={targetURL}
                  onChange={(e) => setTargetURL(e.target.value)}
                  className="flex-1 px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-xs text-slate-200 font-mono"
                />
                <button
                  onClick={handleRunTest}
                  disabled={testing}
                  className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-xs font-medium flex items-center gap-1.5 shadow-md shadow-emerald-600/20 disabled:opacity-50"
                >
                  <Play size={14} weight="bold" />
                  {testing ? '探测中...' : '立即测试'}
                </button>
              </div>
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1">断言规则 (JSON 格式)</label>
              <textarea
                rows={3}
                value={assertionsText}
                onChange={(e) => setAssertionsText(e.target.value)}
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-xs font-mono text-slate-200"
              />
            </div>
            <div>
              <label className="text-xs text-slate-400 font-medium block mb-1">请求头 Headers (JSON 格式)</label>
              <textarea
                rows={3}
                value={headersText}
                onChange={(e) => setHeadersText(e.target.value)}
                className="w-full px-3 py-2 bg-slate-950 border border-slate-800 rounded-xl text-xs font-mono text-slate-200"
              />
            </div>
          </div>

          {/* Test Result Display */}
          {result && (
            <div className="bg-slate-950 p-5 rounded-2xl border border-slate-800 space-y-4">
              <div className="flex items-center justify-between pb-3 border-b border-slate-800">
                <div className="flex items-center gap-2">
                  <span
                    className={`px-2.5 py-0.5 rounded-full text-xs font-bold border ${
                      result.passed
                        ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
                        : 'bg-rose-500/10 text-rose-400 border-rose-500/20'
                    }`}
                  >
                    {result.passed ? '✓ 断言全部通过' : '✗ 断言未通过'}
                  </span>
                  <span className="text-xs text-slate-300 font-mono">
                    HTTP 状态码: <strong>{result.status_code || 'N/A'}</strong>
                  </span>
                </div>
                <div className="text-xs text-slate-400 font-mono">
                  总用时: <strong className="text-white">{result.timing?.total_duration_ms || 0} ms</strong>
                </div>
              </div>

              {result.failed_assertion && (
                <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs font-mono">
                  失败断言详情: {result.failed_assertion}
                </div>
              )}

              {/* Waterfall Bar in Test Modal */}
              {result.timing && (
                <div className="space-y-1.5">
                  <span className="text-xs font-medium text-slate-300">网络流水线时延分解:</span>
                  <WaterfallBar timing={result.timing} />
                </div>
              )}

              {result.dns_answers && result.dns_answers.length > 0 && (
                <div className="text-xs font-mono text-slate-300">
                  <span className="text-slate-500">DoH 解析结果: </span>
                  {result.dns_answers.join(', ')}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function HistoryModal({ target, onClose }) {
  const [history, setHistory] = useState([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchSyntheticHistory(target.id)
      .then((res) => setHistory(res || []))
      .catch((e) => console.error(e))
      .finally(() => setLoading(false))
  }, [target.id])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/80 backdrop-blur-sm">
      <div className="bg-slate-900 border border-slate-800 rounded-2xl w-full max-w-3xl max-h-[85vh] overflow-y-auto shadow-2xl">
        <div className="sticky top-0 bg-slate-900/95 backdrop-blur-md px-6 py-4 border-b border-slate-800 flex items-center justify-between z-10">
          <div>
            <h2 className="text-lg font-bold text-white flex items-center gap-2">
              <Clock size={20} className="text-indigo-400" />
              拨测审计历史: {target.name}
            </h2>
            <div className="text-xs text-slate-400 font-mono mt-0.5">{target.target_url}</div>
          </div>
          <button onClick={onClose} className="p-1.5 text-slate-400 hover:text-white rounded-lg hover:bg-slate-800">
            <X size={18} />
          </button>
        </div>

        <div className="p-6">
          {loading ? (
            <div className="p-10 text-center text-slate-400">
              <ArrowsClockwise size={24} className="animate-spin mx-auto text-indigo-400 mb-2" />
              加载历史记录中...
            </div>
          ) : history.length === 0 ? (
            <div className="p-10 text-center text-slate-500">暂无该目标的拨测历史记录</div>
          ) : (
            <div className="space-y-2">
              {history.map((h, i) => (
                <div
                  key={i}
                  className="flex items-center justify-between p-3 bg-slate-950/60 rounded-xl border border-slate-800 text-xs font-mono"
                >
                  <div className="flex items-center gap-3">
                    <span
                      className={`w-2 h-2 rounded-full ${h.passed ? 'bg-emerald-400' : 'bg-rose-400'}`}
                    />
                    <span className="text-slate-300 font-medium">{h.node_name || h.node_id}</span>
                    <span className="text-slate-500">[{h.protocol}]</span>
                    <span className="text-slate-400">状态码: {h.status_code}</span>
                  </div>
                  <div className="flex items-center gap-4 text-slate-400">
                    <span>总时延: <strong className="text-white">{h.total_ms}ms</strong></span>
                    <span>TTFB: {h.ttfb_ms}ms</span>
                    <span className="text-slate-500">{new Date(h.checked_at).toLocaleTimeString()}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
