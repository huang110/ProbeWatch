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
  const [daySearch, setDaySearch] = useState('')
  const [currentTime, setCurrentTime] = useState(() => formatTimeOfDay(new Date()))

  useEffect(() => {
    const id = setInterval(() => setCurrentTime(formatTimeOfDay(new Date())), 1000)
    return () => clearInterval(id)
  }, [])

  const billingData = useMemo(() => getStoredBillingData(), [])

  // 1. 服务器汇总状态
  const serverStats = useMemo(() => {
    const total = nodes.length || 12
    const online = nodes.length ? nodes.filter((n) => n.status === 'online').length : 11
    const offline = total - online
    return { total, online, offline }
  }, [nodes])

  // 2. 流量与成本汇总计算
  const trafficMetrics = useMemo(() => {
    let todayUploadBytes = 4.93 * 1024 * 1024 * 1024
    let todayDownloadBytes = 6.31 * 1024 * 1024 * 1024
    let todayBilledBytes = 8.29 * 1024 * 1024 * 1024
    let monthTotalCostCNY = 151.91
    let yearTotalCostCNY = 514.50
    let totalResidualCNY = 509.50
    let expiringCount = 4

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

  // 3. 30 天每日计费流量数据
  const dailyTrafficHistory = useMemo(() => {
    const list = []
    const now = new Date()
    for (let i = 29; i >= 0; i--) {
      const d = new Date(now)
      d.setDate(d.getDate() - i)
      const dateStr = `${d.getMonth() + 1}/${d.getDate()}`
      const isToday = i === 0
      const isPeak = i === 11 // 类似原版图上的高峰柱
      let billed = 0
      if (isToday) {
        billed = trafficMetrics.todayBilledBytes
      } else if (isPeak) {
        billed = 445 * 1024 * 1024 * 1024
      } else if (i < 11) {
        billed = Math.round((35 + Math.sin(i * 1.2) * 20 + (i % 3) * 15) * 1024 * 1024 * 1024)
      } else {
        billed = Math.round((2 + Math.random() * 4) * 1024 * 1024 * 1024)
      }
      list.push({
        date: dateStr,
        fullDate: d.toISOString().slice(0, 10),
        billed,
        isToday,
      })
    }
    return list
  }, [trafficMetrics.todayBilledBytes])

  // 4. 榜单数据 (CPU, 内存, 磁盘)
  const resourceRanks = useMemo(() => {
    const mockNodes = [
      { name: '牛马云', cpu: 14.8, mem: 41.3, disk: 91.0 },
      { name: '甲骨文', cpu: 3.7, mem: 24.7, disk: 52.9 },
      { name: '筋斗云', cpu: 2.0, mem: 33.6, disk: 47.8 },
      { name: 'HyVPS', cpu: 1.7, mem: 31.6, disk: 47.4 },
      { name: '腾讯', cpu: 0.5, mem: 40.9, disk: 29.4 },
    ]

    const list = nodes.length > 0 ? nodes : mockNodes
    const cpuRank = [...list].sort((a, b) => (b.cpu ?? 0) - (a.cpu ?? 0)).slice(0, 5)
    const memRank = [...list].sort((a, b) => ((b.memory ?? b.mem) ?? 0) - ((a.memory ?? a.mem) ?? 0)).slice(0, 5)
    const diskRank = [...list].sort((a, b) => ((b.disk) ?? 0) - ((a.disk) ?? 0)).slice(0, 5)

    return { cpuRank, memRank, diskRank }
  }, [nodes])

  // 5. 单日流量消耗排行 (Top 5)
  const trafficRankList = useMemo(() => {
    const defaultList = [
      { name: 'DMIT PRO.WEE', total: '3.93 GB', up: '1.94 GB', down: '1.99 GB', upPct: 49, downPct: 51 },
      { name: 'HyVPS', total: '2.72 GB', up: '818.8 MB', down: '1.92 GB', upPct: 30, downPct: 70 },
      { name: '牛马云', total: '1.52 GB', up: '1.02 GB', down: '806.6 MB', upPct: 55, downPct: 45 },
      { name: '筋斗云', total: '552.9 MB', up: '147.2 MB', down: '552.9 MB', upPct: 21, downPct: 79 },
      { name: '六六云', total: '358.5 MB', up: '175.7 MB', down: '182.8 MB', upPct: 49, downPct: 51 },
    ]
    return defaultList
  }, [])

  // 6. 时延排行 (Top 5)
  const latencyRankList = useMemo(() => {
    return [
      { name: '筋斗云', isp: '四川电信', latency: '380.8 ms', pct: 98 },
      { name: '筋斗云', isp: '重庆联通', latency: '377.8 ms', pct: 97 },
      { name: '筋斗云', isp: '重庆电信', latency: '372.7 ms', pct: 95 },
      { name: 'HyVPS', isp: '四川电信', latency: '357.3 ms', pct: 91 },
      { name: 'HyVPS', isp: '四川联通', latency: '355.1 ms', pct: 90 },
    ]
  }, [])

  // 7. 延迟抖动排行 (Top 5)
  const jitterRankList = useMemo(() => {
    return [
      { name: 'HyVPS', isp: '重庆移动', jitter: '+209.0 ms', detail: '上一分钟 0.0 ms -> 当前分钟 209.0 ms', pct: 95 },
      { name: '甲骨文', isp: '重庆电信', jitter: '+174.0 ms', detail: '上一分钟 172.0 ms -> 当前分钟 346.0 ms', pct: 80 },
      { name: 'DataWave', isp: '四川移动', jitter: '+129.0 ms', detail: '上一分钟 71.0 ms -> 当前分钟 200.0 ms', pct: 60 },
      { name: '腾讯', isp: '四川移动', jitter: '+106.0 ms', detail: '上一分钟 185.0 ms -> 当前分钟 291.0 ms', pct: 50 },
      { name: '筋斗云', isp: '重庆联通', jitter: '+89.0 ms', detail: '上一分钟 388.0 ms -> 当前分钟 477.0 ms', pct: 42 },
    ]
  }, [])

  // 8. 近 15 分钟丢包排行 (Top 5)
  const dropLossRankList = useMemo(() => {
    return [
      { name: 'HyVPS', isp: '四川移动', loss: '40.0%', count: '6 / 15 次', pct: 100 },
      { name: 'HyVPS', isp: '重庆联通', loss: '33.3%', count: '5 / 15 次', pct: 83 },
      { name: 'HyVPS', isp: '四川联通', loss: '26.7%', count: '4 / 15 次', pct: 67 },
      { name: '甲骨文', isp: '重庆电信', loss: '20.0%', count: '3 / 15 次', pct: 50 },
      { name: 'HyVPS', isp: '重庆移动', loss: '20.0%', count: '3 / 15 次', pct: 50 },
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
        <div className="panel p-4 rounded-xl">
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted">数据库占用</span>
            <span className="p-1.5 rounded-lg bg-subtle text-muted">
              <Database size={16} />
            </span>
          </div>
          <div className="text-2xl font-bold mono mt-2 text-foreground">17.48 MB</div>
          <div className="flex items-center justify-between text-xs mt-3 text-muted">
            <span>数据库文件 15.30 MB</span>
            <span>WAL + SHM 2.18 MB</span>
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
          <div className="flex items-center gap-1.5 text-xs text-blue font-medium">
            <span className="w-2 h-2 rounded-full bg-blue inline-block" />
            <span>平均时延</span>
          </div>
        </div>

        <div className="dash-latency-overview pt-1">
          {/* 左侧三个微型指标: 水平并排 */}
          <div className="dash-latency-sidebar">
            <div className="dash-latency-stat">
              <div className="text-xl font-bold text-blue mono">194 ms</div>
              <div className="text-[11px] text-muted">平均时延</div>
            </div>
            <div className="dash-latency-stat">
              <div className="text-xl font-bold text-foreground mono">6</div>
              <div className="text-[11px] text-muted">监测目标</div>
            </div>
            <div className="dash-latency-stat">
              <div className="text-xl font-bold text-amber mono">0</div>
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
                <path
                  d="M 0,42 C 100,45 200,39 300,41 C 400,42 500,36 600,24 L 600,60 L 0,60 Z"
                  fill="url(#latencyAreaGrad)"
                />
                <path
                  d="M 0,42 C 100,45 200,39 300,41 C 400,42 500,36 600,24"
                  fill="none"
                  stroke="#1687e8"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                />
              </svg>
            </div>
            <div className="flex justify-between text-[11px] text-muted mono pt-1.5">
              <span>06:00</span>
              <span>07:00</span>
              <span>08:00</span>
              <span>09:00</span>
              <span>10:00</span>
              <span>11:00</span>
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
              <div className="flex flex-col justify-between text-[10px] text-muted mono text-right pr-1 pb-4 select-none" style={{ minWidth: '46px', height: '144px' }}>
                <span>7.45GB</span>
                <span>5.59GB</span>
                <span>3.73GB</span>
                <span>1.86GB</span>
                <span>0B</span>
              </div>
              {/* 图表主区域 */}
              <div className="flex-1 min-w-0">
                <div className="h-36 relative">
                  <svg className="w-full h-full" viewBox="0 0 500 120" preserveAspectRatio="none">
                    {/* Dual Splines */}
                    <path
                      d="M 0,110 C 50,70 120,65 200,62 C 300,58 380,45 500,28"
                      fill="none"
                      stroke="#ed7100"
                      strokeWidth="2"
                    />
                    <path
                      d="M 0,115 C 60,78 140,75 220,72 C 320,68 400,52 500,42"
                      fill="none"
                      stroke="#1687e8"
                      strokeWidth="2"
                    />
                  </svg>
                  <div className="flex justify-between text-[10px] text-muted mono pt-1 border-t border-subtle">
                    <span>00:00</span>
                    <span>01:00</span>
                    <span>02:00</span>
                    <span>03:00</span>
                    <span>04:00</span>
                    <span>05:00</span>
                    <span>06:00</span>
                    <span>07:00</span>
                    <span>08:00</span>
                    <span>09:00</span>
                    <span>10:00</span>
                    <span>11:00</span>
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
            <div className="flex flex-col justify-between text-[10px] text-muted mono text-right pr-1 pb-4 select-none" style={{ minWidth: '56px', height: '144px' }}>
              <span>556.79GB</span>
              <span>419.10GB</span>
              <span>279.40GB</span>
              <span>139.70GB</span>
              <span>0B</span>
            </div>
            {/* 柱状图主区域 */}
            <div className="flex-1 min-w-0">
              <div className="h-36 flex flex-col justify-end">
                <div className="flex items-end justify-between h-full gap-1">
                  {dailyTrafficHistory.map((item, idx) => {
                    const maxVal = Math.max(...dailyTrafficHistory.map((d) => d.billed), 1)
                    const heightPct = Math.min(100, Math.max(6, Math.round((item.billed / maxVal) * 100)))
                    return (
                      <div
                        key={idx}
                        className="flex-1 h-full flex flex-col justify-end items-center cursor-pointer group"
                        onClick={() => setSelectedDayDetail(item)}
                        title={`${item.fullDate}: ${formatBytes(item.billed)}`}
                      >
                        <div
                          className={`w-full rounded-t transition-all ${item.isToday ? 'bg-blue' : 'bg-blue/80 group-hover:bg-blue'}`}
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
            <Funnel size={15} className="text-muted" />
          </div>

          <div className="flex items-center justify-center gap-6 py-8">
            <div className="w-24 h-24 rounded-full border-8 border-subtle flex items-center justify-center text-muted" />
            <div className="text-xs text-muted">
              暂无正在执行的回程线路监测任务
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
            <Bell size={15} className="text-muted" />
          </div>

          {/* 三个大数字统计 */}
          <div className="grid grid-cols-3 gap-2 pb-3 border-b border-subtle text-center" style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '8px' }}>
            <div>
              <div className="text-xl font-bold text-rose mono">1</div>
              <div className="text-[11px] text-muted">当前告警</div>
            </div>
            <div>
              <div className="text-xl font-bold text-amber mono">1</div>
              <div className="text-[11px] text-muted">受影响节点</div>
            </div>
            <div>
              <div className="text-xl font-bold text-mint mono">0</div>
              <div className="text-[11px] text-muted">今日恢复</div>
            </div>
          </div>

          {/* 六项指标状态网格 */}
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2.5 pt-3 text-xs" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '10px' }}>
            <div className="flex items-center justify-between p-2 rounded bg-subtle/40">
              <span className="flex items-center gap-1.5 text-rose">
                <span className="w-1.5 h-1.5 rounded-full bg-rose inline-block" /> 服务器离线
              </span>
              <span className="mono font-bold text-foreground">1</span>
            </div>

            <div className="flex items-center justify-between p-2 rounded bg-subtle/40">
              <span className="flex items-center gap-1.5 text-mint">
                <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 流量异常
              </span>
              <span className="mono font-bold text-foreground">0</span>
            </div>

            <div className="flex items-center justify-between p-2 rounded bg-subtle/40">
              <span className="flex items-center gap-1.5 text-mint">
                <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 资源超限
              </span>
              <span className="mono font-bold text-foreground">1</span>
            </div>

            <div className="flex items-center justify-between p-2 rounded bg-subtle/40">
              <span className="flex items-center gap-1.5 text-mint">
                <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 回程切换
              </span>
              <span className="mono font-bold text-foreground">0</span>
            </div>

            <div className="flex items-center justify-between p-2 rounded bg-subtle/40">
              <span className="flex items-center gap-1.5 text-mint">
                <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 延迟过高
              </span>
              <span className="mono font-bold text-foreground">0</span>
            </div>

            <div className="flex items-center justify-between p-2 rounded bg-subtle/40">
              <span className="flex items-center gap-1.5 text-mint">
                <span className="w-1.5 h-1.5 rounded-full bg-mint inline-block" /> 账单到期
              </span>
              <span className="mono font-bold text-foreground">0</span>
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
              {resourceRanks.cpuRank.map((n, i) => {
                const val = typeof n.cpu === 'number' ? n.cpu : 0
                return (
                  <div key={i} className="text-xs">
                    <div className="flex justify-between mb-1">
                      <span className="text-foreground">{i + 1}. {n.name}</span>
                      <span className="mono font-bold">{val.toFixed(1)}%</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                      <div className="h-full rounded-full bg-blue" style={{ width: `${Math.min(100, val * 3)}%` }} />
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          {/* 内存使用率 */}
          <div>
            <div className="flex items-center gap-1.5 text-xs font-semibold text-muted mb-3">
              <Gauge size={14} className="text-mint" />
              <span>内存使用率</span>
            </div>
            <div className="space-y-3">
              {resourceRanks.memRank.map((n, i) => {
                const val = typeof n.memory === 'number' ? n.memory : (n.mem || 30)
                return (
                  <div key={i} className="text-xs">
                    <div className="flex justify-between mb-1">
                      <span className="text-foreground">{i + 1}. {n.name}</span>
                      <span className="mono font-bold">{val.toFixed(1)}%</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                      <div className="h-full rounded-full bg-mint" style={{ width: `${Math.min(100, val)}%` }} />
                    </div>
                  </div>
                )
              })}
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
                const val = typeof n.disk === 'number' ? n.disk : 50
                const isHigh = val > 80
                return (
                  <div key={i} className="text-xs">
                    <div className="flex justify-between mb-1">
                      <span className="text-foreground">{i + 1}. {n.name}</span>
                      <span className={`mono font-bold ${isHigh ? 'text-rose' : ''}`}>{val.toFixed(1)}%</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-subtle overflow-hidden">
                      <div className={`h-full rounded-full ${isHigh ? 'bg-rose' : 'bg-amber'}`} style={{ width: `${Math.min(100, val)}%` }} />
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
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {trafficRankList.map((item, idx) => (
              <div key={idx} className="text-xs">
                <div className="flex justify-between items-center mb-1">
                  <span className="font-medium text-foreground">{idx + 1}. {item.name}</span>
                  <span className="mono font-bold text-foreground">{item.total}</span>
                </div>
                {/* 双色堆叠进度条 */}
                <div className="h-2 rounded-full bg-subtle flex overflow-hidden">
                  <div className="bg-amber h-full" style={{ width: `${item.upPct}%` }} />
                  <div className="bg-blue h-full" style={{ width: `${item.downPct}%` }} />
                </div>
                <div className="flex justify-end gap-3 text-[11px] text-muted mono mt-1">
                  <span>● 上传 {item.up}</span>
                  <span>● 下载 {item.down}</span>
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
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {latencyRankList.map((item, idx) => (
              <div key={idx} className="text-xs">
                <div className="flex justify-between items-center mb-1">
                  <div>
                    <span className="font-medium text-foreground">{idx + 1}. {item.name}</span>
                    <span className="text-muted ml-2 text-[11px]">{item.isp}</span>
                  </div>
                  <span className="mono font-bold text-foreground">{item.latency}</span>
                </div>
                <div className="h-2 rounded-full bg-subtle overflow-hidden">
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
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {jitterRankList.map((item, idx) => (
              <div key={idx} className="text-xs">
                <div className="flex justify-between items-center mb-1">
                  <div>
                    <span className="font-medium text-foreground">{idx + 1}. {item.name}</span>
                    <span className="text-muted ml-2 text-[11px]">{item.isp}</span>
                  </div>
                  <span className="mono font-bold text-amber">{item.jitter}</span>
                </div>
                <div className="h-2 rounded-full bg-subtle overflow-hidden">
                  <div className="bg-amber h-full rounded-full" style={{ width: `${item.pct}%` }} />
                </div>
                <div className="text-right text-[10px] text-muted mono mt-0.5">
                  {item.detail}
                </div>
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
            <span className="badge badge-quiet text-xs">Top 5</span>
          </div>

          <div className="space-y-3.5 pt-1">
            {dropLossRankList.map((item, idx) => (
              <div key={idx} className="text-xs">
                <div className="flex justify-between items-center mb-1">
                  <div>
                    <span className="font-medium text-foreground">{idx + 1}. {item.name}</span>
                    <span className="text-muted ml-2 text-[11px]">{item.isp}</span>
                  </div>
                  <span className="mono font-bold text-rose">{item.loss}</span>
                </div>
                <div className="h-2 rounded-full bg-subtle overflow-hidden">
                  <div className="bg-rose h-full rounded-full" style={{ width: `${item.pct}%` }} />
                </div>
                <div className="text-right text-[10px] text-muted mono mt-0.5">
                  {item.count}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* 单日服务器流量明细下钻 Modal (点击 30 天柱形图钻取) */}
      {selectedDayDetail && (
        <div className="modal-backdrop" onClick={() => setSelectedDayDetail(null)}>
          <div className="modal-card modal-lg" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge text-blue"><ChartBar size={18} /></span>
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
                  placeholder="搜索服务器名称 / 分组..."
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
                      <tr key={i}>
                        <td className="font-medium text-foreground">{node.name}</td>
                        <td className="text-muted">默认分组</td>
                        <td className="mono text-amber">1.02 GB</td>
                        <td className="mono text-blue">806.6 MB</td>
                        <td className="text-right mono font-bold text-foreground">1.52 GB</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            <div className="modal-actions justify-end">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setSelectedDayDetail(null)}
              >
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
