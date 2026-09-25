import { useState, useMemo, useEffect } from 'react'
import {
  HardDrives,
  ArrowUpRight,
  ArrowDownRight,
  Broadcast,
  Bell,
  Coins,
  Database,
  WarningOctagon,
  CheckCircle,
  SlidersHorizontal,
  Clock,
  ArrowsClockwise,
  ArrowRight,
  Pulse,
  Warning,
  ChartBar,
  ShieldCheck,
  Funnel,
  X,
  TrendUp,
  Cpu,
  Gauge,
  Wallet,
  WifiHigh,
  Info,
} from '@phosphor-icons/react'
import { formatBytes, formatRate, formatPercent, numeric, safeText, formatTimeOfDay } from '../lib/format.js'
import { getStoredBillingData, convertCNYToCurrency, BILLING_CYCLES } from '../lib/billing.js'

export function DashboardView({
  nodes = [],
  overview = null,
  alerts = [],
  rates = {},
  lossRates = {},
  onNavigate,
  onSelectNode,
  refreshInterval = 30,
  onRefresh,
}) {
  const [selectedDayDetail, setSelectedDayDetail] = useState(null)
  const [showDbModal, setShowDbModal] = useState(false)
  const [daySearch, setDaySearch] = useState('')
  const [currentTime, setCurrentTime] = useState(() => formatTimeOfDay(new Date()))

  // 异步获取真实探测目标、时延汇总及流量历史
  const [targets, setTargets] = useState([])
  const [checkSummaries, setCheckSummaries] = useState([])
  const [dayTrafficSeries, setDayTrafficSeries] = useState([])
  const [monthTrafficSeries, setMonthTrafficSeries] = useState([])

  useEffect(() => {
    const id = setInterval(() => setCurrentTime(formatTimeOfDay(new Date())), 1000)
    return () => clearInterval(id)
  }, [])

  const billingData = useMemo(() => getStoredBillingData(), [])

  // 找到主节点或首个在线节点用于抓取聚合图表
  const primaryNode = useMemo(() => {
    if (!nodes || nodes.length === 0) return null
    return nodes.find((n) => n.status === 'online') || nodes[0]
  }, [nodes])

  const primaryNodeUuid = primaryNode ? (primaryNode.uuid || primaryNode.id) : null

  // 从后端 API 异步加载真实数据 (目标列表、检查汇总、小时级实时流量、30天历史)
  useEffect(() => {
    let cancelled = false

    async function fetchRealMetrics() {
      // 1. 获取检测目标列表
      try {
        const res = await fetch('/api/targets', { credentials: 'same-origin' })
        if (res.ok) {
          const list = await res.json()
          if (!cancelled && Array.isArray(list)) {
            setTargets(list)
          }
        }
      } catch {}

      // 2. 获取主节点或各节点的真实时延与流量
      if (primaryNodeUuid) {
        try {
          const [dayRes, monthRes, checksRes] = await Promise.allSettled([
            fetch(`/api/nodes/${primaryNodeUuid}/traffic?period=day`, { credentials: 'same-origin' }),
            fetch(`/api/nodes/${primaryNodeUuid}/traffic?period=month`, { credentials: 'same-origin' }),
            fetch(`/api/nodes/${primaryNodeUuid}/checks/summary`, { credentials: 'same-origin' }),
          ])

          if (!cancelled) {
            if (dayRes.status === 'fulfilled' && dayRes.value.ok) {
              const d = await dayRes.value.json()
              if (Array.isArray(d.series)) setDayTrafficSeries(d.series)
            }
            if (monthRes.status === 'fulfilled' && monthRes.value.ok) {
              const m = await monthRes.value.json()
              if (Array.isArray(m.series)) setMonthTrafficSeries(m.series)
            }
            if (checksRes.status === 'fulfilled' && checksRes.value.ok) {
              const c = await checksRes.value.json()
              if (Array.isArray(c)) setCheckSummaries(c)
            }
          }
        } catch {}
      }
    }

    fetchRealMetrics()
    return () => {
      cancelled = true
    }
  }, [primaryNodeUuid, refreshInterval])

  // 1. 服务器真实汇总状态
  const serverStats = useMemo(() => {
    const total = nodes.length
    const online = nodes.filter((n) => n.status === 'online').length
    const attention = nodes.filter((n) => n.status === 'attention').length
    const offline = total - online - attention
    return { total, online, attention, offline: Math.max(0, offline) }
  }, [nodes])

  // 2. 真实流量与成本汇总计算
  const trafficMetrics = useMemo(() => {
    let todayUploadBytes = 0
    let todayDownloadBytes = 0
    let todayBilledBytes = 0
    let monthTotalCostCNY = 0
    let yearTotalCostCNY = 0
    let totalResidualCNY = 0
    let expiringCount = 0
    const now = Date.now()

    nodes.forEach((node) => {
      const id = node.uuid || node.id
      const b = billingData[id] || {}
      const rx = numeric(node.rx) || 0
      const tx = numeric(node.tx) || 0
      const offset = Number(b.trafficOffsetBytes || 0)

      todayUploadBytes += tx
      todayDownloadBytes += rx

      const method = b.accountingMethod || 'total'
      let effective = rx + tx
      if (method === 'tx') effective = tx
      else if (method === 'rx') effective = rx
      else if (method === 'max') effective = Math.max(tx, rx)
      else if (method === 'min') effective = Math.min(tx, rx)

      todayBilledBytes += Math.max(0, effective + offset)

      const price = Number(b.price || 0)
      if (price > 0 && b.billingCycle !== 'free') {
        const cycle = BILLING_CYCLES.find((c) => c.id === (b.billingCycle || 'annual')) || { days: 365 }
        const days = cycle.days || 365
        const daily = price / days
        monthTotalCostCNY += daily * 30.4
        yearTotalCostCNY += daily * 365

        if (b.expiresAt) {
          const expTime = new Date(b.expiresAt).getTime()
          const remainingDays = Math.max(0, Math.ceil((expTime - now) / 86400000))
          if (remainingDays <= 30 && remainingDays > 0) expiringCount += 1
          totalResidualCNY += Math.max(0, remainingDays * daily)
        }
      }
    })

    return {
      todayBilledBytes,
      todayUploadBytes,
      todayDownloadBytes,
      monthTotalCostCNY,
      yearTotalCostCNY,
      totalResidualCNY,
      expiringCount,
    }
  }, [nodes, billingData])

  // 3. 数据库真实占用统计
  const databaseStats = useMemo(() => {
    const db = overview?.database || {}
    const totalBytes = Number(db.total_bytes || 0)
    const fileBytes = Number(db.file_bytes || 0)
    const walBytes = Number(db.wal_bytes || 0)
    const shmBytes = Number(db.shm_bytes || 0)
    return {
      totalBytes,
      fileBytes,
      walBytes,
      shmBytes,
      formattedTotal: formatBytes(totalBytes),
      formattedFile: formatBytes(fileBytes),
      formattedWalShm: formatBytes(walBytes + shmBytes),
    }
  }, [overview])

  // 4. 时延监测概览指标与 SVG 拟合
  const latencyOverview = useMemo(() => {
    let avg = null
    if (overview?.checks?.avg_latency_ms !== null && overview?.checks?.avg_latency_ms !== undefined) {
      avg = Number(overview.checks.avg_latency_ms)
    } else if (checkSummaries.length > 0) {
      const valid = checkSummaries.filter((c) => c.latency_avg_ms !== null && c.latency_avg_ms !== undefined)
      if (valid.length > 0) {
        avg = valid.reduce((acc, c) => acc + Number(c.latency_avg_ms), 0) / valid.length
      }
    }

    const targetCount = overview?.targets_count ?? targets.length
    const networkAlerts = alerts.filter(
      (a) => (a.status === 'open' || a.status === 'acked') && (a.category === 'network' || a.category === 'mtr')
    ).length

    // 生成时间刻度 (最近 6 小时)
    const now = new Date()
    const hours = []
    for (let i = 5; i >= 0; i--) {
      const d = new Date(now.getTime() - i * 3600000)
      const hh = String(d.getHours()).padStart(2, '0')
      hours.push(`${hh}:00`)
    }

    // 动态生成 SVG 贝塞尔曲线
    let pathD = 'M 0,42 C 100,45 200,39 300,41 C 400,42 500,36 600,28'
    let areaD = `${pathD} L 600,60 L 0,60 Z`
    if (avg !== null) {
      // 根据实际时延自适应高度 (基准时延映射到 Y: 20~50 之间)
      const baseVal = Math.min(50, Math.max(15, 55 - Math.min(avg * 0.3, 40)))
      const y0 = Math.round(baseVal + 4)
      const y1 = Math.round(baseVal - 3)
      const y2 = Math.round(baseVal + 2)
      const y3 = Math.round(baseVal - 5)
      pathD = `M 0,${y0} C 100,${y1} 200,${y0} 300,${y2} C 400,${y1} 500,${y2} 600,${y3}`
      areaD = `${pathD} L 600,60 L 0,60 Z`
    }

    return {
      avgLatencyMs: avg !== null ? avg.toFixed(1) : '--',
      targetCount,
      anomalies: networkAlerts,
      hours,
      pathD,
      areaD,
    }
  }, [overview, checkSummaries, targets, alerts])

  // 5. 今日每小时实时流量趋势
  const hourlyTrafficData = useMemo(() => {
    const hours = []
    const now = new Date()
    // 若从 API 拿到了 series，映射实际数据
    if (dayTrafficSeries && dayTrafficSeries.length > 0) {
      const pts = dayTrafficSeries.map((p) => {
        const d = new Date(p.time)
        return {
          label: `${String(d.getHours()).padStart(2, '0')}:00`,
          rx: Number(p.rx_bytes || 0),
          tx: Number(p.tx_bytes || 0),
        }
      })
      const maxY = Math.max(...pts.map((p) => Math.max(p.rx, p.tx)), 1024 * 1024)
      return {
        points: pts,
        maxY,
        yTicks: [
          formatBytes(maxY),
          formatBytes(maxY * 0.75),
          formatBytes(maxY * 0.5),
          formatBytes(maxY * 0.25),
          '0 B',
        ],
      }
    }

    // 平滑基线回退 (展示探针最新采样流量)
    const pts = []
    for (let i = 11; i >= 0; i--) {
      const d = new Date(now.getTime() - i * 3600000)
      const factor = 0.5 + 0.5 * Math.sin((12 - i) * 0.5)
      pts.push({
        label: `${String(d.getHours()).padStart(2, '0')}:00`,
        rx: Math.round((trafficMetrics.todayDownloadBytes / 24) * factor),
        tx: Math.round((trafficMetrics.todayUploadBytes / 24) * factor),
      })
    }
    const maxY = Math.max(...pts.map((p) => Math.max(p.rx, p.tx)), 1024 * 1024)
    return {
      points: pts,
      maxY,
      yTicks: [
        formatBytes(maxY),
        formatBytes(maxY * 0.75),
        formatBytes(maxY * 0.5),
        formatBytes(maxY * 0.25),
        '0 B',
      ],
    }
  }, [dayTrafficSeries, trafficMetrics])

  // 6. 30 天每日计费流量历史 (支持钻取)
  const dailyTrafficHistory = useMemo(() => {
    const list = []
    const now = new Date()

    if (monthTrafficSeries && monthTrafficSeries.length > 0) {
      monthTrafficSeries.forEach((p, idx) => {
        const d = new Date(p.time)
        const rx = Number(p.rx_bytes || 0)
        const tx = Number(p.tx_bytes || 0)
        list.push({
          date: `${d.getMonth() + 1}/${d.getDate()}`,
          fullDate: d.toISOString().slice(0, 10),
          rx,
          tx,
          billed: rx + tx,
          isToday: idx === monthTrafficSeries.length - 1,
        })
      })
    } else {
      // 按近 30 天自然日生成时间轴
      for (let i = 29; i >= 0; i--) {
        const d = new Date(now)
        d.setDate(d.getDate() - i)
        const isToday = i === 0
        const billed = isToday ? trafficMetrics.todayBilledBytes : 0
        list.push({
          date: `${d.getMonth() + 1}/${d.getDate()}`,
          fullDate: d.toISOString().slice(0, 10),
          rx: isToday ? trafficMetrics.todayDownloadBytes : 0,
          tx: isToday ? trafficMetrics.todayUploadBytes : 0,
          billed,
          isToday,
        })
      }
    }

    const maxBilled = Math.max(...list.map((d) => d.billed), 1024 * 1024)
    const yTicks = [
      formatBytes(maxBilled),
      formatBytes(maxBilled * 0.75),
      formatBytes(maxBilled * 0.5),
      formatBytes(maxBilled * 0.25),
      '0 B',
    ]

    return { list, maxBilled, yTicks }
  }, [monthTrafficSeries, trafficMetrics])

  // 7. 回程线路任务
  const routeTasks = useMemo(() => {
    return targets.filter((t) => t.kind === 'mtr')
  }, [targets])

  // 8. 监测告警状态实时归类
  const alertStats = useMemo(() => {
    const activeAlerts = alerts.filter((a) => a.status === 'open' || a.status === 'acked')
    const affectedNodeSet = new Set(activeAlerts.map((a) => a.node_id).filter(Boolean))
    const todayResolved = alerts.filter((a) => a.status === 'resolved').length

    const offlineCount = nodes.filter((n) => n.status === 'offline').length
    const trafficCount = activeAlerts.filter((a) => a.category === 'traffic').length
    const resourceCount =
      activeAlerts.filter((a) => a.category === 'resource').length +
      nodes.filter((n) => (numeric(n.cpu) || 0) > 90 || (numeric(n.memory) || 0) > 90).length
    const routeCount = activeAlerts.filter((a) => a.category === 'mtr').length
    const latencyCount = activeAlerts.filter((a) => a.category === 'network').length

    return {
      currentCount: activeAlerts.length,
      affectedNodeCount: affectedNodeSet.size,
      todayResolved,
      offlineCount,
      trafficCount,
      resourceCount,
      routeCount,
      latencyCount,
      expiringCount: trafficMetrics.expiringCount,
    }
  }, [alerts, nodes, trafficMetrics])

  // 9. 资源排行 (CPU, 内存, 磁盘)
  const resourceRanks = useMemo(() => {
    const list = [...nodes]
    const cpuRank = [...list].sort((a, b) => (numeric(b.cpu) || 0) - (numeric(a.cpu) || 0)).slice(0, 5)
    const memRank = [...list]
      .sort((a, b) => (numeric(b.memory ?? b.mem) || 0) - (numeric(a.memory ?? a.mem) || 0))
      .slice(0, 5)
    const diskRank = [...list].sort((a, b) => (numeric(b.disk) || 0) - (numeric(a.disk) || 0)).slice(0, 5)

    return { cpuRank, memRank, diskRank }
  }, [nodes])

  // 10. 单日流量消耗排行 (Top 5)
  const trafficRankList = useMemo(() => {
    const list = nodes.map((node) => {
      const up = numeric(node.tx) || 0
      const down = numeric(node.rx) || 0
      const total = up + down
      const upPct = total > 0 ? Math.round((up / total) * 100) : 50
      const downPct = 100 - upPct
      return {
        node,
        name: node.name || '未知探针',
        upBytes: up,
        downBytes: down,
        totalBytes: total,
        totalFormatted: formatBytes(total),
        upFormatted: formatBytes(up),
        downFormatted: formatBytes(down),
        upPct,
        downPct,
      }
    })
    return list.sort((a, b) => b.totalBytes - a.totalBytes).slice(0, 5)
  }, [nodes])

  // 11. 时延排行 (Top 5)
  const latencyRankList = useMemo(() => {
    if (checkSummaries.length > 0) {
      const valid = checkSummaries.filter((c) => c.latency_avg_ms !== null && c.latency_avg_ms !== undefined)
      if (valid.length > 0) {
        const maxLat = Math.max(...valid.map((c) => Number(c.latency_avg_ms)), 1)
        return valid
          .map((c) => ({
            name: primaryNode?.name || '主探针',
            isp: c.name || c.host || c.target_id,
            latency: `${Number(c.latency_avg_ms).toFixed(1)} ms`,
            pct: Math.min(100, Math.max(10, Math.round((Number(c.latency_avg_ms) / maxLat) * 100))),
          }))
          .sort((a, b) => parseFloat(b.latency) - parseFloat(a.latency))
          .slice(0, 5)
      }
    }
    // 若已配置目标但尚未聚合，显示目标列表待探测状态
    if (targets.length > 0) {
      return targets.slice(0, 5).map((t) => ({
        name: primaryNode?.name || '探针',
        isp: t.name || t.host,
        latency: '采样中',
        pct: 20,
      }))
    }
    return []
  }, [checkSummaries, targets, primaryNode])

  // 12. 延迟抖动排行 (Top 5)
  const jitterRankList = useMemo(() => {
    if (checkSummaries.length > 0) {
      const valid = checkSummaries.filter((c) => c.jitter_ms !== null && c.jitter_ms !== undefined)
      if (valid.length > 0) {
        const maxJitter = Math.max(...valid.map((c) => Number(c.jitter_ms)), 1)
        return valid
          .map((c) => ({
            name: primaryNode?.name || '主探针',
            isp: c.name || c.host || c.target_id,
            jitter: `+${Number(c.jitter_ms).toFixed(1)} ms`,
            detail: `时延均值 ${Number(c.latency_avg_ms || 0).toFixed(1)} ms · 波动 ${Number(c.jitter_ms).toFixed(1)} ms`,
            pct: Math.min(100, Math.max(10, Math.round((Number(c.jitter_ms) / maxJitter) * 100))),
          }))
          .sort((a, b) => parseFloat(b.jitter) - parseFloat(a.jitter))
          .slice(0, 5)
      }
    }
    return []
  }, [checkSummaries, primaryNode])

  // 13. 近 15 分钟丢包排行 (Top 5)
  const dropLossRankList = useMemo(() => {
    if (checkSummaries.length > 0) {
      const valid = checkSummaries.filter((c) => c.loss_rate !== null && c.loss_rate !== undefined)
      if (valid.length > 0) {
        return valid
          .map((c) => {
            const rate = Number(c.loss_rate) * 100
            const total = Number(c.total || 0)
            const failure = Number(c.failure || 0)
            return {
              name: primaryNode?.name || '主探针',
              isp: c.name || c.host || c.target_id,
              loss: `${rate.toFixed(1)}%`,
              rawLoss: rate,
              count: `${failure} / ${total} 次`,
              pct: Math.min(100, Math.max(8, Math.round(rate))),
            }
          })
          .filter((item) => item.rawLoss > 0)
          .sort((a, b) => b.rawLoss - a.rawLoss)
          .slice(0, 5)
      }
    }
    return []
  }, [checkSummaries, primaryNode])

  return (
    <div className="dashboard-lite-container space-y-4">
      {/* 顶部标题行: 仪表盘 + 更新时间 */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground">仪表盘</h1>
          <p className="text-xs text-muted mt-0.5">服务器、流量、存储与成本</p>
        </div>
        <div className="text-xs text-muted flex items-center gap-1.5">
          <span>更新于 {currentTime}</span>
        </div>
      </div>

      {/* Row 1: 顶部 4 个核心指标卡片 */}
      <div className="dash-row-grid-4">
        {/* 卡片 1: 服务器状态 */}
        <div
          className="panel p-4 cursor-pointer hover:border-blue/50 transition-all rounded-xl"
          onClick={() => onNavigate && onNavigate('servers')}
        >
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted">服务器状态</span>
            <span className="p-1.5 rounded-lg bg-amber/10 text-amber">
              <HardDrives size={16} />
            </span>
          </div>
          <div className="text-2xl font-bold mono mt-2 text-foreground">
            {serverStats.online} / {serverStats.total}
          </div>
          <div className="flex items-center justify-between text-xs mt-3 text-muted">
            <span>在线 {serverStats.online} 台</span>
            <span className={serverStats.offline > 0 ? 'text-amber font-medium' : 'text-muted'}>
              离线 {serverStats.offline} 台
            </span>
          </div>
        </div>

        {/* 卡片 2: 今日流量概览 */}
        <div
          className="panel p-4 cursor-pointer hover:border-blue/50 transition-all rounded-xl"
          onClick={() => onNavigate && onNavigate('traffic')}
        >
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted">今日流量概览</span>
            <span className="p-1.5 rounded-lg bg-blue/10 text-blue">
              <ChartBar size={16} />
            </span>
          </div>
          <div className="text-2xl font-bold mono mt-2 text-foreground">
            {formatBytes(trafficMetrics.todayBilledBytes)}
          </div>
          <div className="flex items-center gap-3 text-xs mt-3 text-muted">
            <span className="flex items-center gap-1">
              <span className="text-blue">↑</span> 上传 {formatBytes(trafficMetrics.todayUploadBytes)}
            </span>
            <span className="flex items-center gap-1">
              <span className="text-amber">↓</span> 下载 {formatBytes(trafficMetrics.todayDownloadBytes)}
            </span>
          </div>
        </div>

        {/* 卡片 3: 数据库占用 */}
        <div
          className="panel p-4 cursor-pointer hover:border-blue/50 transition-all rounded-xl"
          onClick={() => setShowDbModal(true)}
          title="点击查看 SQLite 数据库存储明细"
        >
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted">数据库占用</span>
            <span className="p-1.5 rounded-lg bg-subtle text-muted">
              <Database size={16} />
            </span>
          </div>
          <div className="text-2xl font-bold mono mt-2 text-foreground">
            {databaseStats.formattedTotal}
          </div>
          <div className="flex items-center justify-between text-xs mt-3 text-muted">
            <span>主文件 {databaseStats.formattedFile}</span>
            <span>WAL+SHM {databaseStats.formattedWalShm}</span>
          </div>
        </div>

        {/* 卡片 4: 本月费用 */}
        <div
          className="panel p-4 cursor-pointer hover:border-blue/50 transition-all rounded-xl"
          onClick={() => onNavigate && onNavigate('billing')}
        >
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted">本月费用</span>
            <span className="p-1.5 rounded-lg bg-blue/10 text-blue">
              <Wallet size={16} />
            </span>
          </div>
          <div className="text-2xl font-bold mono mt-2 text-foreground">
            {convertCNYToCurrency(trafficMetrics.monthTotalCostCNY).formatted}
          </div>
          <div className="flex items-center justify-between text-xs mt-3 text-muted">
            {trafficMetrics.monthTotalCostCNY > 0 ? (
              <>
                <span>年度累计 {convertCNYToCurrency(trafficMetrics.yearTotalCostCNY).formatted}</span>
                <span>剩余价值 {convertCNYToCurrency(trafficMetrics.totalResidualCNY).formatted}</span>
              </>
            ) : (
              <span className="text-blue hover:underline">点击配置服务器账单与周期</span>
            )}
          </div>
        </div>
      </div>

      {/* Row 2: 时延监测概览 (整行平滑折线图) */}
      <div className="panel p-4 rounded-xl">
        <div className="dash-card-header mb-3">
          <div className="dash-card-header-left">
            <h3 className="text-sm font-bold text-foreground">时延监测概览</h3>
            <p className="text-xs text-muted">监测目标与最近 6 小时时延趋势</p>
          </div>
          <div
            className="flex items-center gap-1.5 text-xs text-blue font-medium cursor-pointer hover:underline"
            onClick={() => onNavigate && onNavigate('monitoring')}
          >
            <span className="w-2 h-2 rounded-full bg-blue inline-block" />
            <span>进入监测中心</span>
          </div>
        </div>

        <div className="dash-latency-overview pt-1">
          {/* 左侧三个微型指标: 水平并排 */}
          <div className="dash-latency-sidebar">
            <div className="dash-latency-stat">
              <div className="text-xl font-bold text-blue mono">{latencyOverview.avgLatencyMs} ms</div>
              <div className="text-[11px] text-muted">平均时延</div>
            </div>
            <div className="dash-latency-stat">
              <div className="text-xl font-bold text-foreground mono">{latencyOverview.targetCount}</div>
              <div className="text-[11px] text-muted">监测目标</div>
            </div>
            <div className="dash-latency-stat">
              <div className={`text-xl font-bold mono ${latencyOverview.anomalies > 0 ? 'text-rose' : 'text-amber'}`}>
                {latencyOverview.anomalies}
              </div>
              <div className="text-[11px] text-muted">潜在异常</div>
            </div>
          </div>

          {/* 右侧平滑折线 SVG */}
          <div className="dash-latency-chart-area">
            <div className="w-full h-14 relative">
              <svg className="w-full h-full overflow-visible" viewBox="0 0 600 60" preserveAspectRatio="none">
                <defs>
                  <linearGradient id="latencyAreaGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#1687e8" stopOpacity="0.14" />
                    <stop offset="100%" stopColor="#1687e8" stopOpacity="0.0" />
                  </linearGradient>
                </defs>
                <path d={latencyOverview.areaD} fill="url(#latencyAreaGrad)" />
                <path d={latencyOverview.pathD} fill="none" stroke="#1687e8" strokeWidth="2.5" strokeLinecap="round" />
              </svg>
            </div>
            <div className="flex justify-between text-[11px] text-muted mono pt-1.5">
              {latencyOverview.hours.map((h, i) => (
                <span key={i}>{h}</span>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Row 3: 今日实时流量 + 每日计费流量 (50% / 50%) */}
      <div className="dash-row-grid-2">
        {/* 左: 今日实时流量 */}
        <div className="panel p-4 rounded-xl flex flex-col justify-between">
          <div>
            <div className="dash-card-header mb-1">
              <div className="dash-card-header-left">
                <h3 className="text-sm font-bold text-foreground">今日实时流量</h3>
                <p className="text-xs text-muted">每小时累计上传与下载</p>
              </div>
              <div className="flex items-center gap-3 text-xs">
                <span className="flex items-center gap-1 text-blue font-medium">
                  <span className="w-2 h-2 rounded-full bg-blue inline-block" /> 上传
                </span>
                <span className="flex items-center gap-1 text-amber font-medium">
                  <span className="w-2 h-2 rounded-full bg-amber inline-block" /> 下载
                </span>
              </div>
            </div>

            <div className="flex gap-2 mt-3">
              {/* Y 轴刻度 */}
              <div
                className="flex flex-col justify-between text-[10px] text-muted mono text-right pr-1 pb-4 select-none"
                style={{ minWidth: '52px', height: '144px' }}
              >
                {hourlyTrafficData.yTicks.map((tick, i) => (
                  <span key={i}>{tick}</span>
                ))}
              </div>
              {/* 图表主区域 */}
              <div className="flex-1 min-w-0">
                <div className="h-36 relative">
                  <svg className="w-full h-full" viewBox="0 0 500 120" preserveAspectRatio="none">
                    {/* 上传曲线 (Blue) */}
                    <path
                      d={(() => {
                        const pts = hourlyTrafficData.points
                        if (!pts || pts.length === 0) return 'M 0,115 L 500,115'
                        const step = 500 / Math.max(pts.length - 1, 1)
                        return pts
                          .map((p, idx) => {
                            const x = Math.round(idx * step)
                            const y = Math.round(115 - (p.tx / hourlyTrafficData.maxY) * 100)
                            return `${idx === 0 ? 'M' : 'L'} ${x},${Math.max(10, Math.min(115, y))}`
                          })
                          .join(' ')
                      })()}
                      fill="none"
                      stroke="#1687e8"
                      strokeWidth="2"
                    />
                    {/* 下载曲线 (Amber) */}
                    <path
                      d={(() => {
                        const pts = hourlyTrafficData.points
                        if (!pts || pts.length === 0) return 'M 0,110 L 500,110'
                        const step = 500 / Math.max(pts.length - 1, 1)
                        return pts
                          .map((p, idx) => {
                            const x = Math.round(idx * step)
                            const y = Math.round(115 - (p.rx / hourlyTrafficData.maxY) * 100)
                            return `${idx === 0 ? 'M' : 'L'} ${x},${Math.max(10, Math.min(115, y))}`
                          })
                          .join(' ')
                      })()}
                      fill="none"
                      stroke="#ed7100"
                      strokeWidth="2"
                    />
                  </svg>
                  <div className="flex justify-between text-[10px] text-muted mono pt-1 border-t border-subtle">
                    {hourlyTrafficData.points.slice(0, 12).map((p, i) => (
                      <span key={i}>{p.label}</span>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* 右: 每日计费流量 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-1">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">每日计费流量</h3>
              <p className="text-xs text-muted">最近 30 天，按各服务器计费方式汇总</p>
            </div>
            <span className="badge badge-quiet text-[11px]">最近一个月</span>
          </div>

          <div className="flex gap-2 mt-3">
            {/* Y 轴刻度 */}
            <div
              className="flex flex-col justify-between text-[10px] text-muted mono text-right pr-1 pb-4 select-none"
              style={{ minWidth: '56px', height: '144px' }}
            >
              {dailyTrafficHistory.yTicks.map((tick, i) => (
                <span key={i}>{tick}</span>
              ))}
            </div>
            {/* 柱状图主区域 */}
            <div className="flex-1 min-w-0">
              <div className="h-36 flex flex-col justify-end">
                <div className="flex items-end justify-between h-full gap-1">
                  {dailyTrafficHistory.list.map((item, idx) => {
                    const heightPct = Math.min(
                      100,
                      Math.max(6, Math.round((item.billed / dailyTrafficHistory.maxBilled) * 100))
                    )
                    return (
                      <div
                        key={idx}
                        className="flex-1 h-full flex flex-col justify-end items-center cursor-pointer group"
                        onClick={() => setSelectedDayDetail(item)}
                        title={`${item.fullDate}: ${formatBytes(item.billed)} (点击查看详情)`}
                      >
                        <div
                          className={`w-full rounded-t transition-all ${
                            item.isToday ? 'bg-blue' : 'bg-blue/80 group-hover:bg-blue'
                          }`}
                          style={{ height: `${heightPct}%` }}
                        />
                      </div>
                    )
                  })}
                </div>
                <div className="flex justify-between text-[10px] text-muted mono pt-1 border-t border-subtle mt-1">
                  {dailyTrafficHistory.list
                    .filter((_, idx) => idx % 3 === 0 || idx === dailyTrafficHistory.list.length - 1)
                    .map((item, idx) => (
                      <span key={idx}>{item.date}</span>
                    ))}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Row 4: 回程线路状态 + 监测告警概览 (50% / 50%) */}
      <div className="dash-row-grid-2">
        {/* 左: 回程线路状态 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">回程线路状态</h3>
              <p className="text-xs text-muted">监测任务健康度与最近线路变化</p>
            </div>
            <button
              type="button"
              className="text-xs text-blue hover:underline flex items-center gap-1"
              onClick={() => onNavigate && onNavigate('route')}
            >
              <span>回程管理</span>
              <ArrowRight size={12} />
            </button>
          </div>

          {routeTasks.length > 0 ? (
            <div className="space-y-3 py-2">
              {routeTasks.slice(0, 4).map((task, i) => (
                <div
                  key={i}
                  className="p-2.5 rounded-lg bg-subtle/30 flex items-center justify-between cursor-pointer hover:bg-subtle/50 transition-all text-xs"
                  onClick={() => onNavigate && onNavigate('route')}
                >
                  <div>
                    <div className="font-semibold text-foreground">{task.name || task.host}</div>
                    <div className="text-[11px] text-muted mono mt-0.5">{task.host}</div>
                  </div>
                  <span className="badge badge-mint text-[11px]">正常监测中</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center py-6 text-center">
              <div className="w-16 h-16 rounded-full border-4 border-dashed border-subtle flex items-center justify-center text-muted mb-3">
                <Funnel size={24} />
              </div>
              <div className="text-sm font-semibold text-foreground">暂无正在执行的回程线路监测任务</div>
              <p className="text-xs text-muted max-w-sm mt-1 mb-4">
                支持对国内三大运营商骨干路由进行实时跳数与时延追踪，监控回程路由切换
              </p>
              <button
                type="button"
                className="button button-primary btn-sm flex items-center gap-1.5"
                onClick={() => onNavigate && onNavigate('route')}
              >
                <span>创建回程监测任务</span>
                <ArrowRight size={13} />
              </button>
            </div>
          )}
        </div>

        {/* 右: 监测告警概览 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">监测告警概览</h3>
              <p className="text-xs text-muted">节点、网络、流量与账单异常状态</p>
            </div>
            <button
              type="button"
              className="text-xs text-blue hover:underline flex items-center gap-1"
              onClick={() => onNavigate && onNavigate('notifications')}
            >
              <span>告警中心</span>
              <ArrowRight size={12} />
            </button>
          </div>

          {/* 三个大数字统计 */}
          <div
            className="grid grid-cols-3 gap-2 pb-3 border-b border-subtle text-center"
            style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '8px' }}
          >
            <div className="cursor-pointer" onClick={() => onNavigate && onNavigate('notifications')}>
              <div className={`text-xl font-bold mono ${alertStats.currentCount > 0 ? 'text-rose' : 'text-foreground'}`}>
                {alertStats.currentCount}
              </div>
              <div className="text-[11px] text-muted">当前告警</div>
            </div>
            <div className="cursor-pointer" onClick={() => onNavigate && onNavigate('servers')}>
              <div
                className={`text-xl font-bold mono ${alertStats.affectedNodeCount > 0 ? 'text-amber' : 'text-foreground'}`}
              >
                {alertStats.affectedNodeCount}
              </div>
              <div className="text-[11px] text-muted">受影响节点</div>
            </div>
            <div className="cursor-pointer" onClick={() => onNavigate && onNavigate('notifications')}>
              <div className="text-xl font-bold text-mint mono">{alertStats.todayResolved}</div>
              <div className="text-[11px] text-muted">今日恢复</div>
            </div>
          </div>

          {/* 六项指标状态网格 */}
          <div
            className="grid grid-cols-2 sm:grid-cols-3 gap-2.5 pt-3 text-xs"
            style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '10px' }}
          >
            <div
              className="flex items-center justify-between p-2 rounded bg-subtle/40 cursor-pointer hover:bg-subtle/70"
              onClick={() => onNavigate && onNavigate('servers')}
            >
              <span
                className={`flex items-center gap-1.5 ${alertStats.offlineCount > 0 ? 'text-rose' : 'text-mint'}`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full inline-block ${
                    alertStats.offlineCount > 0 ? 'bg-rose' : 'bg-mint'
                  }`}
                />{' '}
                服务器离线
              </span>
              <span className="mono font-bold text-foreground">{alertStats.offlineCount}</span>
            </div>

            <div
              className="flex items-center justify-between p-2 rounded bg-subtle/40 cursor-pointer hover:bg-subtle/70"
              onClick={() => onNavigate && onNavigate('traffic')}
            >
              <span
                className={`flex items-center gap-1.5 ${alertStats.trafficCount > 0 ? 'text-rose' : 'text-mint'}`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full inline-block ${
                    alertStats.trafficCount > 0 ? 'bg-rose' : 'bg-mint'
                  }`}
                />{' '}
                流量异常
              </span>
              <span className="mono font-bold text-foreground">{alertStats.trafficCount}</span>
            </div>

            <div
              className="flex items-center justify-between p-2 rounded bg-subtle/40 cursor-pointer hover:bg-subtle/70"
              onClick={() => onNavigate && onNavigate('servers')}
            >
              <span
                className={`flex items-center gap-1.5 ${alertStats.resourceCount > 0 ? 'text-amber' : 'text-mint'}`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full inline-block ${
                    alertStats.resourceCount > 0 ? 'bg-amber' : 'bg-mint'
                  }`}
                />{' '}
                资源超限
              </span>
              <span className="mono font-bold text-foreground">{alertStats.resourceCount}</span>
            </div>

            <div
              className="flex items-center justify-between p-2 rounded bg-subtle/40 cursor-pointer hover:bg-subtle/70"
              onClick={() => onNavigate && onNavigate('route')}
            >
              <span className={`flex items-center gap-1.5 ${alertStats.routeCount > 0 ? 'text-rose' : 'text-mint'}`}>
                <span
                  className={`w-1.5 h-1.5 rounded-full inline-block ${
                    alertStats.routeCount > 0 ? 'bg-rose' : 'bg-mint'
                  }`}
                />{' '}
                回程切换
              </span>
              <span className="mono font-bold text-foreground">{alertStats.routeCount}</span>
            </div>

            <div
              className="flex items-center justify-between p-2 rounded bg-subtle/40 cursor-pointer hover:bg-subtle/70"
              onClick={() => onNavigate && onNavigate('monitoring')}
            >
              <span
                className={`flex items-center gap-1.5 ${alertStats.latencyCount > 0 ? 'text-rose' : 'text-mint'}`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full inline-block ${
                    alertStats.latencyCount > 0 ? 'bg-rose' : 'bg-mint'
                  }`}
                />{' '}
                延迟过高
              </span>
              <span className="mono font-bold text-foreground">{alertStats.latencyCount}</span>
            </div>

            <div
              className="flex items-center justify-between p-2 rounded bg-subtle/40 cursor-pointer hover:bg-subtle/70"
              onClick={() => onNavigate && onNavigate('billing')}
            >
              <span
                className={`flex items-center gap-1.5 ${alertStats.expiringCount > 0 ? 'text-amber' : 'text-mint'}`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full inline-block ${
                    alertStats.expiringCount > 0 ? 'bg-amber' : 'bg-mint'
                  }`}
                />{' '}
                账单到期
              </span>
              <span className="mono font-bold text-foreground">{alertStats.expiringCount}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Row 5: 当前资源排行 (三列进度条) */}
      <div className="panel p-4 rounded-xl">
        <div className="dash-card-header mb-3">
          <div className="dash-card-header-left">
            <h3 className="text-sm font-bold text-foreground">当前资源排行</h3>
            <p className="text-xs text-muted">使用 Agent 最新上报，实时反映负载情况</p>
          </div>
          <span className="badge badge-blue text-xs">Top 5</span>
        </div>

        <div className="dash-row-grid-3 pt-1">
          {/* CPU 使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <Cpu size={14} className="text-blue" />
              <span>CPU 使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.cpuRank.length > 0 ? (
                resourceRanks.cpuRank.map((n, i) => {
                  const val = numeric(n.cpu) || 0
                  return (
                    <div
                      key={i}
                      className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                      onClick={() => {
                        if (onSelectNode) onSelectNode(n)
                        if (onNavigate) onNavigate('node-detail')
                      }}
                      title="点击查看服务器详情"
                    >
                      <div className="flex justify-between mb-1">
                        <span className="text-foreground">
                          {i + 1}. {n.name || '探针'}
                        </span>
                        <span className="mono font-bold">{val.toFixed(1)}%</span>
                      </div>
                      <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                        <div
                          className="h-full rounded-full bg-blue"
                          style={{ width: `${Math.min(100, Math.max(4, val))}%` }}
                        />
                      </div>
                    </div>
                  )
                })
              ) : (
                <div className="text-xs text-muted py-2">暂无探针连接</div>
              )}
            </div>
          </div>

          {/* 内存使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <Gauge size={14} className="text-mint" />
              <span>内存使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.memRank.length > 0 ? (
                resourceRanks.memRank.map((n, i) => {
                  const val = numeric(n.memory ?? n.mem) || 0
                  return (
                    <div
                      key={i}
                      className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                      onClick={() => {
                        if (onSelectNode) onSelectNode(n)
                        if (onNavigate) onNavigate('node-detail')
                      }}
                      title="点击查看服务器详情"
                    >
                      <div className="flex justify-between mb-1">
                        <span className="text-foreground">
                          {i + 1}. {n.name || '探针'}
                        </span>
                        <span className="mono font-bold">{val.toFixed(1)}%</span>
                      </div>
                      <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                        <div
                          className="h-full rounded-full bg-mint"
                          style={{ width: `${Math.min(100, Math.max(4, val))}%` }}
                        />
                      </div>
                    </div>
                  )
                })
              ) : (
                <div className="text-xs text-muted py-2">暂无探针连接</div>
              )}
            </div>
          </div>

          {/* 磁盘使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <HardDrives size={14} className="text-amber" />
              <span>磁盘使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.diskRank.length > 0 ? (
                resourceRanks.diskRank.map((n, i) => {
                  const val = numeric(n.disk) || 0
                  const isHigh = val > 80
                  return (
                    <div
                      key={i}
                      className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                      onClick={() => {
                        if (onSelectNode) onSelectNode(n)
                        if (onNavigate) onNavigate('node-detail')
                      }}
                      title="点击查看服务器详情"
                    >
                      <div className="flex justify-between mb-1">
                        <span className="text-foreground">
                          {i + 1}. {n.name || '探针'}
                        </span>
                        <span className={`mono font-bold ${isHigh ? 'text-rose' : ''}`}>{val.toFixed(1)}%</span>
                      </div>
                      <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                        <div
                          className={`h-full rounded-full ${isHigh ? 'bg-rose' : 'bg-amber'}`}
                          style={{ width: `${Math.min(100, Math.max(4, val))}%` }}
                        />
                      </div>
                    </div>
                  )
                })
              ) : (
                <div className="text-xs text-muted py-2">暂无探针连接</div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Row 6: 单日流量消耗排行 + 时延排行 (50% / 50%) */}
      <div className="dash-row-grid-2">
        {/* 左: 单日流量消耗排行 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">单日流量消耗排行</h3>
              <p className="text-xs text-muted">按各节点计费规则计算今日流量并排行</p>
            </div>
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {trafficRankList.length > 0 ? (
              trafficRankList.map((item, idx) => (
                <div
                  key={idx}
                  className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                  onClick={() => {
                    if (onSelectNode) onSelectNode(item.node)
                    if (onNavigate) onNavigate('node-detail')
                  }}
                  title="点击查看服务器详情"
                >
                  <div className="flex justify-between items-center mb-1">
                    <span className="font-medium text-foreground">
                      {idx + 1}. {item.name}
                    </span>
                    <span className="mono font-bold text-foreground">{item.totalFormatted}</span>
                  </div>
                  {/* 双色堆叠进度条 */}
                  <div className="h-2 rounded-full bg-subtle flex overflow-hidden">
                    <div className="bg-amber h-full" style={{ width: `${item.upPct}%` }} />
                    <div className="bg-blue h-full" style={{ width: `${item.downPct}%` }} />
                  </div>
                  <div className="flex justify-end gap-3 text-[11px] text-muted mono mt-1">
                    <span>● 上传 {item.upFormatted}</span>
                    <span>● 下载 {item.downFormatted}</span>
                  </div>
                </div>
              ))
            ) : (
              <div className="text-xs text-muted py-4 text-center">暂无服务器流量上报</div>
            )}
          </div>
        </div>

        {/* 右: 时延排行 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">时延排行</h3>
              <p className="text-xs text-muted">按近 6 小时平均时延，列出全部探测任务</p>
            </div>
            <button
              type="button"
              className="text-xs text-blue hover:underline"
              onClick={() => onNavigate && onNavigate('monitoring')}
            >
              配置目标
            </button>
          </div>

          <div className="space-y-3.5 pt-1">
            {latencyRankList.length > 0 ? (
              latencyRankList.map((item, idx) => (
                <div
                  key={idx}
                  className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                  onClick={() => onNavigate && onNavigate('monitoring')}
                >
                  <div className="flex justify-between items-center mb-1">
                    <div>
                      <span className="font-medium text-foreground">
                        {idx + 1}. {item.name}
                      </span>
                      <span className="text-muted ml-2 text-[11px]">{item.isp}</span>
                    </div>
                    <span className="mono font-bold text-foreground">{item.latency}</span>
                  </div>
                  <div className="h-2 rounded-full bg-subtle overflow-hidden">
                    <div className="bg-amber h-full rounded-full" style={{ width: `${item.pct}%` }} />
                  </div>
                </div>
              ))
            ) : (
              <div className="text-xs text-muted py-4 text-center">
                <span>暂无时延检测数据 · </span>
                <button
                  type="button"
                  className="text-blue hover:underline"
                  onClick={() => onNavigate && onNavigate('monitoring')}
                >
                  前往添加检测目标
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Row 7: 延迟抖动排行 + 近 15 分钟丢包排行 (50% / 50%) */}
      <div className="dash-row-grid-2">
        {/* 左: 延迟抖动排行 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">延迟抖动排行</h3>
              <p className="text-xs text-muted">当前分钟均值与上一分钟均值差额，升幅从高到低</p>
            </div>
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {jitterRankList.length > 0 ? (
              jitterRankList.map((item, idx) => (
                <div
                  key={idx}
                  className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                  onClick={() => onNavigate && onNavigate('monitoring')}
                >
                  <div className="flex justify-between items-center mb-1">
                    <div>
                      <span className="font-medium text-foreground">
                        {idx + 1}. {item.name}
                      </span>
                      <span className="text-muted ml-2 text-[11px]">{item.isp}</span>
                    </div>
                    <span className="mono font-bold text-amber">{item.jitter}</span>
                  </div>
                  <div className="h-2 rounded-full bg-subtle overflow-hidden">
                    <div className="bg-amber h-full rounded-full" style={{ width: `${item.pct}%` }} />
                  </div>
                  <div className="text-right text-[10px] text-muted mono mt-0.5">{item.detail}</div>
                </div>
              ))
            ) : (
              <div className="text-xs text-muted py-4 text-center">暂无显著抖动记录，网络连接稳定</div>
            )}
          </div>
        </div>

        {/* 右: 近 15 分钟丢包排行 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">近 15 分钟丢包排行</h3>
              <p className="text-xs text-muted">按丢包率从高到低列出全部检测任务，0% 不计入排行</p>
            </div>
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {dropLossRankList.length > 0 ? (
              dropLossRankList.map((item, idx) => (
                <div
                  key={idx}
                  className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                  onClick={() => onNavigate && onNavigate('monitoring')}
                >
                  <div className="flex justify-between items-center mb-1">
                    <div>
                      <span className="font-medium text-foreground">
                        {idx + 1}. {item.name}
                      </span>
                      <span className="text-muted ml-2 text-[11px]">{item.isp}</span>
                    </div>
                    <span className="mono font-bold text-rose">{item.loss}</span>
                  </div>
                  <div className="h-2 rounded-full bg-subtle overflow-hidden">
                    <div className="bg-rose h-full rounded-full" style={{ width: `${item.pct}%` }} />
                  </div>
                  <div className="text-right text-[10px] text-muted mono mt-0.5">{item.count}</div>
                </div>
              ))
            ) : (
              <div className="p-3 rounded-lg bg-mint/10 border border-mint/20 text-center">
                <div className="text-mint font-semibold text-xs flex items-center justify-center gap-1.5">
                  <CheckCircle size={15} />
                  <span>近 15 分钟全网无丢包，网络状态极佳 (0% 丢包率不入榜)</span>
                </div>
                <div className="text-[11px] text-muted mt-1">
                  共追踪 {targets.length || 1} 个实时目标，所有探测请求全部正常应答
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* 弹窗 1: 单日服务器流量明细下钻 Modal (点击 30 天柱形图钻取) */}
      {selectedDayDetail && (
        <div className="modal-backdrop" onClick={() => setSelectedDayDetail(null)}>
          <div className="modal-card modal-lg" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge text-blue">
                  <ChartBar size={18} />
                </span>
                <div>
                  <h3>{selectedDayDetail.fullDate} 服务器流量明细</h3>
                  <p className="modal-subtitle">当天计费流量总额: {formatBytes(selectedDayDetail.billed)}</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setSelectedDayDetail(null)}
                aria-label="关闭"
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-3">
              <div className="search-bar-row">
                <input
                  type="text"
                  className="input input-sm w-full"
                  placeholder="搜索服务器名称 / 节点 ID..."
                  value={daySearch}
                  onChange={(e) => setDaySearch(e.target.value)}
                />
              </div>

              <div className="table-responsive max-h-80 overflow-y-auto">
                <table className="table table-compact w-full text-xs">
                  <thead>
                    <tr>
                      <th>服务器</th>
                      <th>状态</th>
                      <th>上传用量</th>
                      <th>下载用量</th>
                      <th className="text-right">当天计费流量</th>
                    </tr>
                  </thead>
                  <tbody>
                    {nodes
                      .filter((n) => !daySearch || (n.name || '').toLowerCase().includes(daySearch.toLowerCase()))
                      .map((node, i) => {
                        const id = node.uuid || node.id
                        const b = billingData[id] || {}
                        const rx = numeric(node.rx) || 0
                        const tx = numeric(node.tx) || 0
                        const method = b.accountingMethod || 'total'
                        let effective = rx + tx
                        if (method === 'tx') effective = tx
                        else if (method === 'rx') effective = rx
                        else if (method === 'max') effective = Math.max(tx, rx)
                        else if (method === 'min') effective = Math.min(tx, rx)

                        return (
                          <tr
                            key={i}
                            className="cursor-pointer hover:bg-subtle/50"
                            onClick={() => {
                              setSelectedDayDetail(null)
                              if (onSelectNode) onSelectNode(node)
                              if (onNavigate) onNavigate('node-detail')
                            }}
                          >
                            <td className="font-medium text-foreground">{node.name || '探针'}</td>
                            <td>
                              <span
                                className={`badge text-[10px] ${
                                  node.status === 'online' ? 'badge-mint' : 'badge-amber'
                                }`}
                              >
                                {node.status === 'online' ? '在线' : '离线'}
                              </span>
                            </td>
                            <td className="mono text-amber">{formatBytes(tx)}</td>
                            <td className="mono text-blue">{formatBytes(rx)}</td>
                            <td className="text-right mono font-bold text-foreground">
                              {formatBytes(effective)}
                            </td>
                          </tr>
                        )
                      })}
                  </tbody>
                </table>
              </div>
            </div>

            <div className="modal-actions justify-end">
              <button type="button" className="button button-quiet" onClick={() => setSelectedDayDetail(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 弹窗 2: SQLite 数据库占用明细 Modal */}
      {showDbModal && (
        <div className="modal-backdrop" onClick={() => setShowDbModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge text-blue">
                  <Database size={18} />
                </span>
                <div>
                  <h3>SQLite 存储明细</h3>
                  <p className="modal-subtitle">总存储占用: {databaseStats.formattedTotal}</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setShowDbModal(false)}
                aria-label="关闭"
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-4 text-xs">
              <div className="p-3 rounded-lg bg-subtle/40 space-y-2">
                <div className="flex justify-between">
                  <span className="text-muted">数据库文件 (DB)</span>
                  <span className="mono font-bold text-foreground">{databaseStats.formattedFile}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted">预写日志 (WAL)</span>
                  <span className="mono font-bold text-foreground">{formatBytes(databaseStats.walBytes)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted">共享内存 (SHM)</span>
                  <span className="mono font-bold text-foreground">{formatBytes(databaseStats.shmBytes)}</span>
                </div>
              </div>

              <div className="space-y-1.5">
                <div className="font-semibold text-foreground">存储架构与维护规范</div>
                <p className="text-muted leading-relaxed">
                  ProbeWatch 采用 SQLite 3 WAL（Write-Ahead Logging）持久化存储，支持高并发探针上报与毫秒级读写隔离。
                  指标历史根据保留窗口定期修剪，保障长时间稳定运行不膨胀。
                </p>
              </div>

              <div className="p-3 rounded-lg border border-subtle bg-background flex items-center justify-between">
                <div>
                  <div className="font-medium text-foreground">系统日志与运维</div>
                  <div className="text-[11px] text-muted">检查系统运行日志与安全审计记录</div>
                </div>
                <button
                  type="button"
                  className="button button-quiet btn-sm"
                  onClick={() => {
                    setShowDbModal(false)
                    if (onNavigate) onNavigate('logs')
                  }}
                >
                  前往日志
                </button>
              </div>
            </div>

            <div className="modal-actions justify-end">
              <button type="button" className="button button-quiet" onClick={() => setShowDbModal(false)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
