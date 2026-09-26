import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowsClockwise,
  CheckCircle,
  Compass,
  Cpu,
  Globe,
  Lightning,
  MagnifyingGlass,
  PencilSimple,
  ShieldCheck,
  Tag,
  TrendUp,
  WarningCircle,
  X,
  XCircle,
} from '@phosphor-icons/react'
import { fetchMeshMatrix, updateNode } from '../lib/api.js'

export function MeshMatrixView() {
  const [data, setData] = useState({ nodes: [], matrix: [], relays: [], stats: {} })
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState(null)

  // Filters
  const [selectedTag, setSelectedTag] = useState('all')
  const [search, setSearch] = useState('')
  const [hoveredCell, setHoveredCell] = useState(null)
  const [selectedCell, setSelectedCell] = useState(null)

  // Edit Node Modal
  const [editingNode, setEditingNode] = useState(null)
  const [editName, setEditName] = useState('')
  const [editTags, setEditTags] = useState('')
  const [savingNode, setSavingNode] = useState(false)
  const [saveError, setSaveError] = useState(null)

  const loadData = useCallback(async (isManual = false) => {
    if (isManual) setRefreshing(true)
    setError(null)
    try {
      const res = await fetchMeshMatrix()
      setData(res || { nodes: [], matrix: [], relays: [], stats: {} })
    } catch (err) {
      setError(err.message || '加载全球互联延迟网格失败')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    const timer = setInterval(() => loadData(false), 30000)
    return () => clearInterval(timer)
  }, [loadData])

  // Extract all unique tags
  const allTags = useMemo(() => {
    const set = new Set()
    for (const node of data.nodes || []) {
      for (const t of node.tags || []) {
        if (t.trim()) set.add(t.trim())
      }
    }
    return Array.from(set).sort()
  }, [data.nodes])

  // Filter nodes
  const filteredNodes = useMemo(() => {
    return (data.nodes || []).filter((node) => {
      if (selectedTag !== 'all') {
        if (!node.tags || !node.tags.includes(selectedTag)) return false
      }
      if (search) {
        const q = search.toLowerCase()
        const matchName = (node.name || '').toLowerCase().includes(q)
        const matchIP = (node.ip || '').toLowerCase().includes(q)
        const matchHost = (node.hostname || '').toLowerCase().includes(q)
        const matchTag = (node.tags || []).some((t) => t.toLowerCase().includes(q))
        if (!matchName && !matchIP && !matchHost && !matchTag) return false
      }
      return true
    })
  }, [data.nodes, selectedTag, search])

  // Filtered node ID set for fast lookup
  const filteredNodeIDs = useMemo(() => {
    return new Set(filteredNodes.map((n) => n.id))
  }, [filteredNodes])

  // Map original node indices
  const originalNodeIndexMap = useMemo(() => {
    const map = new Map()
    ;(data.nodes || []).forEach((n, idx) => {
      map.set(n.id, idx)
    })
    return map
  }, [data.nodes])

  const handleEditNodeClick = (node) => {
    setEditingNode(node)
    setEditName(node.name || '')
    setEditTags((node.tags || []).join(', '))
    setSaveError(null)
  }

  const handleSaveNode = async (e) => {
    e.preventDefault()
    if (!editingNode) return
    setSavingNode(true)
    setSaveError(null)
    try {
      await updateNode(editingNode.id, {
        name: editName.trim(),
        tags: editTags
          .split(',')
          .map((t) => t.trim())
          .filter(Boolean),
      })
      setEditingNode(null)
      loadData(false)
    } catch (err) {
      setSaveError(err.message || '保存节点设置失败')
    } finally {
      setSavingNode(false)
    }
  }

  const getCellStatusClass = (status, latency) => {
    if (status === 'self') {
      return 'bg-gray-800/40 text-gray-500 border-gray-700/30'
    }
    if (status === 'loss') {
      return 'bg-red-500/20 text-red-400 border-red-500/40 font-bold'
    }
    if (latency < 0 || status === 'unmeasured') {
      return 'bg-gray-900/30 text-gray-600 border-dashed border-gray-800'
    }
    if (latency < 50) {
      return 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30 hover:bg-emerald-500/30'
    }
    if (latency < 100) {
      return 'bg-sky-500/20 text-sky-300 border-sky-500/30 hover:bg-sky-500/30'
    }
    if (latency < 200) {
      return 'bg-amber-500/20 text-amber-300 border-amber-500/30 hover:bg-amber-500/30'
    }
    return 'bg-rose-500/20 text-rose-300 border-rose-500/30 hover:bg-rose-500/30'
  }

  // Look for any relay advice for a cell
  const getCellRelay = (fromId, toId) => {
    return (data.relays || []).find((r) => r.from_node_id === fromId && r.to_node_id === toId)
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h2 className="text-xl font-bold text-gray-100 flex items-center gap-2">
            <Globe className="w-6 h-6 text-sky-400" />
            全球节点间全互联延迟矩阵 (Mesh Latency Matrix)
          </h2>
          <p className="text-sm text-gray-400 mt-1">
            动态感知多区域边缘节点间双向往返延迟，精准生成全互联热力分布，并智能识别两跳优化中继中转路由。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => loadData(true)}
            disabled={refreshing}
            className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-300 text-sm font-medium border border-gray-700 transition"
          >
            <ArrowsClockwise className={`w-4 h-4 ${refreshing ? 'animate-spin text-sky-400' : ''}`} />
            刷新网格
          </button>
        </div>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-4 flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-sky-500/10 text-sky-400">
            <Globe className="w-5 h-5" />
          </div>
          <div>
            <div className="text-xs text-gray-400">边缘节点规模</div>
            <div className="text-lg font-bold text-gray-100">
              {data.stats?.online_nodes ?? 0} <span className="text-xs font-normal text-gray-500">/ {data.stats?.total_nodes ?? 0} 在线</span>
            </div>
          </div>
        </div>

        <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-4 flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-emerald-500/10 text-emerald-400">
            <TrendUp className="w-5 h-5" />
          </div>
          <div>
            <div className="text-xs text-gray-400">全网平均互联延迟</div>
            <div className="text-lg font-bold text-gray-100">
              {data.stats?.avg_latency_ms ? `${data.stats.avg_latency_ms} ms` : '—'}
            </div>
          </div>
        </div>

        <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-4 flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-amber-500/10 text-amber-400">
            <Lightning className="w-5 h-5" />
          </div>
          <div>
            <div className="text-xs text-gray-400">智能中继优化建议</div>
            <div className="text-lg font-bold text-amber-300">
              {data.stats?.relay_optimizations_count ?? 0} <span className="text-xs font-normal text-gray-500">条更优路径</span>
            </div>
          </div>
        </div>

        <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-4 flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-indigo-500/10 text-indigo-400">
            <Compass className="w-5 h-5" />
          </div>
          <div>
            <div className="text-xs text-gray-400">全网测距覆盖率</div>
            <div className="text-lg font-bold text-gray-100">
              {data.stats?.total_nodes > 1
                ? `${Math.round(((data.stats?.measured_pairs ?? 0) / (data.stats.total_nodes * (data.stats.total_nodes - 1))) * 100)}%`
                : '100%'}
            </div>
          </div>
        </div>
      </div>

      {/* Filter and Tag Bar */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3 bg-gray-900/40 p-3 rounded-xl border border-gray-800">
        <div className="flex items-center gap-2 overflow-x-auto pb-1 sm:pb-0">
          <span className="text-xs text-gray-400 flex items-center gap-1 shrink-0">
            <Tag className="w-3.5 h-3.5 text-gray-500" />
            标签分组:
          </span>
          <button
            onClick={() => setSelectedTag('all')}
            className={`px-2.5 py-1 rounded-md text-xs font-medium transition shrink-0 ${
              selectedTag === 'all' ? 'bg-sky-500 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
            }`}
          >
            全部节点 ({data.nodes?.length || 0})
          </button>
          {allTags.map((tag) => (
            <button
              key={tag}
              onClick={() => setSelectedTag(tag)}
              className={`px-2.5 py-1 rounded-md text-xs font-medium transition shrink-0 flex items-center gap-1 ${
                selectedTag === tag ? 'bg-sky-500 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
              }`}
            >
              #{tag}
            </button>
          ))}
        </div>

        <div className="relative min-w-[200px]">
          <MagnifyingGlass className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
          <input
            type="text"
            placeholder="搜索节点名称/IP/标签..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-gray-950 border border-gray-800 rounded-lg pl-9 pr-3 py-1.5 text-xs text-gray-200 placeholder-gray-500 focus:outline-none focus:border-sky-500 transition"
          />
        </div>
      </div>

      {/* Matrix Table */}
      <div className="bg-gray-900/50 border border-gray-800 rounded-xl overflow-hidden shadow-xl">
        <div className="p-4 border-b border-gray-800/80 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-gray-200">
            <span>延迟热力图表 (矩阵行: 发起节点 ➔ 矩阵列: 目标节点)</span>
          </div>

          <div className="flex items-center gap-3 text-xs text-gray-400">
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-400 inline-block"></span> 极佳 &lt;50ms
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-sky-400 inline-block"></span> 良好 50-100ms
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-amber-400 inline-block"></span> 一般 100-200ms
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-rose-400 inline-block"></span> 较高 &gt;200ms
            </span>
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-2.5 rounded-full bg-red-600 inline-block"></span> 丢包/超时
            </span>
          </div>
        </div>

        {loading ? (
          <div className="py-20 text-center text-gray-400 flex flex-col items-center gap-3">
            <ArrowsClockwise className="w-8 h-8 animate-spin text-sky-400" />
            <span>正在测算全球全互联延迟矩阵...</span>
          </div>
        ) : filteredNodes.length === 0 ? (
          <div className="py-16 text-center text-gray-500 text-sm">
            未找到符合条件的节点。
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="bg-gray-950/60 border-b border-gray-800">
                  <th className="p-3 text-xs font-semibold text-gray-400 sticky left-0 bg-gray-950/90 z-20 min-w-[200px]">
                    发起节点 \ 目标节点
                  </th>
                  {filteredNodes.map((dstNode) => (
                    <th key={dstNode.id} className="p-3 text-center min-w-[120px]">
                      <div className="text-xs font-semibold text-gray-200 truncate max-w-[120px] mx-auto" title={dstNode.name}>
                        {dstNode.name}
                      </div>
                      <div className="text-[10px] text-gray-500 font-mono mt-0.5 truncate max-w-[120px] mx-auto">
                        {dstNode.ip || '—'}
                      </div>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {filteredNodes.map((srcNode) => {
                  const srcIdx = originalNodeIndexMap.get(srcNode.id)
                  return (
                    <tr key={srcNode.id} className="hover:bg-gray-800/20 transition">
                      <td className="p-3 sticky left-0 bg-gray-950/90 z-10 border-r border-gray-800/60">
                        <div className="flex items-center justify-between gap-2">
                          <div className="min-w-0">
                            <div className="flex items-center gap-1.5">
                              <span
                                className={`w-2 h-2 rounded-full shrink-0 ${
                                  srcNode.status === 'online'
                                    ? 'bg-emerald-400'
                                    : srcNode.status === 'attention'
                                    ? 'bg-amber-400'
                                    : 'bg-gray-500'
                                }`}
                              />
                              <span className="text-xs font-semibold text-gray-100 truncate" title={srcNode.name}>
                                {srcNode.name}
                              </span>
                            </div>
                            <div className="flex items-center gap-1.5 mt-1">
                              <span className="text-[10px] font-mono text-gray-400">{srcNode.ip || '—'}</span>
                              {(srcNode.tags || []).slice(0, 2).map((t) => (
                                <span key={t} className="px-1.5 py-0.2 text-[9px] rounded bg-gray-800 text-sky-400">
                                  {t}
                                </span>
                              ))}
                            </div>
                          </div>
                          <button
                            onClick={() => handleEditNodeClick(srcNode)}
                            title="编辑节点标签与名称"
                            className="p-1 rounded text-gray-500 hover:text-gray-200 hover:bg-gray-800 transition shrink-0"
                          >
                            <PencilSimple className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </td>

                      {filteredNodes.map((dstNode) => {
                        const dstIdx = originalNodeIndexMap.get(dstNode.id)
                        const cell =
                          srcIdx !== undefined && dstIdx !== undefined && data.matrix?.[srcIdx]?.[dstIdx]
                            ? data.matrix[srcIdx][dstIdx]
                            : { latency_ms: -1, status: 'unmeasured' }

                        const relay = getCellRelay(srcNode.id, dstNode.id)
                        const statusClass = getCellStatusClass(cell.status, cell.latency_ms)

                        return (
                          <td
                            key={dstNode.id}
                            className="p-2 text-center"
                            onMouseEnter={() => setHoveredCell({ srcNode, dstNode, cell, relay })}
                            onMouseLeave={() => setHoveredCell(null)}
                            onClick={() => setSelectedCell({ srcNode, dstNode, cell, relay })}
                          >
                            <div
                              className={`relative cursor-pointer py-2 px-1 rounded-lg border text-xs transition duration-150 select-none ${statusClass}`}
                            >
                              {cell.status === 'self' ? (
                                <span className="text-gray-500">—</span>
                              ) : cell.status === 'loss' ? (
                                <span>丢包</span>
                              ) : cell.latency_ms > 0 ? (
                                <div className="font-mono">
                                  <span>{cell.latency_ms}</span>
                                  <span className="text-[10px] ml-0.5 opacity-75">ms</span>
                                </div>
                              ) : (
                                <span className="text-gray-600 font-mono">—</span>
                              )}

                              {relay && (
                                <span
                                  title={`智能中继加速: 经 ${relay.relay_node_name} 可降至 ${relay.relay_ms}ms (节省 ${relay.savings_percent}%)`}
                                  className="absolute -top-1 -right-1 flex h-3 w-3"
                                >
                                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"></span>
                                  <span className="relative inline-flex rounded-full h-3 w-3 bg-amber-500 text-[8px] items-center justify-center text-black font-bold">
                                    ⚡
                                  </span>
                                </span>
                              )}
                            </div>
                          </td>
                        )
                      })}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Hover / Selected Cell Detail Popover */}
      {(hoveredCell || selectedCell) && (
        <div className="bg-gray-900/90 border border-sky-500/30 rounded-xl p-4 shadow-2xl flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="text-xs font-semibold text-sky-400 flex items-center gap-1.5">
              <span>互联测速详情</span>
              {(hoveredCell || selectedCell).cell.direct ? (
                <span className="px-1.5 py-0.5 text-[10px] rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
                  真实直连探测
                </span>
              ) : (
                <span className="px-1.5 py-0.5 text-[10px] rounded bg-gray-800 text-gray-400">
                  三角测量推算
                </span>
              )}
            </div>
            <div className="text-sm text-gray-200 flex items-center gap-2">
              <span className="font-semibold text-gray-100">{(hoveredCell || selectedCell).srcNode.name}</span>
              <span className="text-gray-500">➔</span>
              <span className="font-semibold text-gray-100">{(hoveredCell || selectedCell).dstNode.name}</span>
              <span className="text-xs font-mono text-gray-400">
                ({(hoveredCell || selectedCell).srcNode.ip || '—'} ➔ {(hoveredCell || selectedCell).dstNode.ip || '—'})
              </span>
            </div>
          </div>

          <div className="flex items-center gap-6">
            <div>
              <div className="text-[11px] text-gray-400">往返延迟 (RTT)</div>
              <div className="text-lg font-bold font-mono text-gray-100">
                {(hoveredCell || selectedCell).cell.latency_ms > 0
                  ? `${(hoveredCell || selectedCell).cell.latency_ms} ms`
                  : '未测算'}
              </div>
            </div>

            {(hoveredCell || selectedCell).relay && (
              <div className="pl-6 border-l border-gray-800">
                <div className="text-[11px] text-amber-400 flex items-center gap-1">
                  <Lightning className="w-3.5 h-3.5" />
                  智能中继提速方案
                </div>
                <div className="text-xs text-gray-200 mt-0.5">
                  经由 <span className="text-amber-300 font-semibold">{(hoveredCell || selectedCell).relay.relay_node_name}</span> 中转
                  仅需 <span className="font-mono text-emerald-400 font-bold">{(hoveredCell || selectedCell).relay.relay_ms} ms</span>
                  <span className="ml-1.5 px-1.5 py-0.5 rounded text-[10px] bg-amber-500/10 text-amber-300 border border-amber-500/30">
                    ⚡ 节省 {(hoveredCell || selectedCell).relay.savings_percent}%
                  </span>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Relay Optimizations Panel */}
      {data.relays && data.relays.length > 0 && (
        <div className="bg-gray-900/60 border border-amber-500/20 rounded-xl p-5 space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-base font-bold text-gray-100 flex items-center gap-2">
              <Lightning className="w-5 h-5 text-amber-400" />
              智能两跳中继路由建议 (BGP / Overlay Route Acceleration)
            </h3>
            <span className="text-xs text-gray-400">
              根据两两节点间实测延迟，自动挖掘打破三角不等式的更优中转路径
            </span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {data.relays.map((r, i) => (
              <div
                key={`${r.from_node_id}-${r.to_node_id}`}
                className="bg-gray-950/70 border border-gray-800 rounded-lg p-3 hover:border-amber-500/40 transition flex flex-col justify-between gap-3"
              >
                <div>
                  <div className="flex items-center justify-between text-xs text-gray-400">
                    <span className="flex items-center gap-1 font-semibold text-gray-300">
                      {r.from_node_name} ➔ {r.to_node_name}
                    </span>
                    <span className="px-1.5 py-0.5 text-[10px] rounded bg-amber-500/10 text-amber-400 font-bold">
                      节省 {r.savings_percent}%
                    </span>
                  </div>

                  <div className="flex items-center justify-between text-xs font-mono mt-2">
                    <span className="text-gray-400 line-through">直连: {r.direct_ms} ms</span>
                    <span className="text-emerald-400 font-bold text-sm">中继: {r.relay_ms} ms</span>
                  </div>
                </div>

                <div className="pt-2 border-t border-gray-800/80 text-[11px] text-gray-400 flex items-center justify-between">
                  <span>推荐中继站:</span>
                  <span className="text-sky-300 font-medium">{r.relay_node_name}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Edit Node Modal */}
      {editingNode && (
        <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
          <div className="bg-gray-900 border border-gray-800 rounded-xl max-w-md w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-bold text-gray-100 flex items-center gap-2">
                <PencilSimple className="w-5 h-5 text-sky-400" />
                配置节点属性与调度标签
              </h3>
              <button
                onClick={() => setEditingNode(null)}
                className="text-gray-500 hover:text-gray-300 transition"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {saveError && (
              <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-xs">
                {saveError}
              </div>
            )}

            <form onSubmit={handleSaveNode} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-gray-300 mb-1">节点名称</label>
                <input
                  type="text"
                  required
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  className="w-full bg-gray-950 border border-gray-800 rounded-lg px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-sky-500"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-gray-300 mb-1">
                  分组标签 (逗号分隔，用于探针任务定向调度与网格筛选)
                </label>
                <input
                  type="text"
                  placeholder="例如: asia, sg, prod, edge"
                  value={editTags}
                  onChange={(e) => setEditTags(e.target.value)}
                  className="w-full bg-gray-950 border border-gray-800 rounded-lg px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-sky-500"
                />
                <p className="text-[11px] text-gray-500 mt-1">
                  目标监控任务可以通过设置 <code>node_tags</code> 指定仅让带有对应标签的边缘节点下发执行。
                </p>
              </div>

              <div className="flex items-center justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={() => setEditingNode(null)}
                  className="px-4 py-2 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-300 text-sm font-medium transition"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={savingNode}
                  className="px-4 py-2 rounded-lg bg-sky-500 hover:bg-sky-400 text-white text-sm font-semibold transition flex items-center gap-2"
                >
                  {savingNode ? '保存中...' : '保存更改'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
