import { useCallback, useEffect, useState } from 'react'
import { ArrowRight, Broadcast, CheckCircle, CircleNotch, Clock, FlowArrow, Globe, Plus, Sparkle, Trash, WarningCircle } from '@phosphor-icons/react'
import { formatTimeOfDay, numeric, safeArray, safeObject, safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'
import { fetchCsrfToken } from '../lib/api.js'

const PRESET_MTR_TARGETS = [
  { id: 'mtr-cloudflare', name: 'Cloudflare DNS (1.1.1.1)', host: '1.1.1.1', max_hops: 20 },
  { id: 'mtr-google', name: 'Google DNS (8.8.8.8)', host: '8.8.8.8', max_hops: 20 },
  { id: 'mtr-alidns', name: '阿里公共DNS (223.5.5.5)', host: '223.5.5.5', max_hops: 20 },
  { id: 'mtr-114dns', name: '114 国内常用DNS (114.114.114.114)', host: '114.114.114.114', max_hops: 20 },
]

export function MTRRouteView({ nodes = [] }) {
  const [selectedNodeUuid, setSelectedNodeUuid] = useState(nodes[0]?.uuid || nodes[0]?.id || '')
  const [mtrData, setMtrData] = useState([])
  const [configuredTargets, setConfiguredTargets] = useState([])
  const [loading, setLoading] = useState(false)
  const [selectedTargetId, setSelectedTargetId] = useState(null)
  const [addingPreset, setAddingPreset] = useState(false)
  const [showAddForm, setShowAddForm] = useState(false)
  const [newTarget, setNewTarget] = useState({ id: '', name: '', host: '', maxHops: '20' })
  const [formError, setFormError] = useState('')
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  // Keep selected node updated
  useEffect(() => {
    if (!selectedNodeUuid && nodes.length > 0) {
      setSelectedNodeUuid(nodes[0].uuid || nodes[0].id)
    }
  }, [nodes, selectedNodeUuid])

  // Fetch MTR results and configured targets
  const fetchMtr = useCallback(async () => {
    if (!selectedNodeUuid) return
    setLoading(true)
    try {
      fetch('/api/targets', { credentials: 'same-origin' })
        .then((res) => (res.ok ? res.json() : []))
        .then((list) => {
          if (Array.isArray(list)) {
            setConfiguredTargets(list.filter((t) => t.kind === 'mtr'))
          }
        })
        .catch(() => {})

      const res = await fetch(`/api/nodes/${encodeURIComponent(selectedNodeUuid)}/mtr`, {
        credentials: 'same-origin',
      })
      if (!res.ok) throw new Error('mtr-failed')
      const json = await res.json()
      if (Array.isArray(json)) {
        setMtrData(json)
        if (json.length > 0 && !selectedTargetId) {
          setSelectedTargetId(json[0].target_id || json[0].id || json[0]?.result?.host)
        }
      }
    } catch {
      setMtrData([])
    } finally {
      setLoading(false)
    }
  }, [selectedNodeUuid, selectedTargetId])

  useEffect(() => {
    fetchMtr()
    const timer = setInterval(fetchMtr, 30000)
    return () => clearInterval(timer)
  }, [fetchMtr, refreshTrigger])

  const handleDeleteTarget = async (tId) => {
    if (!tId) return
    if (!window.confirm(`确定删除 MTR 探测目标 [${tId}] 吗？`)) return
    try {
      const csrfToken = await fetchCsrfToken()
      await fetch(`/api/targets/${encodeURIComponent(tId)}`, {
        method: 'DELETE',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
      })
      setSelectedTargetId(null)
      setRefreshTrigger((v) => v + 1)
    } catch {}
  }

  const allMtrTabs = (() => {
    const list = [...mtrData]
    configuredTargets.forEach((t) => {
      const exists = list.some((item) => (item.target_id || item.id) === t.id)
      if (!exists) {
        list.push({ target_id: t.id, id: t.id, isPending: true, result: { host: t.host, reached: false, hops: [] } })
      }
    })
    return list
  })()

  const selectedReport = allMtrTabs.find(
    (item) => (item.target_id || item.id || item?.result?.host) === selectedTargetId
  ) || allMtrTabs[0] || null

  const handleAddPreset = async (preset) => {
    setAddingPreset(true)
    try {
      const csrfToken = await fetchCsrfToken()
      await fetch('/api/targets', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          id: preset.id,
          name: preset.name,
          kind: 'mtr',
          host: preset.host,
          max_hops: preset.max_hops,
          interval_seconds: 30,
          timeout_ms: 3000,
          enabled: true,
        }),
      })
      setRefreshTrigger((prev) => prev + 1)
    } catch {
      // Ignored
    } finally {
      setAddingPreset(false)
    }
  }

  const handleCreateCustom = async (e) => {
    e.preventDefault()
    if (!newTarget.name.trim() || !newTarget.host.trim()) {
      setFormError('请填写目标名称与主机地址')
      return
    }
    const targetId = newTarget.id.trim() || `mtr-${newTarget.host.trim().replace(/[^a-zA-Z0-9]/g, '-')}`
    setLoading(true)
    setFormError('')
    try {
      const csrfToken = await fetchCsrfToken()
      const res = await fetch('/api/targets', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          id: targetId,
          name: newTarget.name.trim(),
          kind: 'mtr',
          host: newTarget.host.trim(),
          max_hops: Number(newTarget.maxHops) || 20,
          interval_seconds: 30,
          timeout_ms: 3000,
          enabled: true,
        }),
      })
      if (!res.ok) {
        const errJson = await res.json().catch(() => ({}))
        throw new Error(errJson.error || '创建探测目标失败')
      }
      setShowAddForm(false)
      setNewTarget({ id: '', name: '', host: '', maxHops: '20' })
      setRefreshTrigger((prev) => prev + 1)
    } catch (err) {
      setFormError(err.message || '创建失败，请检查参数')
    } finally {
      setLoading(false)
    }
  }

  const result = selectedReport?.result || {}
  const hops = safeArray(result.hops)
  const isReached = result.reached === true

  return (
    <div className="mtr-module-view">
      {/* 头部选择栏 */}
      <div className="mtr-header-bar panel">
        <div className="mtr-controls-row">
          <div className="mtr-select-group">
            <span className="control-label"><Globe size={15} /> 探测源节点:</span>
            <select
              className="field-select mtr-node-select"
              value={selectedNodeUuid}
              onChange={(e) => setSelectedNodeUuid(e.target.value)}
            >
              {nodes.map((node) => (
                <option key={node.uuid || node.id} value={node.uuid || node.id}>
                  {node.flag || '🌐'} {node.name} ({node.region || '公网'})
                </option>
              ))}
            </select>
          </div>

          <div className="mtr-header-actions">
            <button
              type="button"
              className="button button-quiet btn-sm"
              onClick={() => setShowAddForm((v) => !v)}
            >
              <Plus size={14} /> 新建 MTR 目标
            </button>
            <button
              type="button"
              className="button button-primary btn-sm"
              onClick={fetchMtr}
              disabled={loading}
            >
              {loading ? <CircleNotch size={14} className="spin" /> : <Broadcast size={14} />}
              <span>{loading ? '正在追踪…' : '刷新路由'}</span>
            </button>
          </div>
        </div>

        {/* 常用预设快捷添加胶囊 */}
        <div className="mtr-presets-strip">
          <span className="preset-label"><Sparkle size={13} className="text-amber" /> 快捷添加常用骨干路由探测:</span>
          <div className="preset-tags">
            {PRESET_MTR_TARGETS.map((preset) => (
              <button
                key={preset.id}
                type="button"
                className="preset-btn"
                disabled={addingPreset}
                onClick={() => handleAddPreset(preset)}
                title={`自动添加对 ${preset.host} 的 MTR 追踪`}
              >
                + {preset.name}
              </button>
            ))}
          </div>
        </div>

        {/* 新建表单折叠卡片 */}
        {showAddForm && (
          <form className="mtr-add-form" onSubmit={handleCreateCustom}>
            <div className="form-grid-mtr">
              <div className="field">
                <label className="field-label">目标名称</label>
                <input
                  className="field-input"
                  placeholder="如: 香港 CN2 路由"
                  value={newTarget.name}
                  onChange={(e) => setNewTarget({ ...newTarget, name: e.target.value })}
                />
              </div>
              <div className="field">
                <label className="field-label">目标 IP / 域名</label>
                <input
                  className="field-input mono"
                  placeholder="如: 1.1.1.1 或 hk.example.com"
                  value={newTarget.host}
                  onChange={(e) => setNewTarget({ ...newTarget, host: e.target.value })}
                />
              </div>
              <div className="field">
                <label className="field-label">最大跳数 (1-30)</label>
                <input
                  type="number"
                  className="field-input mono"
                  value={newTarget.maxHops}
                  onChange={(e) => setNewTarget({ ...newTarget, maxHops: e.target.value })}
                />
              </div>
            </div>
            {formError && <div className="form-error-msg">{formError}</div>}
            <div className="form-actions-mtr">
              <button type="button" className="button button-quiet btn-sm" onClick={() => setShowAddForm(false)}>取消</button>
              <button type="submit" className="button button-primary btn-sm">提交并启动追踪</button>
            </div>
          </form>
        )}
      </div>

      {/* MTR 目标选择 Tab 栏 */}
      {allMtrTabs.length > 0 && (
        <div className="mtr-targets-tabs">
          {allMtrTabs.map((item) => {
            const tId = item.target_id || item.id || item?.result?.host
            const isCur = tId === selectedTargetId
            return (
              <button
                key={tId}
                type="button"
                className={`mtr-tab-chip ${isCur ? 'active' : ''}`}
                onClick={() => setSelectedTargetId(tId)}
              >
                <FlowArrow size={15} />
                <span className="target-chip-name">{item?.result?.host || tId}</span>
                <span className={`target-chip-status ${item?.result?.reached ? 'status-reached' : ''}`}>
                  {item?.isPending ? '等待探测' : item?.result?.reached ? '已抵达' : '追踪中'}
                </span>
              </button>
            )
          })}
        </div>
      )}

      {/* MTR 追踪详情卡片 */}
      {selectedReport && result ? (
        <div className="panel mtr-result-panel">
          <div className="mtr-meta-header">
            <div className="meta-left">
              <div className="destination-title">
                <strong>{result.host}</strong>
                {result.destination_ip && (
                  <span className="destination-ip mono">({result.destination_ip})</span>
                )}
                <span className={`reached-pill ${isReached ? 'reached' : 'unreached'}`}>
                  {isReached ? <CheckCircle size={14} weight="fill" /> : <WarningCircle size={14} weight="fill" />}
                  <span>{isReached ? '已成功抵达目标' : '路由未完全到达'}</span>
                </span>
              </div>
              <div className="meta-sub">
                <span>跳数上限: <b>{hops.length} 跳</b></span>
                {result.fingerprint && (
                  <span className="fingerprint-tag mono">指纹: {result.fingerprint}</span>
                )}
              </div>
            </div>

            <div className="meta-right" style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
              <span className="update-time">
                <Clock size={14} /> 检测时间: <b>{result.checked_at ? formatTimeOfDay(result.checked_at * 1000) : '刚刚'}</b>
              </span>
              {(selectedReport?.target_id || selectedReport?.id || selectedTargetId) && (
                <button
                  type="button"
                  className="button button-quiet btn-sm text-rose"
                  onClick={() => handleDeleteTarget(selectedReport?.target_id || selectedReport?.id || selectedTargetId)}
                  title="删除该 MTR 探测目标"
                >
                  <Trash size={13} /> 删除目标
                </button>
              )}
            </div>
          </div>

          {/* 逐跳路由表格 */}
          <div className="table-scroll">
            <table className="node-table mtr-hops-table">
              <thead>
                <tr>
                  <th style={{ width: '60px' }}>跳数</th>
                  <th>路由节点 IP</th>
                  <th>单跳延迟</th>
                  <th style={{ width: '220px' }}>链路时延阶梯</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {hops.map((hop, idx) => {
                  const ttl = hop.ttl ?? idx + 1
                  const isTimedOut = hop.timed_out === true || !hop.ip
                  const latency = numeric(hop.latency_ms)
                  const latencyPercent = latency !== null ? Math.min(100, Math.max(5, (latency / 200) * 100)) : 0
                  const latencyTone = latency === null ? 'muted' : latency < 30 ? 'mint' : latency < 80 ? 'blue' : latency < 150 ? 'amber' : 'rose'

                  return (
                    <tr key={`${ttl}-${hop.ip || 'timeout'}`} className={isTimedOut ? 'hop-timeout-row' : ''}>
                      <td className="mono text-center">
                        <span className="hop-ttl-badge">#{ttl}</span>
                      </td>
                      <td>
                        {isTimedOut ? (
                          <span className="hop-timeout-text mono">* * * (节点超时无响应)</span>
                        ) : (
                          <div className="hop-ip-group">
                            <span className="mono hop-ip">{hop.ip}</span>
                          </div>
                        )}
                      </td>
                      <td className="mono">
                        {latency !== null ? (
                          <b className={`text-${latencyTone}`}>{latency} ms</b>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                      <td>
                        {latency !== null ? (
                          <div className="latency-bar-container">
                            <div
                              className={`latency-bar-fill bar-${latencyTone}`}
                              style={{ width: `${latencyPercent}%` }}
                            />
                          </div>
                        ) : (
                          <span className="muted" style={{ fontSize: '11px' }}>超时无回显</span>
                        )}
                      </td>
                      <td>
                        {ttl === hops.length && isReached ? (
                          <span className="status-pill-final">
                            <CheckCircle size={13} weight="fill" /> 目标宿主机
                          </span>
                        ) : isTimedOut ? (
                          <span className="status-pill-timeout">ICMP 过滤</span>
                        ) : (
                          <span className="status-pill-relay">中继路由</span>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        <div className="panel placeholder-panel">
          <EmptyState
            title="暂无 MTR 路由追踪数据"
            detail="请点击上方“快捷添加常用骨干路由探测”或新建一个 MTR 目标，Agent 将在 30 秒内完成路由追踪并上报。"
          />
        </div>
      )}
    </div>
  )
}
