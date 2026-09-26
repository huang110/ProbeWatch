import { useState, useEffect, useMemo, useRef } from 'react'
import {
  Scroll,
  MagnifyingGlass,
  Funnel,
  X,
  FileCode,
  ShieldCheck,
  WarningCircle,
  Clock,
  ArrowsClockwise,
  CheckCircle,
  Terminal,
  Copy,
  Check,
  HardDrives,
  Desktop,
  Warning,
  CircleNotch,
} from '@phosphor-icons/react'
import {
  fetchEventsOverview,
  fetchFleetEvents,
  fetchNodeEvents,
  queryNodeLogs,
  fetchAuditLogs,
} from '../lib/api.js'
import { formatAlertTime } from '../lib/format.js'

export function LogStreamingView({ nodes = [] }) {
  const [activeTab, setActiveTab] = useState('events') // 'events' | 'stream' | 'audit'

  // Overview stats
  const [overview, setOverview] = useState(null)
  const [loadingOverview, setLoadingOverview] = useState(false)

  // Events tab state
  const [events, setEvents] = useState([])
  const [loadingEvents, setLoadingEvents] = useState(false)
  const [selectedNodeId, setSelectedNodeId] = useState('')
  const [selectedCategory, setSelectedCategory] = useState('')
  const [selectedSeverity, setSelectedSeverity] = useState('')
  const [eventSearch, setEventSearch] = useState('')
  const [expandedEventId, setExpandedEventId] = useState(null)

  // Log Stream tab state
  const [streamNodeUuid, setStreamNodeUuid] = useState(nodes[0]?.uuid || '')
  const [streamUnit, setStreamUnit] = useState('probewatch.service')
  const [streamPriority, setStreamPriority] = useState('')
  const [streamGrep, setStreamGrep] = useState('')
  const [streamLines, setStreamLines] = useState(100)
  const [streamSince, setStreamSince] = useState('1h')
  const [streamOutput, setStreamOutput] = useState([])
  const [loadingStream, setLoadingStream] = useState(false)
  const [streamAutoPoll, setStreamAutoPoll] = useState(false)
  const [streamFilter, setStreamFilter] = useState('')
  const [copiedStream, setCopiedStream] = useState(false)

  // Audit logs tab state
  const [auditLogs, setAuditLogs] = useState([])
  const [loadingAudit, setLoadingAudit] = useState(false)
  const [auditSearch, setAuditSearch] = useState('')

  // Copy helper
  const [copiedId, setCopiedId] = useState(null)
  const copyText = (text, id) => {
    if (!text) return
    navigator.clipboard.writeText(text)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  // 1. Fetch overview
  const loadOverview = async () => {
    try {
      setLoadingOverview(true)
      const data = await fetchEventsOverview()
      setOverview(data)
    } catch (e) {
      console.error('Failed to load events overview:', e)
    } finally {
      setLoadingOverview(false)
    }
  }

  // 2. Fetch events
  const loadEvents = async () => {
    try {
      setLoadingEvents(true)
      let data
      if (selectedNodeId) {
        const targetNode = nodes.find((n) => n.id === selectedNodeId || n.uuid === selectedNodeId)
        const uuid = targetNode?.uuid || selectedNodeId
        data = await fetchNodeEvents(uuid, {
          category: selectedCategory,
          severity: selectedSeverity,
          limit: 150,
        })
        setEvents(data.events || [])
      } else {
        data = await fetchFleetEvents({
          category: selectedCategory,
          severity: selectedSeverity,
          limit: 150,
        })
        setEvents(data.events || [])
      }
    } catch (e) {
      console.error('Failed to load events:', e)
    } finally {
      setLoadingEvents(false)
    }
  }

  // 3. Query logs
  const handleQueryLogs = async () => {
    if (!streamNodeUuid) return
    try {
      setLoadingStream(true)
      const res = await queryNodeLogs(streamNodeUuid, {
        unit: streamUnit,
        priority: streamPriority,
        grep: streamGrep,
        lines: streamLines,
        since: streamSince,
      })
      setStreamOutput(res.lines || [])
    } catch (e) {
      setStreamOutput([`[错误] 日志查询失败: ${e.message}`])
    } finally {
      setLoadingStream(false)
    }
  }

  // 4. Fetch audit logs
  const loadAuditLogs = async () => {
    try {
      setLoadingAudit(true)
      const res = await fetchAuditLogs({ limit: 100 })
      setAuditLogs(res.logs || [])
    } catch (e) {
      console.error('Failed to load audit logs:', e)
    } finally {
      setLoadingAudit(false)
    }
  }

  // Initialize
  useEffect(() => {
    loadOverview()
    loadEvents()
    loadAuditLogs()
  }, [])

  // Auto select default node for streaming if available
  useEffect(() => {
    if (!streamNodeUuid && nodes.length > 0) {
      setStreamNodeUuid(nodes[0].uuid)
    }
  }, [nodes])

  // Reload events when filters change
  useEffect(() => {
    loadEvents()
  }, [selectedNodeId, selectedCategory, selectedSeverity])

  // Auto poll for log stream
  useEffect(() => {
    if (!streamAutoPoll || activeTab !== 'stream') return
    const timer = setInterval(() => {
      handleQueryLogs()
    }, 5000)
    return () => clearInterval(timer)
  }, [streamAutoPoll, activeTab, streamNodeUuid, streamUnit, streamPriority, streamGrep, streamLines, streamSince])

  // Filtered events
  const filteredEvents = useMemo(() => {
    if (!eventSearch) return events
    const q = eventSearch.toLowerCase()
    return events.filter(
      (ev) =>
        ev.title?.toLowerCase().includes(q) ||
        ev.message?.toLowerCase().includes(q) ||
        ev.node_name?.toLowerCase().includes(q) ||
        ev.category?.toLowerCase().includes(q)
    )
  }, [events, eventSearch])

  // Filtered log lines in terminal
  const filteredStreamLines = useMemo(() => {
    if (!streamFilter) return streamOutput
    const q = streamFilter.toLowerCase()
    return streamOutput.filter((line) => line.toLowerCase().includes(q))
  }, [streamOutput, streamFilter])

  // Filtered audit logs
  const filteredAuditLogs = useMemo(() => {
    if (!auditSearch) return auditLogs
    const q = auditSearch.toLowerCase()
    return auditLogs.filter(
      (log) =>
        log.action?.toLowerCase().includes(q) ||
        log.actor_name?.toLowerCase().includes(q) ||
        log.resource_type?.toLowerCase().includes(q) ||
        log.detail?.toLowerCase().includes(q)
    )
  }, [auditLogs, auditSearch])

  return (
    <div className="log-streaming-view" style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      {/* Top KPI Cards */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
          gap: '16px',
        }}
      >
        <div className="panel" style={{ padding: '16px 20px', borderRadius: '12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '8px' }}>
            <span style={{ fontSize: '13px', color: 'var(--text-muted)' }}>内核安全告警 (24h)</span>
            <WarningCircle size={20} color="var(--error, #ef4444)" weight="duotone" />
          </div>
          <div style={{ fontSize: '26px', fontWeight: '700', color: 'var(--error, #ef4444)', fontFamily: 'var(--font-mono)' }}>
            {overview ? overview.critical_events_24h : '-'}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            OOM-Killer 与 Kernel Panic
          </div>
        </div>

        <div className="panel" style={{ padding: '16px 20px', borderRadius: '12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '8px' }}>
            <span style={{ fontSize: '13px', color: 'var(--text-muted)' }}>SSH 爆破探测 (24h)</span>
            <ShieldCheck size={20} color="var(--warning, #f59e0b)" weight="duotone" />
          </div>
          <div style={{ fontSize: '26px', fontWeight: '700', color: 'var(--warning, #f59e0b)', fontFamily: 'var(--font-mono)' }}>
            {overview?.category_counts?.ssh_auth || 0}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            非法凭据与未授权暴力尝试
          </div>
        </div>

        <div className="panel" style={{ padding: '16px 20px', borderRadius: '12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '8px' }}>
            <span style={{ fontSize: '13px', color: 'var(--text-muted)' }}>系统服务崩溃 (24h)</span>
            <Warning size={20} color="var(--primary, #3b82f6)" weight="duotone" />
          </div>
          <div style={{ fontSize: '26px', fontWeight: '700', color: 'var(--primary, #3b82f6)', fontFamily: 'var(--font-mono)' }}>
            {overview?.category_counts?.service || 0}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            Systemd 异常退出 Failed 实例
          </div>
        </div>

        <div className="panel" style={{ padding: '16px 20px', borderRadius: '12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '8px' }}>
            <span style={{ fontSize: '13px', color: 'var(--text-muted)' }}>全网事件总计 (24h)</span>
            <Scroll size={20} color="var(--text-main)" weight="duotone" />
          </div>
          <div style={{ fontSize: '26px', fontWeight: '700', color: 'var(--text-main)', fontFamily: 'var(--font-mono)' }}>
            {overview ? overview.total_events_24h : '-'}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
            分布式节点聚合日志流水
          </div>
        </div>
      </div>

      {/* Navigation Tabs */}
      <div style={{ display: 'flex', gap: '8px', borderBottom: '1px solid var(--border-subtle, rgba(255,255,255,0.08))', paddingBottom: '12px' }}>
        <button
          type="button"
          className={`button ${activeTab === 'events' ? 'button-primary' : 'button-quiet'}`}
          onClick={() => setActiveTab('events')}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '8px', padding: '8px 16px', borderRadius: '8px' }}
        >
          <WarningCircle size={18} />
          <span>内核与安全事件审计</span>
        </button>
        <button
          type="button"
          className={`button ${activeTab === 'stream' ? 'button-primary' : 'button-quiet'}`}
          onClick={() => setActiveTab('stream')}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '8px', padding: '8px 16px', borderRadius: '8px' }}
        >
          <Terminal size={18} />
          <span>实时日志流与终端排障</span>
        </button>
        <button
          type="button"
          className={`button ${activeTab === 'audit' ? 'button-primary' : 'button-quiet'}`}
          onClick={() => setActiveTab('audit')}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '8px', padding: '8px 16px', borderRadius: '8px' }}
        >
          <Scroll size={18} />
          <span>控制台操作审计</span>
        </button>
      </div>

      {/* Tab 1: Kernel & Security Events */}
      {activeTab === 'events' && (
        <div className="panel" style={{ padding: '20px', borderRadius: '12px' }}>
          {/* Filter Bar */}
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: '12px',
              alignItems: 'center',
              justifyContent: 'space-between',
              marginBottom: '20px',
            }}
          >
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' }}>
              <select
                className="input"
                value={selectedNodeId}
                onChange={(e) => setSelectedNodeId(e.target.value)}
                style={{ padding: '6px 12px', borderRadius: '6px', fontSize: '13px' }}
              >
                <option value="">全部节点</option>
                {nodes.map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.name}
                  </option>
                ))}
              </select>

              <select
                className="input"
                value={selectedCategory}
                onChange={(e) => setSelectedCategory(e.target.value)}
                style={{ padding: '6px 12px', borderRadius: '6px', fontSize: '13px' }}
              >
                <option value="">全部事件类型</option>
                <option value="oom">OOM Killer 内存耗尽</option>
                <option value="kernel">Kernel Panic / 段错误</option>
                <option value="ssh_auth">SSH 暴力认证失败</option>
                <option value="service">Systemd 服务崩溃</option>
              </select>

              <select
                className="input"
                value={selectedSeverity}
                onChange={(e) => setSelectedSeverity(e.target.value)}
                style={{ padding: '6px 12px', borderRadius: '6px', fontSize: '13px' }}
              >
                <option value="">全部等级</option>
                <option value="critical">严重 (Critical)</option>
                <option value="warning">警告 (Warning)</option>
                <option value="info">常规 (Info)</option>
              </select>

              <div style={{ position: 'relative', width: '220px' }}>
                <MagnifyingGlass
                  size={16}
                  style={{ position: 'absolute', left: '10px', top: '50%', transform: 'translateY(-50%)', color: 'var(--text-muted)' }}
                />
                <input
                  type="text"
                  className="input"
                  placeholder="搜索事件标题或内容..."
                  value={eventSearch}
                  onChange={(e) => setEventSearch(e.target.value)}
                  style={{ paddingLeft: '32px', width: '100%', padding: '6px 12px 6px 32px', borderRadius: '6px', fontSize: '13px' }}
                />
              </div>
            </div>

            <button
              type="button"
              className="button button-quiet"
              onClick={() => {
                loadEvents()
                loadOverview()
              }}
              disabled={loadingEvents}
              style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', padding: '6px 14px', borderRadius: '6px' }}
            >
              <ArrowsClockwise size={16} className={loadingEvents ? 'spin' : ''} />
              <span>刷新</span>
            </button>
          </div>

          {/* Events List */}
          {loadingEvents && events.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '40px', color: 'var(--text-muted)' }}>
              <CircleNotch size={28} className="spin" style={{ margin: '0 auto 12px' }} />
              <p>加载安全与内核事件中...</p>
            </div>
          ) : filteredEvents.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '40px', color: 'var(--text-muted)' }}>
              <CheckCircle size={36} color="var(--success, #10b981)" style={{ margin: '0 auto 12px' }} weight="duotone" />
              <p style={{ fontWeight: '500', color: 'var(--text-main)', fontSize: '15px' }}>暂无系统安全或异常事件</p>
              <p style={{ fontSize: '13px', marginTop: '4px' }}>集群节点内核运行稳定，未检测到 OOM、段错误或未授权 SSH 暴力试探。</p>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
              {filteredEvents.map((ev) => {
                const isExpanded = expandedEventId === ev.id
                const isCrit = ev.severity === 'critical'
                const isWarn = ev.severity === 'warning'
                const badgeColor = isCrit ? '#ef4444' : isWarn ? '#f59e0b' : '#3b82f6'

                return (
                  <div
                    key={ev.id}
                    style={{
                      background: 'var(--bg-subtle, rgba(255,255,255,0.03))',
                      border: `1px solid ${isCrit ? 'rgba(239,68,68,0.3)' : 'var(--border-subtle, rgba(255,255,255,0.08))'}`,
                      borderRadius: '8px',
                      padding: '14px 16px',
                      cursor: 'pointer',
                      transition: 'all 0.15s ease',
                    }}
                    onClick={() => setExpandedEventId(isExpanded ? null : ev.id)}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', flex: 1, minWidth: 0 }}>
                        <span
                          style={{
                            fontSize: '11px',
                            fontWeight: '700',
                            textTransform: 'uppercase',
                            padding: '2px 8px',
                            borderRadius: '4px',
                            background: `${badgeColor}20`,
                            color: badgeColor,
                            border: `1px solid ${badgeColor}40`,
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {ev.severity}
                        </span>

                        <span
                          style={{
                            fontSize: '12px',
                            color: 'var(--text-muted)',
                            background: 'rgba(255,255,255,0.05)',
                            padding: '2px 8px',
                            borderRadius: '4px',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {ev.category === 'oom'
                            ? 'OOM-Killer'
                            : ev.category === 'kernel'
                            ? 'Kernel'
                            : ev.category === 'ssh_auth'
                            ? 'SSH 认证'
                            : ev.category === 'service'
                            ? 'Systemd'
                            : ev.category}
                        </span>

                        <span style={{ fontSize: '13px', fontWeight: '600', color: 'var(--text-main)', whiteSpace: 'nowrap' }}>
                          [{ev.node_name || '节点'}]
                        </span>

                        <span
                          style={{
                            fontSize: '13px',
                            color: isCrit ? 'var(--text-main)' : 'var(--text-main)',
                            fontWeight: isCrit ? '600' : '400',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {ev.title}
                        </span>
                      </div>

                      <div style={{ display: 'flex', alignItems: 'center', gap: '12px', whiteSpace: 'nowrap' }}>
                        <span style={{ fontSize: '12px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
                          {formatAlertTime(ev.occurred_at || ev.RecordedAt)}
                        </span>
                        <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{isExpanded ? '收起 ▲' : '详情 ▼'}</span>
                      </div>
                    </div>

                    {isExpanded && (
                      <div
                        style={{
                          marginTop: '12px',
                          paddingTop: '12px',
                          borderTop: '1px solid rgba(255,255,255,0.06)',
                          fontSize: '13px',
                        }}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
                          <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                            来源: <code>{ev.source || 'syslog'}</code> · 时间戳: <code>{ev.occurred_at}</code>
                          </span>
                          <button
                            type="button"
                            className="button button-quiet"
                            onClick={() => copyText(ev.message || ev.title, ev.id)}
                            style={{ padding: '2px 8px', fontSize: '12px', display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                          >
                            {copiedId === ev.id ? <Check size={14} color="#10b981" /> : <Copy size={14} />}
                            <span>{copiedId === ev.id ? '已复制' : '复制日志'}</span>
                          </button>
                        </div>
                        <pre
                          style={{
                            background: '#0d1117',
                            padding: '12px',
                            borderRadius: '6px',
                            fontSize: '12px',
                            fontFamily: 'var(--font-mono)',
                            color: '#e6edf3',
                            overflowX: 'auto',
                            whiteSpace: 'pre-wrap',
                            wordBreak: 'break-all',
                            margin: 0,
                          }}
                        >
                          {ev.message || ev.title}
                        </pre>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* Tab 2: Real-time Log Stream & Journal */}
      {activeTab === 'stream' && (
        <div className="panel" style={{ padding: '20px', borderRadius: '12px' }}>
          {/* Query Controls */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
              gap: '12px',
              marginBottom: '16px',
            }}
          >
            <div>
              <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                目标边缘节点
              </label>
              <select
                className="input"
                value={streamNodeUuid}
                onChange={(e) => setStreamNodeUuid(e.target.value)}
                style={{ width: '100%', padding: '7px 10px', borderRadius: '6px', fontSize: '13px' }}
              >
                {nodes.map((n) => (
                  <option key={n.uuid} value={n.uuid}>
                    {n.name} ({n.uuid.slice(0, 8)})
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                系统单元 (Systemd Unit)
              </label>
              <input
                type="text"
                className="input"
                placeholder="例如: probewatch.service"
                value={streamUnit}
                onChange={(e) => setStreamUnit(e.target.value)}
                style={{ width: '100%', padding: '7px 10px', borderRadius: '6px', fontSize: '13px' }}
              />
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                优先级 (Priority)
              </label>
              <select
                className="input"
                value={streamPriority}
                onChange={(e) => setStreamPriority(e.target.value)}
                style={{ width: '100%', padding: '7px 10px', borderRadius: '6px', fontSize: '13px' }}
              >
                <option value="">全部日志等级</option>
                <option value="err">仅错误 (err & 严重)</option>
                <option value="warning">警告与错误 (warning, err)</option>
                <option value="info">常规信息 (info)</option>
                <option value="debug">完整调试 (debug)</option>
              </select>
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                内容关键字 (Grep)
              </label>
              <input
                type="text"
                className="input"
                placeholder="关键字或错误短语..."
                value={streamGrep}
                onChange={(e) => setStreamGrep(e.target.value)}
                style={{ width: '100%', padding: '7px 10px', borderRadius: '6px', fontSize: '13px' }}
              />
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                行数与时段
              </label>
              <div style={{ display: 'flex', gap: '8px' }}>
                <select
                  className="input"
                  value={streamLines}
                  onChange={(e) => setStreamLines(Number(e.target.value))}
                  style={{ flex: 1, padding: '7px 10px', borderRadius: '6px', fontSize: '13px' }}
                >
                  <option value={50}>50 行</option>
                  <option value={100}>100 行</option>
                  <option value={200}>200 行</option>
                  <option value={500}>500 行</option>
                </select>

                <select
                  className="input"
                  value={streamSince}
                  onChange={(e) => setStreamSince(e.target.value)}
                  style={{ flex: 1, padding: '7px 10px', borderRadius: '6px', fontSize: '13px' }}
                >
                  <option value="15m">最近 15m</option>
                  <option value="1h">最近 1h</option>
                  <option value="6h">最近 6h</option>
                  <option value="1d">最近 1天</option>
                </select>
              </div>
            </div>
          </div>

          {/* Quick Presets & Action Buttons */}
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              justifyContent: 'space-between',
              alignItems: 'center',
              gap: '12px',
              marginBottom: '16px',
            }}
          >
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', alignItems: 'center' }}>
              <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>快速单元预设:</span>
              {['probewatch.service', 'docker.service', 'sshd.service', 'nginx.service', ''].map((u) => (
                <button
                  key={u || 'all'}
                  type="button"
                  className="button button-quiet"
                  onClick={() => setStreamUnit(u)}
                  style={{
                    padding: '3px 8px',
                    fontSize: '11px',
                    borderRadius: '4px',
                    background: streamUnit === u ? 'rgba(59,130,246,0.2)' : 'rgba(255,255,255,0.04)',
                    color: streamUnit === u ? '#60a5fa' : 'var(--text-muted)',
                  }}
                >
                  {u || '全局日志 (syslog)'}
                </button>
              ))}
            </div>

            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <label
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '6px',
                  fontSize: '13px',
                  color: 'var(--text-muted)',
                  cursor: 'pointer',
                }}
              >
                <input
                  type="checkbox"
                  checked={streamAutoPoll}
                  onChange={(e) => setStreamAutoPoll(e.target.checked)}
                />
                <span>实时轮询 (5s)</span>
              </label>

              <button
                type="button"
                className="button button-primary"
                onClick={handleQueryLogs}
                disabled={loadingStream}
                style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', padding: '6px 16px', borderRadius: '6px' }}
              >
                <Terminal size={16} />
                <span>{loadingStream ? '获取中...' : '拉取日志'}</span>
              </button>
            </div>
          </div>

          {/* Interactive Terminal Log Viewer */}
          <div
            style={{
              background: '#090d16',
              border: '1px solid #1f2937',
              borderRadius: '8px',
              overflow: 'hidden',
              boxShadow: '0 4px 20px rgba(0,0,0,0.4)',
            }}
          >
            {/* Terminal Header */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                padding: '8px 14px',
                background: '#111827',
                borderBottom: '1px solid #1f2937',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', background: '#ef4444', display: 'inline-block' }} />
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', background: '#f59e0b', display: 'inline-block' }} />
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', background: '#10b981', display: 'inline-block' }} />
                <span style={{ fontSize: '12px', color: '#9ca3af', marginLeft: '6px', fontFamily: 'var(--font-mono)' }}>
                  journalctl -u {streamUnit || 'all'} ({filteredStreamLines.length} 行)
                </span>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                <input
                  type="text"
                  placeholder="终端内过滤行..."
                  value={streamFilter}
                  onChange={(e) => setStreamFilter(e.target.value)}
                  style={{
                    background: '#1f2937',
                    border: '1px solid #374151',
                    borderRadius: '4px',
                    color: '#e5e7eb',
                    fontSize: '11px',
                    padding: '3px 8px',
                    outline: 'none',
                    width: '140px',
                  }}
                />

                <button
                  type="button"
                  onClick={() => {
                    const text = filteredStreamLines.join('\n')
                    navigator.clipboard.writeText(text)
                    setCopiedStream(true)
                    setTimeout(() => setCopiedStream(false), 2000)
                  }}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: copiedStream ? '#10b981' : '#9ca3af',
                    cursor: 'pointer',
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: '4px',
                    fontSize: '11px',
                  }}
                >
                  {copiedStream ? <Check size={14} /> : <Copy size={14} />}
                  <span>{copiedStream ? '已复制' : '复制终端'}</span>
                </button>
              </div>
            </div>

            {/* Terminal Body */}
            <div
              style={{
                padding: '12px 16px',
                minHeight: '320px',
                maxHeight: '520px',
                overflowY: 'auto',
                fontFamily: 'var(--font-mono, monospace)',
                fontSize: '12px',
                lineHeight: '1.6',
                color: '#d1d5db',
              }}
            >
              {loadingStream && streamOutput.length === 0 ? (
                <div style={{ color: '#6b7280', padding: '20px 0' }}>
                  <CircleNotch size={16} className="spin" style={{ display: 'inline', marginRight: '6px' }} />
                  连接边缘节点日志流中...
                </div>
              ) : filteredStreamLines.length === 0 ? (
                <div style={{ color: '#6b7280', padding: '20px 0' }}>
                  未检索到匹配的系统日志。请检查服务单元名称或点击上方 [拉取日志]。
                </div>
              ) : (
                filteredStreamLines.map((line, idx) => {
                  const lower = line.toLowerCase()
                  const isErr = lower.includes('error') || lower.includes('failed') || lower.includes('fatal') || lower.includes('panic')
                  const isWarn = lower.includes('warning') || lower.includes('warn')
                  const textColor = isErr ? '#f87171' : isWarn ? '#fbbf24' : '#e5e7eb'

                  return (
                    <div
                      key={idx}
                      style={{
                        display: 'flex',
                        gap: '12px',
                        background: isErr ? 'rgba(239, 68, 68, 0.08)' : 'transparent',
                        padding: '1px 4px',
                        borderRadius: '2px',
                      }}
                    >
                      <span style={{ color: '#4b5563', userSelect: 'none', minWidth: '32px', textAlign: 'right' }}>
                        {idx + 1}
                      </span>
                      <span style={{ color: textColor, whiteSpace: 'pre-wrap', wordBreak: 'break-all', flex: 1 }}>
                        {line}
                      </span>
                    </div>
                  )
                })
              )}
            </div>
          </div>
        </div>
      )}

      {/* Tab 3: Console Audit Logs */}
      {activeTab === 'audit' && (
        <div className="panel" style={{ padding: '20px', borderRadius: '12px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
            <div style={{ position: 'relative', width: '260px' }}>
              <MagnifyingGlass
                size={16}
                style={{ position: 'absolute', left: '10px', top: '50%', transform: 'translateY(-50%)', color: 'var(--text-muted)' }}
              />
              <input
                type="text"
                className="input"
                placeholder="搜索操作者、行为或资源..."
                value={auditSearch}
                onChange={(e) => setAuditSearch(e.target.value)}
                style={{ paddingLeft: '32px', width: '100%', padding: '6px 12px 6px 32px', borderRadius: '6px', fontSize: '13px' }}
              />
            </div>

            <button
              type="button"
              className="button button-quiet"
              onClick={loadAuditLogs}
              disabled={loadingAudit}
              style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', padding: '6px 14px', borderRadius: '6px' }}
            >
              <ArrowsClockwise size={16} className={loadingAudit ? 'spin' : ''} />
              <span>刷新流水</span>
            </button>
          </div>

          {loadingAudit && auditLogs.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '40px', color: 'var(--text-muted)' }}>
              <CircleNotch size={28} className="spin" style={{ margin: '0 auto 12px' }} />
              <p>加载审计日志中...</p>
            </div>
          ) : filteredAuditLogs.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '40px', color: 'var(--text-muted)' }}>
              <p>暂无操作审计记录。</p>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="table" style={{ width: '100%', textAlign: 'left', borderCollapse: 'collapse' }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border-subtle, rgba(255,255,255,0.08))', color: 'var(--text-muted)', fontSize: '12px' }}>
                    <th style={{ padding: '8px 12px' }}>时间</th>
                    <th style={{ padding: '8px 12px' }}>操作者</th>
                    <th style={{ padding: '8px 12px' }}>动作 (Action)</th>
                    <th style={{ padding: '8px 12px' }}>目标资源</th>
                    <th style={{ padding: '8px 12px' }}>状态</th>
                    <th style={{ padding: '8px 12px' }}>详情</th>
                  </tr>
                </thead>
                <tbody style={{ fontSize: '13px' }}>
                  {filteredAuditLogs.map((log, index) => (
                    <tr
                      key={log.id || index}
                      style={{
                        borderBottom: '1px solid var(--border-subtle, rgba(255,255,255,0.04))',
                      }}
                    >
                      <td style={{ padding: '10px 12px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
                        {formatAlertTime(log.created_at)}
                      </td>
                      <td style={{ padding: '10px 12px', fontWeight: '500' }}>
                        {log.actor_name || log.actor_id || 'System'}
                      </td>
                      <td style={{ padding: '10px 12px' }}>
                        <code style={{ background: 'rgba(255,255,255,0.06)', padding: '2px 6px', borderRadius: '4px', fontSize: '12px' }}>
                          {log.action}
                        </code>
                      </td>
                      <td style={{ padding: '10px 12px', color: 'var(--text-muted)' }}>
                        {log.resource_type ? `${log.resource_type}: ${log.resource_id || '-'}` : '-'}
                      </td>
                      <td style={{ padding: '10px 12px' }}>
                        <span
                          style={{
                            fontSize: '11px',
                            fontWeight: '600',
                            padding: '2px 6px',
                            borderRadius: '4px',
                            background: log.status_code < 400 ? 'rgba(16,185,129,0.15)' : 'rgba(239,68,68,0.15)',
                            color: log.status_code < 400 ? '#10b981' : '#ef4444',
                          }}
                        >
                          {log.status_code || 200}
                        </span>
                      </td>
                      <td style={{ padding: '10px 12px', color: 'var(--text-muted)', maxWidth: '280px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {log.detail || '-'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default LogStreamingView
