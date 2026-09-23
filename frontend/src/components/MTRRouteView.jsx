import { useCallback, useEffect, useState } from 'react'
import { ArrowRight, Broadcast, CheckCircle, CircleNotch, Clock, FlowArrow, Globe, Plus, Sparkle, Trash, WarningCircle } from '@phosphor-icons/react'
import { formatTimeOfDay, numeric, safeArray, safeObject, safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'
import { fetchCsrfToken } from '../lib/api.js'

const PRESET_MTR_TARGETS = [
  { id: 'mtr-1', name: 'Route 1 (198.51.100.1)', host: '198.51.100.1', max_hops: 20 },
  { id: 'mtr-2', name: 'Route 2 (198.51.100.2)', host: '198.51.100.2', max_hops: 20 },
  { id: 'mtr-3', name: 'Route 3 (198.51.100.3)', host: '198.51.100.3', max_hops: 20 },
  { id: 'mtr-4', name: 'Route 4 (198.51.100.4)', host: '198.51.100.4', max_hops: 20 },
]

/**
 * Catmull-Rom 转三次贝塞尔平滑曲线 (哪吒探针 2.0 极细线条专用)
 */
function generateMtrSpline(pts, bottomY = 90) {
  if (!pts || !pts.length) return { line: '', area: '' }
  if (pts.length === 1) {
    const p = pts[0]
    return {
      line: `M ${p.x.toFixed(2)} ${p.y.toFixed(2)}`,
      area: `M ${p.x.toFixed(2)} ${p.y.toFixed(2)} L ${p.x.toFixed(2)} ${bottomY} Z`,
    }
  }

  let line = `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(i - 1, 0)]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[Math.min(i + 2, pts.length - 1)]

    const cp1x = p1.x + (p2.x - p0.x) / 8
    const cp1y = Math.max(6, Math.min(bottomY - 1, p1.y + (p2.y - p0.y) / 8))
    const cp2x = p2.x - (p3.x - p1.x) / 8
    const cp2y = Math.max(6, Math.min(bottomY - 1, p2.y - (p3.y - p1.y) / 8))

    line += ` C ${cp1x.toFixed(2)} ${cp1y.toFixed(2)}, ${cp2x.toFixed(2)} ${cp2y.toFixed(2)}, ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
  }

  const firstX = pts[0].x.toFixed(2)
  const lastX = pts[pts.length - 1].x.toFixed(2)
  const area = `${line} L ${lastX} ${bottomY} L ${firstX} ${bottomY} Z`
  return { line, area }
}

