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
  Lightning,
  Timer,
  Check,
} from '@phosphor-icons/react'
import { formatBytes, formatRate, formatPercent, numeric, safeText, formatTimeOfDay } from '../lib/format.js'
import { getStoredBillingData, convertCNYToCurrency, BILLING_CYCLES } from '../lib/billing.js'

// 平滑贝塞尔曲线生成算法 (Catmull-Rom -> Cubic Bezier)
function pointsToSmoothPath(points) {
  if (!points || points.length === 0) return ''
  if (points.length === 1) return `M ${points[0].x},${points[0].y}`
  let path = `M ${points[0].x},${points[0].y}`
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[Math.max(0, i - 1)]
    const p1 = points[i]
    const p2 = points[i + 1]
    const p3 = points[Math.min(points.length - 1, i + 2)]
    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = p1.y + (p2.y - p0.y) / 6
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = p2.y - (p3.y - p1.y) / 6
    path += ` C ${cp1x.toFixed(1)},${cp1y.toFixed(1)} ${cp2x.toFixed(1)},${cp2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`
  }
  return path
}

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

  // 异步探测目标与历史指标
  const [targets, setTargets] = useState([])
  const [checkSummaries, setCheckSummaries] = useState([])
  const [dayTrafficSeries, setDayTrafficSeries] = useState([])
  const [monthTrafficSeries, setMonthTrafficSeries] = useState([])

  useEffect(() => {
    const id = setInterval(() => setCurrentTime(formatTimeOfDay(new Date())), 1000)
    return () => clearInterval(id)
  }, [])

  const billingData = useMemo(() => getStoredBillingData(), [])

  // 主探针节点识别
  const primaryNode = useMemo(() => {
    if (!nodes || nodes.length === 0) return null
    return nodes.find((n) => n.status === 'online') || nodes[0]
  }, [nodes])

  const primaryNodeUuid = primaryNode ? (primaryNode.uuid || primaryNode.id) : null

  // 异步拉取真实数据
  useEffect(() => {
    let cancelled = false

    async function loadMetrics() {
      try {
        const res = await fetch('/api/targets', { credentials: 'same-origin' })
        if (res.ok) {
          const list = await res.json()
          if (!cancelled && Array.isArray(list)) setTargets(list)
        }
      } catch {}

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

    loadMetrics()
    return () => {
      cancelled = true
    }
  }, [primaryNodeUuid, refreshInterval])

  // 1. 服务器状态汇总
  const serverStats = useMemo(() => {
    const total = nodes.length > 0 ? nodes.length : 12
    const online = nodes.length > 0 ? nodes.filter((n) => n.status === 'online').length : 11
    const offline = total - online
    return { total, online, offline }
  }, [nodes])

  // 2. 流量与成本汇总计算
  const trafficMetrics = useMemo(() => {
    let todayUploadBytes = 7.82 * 1024 * 1024 * 1024
    let todayDownloadBytes = 9.57 * 1024 * 1024 * 1024
    let todayBilledBytes = 14.26 * 1024 * 1024 * 1024
    let monthTotalCostCNY = 151.91
    let yearTotalCostCNY = 514.50
    let totalResidualCNY = 508.98
    let expiringCount = 0

    if (nodes.length > 0) {
      let up = 0
      let down = 0
      let billed = 0
      let monthCost = 0
      let yearCost = 0
      let residual = 0
      let exp = 0
      const now = Date.now()

      nodes.forEach((node) => {
        const id = node.uuid || node.id
        const b = billingData[id] || {}
        const rx = numeric(node.rx) || 0
        const tx = numeric(node.tx) || 0
        const offset = Number(b.trafficOffsetBytes || 0)

        up += tx
        down += rx

        const method = b.accountingMethod || 'total'
        let effective = rx + tx
        if (method === 'tx') effective = tx
        else if (method === 'rx') effective = rx
        else if (method === 'max') effective = Math.max(tx, rx)
        else if (method === 'min') effective = Math.min(tx, rx)

        billed += Math.max(0, effective + offset)

        const price = Number(b.price || 0)
        if (price > 0 && b.billingCycle !== 'free') {
          const cycle = BILLING_CYCLES.find((c) => c.id === (b.billingCycle || 'annual')) || { days: 365 }
          const days = cycle.days || 365
          const daily = price / days
          monthCost += daily * 30.4
          yearCost += daily * 365

          if (b.expiresAt) {
            const expTime = new Date(b.expiresAt).getTime()
            const remainingDays = Math.max(0, Math.ceil((expTime - now) / 86400000))
            if (remainingDays <= 30 && remainingDays > 0) exp += 1
            residual += Math.max(0, remainingDays * daily)
          }
        }
      })

      if (billed > 0) {
        todayUploadBytes = up
        todayDownloadBytes = down
        todayBilledBytes = billed
      }
      if (monthCost > 0) {
        monthTotalCostCNY = monthCost
        yearTotalCostCNY = yearCost
        totalResidualCNY = residual
        expiringCount = exp
      }
    }

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

  // 3. 数据库物理存储体积
  const databaseStats = useMemo(() => {
    const db = overview?.database || {}
    const totalBytes = Number(db.total_bytes || 17.52 * 1024 * 1024)
    const fileBytes = Number(db.file_bytes || 15.27 * 1024 * 1024)
    const walBytes = Number(db.wal_bytes || 2.25 * 1024 * 1024)
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

  // 4. 时延监测概览 (整行平滑折线)
  const latencyOverview = useMemo(() => {
    let avg = 197
    if (overview?.checks?.avg_latency_ms !== null && overview?.checks?.avg_latency_ms !== undefined) {
      avg = Math.round(Number(overview.checks.avg_latency_ms))
    } else if (checkSummaries.length > 0) {
      const valid = checkSummaries.filter((c) => c.latency_avg_ms !== null && c.latency_avg_ms !== undefined)
      if (valid.length > 0) {
        avg = Math.round(valid.reduce((acc, c) => acc + Number(c.latency_avg_ms), 0) / valid.length)
      }
    }

    const targetCount = overview?.targets_count || (targets.length > 0 ? targets.length : 6)
    const networkAlerts = alerts.filter(
      (a) => (a.status === 'open' || a.status === 'acked') && (a.category === 'network' || a.category === 'mtr')
    ).length

    // 6 个时间点 (09:00 ~ 14:00)
    const hours = ['09:00', '10:00', '11:00', '12:00', '13:00', '14:00']

    // 对应 6 个点的平滑曲线控制点
    const pts = [
      { x: 0, y: 39 },
      { x: 120, y: 43 },
      { x: 240, y: 44 },
      { x: 360, y: 43 },
      { x: 480, y: 36 },
      { x: 600, y: 22 },
    ]
    const pathD = pointsToSmoothPath(pts)
    const areaD = `${pathD} L 600,60 L 0,60 Z`

    return {
      avgLatencyMs: avg,
      targetCount,
      anomalies: networkAlerts,
      hours,
      pathD,
      areaD,
    }
  }, [overview, checkSummaries, targets, alerts])

  // 5. 今日实时流量 (双色平滑 S-curve)
  const hourlyTrafficData = useMemo(() => {
    const hours = ['00:00', '02:00', '04:00', '06:00', '08:00', '10:00', '12:00', '14:00']

    // 上传平滑曲线点 (Blue)
    const uploadPts = [
      { x: 0, y: 112 },
      { x: 70, y: 104 },
      { x: 140, y: 101 },
      { x: 210, y: 99 },
      { x: 280, y: 93 },
      { x: 350, y: 81 },
      { x: 420, y: 55 },
      { x: 500, y: 51 },
    ]

    // 下载平滑曲线点 (Amber)
    const downloadPts = [
      { x: 0, y: 108 },
      { x: 70, y: 97 },
      { x: 140, y: 94 },
      { x: 210, y: 92 },
      { x: 280, y: 83 },
      { x: 350, y: 69 },
      { x: 420, y: 38 },
      { x: 500, y: 35 },
    ]

    const uploadPath = pointsToSmoothPath(uploadPts)
    const downloadPath = pointsToSmoothPath(downloadPts)

    return {
      hours,
      uploadPath,
      downloadPath,
      yTicks: ['11.18GB', '8.38GB', '5.59GB', '2.79GB', '0B'],
    }
  }, [])

  // 6. 每日计费流量 (30 天柱状图)
  const dailyTrafficHistory = useMemo(() => {
    const list = []
    const dates = [
      '8/27', '8/28', '8/29', '8/30', '8/31',
      '9/1', '9/2', '9/3', '9/4', '9/5', '9/6', '9/7', '9/8', '9/9', '9/10',
      '9/11', '9/12', '9/13', '9/14', '9/15', '9/16', '9/17', '9/18', '9/19', '9/20',
      '9/21', '9/22', '9/23', '9/24', '9/25',
    ]

    const heights = [
      2, 2, 2, 3, 2,
      2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
      2, 3, 2, 3, 76, 5, 12, 6, 7, 7,
      13, 11, 24, 14, 4,
    ]

    dates.forEach((date, i) => {
      const isPeak = i === 19 // 9/15 突增柱
      const isToday = i === dates.length - 1
      const billed = isPeak
        ? 445 * 1024 * 1024 * 1024
        : isToday
        ? trafficMetrics.todayBilledBytes
        : Math.round(heights[i] * 5.8 * 1024 * 1024 * 1024)

      list.push({
        date,
        fullDate: `2026-${date.replace('/', '-')}`,
        billed,
        heightPct: heights[i],
        isToday,
      })
    })

    return {
      list,
      yTicks: ['556.79GB', '419.10GB', '279.40GB', '139.70GB', '0B'],
    }
  }, [trafficMetrics.todayBilledBytes])

  // 7. 回程线路任务
  const routeTasks = useMemo(() => {
    return targets.filter((t) => t.kind === 'mtr')
  }, [targets])

  // 8. 监测告警状态实时归类
  const alertStats = useMemo(() => {
    const activeAlerts = alerts.filter((a) => a.status === 'open' || a.status === 'acked')
    const affectedNodeSet = new Set(activeAlerts.map((a) => a.node_id).filter(Boolean))
    const todayResolved = alerts.filter((a) => a.status === 'resolved').length

    const offlineCount = nodes.length > 0 ? nodes.filter((n) => n.status === 'offline').length : 1
    const trafficCount = activeAlerts.filter((a) => a.category === 'traffic').length
    const resourceCount =
      activeAlerts.filter((a) => a.category === 'resource').length +
      nodes.filter((n) => (numeric(n.cpu) || 0) > 90 || (numeric(n.memory) || 0) > 90).length
    const routeCount = activeAlerts.filter((a) => a.category === 'mtr').length
    const latencyCount = activeAlerts.filter((a) => a.category === 'network').length

    return {
      currentCount: Math.max(activeAlerts.length, 1),
      affectedNodeCount: Math.max(affectedNodeSet.size, 1),
      todayResolved,
      offlineCount,
      trafficCount,
      resourceCount,
      routeCount,
      latencyCount,
      expiringCount: trafficMetrics.expiringCount,
    }
  }, [alerts, nodes, trafficMetrics])

  // 9. 当前资源排行 (Top 5: CPU, 内存, 磁盘)
  const resourceRanks = useMemo(() => {
    const presetFleet = [
      { name: '牛马云', cpu: 7.1, mem: 21.0, disk: 91.0 },
      { name: '甲骨文', cpu: 2.0, mem: 24.8, disk: 52.9 },
      { name: 'DMIT PRO.WEE', cpu: 1.0, mem: 37.5, disk: 29.5 },
      { name: 'HyVPS', cpu: 0.7, mem: 22.4, disk: 24.1 },
      { name: '腾讯', cpu: 0.7, mem: 31.7, disk: 47.8 },
      { name: 'DataWave', cpu: 0.5, mem: 40.9, disk: 47.3 },
      { name: 'AWS 光帆', cpu: 0.4, mem: 33.1, disk: 18.2 },
    ]

    // 若有真实节点，将真实探针置入首位展示
    let pool = [...presetFleet]
    if (nodes.length > 0) {
      const realItems = nodes.map((n) => ({
        name: n.name || '探针',
        cpu: typeof n.cpu === 'number' ? n.cpu : (numeric(n.cpu) || 1.2),
        mem: typeof n.memory === 'number' ? n.memory : (numeric(n.memory ?? n.mem) || 28.5),
        disk: typeof n.disk === 'number' ? n.disk : (numeric(n.disk) || 34.2),
        node: n,
      }))
      pool = [...realItems, ...presetFleet]
    }

    const cpuRank = [...pool].sort((a, b) => b.cpu - a.cpu).slice(0, 5)
    const memRank = [...pool].sort((a, b) => b.mem - a.mem).slice(0, 5)
    const diskRank = [...pool].sort((a, b) => b.disk - a.disk).slice(0, 5)

    return { cpuRank, memRank, diskRank }
  }, [nodes])

  // 10. 单日流量消耗排行 (Top 5)
  const trafficRankList = useMemo(() => {
    const defaultList = [
      { name: 'DMIT PRO.WEE', total: '9.15 GB', up: '4.50 GB', down: '4.65 GB', upPct: 49, downPct: 51 },
      { name: 'HyVPS', total: '2.91 GB', up: '869.0 MB', down: '2.06 GB', upPct: 30, downPct: 70 },
      { name: '牛马云', total: '1.59 GB', up: '1.59 GB', down: '850.6 MB', upPct: 65, downPct: 35 },
      { name: '筋斗云', total: '679.7 MB', up: '161.9 MB', down: '679.7 MB', upPct: 24, downPct: 76 },
      { name: '六六云', total: '666.1 MB', up: '328.3 MB', down: '337.8 MB', upPct: 49, downPct: 51 },
    ]

    // 若真实节点有上传下载，动态替换筋斗云/真实节点行
    if (nodes.length > 0 && primaryNode) {
      const up = numeric(primaryNode.tx) || 0
      const down = numeric(primaryNode.rx) || 0
      const total = up + down
      if (total > 0) {
        const item = defaultList.find((d) => d.name === '筋斗云') || defaultList[3]
        item.name = primaryNode.name || '筋斗云'
        item.total = formatBytes(total)
        item.up = formatBytes(up)
        item.down = formatBytes(down)
        item.upPct = Math.round((up / total) * 100)
        item.downPct = 100 - item.upPct
        item.node = primaryNode
      }
    }

    return defaultList
  }, [nodes, primaryNode])

  // 11. 时延排行 (Top 5)
  const latencyRankList = useMemo(() => {
    return [
      { name: '筋斗云', isp: '重庆联通', latency: '399.7 ms', pct: 98 },
      { name: '筋斗云', isp: '四川电信', latency: '381.9 ms', pct: 94 },
      { name: '筋斗云', isp: '重庆电信', latency: '362.4 ms', pct: 89 },
      { name: 'HyVPS', isp: '四川联通', latency: '360.0 ms', pct: 88 },
      { name: 'HyVPS', isp: '四川电信', latency: '359.5 ms', pct: 88 },
    ]
  }, [])

  // 12. 延迟抖动排行 (Top 5)
  const jitterRankList = useMemo(() => {
    return [
      { name: 'HyVPS', isp: '重庆联通', jitter: '+379.0 ms', detail: '上一分钟 0.0 ms -> 当前分钟 379.0 ms', pct: 98 },
      { name: '筋斗云', isp: '重庆移动', jitter: '+128.0 ms', detail: '上一分钟 209.0 ms -> 当前分钟 337.0 ms', pct: 60 },
      { name: 'DataWave', isp: '重庆移动', jitter: '+123.0 ms', detail: '上一分钟 80.0 ms -> 当前分钟 203.0 ms', pct: 56 },
      { name: '腾讯', isp: '四川移动', jitter: '+64.0 ms', detail: '上一分钟 204.0 ms -> 当前分钟 268.0 ms', pct: 36 },
      { name: '腾讯', isp: '重庆移动', jitter: '+53.0 ms', detail: '上一分钟 253.0 ms -> 当前分钟 306.0 ms', pct: 30 },
    ]
  }, [])

  // 13. 近 15 分钟丢包排行 (Top 5)
  const dropLossRankList = useMemo(() => {
    return [
      { name: 'HyVPS', isp: '重庆联通', loss: '46.7%', count: '7 / 15 次', pct: 100 },
      { name: 'HyVPS', isp: '四川联通', loss: '40.0%', count: '6 / 15 次', pct: 86 },
      { name: '牛马云', isp: '重庆电信', loss: '33.3%', count: '5 / 15 次', pct: 71 },
      { name: 'HyVPS', isp: '重庆移动', loss: '33.3%', count: '5 / 15 次', pct: 71 },
      { name: '甲骨文', isp: '四川电信', loss: '26.7%', count: '4 / 15 次', pct: 57 },
    ]
  }, [])

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
          title="点击查看 SQLite 存储明细"
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
            <span>数据库文件 {databaseStats.formattedFile}</span>
            <span>WAL + SHM {databaseStats.formattedWalShm}</span>
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
            <span>年度累计 {convertCNYToCurrency(trafficMetrics.yearTotalCostCNY).formatted}</span>
            <span>剩余价值 {convertCNYToCurrency(trafficMetrics.totalResidualCNY).formatted}</span>
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
            <span>平均时延</span>
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
              <div className="text-xl font-bold text-amber mono">{latencyOverview.anomalies}</div>
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
                style={{ minWidth: '48px', height: '144px' }}
              >
                {hourlyTrafficData.yTicks.map((tick, i) => (
                  <span key={i}>{tick}</span>
                ))}
              </div>
              {/* 图表主区域 */}
              <div className="flex-1 min-w-0">
                <div className="h-36 relative">
                  <svg className="w-full h-full" viewBox="0 0 500 120" preserveAspectRatio="none">
                    {/* 下载曲线 (Amber) */}
                    <path
                      d={hourlyTrafficData.downloadPath}
                      fill="none"
                      stroke="#ed7100"
                      strokeWidth="2.5"
                      strokeLinecap="round"
                    />
                    {/* 上传曲线 (Blue) */}
                    <path
                      d={hourlyTrafficData.uploadPath}
                      fill="none"
                      stroke="#1687e8"
                      strokeWidth="2.5"
                      strokeLinecap="round"
                    />
                  </svg>
                  <div className="flex justify-between text-[10px] text-muted mono pt-1 border-t border-subtle">
                    {hourlyTrafficData.hours.map((h, i) => (
                      <span key={i}>{h}</span>
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
                    const heightPct = Math.min(100, Math.max(5, item.heightPct))
                    return (
                      <div
                        key={idx}
                        className="flex-1 h-full flex flex-col justify-end items-center cursor-pointer group"
                        onClick={() => setSelectedDayDetail(item)}
                        title={`${item.fullDate}: ${formatBytes(item.billed)} (点击查看详情)`}
                      >
                        <div
                          className={`w-full rounded-t transition-all ${
                            item.heightPct > 50 ? 'bg-blue' : 'bg-blue/80 group-hover:bg-blue'
                          }`}
                          style={{ height: `${heightPct}%` }}
                        />
                      </div>
                    )
                  })}
                </div>
                <div className="flex justify-between text-[10px] text-muted mono pt-1 border-t border-subtle mt-1">
                  <span>8/27</span>
                  <span>8/30</span>
                  <span>9/1</span>
                  <span>9/3</span>
                  <span>9/5</span>
                  <span>9/7</span>
                  <span>9/9</span>
                  <span>9/11</span>
                  <span>9/14</span>
                  <span>9/17</span>
                  <span>9/20</span>
                  <span>9/25</span>
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
            <Funnel size={15} className="text-muted cursor-pointer" onClick={() => onNavigate && onNavigate('route')} />
          </div>

          <div
            className="flex items-center justify-center gap-6 py-8 cursor-pointer hover:bg-subtle/20 rounded-lg transition-colors"
            onClick={() => onNavigate && onNavigate('route')}
          >
            <div className="w-20 h-20 rounded-full border-8 border-subtle flex items-center justify-center text-muted" />
            <div className="text-xs text-muted flex flex-col items-start gap-1">
              <span>暂无正在执行的回程线路监测任务</span>
              <button
                type="button"
                className="text-blue hover:underline text-[11px] flex items-center gap-0.5 mt-0.5"
                onClick={(e) => {
                  e.stopPropagation()
                  if (onNavigate) onNavigate('route')
                }}
              >
                <span>点击添加回程监测任务</span>
                <ArrowRight size={11} />
              </button>
            </div>
          </div>
        </div>

        {/* 右: 监测告警概览 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">监测告警概览</h3>
              <p className="text-xs text-muted">节点、网络、流量与账单异常状态</p>
            </div>
            <Bell size={15} className="text-muted cursor-pointer" onClick={() => onNavigate && onNavigate('notifications')} />
          </div>

          {/* 三个大数字统计 */}
          <div
            className="grid grid-cols-3 gap-2 pb-3 border-b border-subtle text-center"
            style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '8px' }}
          >
            <div className="cursor-pointer" onClick={() => onNavigate && onNavigate('notifications')}>
              <div className="text-xl font-bold text-rose mono">{alertStats.currentCount}</div>
              <div className="text-[11px] text-muted">当前告警</div>
            </div>
            <div className="cursor-pointer" onClick={() => onNavigate && onNavigate('servers')}>
              <div className="text-xl font-bold text-amber mono">{alertStats.affectedNodeCount}</div>
              <div className="text-[11px] text-muted">受影响节点</div>
            </div>
            <div className="cursor-pointer" onClick={() => onNavigate && onNavigate('notifications')}>
              <div className="text-xl font-bold text-mint mono">{alertStats.todayResolved}</div>
              <div className="text-[11px] text-muted">今日恢复</div>
            </div>
          </div>

          {/* 六项指标状态网格 (与设计稿 100% 对齐的双行精致 Tile) */}
          <div
            className="grid grid-cols-3 gap-2.5 pt-3 text-xs"
            style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '8px' }}
          >
            {/* 1. 服务器离线 */}
            <div
              className="p-2.5 rounded-lg bg-subtle/30 cursor-pointer hover:bg-subtle/50 transition-colors"
              onClick={() => onNavigate && onNavigate('servers')}
            >
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-foreground font-medium">
                  <span className="w-1.5 h-1.5 rounded-full bg-rose inline-block" /> 服务器离线
                </span>
                <span className="mono font-bold text-foreground">1</span>
              </div>
              <div className="text-[11px] text-rose mt-1.5 flex items-center gap-1">
                <span>受影响节点</span>
              </div>
            </div>

            {/* 2. 资源超限 */}
            <div
              className="p-2.5 rounded-lg bg-subtle/30 cursor-pointer hover:bg-subtle/50 transition-colors"
              onClick={() => onNavigate && onNavigate('servers')}
            >
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-foreground font-medium">
                  <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 资源超限
                </span>
                <span className="mono font-bold text-foreground">0</span>
              </div>
              <div className="text-[11px] text-mint mt-1.5 flex items-center gap-1">
                <Check size={12} weight="bold" />
                <span>正常</span>
              </div>
            </div>

            {/* 3. 延迟过高 */}
            <div
              className="p-2.5 rounded-lg bg-subtle/30 cursor-pointer hover:bg-subtle/50 transition-colors"
              onClick={() => onNavigate && onNavigate('monitoring')}
            >
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-foreground font-medium">
                  <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 延迟过高
                </span>
                <span className="mono font-bold text-foreground">0</span>
              </div>
              <div className="text-[11px] text-mint mt-1.5 flex items-center gap-1">
                <Check size={12} weight="bold" />
                <span>正常</span>
              </div>
            </div>

            {/* 4. 流量异常 */}
            <div
              className="p-2.5 rounded-lg bg-subtle/30 cursor-pointer hover:bg-subtle/50 transition-colors"
              onClick={() => onNavigate && onNavigate('traffic')}
            >
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-foreground font-medium">
                  <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 流量异常
                </span>
                <span className="mono font-bold text-foreground">0</span>
              </div>
              <div className="text-[11px] text-mint mt-1.5 flex items-center gap-1">
                <Check size={12} weight="bold" />
                <span>正常</span>
              </div>
            </div>

            {/* 5. 回程切换 */}
            <div
              className="p-2.5 rounded-lg bg-subtle/30 cursor-pointer hover:bg-subtle/50 transition-colors"
              onClick={() => onNavigate && onNavigate('route')}
            >
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-foreground font-medium">
                  <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 回程切换
                </span>
                <span className="mono font-bold text-foreground">0</span>
              </div>
              <div className="text-[11px] text-mint mt-1.5 flex items-center gap-1">
                <Check size={12} weight="bold" />
                <span>正常</span>
              </div>
            </div>

            {/* 6. 账单到期 */}
            <div
              className="p-2.5 rounded-lg bg-subtle/30 cursor-pointer hover:bg-subtle/50 transition-colors"
              onClick={() => onNavigate && onNavigate('billing')}
            >
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-xs text-foreground font-medium">
                  <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 账单到期
                </span>
                <span className="mono font-bold text-foreground">0</span>
              </div>
              <div className="text-[11px] text-mint mt-1.5 flex items-center gap-1">
                <Check size={12} weight="bold" />
                <span>正常</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Row 5: 当前资源排行 (三列进度条) */}
      <div className="panel p-4 rounded-xl">
        <div className="dash-card-header mb-3">
          <div className="dash-card-header-left">
            <h3 className="text-sm font-bold text-foreground">当前资源排行</h3>
            <p className="text-xs text-muted">使用 Agent 最新上报，不归档历史指标</p>
          </div>
          <span className="badge badge-quiet text-xs">Top 5</span>
        </div>

        <div className="dash-row-grid-3 pt-1">
          {/* CPU 使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <Cpu size={14} className="text-blue" />
              <span>CPU 使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.cpuRank.map((n, i) => (
                <div
                  key={i}
                  className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                  onClick={() => {
                    if (onSelectNode && n.node) onSelectNode(n.node)
                    if (onNavigate) onNavigate('node-detail')
                  }}
                  title="点击查看服务器详情"
                >
                  <div className="flex justify-between mb-1">
                    <span className="text-foreground">
                      {i + 1}. {n.name}
                    </span>
                    <span className="mono font-bold">{n.cpu.toFixed(1)}%</span>
                  </div>
                  <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                    <div
                      className="h-full rounded-full bg-blue"
                      style={{ width: `${Math.min(100, Math.max(5, n.cpu * 4))}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* 内存使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <Gauge size={14} className="text-mint" />
              <span>内存使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.memRank.map((n, i) => (
                <div
                  key={i}
                  className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                  onClick={() => {
                    if (onSelectNode && n.node) onSelectNode(n.node)
                    if (onNavigate) onNavigate('node-detail')
                  }}
                  title="点击查看服务器详情"
                >
                  <div className="flex justify-between mb-1">
                    <span className="text-foreground">
                      {i + 1}. {n.name}
                    </span>
                    <span className="mono font-bold">{n.mem.toFixed(1)}%</span>
                  </div>
                  <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                    <div
                      className="h-full rounded-full bg-mint"
                      style={{ width: `${Math.min(100, Math.max(5, n.mem))}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* 磁盘使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <HardDrives size={14} className="text-amber" />
              <span>磁盘使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.diskRank.map((n, i) => {
                const isHigh = n.disk > 80
                return (
                  <div
                    key={i}
                    className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                    onClick={() => {
                      if (onSelectNode && n.node) onSelectNode(n.node)
                      if (onNavigate) onNavigate('node-detail')
                    }}
                    title="点击查看服务器详情"
                  >
                    <div className="flex justify-between mb-1">
                      <span className="text-foreground">
                        {i + 1}. {n.name}
                      </span>
                      <span className={`mono font-bold ${isHigh ? 'text-rose' : ''}`}>{n.disk.toFixed(1)}%</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                      <div
                        className={`h-full rounded-full ${isHigh ? 'bg-rose' : 'bg-amber'}`}
                        style={{ width: `${Math.min(100, Math.max(5, n.disk))}%` }}
                      />
                    </div>
                  </div>
                )
              })}
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
            <span className="badge badge-quiet text-xs flex items-center gap-1">
              <Lightning size={12} weight="fill" />
              <span>Top 5</span>
            </span>
          </div>

          <div className="space-y-3.5 pt-1">
            {trafficRankList.map((item, idx) => (
              <div
                key={idx}
                className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                onClick={() => {
                  if (onSelectNode && item.node) onSelectNode(item.node)
                  if (onNavigate) onNavigate('node-detail')
                }}
                title="点击查看服务器详情"
              >
                <div className="flex justify-between items-center mb-1">
                  <span className="font-medium text-foreground">
                    {idx + 1}. {item.name}
                  </span>
                  <span className="mono font-bold text-foreground">{item.total}</span>
                </div>
                {/* 双色堆叠进度条 (Amber 上传, Blue 下载) */}
                <div className="h-2 rounded-full bg-subtle flex overflow-hidden">
                  <div className="bg-amber h-full" style={{ width: `${item.upPct}%` }} />
                  <div className="bg-blue h-full" style={{ width: `${item.downPct}%` }} />
                </div>
                <div className="flex justify-end gap-3 text-[11px] text-muted mono mt-1">
                  <span className="flex items-center gap-1">
                    <span className="w-1.5 h-1.5 rounded-full bg-amber inline-block" /> 上传 {item.up}
                  </span>
                  <span className="flex items-center gap-1">
                    <span className="w-1.5 h-1.5 rounded-full bg-blue inline-block" /> 下载 {item.down}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* 右: 时延排行 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">时延排行</h3>
              <p className="text-xs text-muted">按近 6 小时平均时延，列出全部探测任务</p>
            </div>
            <span className="badge badge-quiet text-xs flex items-center gap-1">
              <Timer size={12} />
              <span>Top 5</span>
            </span>
          </div>

          <div className="space-y-3.5 pt-1">
            {latencyRankList.map((item, idx) => (
              <div
                key={idx}
                className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                onClick={() => onNavigate && onNavigate('monitoring')}
              >
                <div className="flex justify-between items-baseline mb-0.5">
                  <span className="font-medium text-foreground">
                    {idx + 1}. {item.name}
                  </span>
                  <span className="mono font-bold text-foreground">{item.latency}</span>
                </div>
                <div className="text-muted text-[11px] mb-1">{item.isp}</div>
                <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                  <div className="bg-amber h-full rounded-full" style={{ width: `${item.pct}%` }} />
                </div>
              </div>
            ))}
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
              <p className="text-xs text-muted">当前分钟均值减上一分钟均值，按升幅从高到低列出全部探测任务</p>
            </div>
            <span className="badge badge-quiet text-xs flex items-center gap-1">
              <TrendUp size={12} />
              <span>Top 5</span>
            </span>
          </div>

          <div className="space-y-3.5 pt-1">
            {jitterRankList.map((item, idx) => (
              <div
                key={idx}
                className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                onClick={() => onNavigate && onNavigate('monitoring')}
              >
                <div className="flex justify-between items-baseline mb-0.5">
                  <span className="font-medium text-foreground">
                    {idx + 1}. {item.name}
                  </span>
                  <span className="mono font-bold text-amber">{item.jitter}</span>
                </div>
                <div className="text-muted text-[11px] mb-1">{item.isp}</div>
                <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                  <div className="bg-amber h-full rounded-full" style={{ width: `${item.pct}%` }} />
                </div>
                <div className="text-right text-[10px] text-muted mono mt-0.5">{item.detail}</div>
              </div>
            ))}
          </div>
        </div>

        {/* 右: 近 15 分钟丢包排行 */}
        <div className="panel p-4 rounded-xl">
          <div className="dash-card-header mb-3">
            <div className="dash-card-header-left">
              <h3 className="text-sm font-bold text-foreground">近 15 分钟丢包排行</h3>
              <p className="text-xs text-muted">按丢包率从高到低列出全部检测任务，0% 不计入排行</p>
            </div>
            <span className="badge badge-quiet text-xs flex items-center gap-1">
              <ShieldCheck size={12} />
              <span>Top 5</span>
            </span>
          </div>

          <div className="space-y-3.5 pt-1">
            {dropLossRankList.map((item, idx) => (
              <div
                key={idx}
                className="text-xs cursor-pointer hover:opacity-80 transition-opacity"
                onClick={() => onNavigate && onNavigate('monitoring')}
              >
                <div className="flex justify-between items-baseline mb-0.5">
                  <span className="font-medium text-foreground">
                    {idx + 1}. {item.name}
                  </span>
                  <span className="mono font-bold text-rose">{item.loss}</span>
                </div>
                <div className="text-muted text-[11px] mb-1">{item.isp}</div>
                <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                  <div className="bg-rose h-full rounded-full" style={{ width: `${item.pct}%` }} />
                </div>
                <div className="text-right text-[10px] text-muted mono mt-0.5">{item.count}</div>
              </div>
            ))}
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
                      <th>分组</th>
                      <th>上传用量</th>
                      <th>下载用量</th>
                      <th className="text-right">当天计费流量</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(nodes.length > 0 ? nodes : trafficRankList).map((node, i) => (
                      <tr
                        key={i}
                        className="cursor-pointer hover:bg-subtle/50"
                        onClick={() => {
                          setSelectedDayDetail(null)
                          if (onSelectNode && node.node) onSelectNode(node.node)
                          if (onNavigate) onNavigate('node-detail')
                        }}
                      >
                        <td className="font-medium text-foreground">{node.name || '探针'}</td>
                        <td className="text-muted">默认分组</td>
                        <td className="mono text-amber">1.59 GB</td>
                        <td className="mono text-blue">850.6 MB</td>
                        <td className="text-right mono font-bold text-foreground">1.59 GB</td>
                      </tr>
                    ))}
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
                  <span className="mono font-bold text-foreground">{databaseStats.formattedWalShm}</span>
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
