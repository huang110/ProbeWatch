import { useEffect, useState, useMemo } from 'react'
import {
  CheckCircle,
  WarningCircle,
  XCircle,
  Wrench,
  Clock,
  ArrowClockwise,
  SignIn,
  Globe,
  Broadcast,
  ShieldCheck,
  CaretDown,
  CaretRight,
  Info,
} from '@phosphor-icons/react'
import { fetchPublicStatusPage, fetchPublicIncidents } from '../lib/api.js'
import { ThemeToggle } from './ThemeToggle.jsx'

export function StatusPageView({ onOpenLogin, theme, onThemeChange }) {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [lastRefreshed, setLastRefreshed] = useState(new Date())
  const [countdown, setCountdown] = useState(30)
  const [hoveredDay, setHoveredDay] = useState(null)
  const [pastIncidents, setPastIncidents] = useState([])
  const [showPastArchive, setShowPastArchive] = useState(false)
  const [expandedPastIncidents, setExpandedPastIncidents] = useState({})

  const loadData = async (isManual = false) => {
    if (isManual) setLoading(true)
    try {
      const res = await fetchPublicStatusPage()
      setData(res)
      setLastRefreshed(new Date())
      setCountdown(30)
      setError(null)
    } catch (err) {
      console.error('Failed to load status page:', err)
      setError('无法获取服务状态数据，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          loadData()
          return 30
        }
        return prev - 1
      })
    }, 1000)
    return () => clearInterval(timer)
  }, [])

  // Load past resolved incidents
  const loadPastIncidents = async () => {
    try {
      const res = await fetchPublicIncidents({ limit: 15, offset: 0 })
      setPastIncidents(res.incidents || [])
    } catch (err) {
      console.error('Failed to load past incidents:', err)
    }
  }

  const togglePastArchive = () => {
    if (!showPastArchive && pastIncidents.length === 0) {
      loadPastIncidents()
    }
    setShowPastArchive((prev) => !prev)
  }

  const togglePastItem = (id) => {
    setExpandedPastIncidents((prev) => ({ ...prev, [id]: !prev[id] }))
  }

  // Group components by their configured group name
  const groupedComponents = useMemo(() => {
    if (!data?.components) return {}
    const groups = {}
    for (const item of data.components) {
      const groupName = item.component.group || '通用服务'
      if (!groups[groupName]) {
        groups[groupName] = []
      }
      groups[groupName].push(item)
    }
    return groups
  }, [data])

  const overallStatus = data?.overallStatus || 'operational'
  const activeIncidents = data?.activeIncidents || []
  const maintenance = data?.maintenance || []

  const getStatusHero = () => {
    switch (overallStatus) {
      case 'major_outage':
        return {
          icon: <XCircle size={32} weight="fill" className="text-rose-500 animate-pulse" />,
          title: data?.overallMessage || '系统发生重大服务故障',
          subtitle: '部分核心服务或节点处于中断状态，运维团队正在全力抢修。',
          bgClass: 'bg-rose-500/10 border-rose-500/30 text-rose-400',
          dotClass: 'bg-rose-500',
        }
      case 'partial_outage':
        return {
          icon: <WarningCircle size={32} weight="fill" className="text-orange-500 animate-pulse" />,
          title: data?.overallMessage || '部分服务发生中断',
          subtitle: '检测到个别服务节点异常，正在定位排查。',
          bgClass: 'bg-orange-500/10 border-orange-500/30 text-orange-400',
          dotClass: 'bg-orange-500',
        }
      case 'degraded':
        return {
          icon: <WarningCircle size={32} weight="fill" className="text-amber-500 animate-pulse" />,
          title: data?.overallMessage || '部分服务性能有所降级',
          subtitle: '部分服务响应延迟偏高，核心功能仍可正常使用。',
          bgClass: 'bg-amber-500/10 border-amber-500/30 text-amber-400',
          dotClass: 'bg-amber-500',
        }
      case 'under_maintenance':
        return {
          icon: <Wrench size={32} weight="fill" className="text-sky-500 animate-pulse" />,
          title: data?.overallMessage || '系统正在进行计划维护',
          subtitle: '系统处于预定升级或维护窗口期，稍后即可恢复。',
          bgClass: 'bg-sky-500/10 border-sky-500/30 text-sky-400',
          dotClass: 'bg-sky-500',
        }
      default:
        return {
          icon: <CheckCircle size={32} weight="fill" className="text-emerald-500" />,
          title: data?.overallMessage || '所有系统均正常运行',
          subtitle: '所有被监控节点、接口与关键网络链路运行平稳。',
          bgClass: 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400',
          dotClass: 'bg-emerald-500',
        }
    }
  }

  const hero = getStatusHero()

  const formatTimestamp = (ts) => {
    if (!ts) return ''
    const d = new Date(ts * 1000)
    return d.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
  }

  const getDayBarClass = (status) => {
    switch (status) {
      case 'operational':
        return 'bg-emerald-500 hover:bg-emerald-400'
      case 'degraded':
        return 'bg-amber-500 hover:bg-amber-400'
      case 'outage':
        return 'bg-rose-500 hover:bg-rose-400'
      default:
        return 'bg-slate-700/40 hover:bg-slate-600/50'
    }
  }

  const getComponentStatusBadge = (status) => {
    switch (status) {
      case 'operational':
        return <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-emerald-500/15 text-emerald-400 border border-emerald-500/20"><span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>正常运行</span>
      case 'degraded':
        return <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-amber-500/15 text-amber-400 border border-amber-500/20"><span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-ping"></span>性能降级</span>
      case 'outage':
        return <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-rose-500/15 text-rose-400 border border-rose-500/20"><span className="w-1.5 h-1.5 rounded-full bg-rose-500 animate-ping"></span>服务中断</span>
      default:
        return <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-slate-500/15 text-slate-400 border border-slate-500/20"><span className="w-1.5 h-1.5 rounded-full bg-slate-400"></span>无数据</span>
    }
  }

  return (
    <div className="status-page min-h-screen bg-[#090a0f] text-slate-100 flex flex-col font-sans selection:bg-indigo-500/30 selection:text-indigo-200">
      {/* Top Navbar */}
      <header className="border-b border-white/5 bg-[#0f111a]/80 backdrop-blur-md sticky top-0 z-50">
        <div className="max-w-6xl mx-auto px-4 h-16 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-xl bg-gradient-to-tr from-indigo-600 to-violet-500 flex items-center justify-center shadow-lg shadow-indigo-500/20">
              <Broadcast size={20} weight="bold" className="text-white" />
            </div>
            <div>
              <div className="text-base font-bold text-white flex items-center gap-2">
                <span>{data?.config?.title || 'ProbeWatch 服务状态'}</span>
                <span className="text-[11px] font-mono px-1.5 py-0.5 rounded bg-indigo-500/20 text-indigo-400 font-semibold border border-indigo-500/30">STATUS</span>
              </div>
              <p className="text-xs text-slate-400 hidden sm:block">实时监控与 90 天 SLA 可用率引擎</p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="hidden sm:flex items-center gap-1.5 text-xs text-slate-400 bg-white/5 px-2.5 py-1 rounded-lg border border-white/5">
              <Clock size={13} className="text-slate-400" />
              <span>{countdown}s 后自动刷新</span>
            </div>
            <button
              onClick={() => loadData(true)}
              disabled={loading}
              className="p-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 hover:text-white transition-all border border-white/5 disabled:opacity-50"
              title="立即刷新"
            >
              <ArrowClockwise size={16} className={loading ? 'animate-spin' : ''} />
            </button>
            <ThemeToggle theme={theme} onThemeChange={onThemeChange} compact={true} />
            <button
              onClick={onOpenLogin}
              className="flex items-center gap-1.5 text-xs font-semibold px-3 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white transition-all shadow-md shadow-indigo-600/20"
            >
              <SignIn size={14} weight="bold" />
              <span>登录控制台</span>
            </button>
          </div>
        </div>
      </header>

      {/* Main Container */}
      <main className="flex-1 max-w-6xl w-full mx-auto px-4 py-8 space-y-8">
        {/* Top Announcement Banner (if configured) */}
        {data?.config?.announcement && (
          <div className="p-4 rounded-xl bg-indigo-950/40 border border-indigo-500/30 text-indigo-200 flex items-start gap-3 shadow-lg">
            <Info size={20} className="text-indigo-400 shrink-0 mt-0.5" />
            <div className="text-sm leading-relaxed">{data.config.announcement}</div>
          </div>
        )}

        {/* Hero System Status Banner */}
        <div className={`p-6 sm:p-8 rounded-2xl border transition-all ${hero.bgClass} flex flex-col sm:flex-row sm:items-center justify-between gap-6 shadow-xl`}>
          <div className="flex items-center gap-4">
            <div className="shrink-0">{hero.icon}</div>
            <div>
              <h1 className="text-xl sm:text-2xl font-bold text-white tracking-tight">{hero.title}</h1>
              <p className="text-sm opacity-80 mt-1">{hero.subtitle}</p>
            </div>
          </div>
          <div className="text-xs font-mono opacity-70 flex sm:flex-col items-center sm:items-end justify-between sm:justify-center border-t sm:border-t-0 pt-3 sm:pt-0 border-white/10">
            <span>最后更新于</span>
            <span className="font-semibold text-white mt-0.5">{lastRefreshed.toLocaleTimeString()}</span>
          </div>
        </div>

        {/* Active Incidents or Scheduled Maintenance */}
        {(activeIncidents.length > 0 || maintenance.length > 0) && (
          <div className="space-y-4">
            <h2 className="text-base font-bold text-white flex items-center gap-2">
              <WarningCircle size={18} className="text-amber-400" />
              <span>当前进行中的事件与维护</span>
            </h2>

            {/* Active Incidents */}
            {activeIncidents.map((inc) => (
              <div key={inc.id} className="p-5 rounded-xl bg-[#131520] border border-amber-500/30 space-y-4 shadow-lg">
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                  <div className="flex items-center gap-2.5">
                    <span className="px-2 py-0.5 rounded text-xs font-bold uppercase tracking-wider bg-rose-500/20 text-rose-300 border border-rose-500/30">
                      {inc.impact === 'critical' ? '严重故障' : inc.impact === 'major' ? '主要故障' : '轻微异常'}
                    </span>
                    <h3 className="text-base font-bold text-white">{inc.title}</h3>
                  </div>
                  <span className="text-xs text-slate-400 font-mono">创建于 {formatTimestamp(inc.created_at)}</span>
                </div>

                {/* Updates timeline */}
                <div className="space-y-3 pl-2 sm:pl-4 border-l-2 border-slate-700/60 ml-2">
                  {inc.updates && inc.updates.length > 0 ? (
                    inc.updates.map((upd, idx) => (
                      <div key={upd.id || idx} className="relative pl-4 space-y-1">
                        <div className="absolute -left-[21px] top-1 w-2.5 h-2.5 rounded-full bg-amber-400 ring-4 ring-[#131520]"></div>
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-bold text-amber-300 uppercase">
                            {upd.status === 'investigating' ? '调查中' : upd.status === 'identified' ? '已排查定位' : upd.status === 'monitoring' ? '观察恢复中' : '已解决'}
                          </span>
                          <span className="text-[11px] text-slate-400 font-mono">{formatTimestamp(upd.created_at)}</span>
                        </div>
                        <p className="text-sm text-slate-200 leading-relaxed">{upd.message}</p>
                      </div>
                    ))
                  ) : (
                    <p className="text-sm text-slate-400">正在积极跟进调查中...</p>
                  )}
                </div>
              </div>
            ))}

            {/* Scheduled Maintenance */}
            {maintenance.map((m) => (
              <div key={m.id} className="p-5 rounded-xl bg-[#131520] border border-sky-500/30 space-y-4 shadow-lg">
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                  <div className="flex items-center gap-2.5">
                    <span className="px-2 py-0.5 rounded text-xs font-bold uppercase tracking-wider bg-sky-500/20 text-sky-300 border border-sky-500/30">
                      计划维护
                    </span>
                    <h3 className="text-base font-bold text-white">{m.title}</h3>
                  </div>
                  {m.scheduled_start_at && (
                    <span className="text-xs text-sky-300 font-mono">
                      窗口期: {formatTimestamp(m.scheduled_start_at)} ~ {formatTimestamp(m.scheduled_end_at)}
                    </span>
                  )}
                </div>
                {m.updates && m.updates.length > 0 && (
                  <div className="text-sm text-slate-300">{m.updates[m.updates.length - 1].message}</div>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Monitored Components & 90-Day SLA Bars */}
        <div className="space-y-6">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 border-b border-white/5 pb-3">
            <div>
              <h2 className="text-lg font-bold text-white tracking-tight">各服务节点与关键链路健康度</h2>
              <p className="text-xs text-slate-400 mt-0.5">展示过去 {data?.config?.show_uptime_days || 90} 天内各服务的每日可用率心跳记录</p>
            </div>
            <div className="flex items-center gap-4 text-xs font-mono text-slate-400">
              <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-sm bg-emerald-500"></span>正常 (≥99.5%)</span>
              <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-sm bg-amber-500"></span>降级</span>
              <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-sm bg-rose-500"></span>中断</span>
              <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-sm bg-slate-700/50"></span>无数据</span>
            </div>
          </div>

          {Object.entries(groupedComponents).map(([groupName, comps]) => (
            <div key={groupName} className="space-y-3">
              <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider flex items-center gap-2">
                <span className="w-1.5 h-3.5 bg-indigo-500 rounded-full"></span>
                <span>{groupName}</span>
                <span className="text-xs text-slate-500 normal-case font-normal">({comps.length} 个监测项)</span>
              </h3>

              <div className="divide-y divide-white/5 rounded-2xl bg-[#0f111a] border border-white/5 overflow-hidden shadow-xl">
                {comps.map((item) => {
                  const comp = item.component
                  const daily = item.daily_uptimes || []
                  const daysCount = daily.length

                  return (
                    <div key={comp.id} className="p-4 sm:p-5 hover:bg-white/[0.02] transition-colors space-y-3">
                      {/* Component header */}
                      <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-3">
                          <span className="font-semibold text-white text-sm sm:text-base">{comp.name}</span>
                          {getComponentStatusBadge(item.current_status)}
                          {item.latency_ms !== null && item.latency_ms !== undefined && (
                            <span className="hidden sm:inline text-xs font-mono text-slate-400 bg-white/5 px-2 py-0.5 rounded">
                              {Math.round(item.latency_ms)} ms 延迟
                            </span>
                          )}
                        </div>
                        <div className="text-right">
                          <span className="font-bold text-sm sm:text-base font-mono text-emerald-400">
                            {item.uptime_90d?.toFixed(2)}%
                          </span>
                          <span className="text-xs text-slate-500 ml-1.5 hidden sm:inline font-mono">可用率 ({daysCount}天)</span>
                        </div>
                      </div>

                      {comp.description && (
                        <p className="text-xs text-slate-400 leading-relaxed">{comp.description}</p>
                      )}

                      {/* 90-Day SLA Bars */}
                      <div className="relative pt-1">
                        <div className="flex items-center gap-[2px] h-8 sm:h-9 w-full">
                          {daily.map((d, dIdx) => (
                            <div
                              key={d.date || dIdx}
                              onMouseEnter={() => setHoveredDay({ ...d, compName: comp.name })}
                              onMouseLeave={() => setHoveredDay(null)}
                              className={`flex-1 h-full rounded-[2px] transition-all cursor-pointer ${getDayBarClass(d.status)}`}
                            />
                          ))}
                        </div>

                        {/* Bar labels */}
                        <div className="flex justify-between items-center text-[11px] font-mono text-slate-500 mt-1.5">
                          <span>{daysCount} 天前</span>
                          <span className="text-slate-400 font-medium">90 天综合 SLA 在线率: {item.uptime_90d?.toFixed(2)}%</span>
                          <span>今天</span>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
        </div>

        {/* Hover Tooltip display */}
        {hoveredDay && (
          <div className="fixed bottom-6 right-6 z-50 p-3.5 rounded-xl bg-slate-900/95 border border-indigo-500/40 text-xs shadow-2xl backdrop-blur-md animate-fade-in pointer-events-none">
            <div className="font-bold text-white flex items-center justify-between gap-4 border-b border-white/10 pb-1.5 mb-1.5">
              <span>{hoveredDay.compName}</span>
              <span className="font-mono text-indigo-400">{hoveredDay.date}</span>
            </div>
            <div className="flex items-center justify-between gap-4">
              <span className="text-slate-400">可用率:</span>
              <span className="font-mono font-bold text-emerald-400">{hoveredDay.uptime_pct?.toFixed(2)}%</span>
            </div>
            <div className="flex items-center justify-between gap-4 mt-0.5">
              <span className="text-slate-400">运行状态:</span>
              <span className="font-semibold text-slate-200">
                {hoveredDay.status === 'operational' ? '正常运行' : hoveredDay.status === 'degraded' ? '性能降级' : hoveredDay.status === 'outage' ? '服务中断' : '无心跳数据'}
              </span>
            </div>
            {hoveredDay.incidents_count > 0 && (
              <div className="text-amber-400 mt-1 font-medium">
                包含 {hoveredDay.incidents_count} 条告警或中断记录
              </div>
            )}
          </div>
        )}

        {/* Historical Incidents Archive */}
        <div className="border-t border-white/5 pt-6 space-y-4">
          <button
            onClick={togglePastArchive}
            className="flex items-center gap-2 text-sm font-bold text-slate-300 hover:text-white transition-colors"
          >
            {showPastArchive ? <CaretDown size={16} /> : <CaretRight size={16} />}
            <span>查看已解决历史事件归档</span>
          </button>

          {showPastArchive && (
            <div className="space-y-3">
              {pastIncidents.length === 0 ? (
                <div className="p-6 text-center text-sm text-slate-500 bg-[#0f111a] rounded-xl border border-white/5">
                  过去 90 天内暂无历史故障事件记录，系统持续稳定运行。
                </div>
              ) : (
                pastIncidents.map((p) => {
                  const isExpanded = expandedPastIncidents[p.id]
                  return (
                    <div key={p.id} className="p-4 rounded-xl bg-[#0f111a] border border-white/5 space-y-2">
                      <div
                        onClick={() => togglePastItem(p.id)}
                        className="flex items-center justify-between cursor-pointer"
                      >
                        <div className="flex items-center gap-2.5">
                          <CheckCircle size={16} className="text-emerald-400" />
                          <span className="font-semibold text-white text-sm">{p.title}</span>
                          <span className="text-[11px] px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-mono">已解决</span>
                        </div>
                        <span className="text-xs text-slate-400 font-mono">{formatTimestamp(p.created_at)}</span>
                      </div>

                      {isExpanded && p.updates && (
                        <div className="mt-3 pt-3 border-t border-white/5 space-y-2 pl-4 text-xs text-slate-300">
                          {p.updates.map((u, uIdx) => (
                            <div key={u.id || uIdx} className="space-y-0.5">
                              <span className="text-slate-400 font-mono">[{formatTimestamp(u.created_at)}]</span>{' '}
                              <span>{u.message}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  )
                })
              )}
            </div>
          )}
        </div>
      </main>

      {/* Footer */}
      <footer className="border-t border-white/5 bg-[#0a0c14] py-8 text-xs text-slate-500 mt-12">
        <div className="max-w-6xl mx-auto px-4 flex flex-col sm:flex-row items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <Broadcast size={16} className="text-indigo-400" />
            <span className="font-semibold text-slate-400">ProbeWatch Cloud Status</span>
            <span>· 实时高可用服务健康中心</span>
          </div>
          <div className="flex items-center gap-4">
            <a href="/api/openapi.json" target="_blank" className="hover:text-slate-300 transition-colors">API 文档</a>
            <a href="/docs" target="_blank" className="hover:text-slate-300 transition-colors">Swagger 调试</a>
            <button onClick={onOpenLogin} className="hover:text-slate-300 transition-colors">管理员登录</button>
          </div>
        </div>
      </footer>
    </div>
  )
}