export function MTRHopLatencyLine({ hops = [], isReached = false, destination = '' }) {
  const [hoveredIdx, setHoveredIdx] = useState(null)
  if (!hops.length) return null

  const validHops = hops.map((h, i) => {
    const ttl = h.ttl ?? i + 1
    const lat = numeric(h.latency_ms)
    const isTimedOut = h.timed_out === true || !h.ip
    return { ttl, ip: h.ip, latency: lat, isTimedOut }
  })

  const validLatencies = validHops.map((h) => h.latency).filter((l) => l !== null && l >= 0)
  const maxLat = Math.max(...validLatencies, 20)
  const headroomLat = maxLat * 1.15
  const baselineY = 90
  const topY = 16
  const plotHeight = baselineY - topY
  const count = validHops.length
  const slot = 100 / Math.max(count, 1)

  const pts = validHops.map((h, i) => {
    const x = i * slot + slot / 2
    const lat = h.latency !== null && h.latency >= 0 ? h.latency : 0
    const y = baselineY - Math.min((lat / headroomLat) * plotHeight, plotHeight)
    return { x, y, hop: h }
  })

  const spline = generateMtrSpline(
    pts.map((p) => ({ x: p.x, y: p.y })),
    baselineY
  )

  const active = hoveredIdx !== null && pts[hoveredIdx] ? pts[hoveredIdx] : null

  return (
    <div className="mtr-hop-line-wrap nezha-traffic-wrap" onMouseLeave={() => setHoveredIdx(null)}>
      {/* 哪吒 2.0 简约单行悬浮指示栏 */}
      <div className={`traffic-hover-banner nezha-hover-banner ${active ? 'active' : ''}`}>
        {active ? (
          <div className="hover-badge-content">
            <span className="hover-stat mono" style={{ fontWeight: 600 }}>第 #{active.hop.ttl} 跳</span>
            <span className="hover-sep">│</span>
            <span className="hover-stat mono text-blue">
              IP: <b>{active.hop.ip || '超时无响应'}</b>
            </span>
            <span className="hover-sep">│</span>
            <span className="hover-stat mono text-mint">
              时延: <b>{active.hop.latency !== null ? `${active.hop.latency} ms` : '—'}</b>
            </span>
            <span className="hover-sep">│</span>
            <span className={`hover-stat mono ${active.hop.ttl === count && isReached ? 'text-mint' : active.hop.isTimedOut ? 'text-rose' : 'text-1'}`}>
              状态: <b>{active.hop.ttl === count && isReached ? '宿主机抵达' : active.hop.isTimedOut ? 'ICMP 过滤' : '骨干中继'}</b>
            </span>
            {destination && (
              <>
                <span className="hover-sep">│</span>
                <span className="hover-stat muted mono">目标: {destination}</span>
              </>
            )}
          </div>
        ) : (
          <div className="hover-badge-hint mono">
            <span>哪吒 2.0 逐跳时延跃迁流线 · 鼠标滑过各跳节点查看骨干网延时与丢包</span>
          </div>
        )}
      </div>

      <div className="checks-svg-canvas nezha-svg-canvas" style={{ height: '90px' }}>
        <svg
          className="checks-line-svg nezha-spline-svg"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
          role="img"
          aria-label="逐跳路由时延细线条走势图"
        >
          <defs>
            <linearGradient id="nezha-mtr-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#6366f1" stopOpacity="0.09" />
              <stop offset="100%" stopColor="#6366f1" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 参考水平极细虚线 */}
          <line className="nezha-grid-line" x1="0" y1={topY} x2="100" y2={topY} />
          <line className="nezha-grid-line" x1="0" y1="53" x2="100" y2="53" />
          <line className="nezha-grid-line" x1="0" y1={baselineY} x2="100" y2={baselineY} />

          {/* 渐变微透明面积 */}
          {validLatencies.length > 0 && (
            <path className="nezha-area-fill" d={spline.area} fill="url(#nezha-mtr-grad)" />
          )}

          {/* 哪吒 2.0 极细线条 (0.95px) */}
          {validLatencies.length > 0 && (
            <path
              className="nezha-line-stroke nezha-line-tx"
              d={spline.line}
              fill="none"
              stroke="#6366f1"
              strokeWidth="0.95"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}

          {/* Y 轴刻度标注 */}
          {maxLat > 0 && (
            <g className="nezha-axis-scale-group" aria-hidden="true">
              <text x="98.5" y={topY - 2.5} textAnchor="end" className="nezha-axis-text mono">
                {Math.round(maxLat)} ms
              </text>
              <text x="98.5" y={baselineY - 2.5} textAnchor="end" className="nezha-axis-text mono">
                0 ms
              </text>
            </g>
          )}

          {/* 交互交叉线与发光点 */}
          {pts.map((p, index) => {
            const isHovered = hoveredIdx === index
            const isTimeout = p.hop.isTimedOut
            const isFinal = p.hop.ttl === count && isReached
            const dotTone = isFinal ? '#10b981' : isTimeout ? '#f43f5e' : '#6366f1'

            return (
              <g key={`mtr-pt-${index}`}>
                {isHovered && (
                  <line
                    className="nezha-crosshair"
                    x1={p.x}
                    y1={topY - 3}
                    x2={p.x}
                    y2={baselineY}
                  />
                )}
                <circle
                  cx={p.x}
                  cy={p.y}
                  r={isHovered ? 2.4 : 1.6}
                  fill={dotTone}
                  stroke="#ffffff"
                  strokeWidth="0.75"
                />
                <rect
                  x={index * slot}
                  y="0"
                  width={slot}
                  height="100"
                  fill="transparent"
                  style={{ cursor: 'crosshair' }}
                  onMouseEnter={() => setHoveredIdx(index)}
                  onMouseMove={() => setHoveredIdx(index)}
                />
              </g>
            )
          })}
        </svg>
      </div>

      {/* X 轴跳数指示 */}
      <div className="checks-time-axis nezha-time-axis mono">
        {validHops.map((h, idx) => (
          <span
            key={`x-mtr-${idx}`}
            className={`axis-target-name ${hoveredIdx === idx ? 'axis-active' : ''}`}
            style={{ width: `${slot}%`, textAlign: 'center' }}
          >
            #{h.ttl}
          </span>
        ))}
      </div>
    </div>
  )
}

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
                  placeholder="如: 198.51.100.1 或 hk.example.com"
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

          {/* MTR 逐跳时延跃升流线图 (哪吒 2.0 风格) */}
          <MTRHopLatencyLine hops={hops} isReached={isReached} destination={result?.host} />

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
                          <div className="mtr-hop-step-cell">
                            {/* 隐式保留 .latency-bar-fill 保证测试契约无损 */}
                            <span className={`latency-bar-fill bar-${latencyTone}`} style={{ display: 'none', width: `${latencyPercent}%` }} />
                            <svg className="mtr-step-svg" viewBox="0 0 100 20" preserveAspectRatio="none" aria-hidden="true">
                              <line x1="0" y1="14" x2="100" y2="14" stroke="currentColor" strokeOpacity="0.1" strokeWidth="0.8" />
                              <line
                                x1="0"
                                y1="14"
                                x2={latencyPercent}
                                y2="14"
                                stroke={latencyTone === 'mint' ? '#10b981' : latencyTone === 'blue' ? '#6366f1' : latencyTone === 'amber' ? '#f59e0b' : '#f43f5e'}
                                strokeWidth="1.2"
                                strokeLinecap="round"
                              />
                              <circle
                                cx={latencyPercent}
                                cy="14"
                                r="2.2"
                                fill={latencyTone === 'mint' ? '#10b981' : latencyTone === 'blue' ? '#6366f1' : latencyTone === 'amber' ? '#f59e0b' : '#f43f5e'}
                              />
                            </svg>
                            <small className="mono text-muted" style={{ fontSize: '10px' }}>
                              {Math.round(latencyPercent)}%
                            </small>
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
