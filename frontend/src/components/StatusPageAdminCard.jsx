import { useEffect, useState } from 'react'
import {
  Broadcast,
  Plus,
  Trash,
  ArrowsDownUp,
  FloppyDisk,
  CheckCircle,
  WarningCircle,
  Clock,
  Wrench,
  ChatCircleText,
  Eye,
  Sliders,
} from '@phosphor-icons/react'
import {
  fetchAdminStatusPageConfig,
  updateAdminStatusPageConfig,
  fetchAdminIncidents,
  createAdminIncident,
  addAdminIncidentUpdate,
  deleteAdminIncident,
} from '../lib/api.js'

export function StatusPageAdminCard({ nodes = [] }) {
  const [config, setConfig] = useState(null)
  const [incidents, setIncidents] = useState([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState(null)

  // Incident Modal State
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [createForm, setCreateForm] = useState({
    title: '',
    status: 'investigating',
    impact: 'minor',
    is_maintenance: false,
    message: '',
    scheduled_start_at: '',
    scheduled_end_at: '',
  })
  const [createLoading, setCreateLoading] = useState(false)

  // Update Incident Modal State
  const [selectedIncident, setSelectedIncident] = useState(null)
  const [updateStatus, setUpdateStatus] = useState('monitoring')
  const [updateMessage, setUpdateMessage] = useState('')
  const [updateLoading, setUpdateLoading] = useState(false)

  // Add Component Modal State
  const [showAddCompModal, setShowAddCompModal] = useState(false)
  const [newComp, setNewComp] = useState({
    name: '',
    group: '核心服务',
    node_id: '',
    description: '',
    show_latency: true,
  })

  const loadAll = async () => {
    setLoading(true)
    try {
      const [cfgRes, incRes] = await Promise.all([
        fetchAdminStatusPageConfig(),
        fetchAdminIncidents({ all: true }),
      ])
      setConfig(cfgRes)
      setIncidents(incRes.incidents || [])
    } catch (err) {
      console.error('Failed to load status page admin data:', err)
      setMsg({ type: 'error', text: '加载状态页数据失败: ' + err.message })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadAll()
  }, [])

  const handleSaveConfig = async () => {
    if (!config) return
    setSaving(true)
    setMsg(null)
    try {
      await updateAdminStatusPageConfig(config)
      setMsg({ type: 'success', text: '状态页配置已成功保存并同步！' })
      setTimeout(() => setMsg(null), 3000)
    } catch (err) {
      setMsg({ type: 'error', text: '保存失败: ' + err.message })
    } finally {
      setSaving(false)
    }
  }

  const handleAddComponent = () => {
    if (!newComp.name.trim()) return
    const id = `comp_${Date.now()}`
    const compObj = {
      id,
      name: newComp.name.trim(),
      group: newComp.group.trim() || '通用服务',
      node_id: newComp.node_id || '',
      description: newComp.description.trim(),
      show_latency: newComp.show_latency,
      order: (config.components?.length || 0) + 1,
    }
    setConfig((prev) => ({
      ...prev,
      components: [...(prev.components || []), compObj],
    }))
    setNewComp({ name: '', group: '核心服务', node_id: '', description: '', show_latency: true })
    setShowAddCompModal(false)
  }

  const handleRemoveComponent = (id) => {
    setConfig((prev) => ({
      ...prev,
      components: prev.components.filter((c) => c.id !== id),
    }))
  }

  const handleCreateIncident = async (e) => {
    e.preventDefault()
    if (!createForm.title.trim()) return
    setCreateLoading(true)
    try {
      const payload = {
        title: createForm.title.trim(),
        status: createForm.status,
        impact: createForm.impact,
        is_maintenance: createForm.is_maintenance,
        message: createForm.message.trim(),
      }
      if (createForm.is_maintenance) {
        if (createForm.scheduled_start_at) {
          payload.scheduled_start_at = Math.floor(new Date(createForm.scheduled_start_at).getTime() / 1000)
        }
        if (createForm.scheduled_end_at) {
          payload.scheduled_end_at = Math.floor(new Date(createForm.scheduled_end_at).getTime() / 1000)
        }
      }
      await createAdminIncident(payload)
      setShowCreateModal(false)
      setCreateForm({
        title: '',
        status: 'investigating',
        impact: 'minor',
        is_maintenance: false,
        message: '',
        scheduled_start_at: '',
        scheduled_end_at: '',
      })
      await loadAll()
    } catch (err) {
      alert('发布失败: ' + err.message)
    } finally {
      setCreateLoading(false)
    }
  }

  const handleAddUpdate = async (e) => {
    e.preventDefault()
    if (!selectedIncident || !updateMessage.trim()) return
    setUpdateLoading(true)
    try {
      await addAdminIncidentUpdate(selectedIncident.id, {
        status: updateStatus,
        message: updateMessage.trim(),
      })
      setSelectedIncident(null)
      setUpdateMessage('')
      await loadAll()
    } catch (err) {
      alert('追加更新失败: ' + err.message)
    } finally {
      setUpdateLoading(false)
    }
  }

  const handleDeleteIncident = async (id) => {
    if (!confirm('确定彻底删除该事件通告？该操作不可逆。')) return
    try {
      await deleteAdminIncident(id)
      await loadAll()
    } catch (err) {
      alert('删除失败: ' + err.message)
    }
  }

  const formatTimestamp = (ts) => {
    if (!ts) return '-'
    return new Date(ts * 1000).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
  }

  if (loading) {
    return (
      <div className="card p-6 flex items-center justify-center space-x-2 text-slate-400">
        <div className="w-5 h-5 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin"></div>
        <span>正在加载状态页配置...</span>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Feedback Alert */}
      {msg && (
        <div className={`p-4 rounded-xl text-sm flex items-center gap-2 ${msg.type === 'success' ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'}`}>
          {msg.type === 'success' ? <CheckCircle size={18} /> : <WarningCircle size={18} />}
          <span>{msg.text}</span>
        </div>
      )}

      {/* Meta Settings */}
      <div className="card p-6 space-y-4">
        <div className="flex items-center justify-between border-b border-white/5 pb-4">
          <div className="flex items-center gap-2">
            <Sliders size={20} className="text-indigo-400" />
            <h2 className="text-base font-bold text-white">公开状态页基础配置</h2>
          </div>
          <div className="flex items-center gap-3">
            <a
              href="/#/status"
              target="_blank"
              rel="noreferrer"
              className="button button-quiet btn-sm flex items-center gap-1.5"
            >
              <Eye size={14} />
              <span>预览公开状态页</span>
            </a>
            <button
              onClick={handleSaveConfig}
              disabled={saving}
              className="button button-primary btn-sm flex items-center gap-1.5"
            >
              <FloppyDisk size={14} weight="bold" />
              <span>{saving ? '保存中...' : '保存全局设置'}</span>
            </button>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-semibold text-slate-400 mb-1">页面大标题</label>
            <input
              type="text"
              value={config?.title || ''}
              onChange={(e) => setConfig({ ...config, title: e.target.value })}
              className="input w-full"
              placeholder="例如: ProbeWatch 服务运行状态"
            />
          </div>
          <div>
            <label className="block text-xs font-semibold text-slate-400 mb-1">展示可用率天数</label>
            <select
              value={config?.show_uptime_days || 90}
              onChange={(e) => setConfig({ ...config, show_uptime_days: Number(e.target.value) })}
              className="input w-full"
            >
              <option value={30}>30 天 SLA 历史</option>
              <option value={60}>60 天 SLA 历史</option>
              <option value={90}>90 天 SLA 历史 (标准推荐)</option>
            </select>
          </div>
          <div className="md:col-span-2">
            <label className="block text-xs font-semibold text-slate-400 mb-1">页面副标题与描述</label>
            <input
              type="text"
              value={config?.description || ''}
              onChange={(e) => setConfig({ ...config, description: e.target.value })}
              className="input w-full"
              placeholder="简要介绍监控范围与 SLA 目标"
            />
          </div>
          <div className="md:col-span-2">
            <label className="block text-xs font-semibold text-slate-400 mb-1">
              置顶全局通告横幅 (选填，留空则不展示)
            </label>
            <input
              type="text"
              value={config?.announcement || ''}
              onChange={(e) => setConfig({ ...config, announcement: e.target.value })}
              className="input w-full"
              placeholder="例如: 预计将于 9月28日 02:00 进行核心网络链路扩容维护，部分节点可能偶发丢包。"
            />
          </div>
        </div>
      </div>

      {/* Component Topology */}
      <div className="card p-6 space-y-4">
        <div className="flex items-center justify-between border-b border-white/5 pb-4">
          <div className="flex items-center gap-2">
            <Broadcast size={20} className="text-indigo-400" />
            <div>
              <h2 className="text-base font-bold text-white">被监测服务组件与节点拓扑</h2>
              <p className="text-xs text-slate-400 mt-0.5">将集群中的物理/云节点或虚拟业务逻辑映射到状态页中</p>
            </div>
          </div>
          <button
            onClick={() => setShowAddCompModal(true)}
            className="button button-quiet btn-sm flex items-center gap-1.5"
          >
            <Plus size={14} weight="bold" />
            <span>添加监控组件</span>
          </button>
        </div>

        {(!config?.components || config.components.length === 0) ? (
          <div className="p-8 text-center text-sm text-slate-500 bg-white/[0.01] rounded-xl border border-dashed border-white/10">
            暂未添加监控组件。点击右上角“添加监控组件”映射集群节点。
          </div>
        ) : (
          <div className="divide-y divide-white/5 border border-white/5 rounded-xl overflow-hidden">
            {config.components.map((comp) => {
              const matchedNode = nodes.find((n) => n.id === comp.node_id)
              return (
                <div key={comp.id} className="p-3 sm:p-4 flex items-center justify-between gap-4 hover:bg-white/[0.02]">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="font-semibold text-white text-sm">{comp.name}</span>
                      <span className="text-[11px] px-2 py-0.5 rounded bg-indigo-500/15 text-indigo-400 font-mono">
                        {comp.group}
                      </span>
                      {comp.show_latency && (
                        <span className="text-[11px] text-slate-400 bg-white/5 px-2 py-0.5 rounded">
                          展示延迟
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-slate-400 flex items-center gap-3">
                      <span>{comp.description || '无描述'}</span>
                      {matchedNode ? (
                        <span className="text-emerald-400">已绑定节点: {matchedNode.name}</span>
                      ) : comp.node_id ? (
                        <span className="text-amber-400">绑定节点 ID: {comp.node_id} (未找到)</span>
                      ) : (
                        <span className="text-slate-500">抽象业务组件 (未绑定独立探针)</span>
                      )}
                    </div>
                  </div>
                  <button
                    onClick={() => handleRemoveComponent(comp.id)}
                    className="p-1.5 rounded-lg text-slate-400 hover:text-rose-400 hover:bg-rose-500/10 transition-colors"
                    title="移除组件"
                  >
                    <Trash size={16} />
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Incidents & Maintenance Management */}
      <div className="card p-6 space-y-4">
        <div className="flex items-center justify-between border-b border-white/5 pb-4">
          <div className="flex items-center gap-2">
            <WarningCircle size={20} className="text-amber-400" />
            <div>
              <h2 className="text-base font-bold text-white">服务异常事件与计划维护管理</h2>
              <p className="text-xs text-slate-400 mt-0.5">面向公众实时通告故障排查进展、原因分析或未来的计划维护窗口</p>
            </div>
          </div>
          <button
            onClick={() => setShowCreateModal(true)}
            className="button button-primary btn-sm flex items-center gap-1.5"
          >
            <Plus size={14} weight="bold" />
            <span>发布新事件 / 维护通告</span>
          </button>
        </div>

        {incidents.length === 0 ? (
          <div className="p-8 text-center text-sm text-slate-500 bg-white/[0.01] rounded-xl border border-dashed border-white/10">
            当前无活跃或历史事件记录。
          </div>
        ) : (
          <div className="divide-y divide-white/5 border border-white/5 rounded-xl overflow-hidden">
            {incidents.map((inc) => (
              <div key={inc.id} className="p-4 space-y-3 hover:bg-white/[0.02]">
                <div className="flex items-center justify-between gap-4">
                  <div className="flex items-center gap-2.5">
                    <span className={`px-2 py-0.5 rounded text-xs font-bold uppercase font-mono ${inc.status === 'resolved' ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30' : inc.is_maintenance ? 'bg-sky-500/20 text-sky-400 border border-sky-500/30' : 'bg-amber-500/20 text-amber-400 border border-amber-500/30'}`}>
                      {inc.status === 'resolved' ? '已解决' : inc.is_maintenance ? '计划维护' : inc.status === 'investigating' ? '调查中' : inc.status === 'identified' ? '已定位' : '观察中'}
                    </span>
                    <span className="font-semibold text-white text-sm">{inc.title}</span>
                    <span className="text-xs text-slate-500 font-mono">({inc.impact})</span>
                  </div>

                  <div className="flex items-center gap-2">
                    {inc.status !== 'resolved' && (
                      <button
                        onClick={() => {
                          setSelectedIncident(inc)
                          setUpdateStatus(inc.status === 'investigating' ? 'identified' : inc.status === 'identified' ? 'monitoring' : 'resolved')
                        }}
                        className="button button-quiet btn-xs flex items-center gap-1 text-indigo-400 hover:text-indigo-300"
                      >
                        <ChatCircleText size={14} />
                        <span>追加进展</span>
                      </button>
                    )}
                    <button
                      onClick={() => handleDeleteIncident(inc.id)}
                      className="p-1.5 rounded-lg text-slate-400 hover:text-rose-400 hover:bg-rose-500/10 transition-colors"
                      title="彻底删除"
                    >
                      <Trash size={15} />
                    </button>
                  </div>
                </div>

                {inc.updates && inc.updates.length > 0 && (
                  <div className="pl-4 border-l-2 border-slate-700/60 ml-2 space-y-1.5 text-xs text-slate-300">
                    {inc.updates.map((u, idx) => (
                      <div key={u.id || idx}>
                        <span className="text-slate-500 font-mono">[{formatTimestamp(u.created_at)}]</span>{' '}
                        <strong className="text-indigo-300 uppercase">{u.status}:</strong> {u.message}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Add Component Modal */}
      {showAddCompModal && (
        <div className="modal-backdrop">
          <div className="modal-card max-w-md w-full p-6 space-y-4">
            <h3 className="text-base font-bold text-white flex items-center gap-2">
              <Plus size={18} className="text-indigo-400" />
              <span>添加监控组件</span>
            </h3>

            <div className="space-y-3">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">组件名称 *</label>
                <input
                  type="text"
                  value={newComp.name}
                  onChange={(e) => setNewComp({ ...newComp, name: e.target.value })}
                  placeholder="例如: API 主控路由 / 香港高防节点"
                  className="input w-full"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">组件分组 *</label>
                <input
                  type="text"
                  value={newComp.group}
                  onChange={(e) => setNewComp({ ...newComp, group: e.target.value })}
                  placeholder="例如: 核心服务 / 边缘加速 / 数据库"
                  className="input w-full"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">关联探针节点 (选填)</label>
                <select
                  value={newComp.node_id}
                  onChange={(e) => setNewComp({ ...newComp, node_id: e.target.value })}
                  className="input w-full"
                >
                  <option value="">不绑定独立节点 (作为纯逻辑业务组件)</option>
                  {nodes.map((n) => (
                    <option key={n.id} value={n.id}>
                      {n.name} ({n.id.slice(0, 8)}...)
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">简要描述</label>
                <input
                  type="text"
                  value={newComp.description}
                  onChange={(e) => setNewComp({ ...newComp, description: e.target.value })}
                  placeholder="例如: 提供公网 HTTPS 接入与跨区路由"
                  className="input w-full"
                />
              </div>

              <div className="flex items-center gap-2 pt-2">
                <input
                  type="checkbox"
                  id="show_latency"
                  checked={newComp.show_latency}
                  onChange={(e) => setNewComp({ ...newComp, show_latency: e.target.checked })}
                  className="rounded border-slate-700 text-indigo-600 focus:ring-0"
                />
                <label htmlFor="show_latency" className="text-xs text-slate-300">
                  在状态页上展示该节点的实时网络延迟
                </label>
              </div>
            </div>

            <div className="flex justify-end gap-3 pt-3 border-t border-white/5">
              <button
                type="button"
                onClick={() => setShowAddCompModal(false)}
                className="button button-quiet btn-sm"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleAddComponent}
                className="button button-primary btn-sm"
              >
                确认添加
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Create Incident Modal */}
      {showCreateModal && (
        <div className="modal-backdrop">
          <div className="modal-card max-w-lg w-full p-6 space-y-4">
            <h3 className="text-base font-bold text-white flex items-center gap-2">
              <WarningCircle size={18} className="text-amber-400" />
              <span>发布新事件 / 计划维护通告</span>
            </h3>

            <form onSubmit={handleCreateIncident} className="space-y-3">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">通告类型</label>
                <div className="flex gap-4">
                  <label className="flex items-center gap-1.5 text-xs text-slate-300 cursor-pointer">
                    <input
                      type="radio"
                      name="is_maint"
                      checked={!createForm.is_maintenance}
                      onChange={() => setCreateForm({ ...createForm, is_maintenance: false })}
                    />
                    <span>服务故障 / 突发异常事件</span>
                  </label>
                  <label className="flex items-center gap-1.5 text-xs text-slate-300 cursor-pointer">
                    <input
                      type="radio"
                      name="is_maint"
                      checked={createForm.is_maintenance}
                      onChange={() => setCreateForm({ ...createForm, is_maintenance: true })}
                    />
                    <span>计划维护窗口</span>
                  </label>
                </div>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">事件标题 *</label>
                <input
                  type="text"
                  required
                  value={createForm.title}
                  onChange={(e) => setCreateForm({ ...createForm, title: e.target.value })}
                  placeholder="例如: 香港节点网络链路抖动调查中"
                  className="input w-full"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">当前阶段</label>
                  <select
                    value={createForm.status}
                    onChange={(e) => setCreateForm({ ...createForm, status: e.target.value })}
                    className="input w-full"
                  >
                    <option value="investigating">调查中 (Investigating)</option>
                    <option value="identified">已确认原因 (Identified)</option>
                    <option value="monitoring">监控观察中 (Monitoring)</option>
                    <option value="resolved">已完全恢复 (Resolved)</option>
                  </select>
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">影响级别</label>
                  <select
                    value={createForm.impact}
                    onChange={(e) => setCreateForm({ ...createForm, impact: e.target.value })}
                    className="input w-full"
                  >
                    <option value="minor">轻微影响 (Minor)</option>
                    <option value="major">主要服务中断 (Major)</option>
                    <option value="critical">严重集群瘫痪 (Critical)</option>
                    <option value="none">无服务感知 (None)</option>
                  </select>
                </div>
              </div>

              {createForm.is_maintenance && (
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs font-semibold text-slate-400 mb-1">计划开始时间</label>
                    <input
                      type="datetime-local"
                      value={createForm.scheduled_start_at}
                      onChange={(e) => setCreateForm({ ...createForm, scheduled_start_at: e.target.value })}
                      className="input w-full"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-semibold text-slate-400 mb-1">预计结束时间</label>
                    <input
                      type="datetime-local"
                      value={createForm.scheduled_end_at}
                      onChange={(e) => setCreateForm({ ...createForm, scheduled_end_at: e.target.value })}
                      className="input w-full"
                    />
                  </div>
                </div>
              )}

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">初次通告进展内容 *</label>
                <textarea
                  rows={3}
                  required
                  value={createForm.message}
                  onChange={(e) => setCreateForm({ ...createForm, message: e.target.value })}
                  placeholder="详细说明排查现象、受影响范围及当前已采取的处理措施..."
                  className="input w-full resize-none"
                />
              </div>

              <div className="flex justify-end gap-3 pt-3 border-t border-white/5">
                <button
                  type="button"
                  onClick={() => setShowCreateModal(false)}
                  className="button button-quiet btn-sm"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={createLoading}
                  className="button button-primary btn-sm"
                >
                  {createLoading ? '发布中...' : '立即发布通告'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Append Update Modal */}
      {selectedIncident && (
        <div className="modal-backdrop">
          <div className="modal-card max-w-md w-full p-6 space-y-4">
            <h3 className="text-base font-bold text-white flex items-center gap-2">
              <ChatCircleText size={18} className="text-indigo-400" />
              <span>追加事件进展更新</span>
            </h3>

            <p className="text-xs text-slate-400">
              事件: <strong className="text-white">{selectedIncident.title}</strong>
            </p>

            <form onSubmit={handleAddUpdate} className="space-y-3">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">更新最新阶段</label>
                <select
                  value={updateStatus}
                  onChange={(e) => setUpdateStatus(e.target.value)}
                  className="input w-full"
                >
                  <option value="investigating">仍在调查中 (Investigating)</option>
                  <option value="identified">已确认原因 (Identified)</option>
                  <option value="monitoring">恢复观察中 (Monitoring)</option>
                  <option value="resolved">彻底解决闭环 (Resolved)</option>
                </select>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">进展说明内容 *</label>
                <textarea
                  rows={3}
                  required
                  value={updateMessage}
                  onChange={(e) => setUpdateMessage(e.target.value)}
                  placeholder="例如: 故障链路已切换至备用线路，流量和延迟已恢复正常水平。"
                  className="input w-full resize-none"
                />
              </div>

              <div className="flex justify-end gap-3 pt-3 border-t border-white/5">
                <button
                  type="button"
                  onClick={() => setSelectedIncident(null)}
                  className="button button-quiet btn-sm"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={updateLoading}
                  className="button button-primary btn-sm"
                >
                  {updateLoading ? '提交中...' : '提交进展更新'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
