import { useCallback, useEffect, useState } from 'react'
import { Check, CheckCircle, CircleNotch, Clock, Globe, Plus, ShieldCheck, Sparkle, Timer, Trash, WarningCircle, WifiHigh } from '@phosphor-icons/react'
import { formatTimeOfDay, numeric, safeArray, safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'
import { fetchCsrfToken } from '../lib/api.js'

const PRESET_NETWORK_TARGETS = [
  { id: 'tcp-baidu', name: '百度搜索入口 (TCP:80)', kind: 'tcp', host: 'www.baidu.com', port: 80 },
  { id: 'https-github', name: 'GitHub 官方状态 (HTTPS)', kind: 'https', host: 'www.githubstatus.com', port: 443, path: '/' },
  { id: 'dns-alidns', name: '公共 DNS (UDP:53)', kind: 'dns', host: '198.51.100.53', port: 53, dns_type: 'A' },
  { id: 'tcp-cf', name: 'Anycast DNS (TCP:443)', kind: 'tcp', host: '198.51.100.1', port: 443 },
]

/**
 * Catmull-Rom 转三次贝塞尔平滑曲线 (哪吒探针 2.0 极细线条专用)
 */
function generateNezhaSpline(pts, bottomY = 90) {
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

export function NetworkLatencyLines({ targets = [], resultsByTargetId = new Map(), history = [], selectedNodeName = '' }) {
  const [hoveredIdx, setHoveredIdx] = useState(null)
  const isTimeMode = Array.isArray(history) && history.length > 0
  if (!isTimeMode && !targets.length) return null

  const baselineY = 90
  const topY = 16
  const plotHeight = baselineY - topY

  let items = []
  let validLatencies = []
  let slot = 0
  let maxLat = 20
  let headroomLat = 20

  if (isTimeMode) {
    const sortedHistory = [...history].sort(
      (a, b) => new Date(a.checked_at || 0) - new Date(b.checked_at || 0)
    )
    validLatencies = sortedHistory
      .map((h) => {
        const res = h.result || {}
        return numeric(res.latency_ms ?? h.latency_ms ?? h.latency)
      })
      .filter((l) => l !== null && l >= 0)
    const rawMax = validLatencies.length ? Math.max(...validLatencies) : 20
    maxLat = Math.max(rawMax, 10)
    headroomLat = maxLat * 1.15
    const count = sortedHistory.length
    slot = 100 / Math.max(count, 1)

    items = sortedHistory.map((h, i) => {
      const res = h.result || {}
      const lat = numeric(res.latency_ms ?? h.latency_ms ?? h.latency)
      const hasLat = lat !== null && lat >= 0
      const effectiveLat = hasLat ? lat : 0
      const x = i * slot + slot / 2
      const y = baselineY - Math.min((effectiveLat / headroomLat) * plotHeight, plotHeight)
      const isOk = res.status === 'success' || (res.status_code && res.status_code < 400)
      const matched = targets.find((t) => t.id === h.target_id || t.name === h.target_id)
      const targetName = matched?.name || h.target_id || 'TCP 探测目标'
      const targetKind = (matched?.kind || 'TCP').toUpperCase()

      return {
        x,
        y,
        hasLat,
        latency: lat,
        isOk,
        targetName,
        targetKind,
        time: h.checked_at,
        error: res.error,
      }
    })
  } else {
    // 降级兼容：按目标列表排布
    const targetItems = targets.map((t) => {
      const report = resultsByTargetId.get(t.id)
      const res = report?.result || {}
      const isOk = res.status === 'success' || (res.status_code && res.status_code < 400)
      const lat = numeric(res.latency_ms)
      return {
        target: t,
        targetName: t.name,
        targetKind: (t.kind || 'TCP').toUpperCase(),
        report,
        latency: lat,
        isOk,
        statusCode: res.status_code,
        error: res.error,
        time: report?.checked_at,
      }
    })
    validLatencies = targetItems.map((i) => i.latency).filter((l) => l !== null && l >= 0)
    const rawMax = validLatencies.length ? Math.max(...validLatencies) : 20
    maxLat = Math.max(rawMax, 10)
    headroomLat = maxLat * 1.15
    const count = targetItems.length
    slot = 100 / Math.max(count, 1)

    items = targetItems.map((it, i) => {
      const x = i * slot + slot / 2
      const lat = it.latency !== null && it.latency >= 0 ? it.latency : 0
      const y = baselineY - Math.min((lat / headroomLat) * plotHeight, plotHeight)
      return { ...it, x, y, hasLat: it.latency !== null && it.latency >= 0 }
    })
  }

  const spline = generateNezhaSpline(
    items.map((p) => ({ x: p.x, y: p.y })),
    baselineY
  )

  const active = hoveredIdx !== null && items[hoveredIdx] ? items[hoveredIdx] : null

  return (
    <div className="network-latency-wrap nezha-traffic-wrap" onMouseLeave={() => setHoveredIdx(null)}>
      {/* 哪吒 2.0 简约单行悬浮指示栏 */}
      <div className={`traffic-hover-banner nezha-hover-banner ${active ? 'active' : ''}`}>
        {active ? (
          <div className="hover-badge-content">
            <span className="hover-stat mono" style={{ fontWeight: 600 }}>{active.targetName}</span>
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat text-mint mono">
              协议: <b>{active.targetKind}</b>
            </span>
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat text-blue mono">
              延迟: <b>{active.latency !== null ? `${active.latency} ms` : '—'}</b>
            </span>
            <span className="hover-sep" aria-hidden="true" />
            <span className={`hover-stat mono ${active.isOk ? 'text-mint' : 'text-rose'}`}>
              状态: <b>{active.isOk ? '连通正常' : active.error || '连接超时'}</b>
            </span>
            {active.time && (
              <>
                <span className="hover-sep" aria-hidden="true" />
                <span className="hover-stat text-amber mono">
                  采样时间: <b>{formatTimeOfDay(active.time)}</b>
                </span>
              </>
            )}
            {selectedNodeName && (
              <>
                <span className="hover-sep" aria-hidden="true" />
                <span className="hover-stat muted mono">源: {selectedNodeName}</span>
              </>
            )}
            <span className="hover-sep" aria-hidden="true" />
            <span className="hover-stat muted mono">{isTimeMode ? '历史时序采样' : '基准 30s'}</span>
          </div>
        ) : (
          <div className="hover-badge-hint mono">
            <Clock size={13} className="text-mint inline-icon" />
            <span>
              {isTimeMode
                ? '哪吒 2.0 TCP 延迟时序走势 · 鼠标滑过节点查看对应时间点的检测延迟与状态'
                : '哪吒 2.0 网络延迟探针流线 · 鼠标滑过节点查看实时连通性与时延'}
            </span>
          </div>
        )}
      </div>

      <div className="checks-svg-canvas nezha-svg-canvas" style={{ height: '90px' }}>
        <svg
          className="checks-line-svg nezha-spline-svg"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
          role="img"
          aria-label="实时网络延迟细线条走势图"
        >
          <defs>
            <linearGradient id="nezha-net-lat-grad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#10b981" stopOpacity="0.09" />
              <stop offset="100%" stopColor="#10b981" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 参考水平极细虚线 */}
          <line className="nezha-grid-line" x1="0" y1={topY} x2="100" y2={topY} />
          <line className="nezha-grid-line" x1="0" y1="53" x2="100" y2="53" />
          <line className="nezha-grid-line" x1="0" y1={baselineY} x2="100" y2={baselineY} />

          {/* 渐变微透明面积 */}
          {validLatencies.length > 0 && (
            <path className="nezha-area-fill" d={spline.area} fill="url(#nezha-net-lat-grad)" />
          )}

          {/* 哪吒 2.0 极细延迟线条 (0.95px) */}
          {validLatencies.length > 0 && (
            <path
              className="nezha-line-stroke"
              d={spline.line}
              fill="none"
              stroke="#10b981"
              strokeWidth="0.95"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          )}

          {/* 交互交叉线 */}
          {items.map((p, index) => {
            const isHovered = hoveredIdx === index

            return (
              <g key={`net-pt-${index}`}>
                {isHovered && (
                  <line
                    className="nezha-crosshair"
                    x1={p.x}
                    y1={topY - 3}
                    x2={p.x}
                    y2={baselineY}
                  />
                )}
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

        {/* 交互真圆高亮指示点（HTML 像素渲染，彻底杜绝 SVG 非等比拉伸导致圆点被压扁拉长） */}
        {hoveredIdx !== null && items[hoveredIdx] && (
          <div className="nezha-chart-dots" aria-hidden="true">
            <div
              className={`nezha-indicator-dot ${
                !items[hoveredIdx].isOk
                  ? 'dot-rose'
                  : items[hoveredIdx].latency !== null && items[hoveredIdx].latency < 50
                  ? 'dot-mint'
                  : 'dot-blue'
              }`}
              style={{ left: `${items[hoveredIdx].x}%`, top: `${items[hoveredIdx].y}%` }}
            />
          </div>
        )}

        {/* Y 轴刻度标注（标准 HTML 浮层，彻底解决 SVG 非等比拉伸导致数字变形变大） */}
        {maxLat > 0 && (
          <div className="nezha-y-axis-labels mono" aria-hidden="true">
            <span style={{ top: `${topY}%` }}>{Math.round(maxLat)} ms</span>
            <span style={{ top: '53%' }}>{Math.round(maxLat / 2)} ms</span>
            <span style={{ top: `${baselineY}%` }}>0 ms</span>
          </div>
        )}
      </div>

      {/* X 轴目标/时间指示 */}
      {isTimeMode ? (
        <div className="checks-time-axis nezha-time-axis mono" aria-hidden="true">
          <span>{formatTimeOfDay(items[0]?.time)}</span>
          {items.length > 2 && (
            <span>{formatTimeOfDay(items[Math.floor(items.length / 2)]?.time)}</span>
          )}
          <span>{formatTimeOfDay(items[items.length - 1]?.time)}</span>
        </div>
      ) : (
        <div className="checks-time-axis nezha-time-axis mono">
          {items.map((it, idx) => (
            <span
              key={`x-net-${idx}`}
              className={`axis-target-name ${hoveredIdx === idx ? 'axis-active' : ''}`}
              style={{ width: `${slot}%`, textAlign: 'center' }}
            >
              {it.targetName}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

export function NetworkMonitorView({ nodes = [] }) {
  const [selectedNodeUuid, setSelectedNodeUuid] = useState(nodes[0]?.uuid || nodes[0]?.id || '')
  const [results, setResults] = useState([])
  const [history, setHistory] = useState([])
  const [targets, setTargets] = useState([])
  const [loading, setLoading] = useState(false)
  const [activeKindFilter, setActiveKindFilter] = useState('all') // 'all' | 'tcp' | 'http' | 'dns'
  const [showAddForm, setShowAddForm] = useState(false)
  const [newTarget, setNewTarget] = useState({
    id: '',
    name: '',
    kind: 'tcp',
    host: '',
    port: '80',
    path: '/',
    intervalSeconds: '30',
    timeoutMs: '3000',
  })
  const [formError, setFormError] = useState('')
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  // Fetch targets and node results
  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      // 1. Fetch all targets
      const tRes = await fetch('/api/targets', { credentials: 'same-origin' })
      if (tRes.ok) {
        const tJson = await tRes.json()
        if (Array.isArray(tJson)) setTargets(tJson)
      }

      // 2. Fetch network results and history from selected node
      if (selectedNodeUuid) {
        const [nRes, hRes] = await Promise.all([
          fetch(`/api/nodes/${encodeURIComponent(selectedNodeUuid)}/network`, {
            credentials: 'same-origin',
          }),
          fetch(`/api/nodes/${encodeURIComponent(selectedNodeUuid)}/network/history?limit=60`, {
            credentials: 'same-origin',
          }),
        ])
        if (nRes.ok) {
          const nJson = await nRes.json()
          if (Array.isArray(nJson)) setResults(nJson)
        }
        if (hRes.ok) {
          const hJson = await hRes.json()
          if (Array.isArray(hJson)) setHistory(hJson)
        }
      }
    } catch {
      // Ignored
    } finally {
      setLoading(false)
    }
  }, [selectedNodeUuid])

  useEffect(() => {
    loadData()
    const timer = setInterval(loadData, 30000)
    return () => clearInterval(timer)
  }, [loadData, refreshTrigger])

  // Handle adding preset
  const handleAddPreset = async (preset) => {
    try {
      const csrfToken = await fetchCsrfToken()
      await fetch('/api/targets', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          id: preset.id,
          name: preset.name,
          kind: preset.kind,
          host: preset.host,
          port: preset.port,
          path: preset.path || '',
          dns_type: preset.dns_type || '',
          interval_seconds: 30,
          timeout_ms: 3000,
          enabled: true,
        }),
      })
      setRefreshTrigger((v) => v + 1)
    } catch {}
  }

  // Handle create custom target
  const handleCreate = async (e) => {
    e.preventDefault()
    if (!newTarget.name.trim() || !newTarget.host.trim()) {
      setFormError('请填写目标名称和主机')
      return
    }
    const targetId = newTarget.id.trim() || `${newTarget.kind}-${newTarget.host.trim().replace(/[^a-zA-Z0-9]/g, '-')}`
    setFormError('')
    try {
      const csrfToken = await fetchCsrfToken()
      const res = await fetch('/api/targets', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          id: targetId,
          name: newTarget.name.trim(),
          kind: newTarget.kind,
          host: newTarget.host.trim(),
          port: Number(newTarget.port) || (newTarget.kind === 'https' ? 443 : 80),
          path: newTarget.path || '',
          interval_seconds: Number(newTarget.intervalSeconds) || 30,
          timeout_ms: Number(newTarget.timeoutMs) || 3000,
          enabled: true,
        }),
      })
      if (!res.ok) {
        const err = await res.json().catch(() => ({}))
        throw new Error(err.error || '创建目标失败')
      }
      setShowAddForm(false)
      setNewTarget({ id: '', name: '', kind: 'tcp', host: '', port: '80', path: '/', intervalSeconds: '30', timeoutMs: '3000' })
      setRefreshTrigger((v) => v + 1)
    } catch (err) {
      setFormError(err.message || '创建失败')
    }
  }

  // Handle delete target
  const handleDelete = async (targetId) => {
    if (!window.confirm(`确定删除探测目标 [${targetId}] 吗？`)) return
    try {
      const csrfToken = await fetchCsrfToken()
      await fetch(`/api/targets/${encodeURIComponent(targetId)}`, {
        method: 'DELETE',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
      })
      setRefreshTrigger((v) => v + 1)
    } catch {}
  }

  // Filter network targets
  const networkTargets = targets.filter((t) => ['tcp', 'http', 'https', 'dns'].includes(t.kind))
  const filteredTargets = networkTargets.filter((t) => {
    if (activeKindFilter === 'all') return true
    if (activeKindFilter === 'http') return t.kind === 'http' || t.kind === 'https'
    return t.kind === activeKindFilter
  })

  // Match target results
  const resultsByTargetId = new Map()
  results.forEach((r) => {
    const id = r.target_id || r.id
    if (id) resultsByTargetId.set(id, r)
  })

  const successCount = results.filter((r) => r?.result?.status === 'success' || (r?.result?.status_code && r.result.status_code < 400)).length
  const totalReports = results.length
  const passRate = totalReports > 0 ? Math.round((successCount / totalReports) * 100) : 100
  const selectedNode = nodes.find((n) => (n.uuid || n.id) === selectedNodeUuid) || nodes[0]

  return (
    <div className="network-module-view">
      {/* 顶部统计大盘 */}
      <div className="network-stats-row">
        <div className="network-stat-card">
          <div className="stat-label-row">
            <WifiHigh size={18} className="text-mint" />
            <span>网络检测目标项</span>
          </div>
          <div className="stat-big-value text-mint mono">{networkTargets.length} <small style={{ fontSize: '13px' }}>项</small></div>
          <div className="stat-sub-text">支持 TCP / HTTP / HTTPS / DNS 探针</div>
        </div>

        <div className="network-stat-card">
          <div className="stat-label-row">
            <ShieldCheck size={18} className="text-blue" />
            <span>当前探测连通率</span>
          </div>
          <div className="stat-big-value text-blue mono">{passRate}%</div>
          <div className="stat-sub-text">{totalReports ? `已成功完成 ${successCount} / ${totalReports} 项` : '等待探针执行初次上报'}</div>
        </div>

        <div className="network-stat-card">
          <div className="stat-label-row">
            <Globe size={18} className="text-amber" />
            <span>探测执行节点</span>
          </div>
          <div className="stat-big-value text-amber mono">{nodes.length} <small style={{ fontSize: '13px' }}>台</small></div>
          <div className="stat-sub-text">分布式出站探测并独立汇报</div>
        </div>
      </div>

      {/* 控制操作栏 */}
      <div className="network-controls-bar panel">
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

          <div className="network-filter-chips">
            {['all', 'tcp', 'http', 'dns'].map((kind) => (
              <button
                key={kind}
                type="button"
                className={`view-toggle-btn ${activeKindFilter === kind ? 'active' : ''}`}
                onClick={() => setActiveKindFilter(kind)}
              >
                {{ all: '全部目标', tcp: 'TCP 端口', http: 'HTTP / HTTPS', dns: 'DNS 解析' }[kind]}
              </button>
            ))}
          </div>

          <div className="mtr-header-actions">
            <button
              type="button"
              className="button button-quiet btn-sm"
              onClick={() => setShowAddForm((v) => !v)}
            >
              <Plus size={14} /> 新增检测目标
            </button>
            <button
              type="button"
              className="button button-primary btn-sm"
              onClick={loadData}
              disabled={loading}
            >
              {loading ? <CircleNotch size={14} className="spin" /> : <WifiHigh size={14} />}
              <span>{loading ? '同步中…' : '刷新检测'}</span>
            </button>
          </div>
        </div>

        {/* 预设探测目标一键添加 */}
        <div className="mtr-presets-strip">
          <span className="preset-label"><Sparkle size={13} className="text-mint" /> 快捷添加常用网络服务探测:</span>
          <div className="preset-tags">
            {PRESET_NETWORK_TARGETS.map((preset) => (
              <button
                key={preset.id}
                type="button"
                className="preset-btn"
                onClick={() => handleAddPreset(preset)}
                title={`添加针对 ${preset.host} 的检测`}
              >
                + {preset.name}
              </button>
            ))}
          </div>
        </div>

        {/* 新建目标表单 */}
        {showAddForm && (
          <form className="mtr-add-form" onSubmit={handleCreate}>
            <div className="form-grid-mtr" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
              <div className="field">
                <label className="field-label">协议类型</label>
                <select
                  className="field-select"
                  value={newTarget.kind}
                  onChange={(e) => setNewTarget({ ...newTarget, kind: e.target.value, port: e.target.value === 'https' ? '443' : e.target.value === 'dns' ? '53' : '80' })}
                >
                  <option value="tcp">TCP 端口连通性</option>
                  <option value="http">HTTP 状态码检测</option>
                  <option value="https">HTTPS 安全握手检测</option>
                  <option value="dns">DNS 域名解析</option>
                </select>
              </div>
              <div className="field">
                <label className="field-label">目标名称</label>
                <input
                  className="field-input"
                  placeholder="如: 百度公共入口"
                  value={newTarget.name}
                  onChange={(e) => setNewTarget({ ...newTarget, name: e.target.value })}
                />
              </div>
              <div className="field">
                <label className="field-label">目标主机 / IP</label>
                <input
                  className="field-input mono"
                  placeholder="如: www.baidu.com"
                  value={newTarget.host}
                  onChange={(e) => setNewTarget({ ...newTarget, host: e.target.value })}
                />
              </div>
              <div className="field">
                <label className="field-label">端口</label>
                <input
                  type="number"
                  className="field-input mono"
                  value={newTarget.port}
                  onChange={(e) => setNewTarget({ ...newTarget, port: e.target.value })}
                />
              </div>
              {(newTarget.kind === 'http' || newTarget.kind === 'https') && (
                <div className="field">
                  <label className="field-label">请求路径</label>
                  <input
                    className="field-input mono"
                    placeholder="/"
                    value={newTarget.path}
                    onChange={(e) => setNewTarget({ ...newTarget, path: e.target.value })}
                  />
                </div>
              )}
            </div>
            {formError && <div className="form-error-msg">{formError}</div>}
            <div className="form-actions-mtr">
              <button type="button" className="button button-quiet btn-sm" onClick={() => setShowAddForm(false)}>取消</button>
              <button type="submit" className="button button-primary btn-sm">保存并启动探测</button>
            </div>
          </form>
        )}
      </div>

      {/* 实时网络延迟探针细流线走势图 (哪吒 2.0 风格) */}
      <NetworkLatencyLines
        targets={filteredTargets}
        resultsByTargetId={resultsByTargetId}
        history={history}
        selectedNodeName={selectedNode?.name}
      />

      {/* 网络检测目标列表 */}
      <div className="panel network-table-panel">
        <div className="panel-header">
          <div>
            <h2>网络检测项清单 ({filteredTargets.length})</h2>
            <p>由受控探针节点定期发起出站检测，保障三网服务与外部 API 可用性</p>
          </div>
        </div>

        {filteredTargets.length > 0 ? (
          <div className="table-scroll">
            <table className="node-table network-table">
              <thead>
                <tr>
                  <th>检测目标</th>
                  <th>协议类型</th>
                  <th>主机与端口</th>
                  <th>最新响应状态</th>
                  <th>响应延迟</th>
                  <th>最近检测时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {filteredTargets.map((target) => {
                  const report = resultsByTargetId.get(target.id)
                  const res = report?.result || {}
                  const isOk = res.status === 'success' || (res.status_code && res.status_code < 400)
                  const latency = numeric(res.latency_ms)

                  return (
                    <tr key={target.id}>
                      <td>
                        <div className="inline-flex items-center gap-2">
                          <span className="network-target-dot" />
                          <strong className="text-1">{target.name}</strong>
                        </div>
                      </td>
                      <td>
                        <span className={`kind-badge kind-${target.kind} mono`}>
                          {target.kind.toUpperCase()}
                        </span>
                      </td>
                      <td className="mono text-muted">
                        {target.host}{target.port ? `:${target.port}` : ''}{target.path || ''}
                      </td>
                      <td>
                        {report ? (
                          <span className={`status-pill ${isOk ? 'status-ok' : 'status-fail'}`}>
                            {isOk ? <CheckCircle size={13} weight="fill" /> : <WarningCircle size={13} weight="fill" />}
                            <span>{res.status_code ? `${res.status_code} OK` : isOk ? '连通正常' : res.error || '连接超时'}</span>
                          </span>
                        ) : (
                          <span className="muted" style={{ fontSize: '11px' }}>等待初次上报</span>
                        )}
                      </td>
                      <td className="mono">
                        {latency !== null ? (
                          <b className={latency < 50 ? 'text-mint' : latency < 150 ? 'text-blue' : 'text-amber'}>
                            {latency} ms
                          </b>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                      <td className="mono muted">
                        {res.checked_at ? formatTimeOfDay(res.checked_at * 1000) : '—'}
                      </td>
                      <td>
                        <button
                          type="button"
                          className="button button-quiet btn-sm text-rose"
                          onClick={() => handleDelete(target.id)}
                          title="删除该探测项"
                        >
                          <Trash size={13} /> 删除
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            title="暂无网络检测目标"
            detail="点击上方“快捷添加常用网络服务探测”或“新增检测目标”即可开启监控。"
          />
        )}
      </div>
    </div>
  )
}
