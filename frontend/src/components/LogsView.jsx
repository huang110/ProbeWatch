import { useState, useMemo } from 'react'
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
  Tag
} from '@phosphor-icons/react'
import { formatAlertTime } from '../lib/format.js'

const MOCK_SYSTEM_LOGS = [
  {
    id: 'log-10082',
    uuid: 'a89f2130-9b4e-4f11-827c-01b87a82c001',
    type: 'billing_change',
    typeLabel: '账单变更',
    severity: 'info',
    ip: '198.51.100.88',
    userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/128.0',
    summary: '管理员更新了 [筋斗云主探针] 的账单资费及流量口径配置',
    timestamp: Date.now() - 120000,
    payload: {
      action: 'update_billing',
      node_id: 'jindouyun-01',
      price: 39.9,
      currency: 'USD',
      accounting_method: 'max',
      traffic_quota_gb: 1500,
      traffic_offset_bytes: 0,
      auto_renew: true,
    },
  },
  {
    id: 'log-10081',
    uuid: 'b71a5401-2e11-4560-911a-12d8a011a002',
    type: 'node_op',
    typeLabel: '节点操作',
    severity: 'info',
    ip: '198.51.100.88',
    userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/128.0',
    summary: '成功为节点 [筋斗云主探针] 执行了流量校准 Offset (+28.4 GB)',
    timestamp: Date.now() - 480000,
    payload: {
      action: 'calibrate_traffic',
      node_id: 'jindouyun-01',
      applied_offset_bytes: 30494261248,
      reason: '对齐商家后台计费已用量',
      operator: 'admin',
    },
  },
  {
    id: 'log-10080',
    uuid: 'c82b9912-3f22-4911-8012-34f9a022b003',
    type: 'security_block',
    typeLabel: '安全拦截',
    severity: 'warning',
    ip: '203.0.113.45',
    userAgent: 'Go-http-client/1.1',
    summary: '拦截未授权越权请求: 尝试未通过 2FA 访问 /api/nodes/enroll/token',
    timestamp: Date.now() - 1500000,
    payload: {
      action: 'security_intercept',
      path: '/api/nodes/enroll/token',
      method: 'POST',
      status_code: 401,
      reason: 'Missing session authentication cookie or valid CSRF token',
    },
  },
  {
    id: 'log-10079',
    uuid: 'd93c1023-4a33-4122-9223-56a0b133c004',
    type: 'network_alert',
    typeLabel: '网络异常',
    severity: 'warning',
    ip: '127.0.0.1',
    userAgent: 'ProbeWatch-Scheduler/1.0',
    summary: '触发网络延迟告警: [筋斗云主探针] 至 国内联通 探测连续丢包超 15%',
    timestamp: Date.now() - 3600000,
    payload: {
      event: 'loss_threshold_exceeded',
      target: 'CU-Shanghai-9929',
      current_loss: 0.18,
      threshold: 0.15,
      carrier: 'unicom',
    },
  },
  {
    id: 'log-10078',
    uuid: 'e04d2134-5b44-4233-8334-67b1c244d005',
    type: 'node_op',
    typeLabel: '节点操作',
    severity: 'info',
    ip: '198.51.100.88',
    userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/128.0',
    summary: '管理员生成了新节点 [筋斗云主探针] 的部署指令并分发专属 Token',
    timestamp: Date.now() - 7200000,
    payload: {
      action: 'generate_enroll_token',
      node_name: '筋斗云主探针',
      token_prefix: 'pb_reg_7U7o***',
      os_target: 'linux_amd64',
    },
  },
  {
    id: 'log-10077',
    uuid: 'f15e3245-6c55-4344-9445-78c2d355e006',
    type: 'system_runtime',
    typeLabel: '系统运行',
    severity: 'info',
    ip: '127.0.0.1',
    userAgent: 'ProbeWatch-Core/0.2.4',
    summary: '系统完成 SQLite WAL 检查点归档 (Checkpoint Pass), WAL 文件收缩',
    timestamp: Date.now() - 14400000,
    payload: {
      event: 'sqlite_wal_checkpoint',
      database: 'probewatch.db',
      wal_size_before_bytes: 4194304,
      wal_size_after_bytes: 65536,
      duration_ms: 12.4,
    },
  },
]

export function LogsView() {
  const [selectedTypes, setSelectedTypes] = useState([])
  const [searchQuery, setSearchQuery] = useState('')
  const [timeFilter, setTimeFilter] = useState('all') // 'all' | '1d' | '3d' | '7d'
  const [activeLogDetail, setActiveLogDetail] = useState(null)

  const typesList = [
    { id: 'node_op', label: '节点操作' },
    { id: 'billing_change', label: '账单变更' },
    { id: 'network_alert', label: '网络异常' },
    { id: 'security_block', label: '安全拦截' },
    { id: 'system_runtime', label: '系统运行' },
  ]

  // 类型计数
  const typeCounts = useMemo(() => {
    const counts = {}
    typesList.forEach((t) => { counts[t.id] = 0 })
    MOCK_SYSTEM_LOGS.forEach((l) => {
      if (counts[l.type] !== undefined) counts[l.type] += 1
    })
    return counts
  }, [])

  const handleToggleType = (typeId) => {
    setSelectedTypes((prev) =>
      prev.includes(typeId) ? prev.filter((t) => t !== typeId) : [...prev, typeId]
    )
  }

  const handleClearFilters = () => {
    setSelectedTypes([])
    setSearchQuery('')
    setTimeFilter('all')
  }

  // 服务端过滤模拟
  const filteredLogs = useMemo(() => {
    const now = Date.now()
    const timeLimitMs =
      timeFilter === '1d' ? 86400000 : timeFilter === '3d' ? 3 * 86400000 : timeFilter === '7d' ? 7 * 86400000 : 0

    return MOCK_SYSTEM_LOGS.filter((log) => {
      if (selectedTypes.length > 0 && !selectedTypes.includes(log.type)) return false
      if (timeLimitMs > 0 && now - log.timestamp > timeLimitMs) return false
      if (searchQuery) {
        const q = searchQuery.toLowerCase()
        const match =
          log.id.toLowerCase().includes(q) ||
          log.ip.toLowerCase().includes(q) ||
          log.summary.toLowerCase().includes(q)
        if (!match) return false
      }
      return true
    })
  }, [selectedTypes, timeFilter, searchQuery])

  const hasActiveFilters = selectedTypes.length > 0 || searchQuery !== '' || timeFilter !== 'all'

  return (
    <div className="logs-view-container space-y-4">
      {/* 顶部搜索与筛选区 */}
      <div className="panel p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-bold flex items-center gap-2">
              <Scroll size={22} className="text-blue" />
              系统与操作日志 (Logs & Audit)
            </h2>
            <p className="text-xs text-muted">
              记录主控系统运行、管理操作行为、网络事件与安全拦截 · 本地时区自动转换 · 支持全量检索
            </p>
          </div>

          <div className="flex items-center gap-2">
            <div className="search-bar-wrap w-64">
              <input
                type="text"
                className="input input-sm w-full"
                placeholder="搜索 IP / 日志内容 / ID..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
              />
            </div>
          </div>
        </div>

        {/* 筛选标签行 */}
        <div className="flex flex-wrap items-center gap-2 pt-1 border-t border-subtle">
          <span className="text-xs text-muted flex items-center gap-1 font-medium">
            <Funnel size={14} /> 分类类型:
          </span>
          {typesList.map((t) => {
            const isSelected = selectedTypes.includes(t.id)
            return (
              <button
                key={t.id}
                type="button"
                className={`preset-pill ${isSelected ? 'preset-pill-active' : ''}`}
                onClick={() => handleToggleType(t.id)}
              >
                <span>{t.label}</span>
                <span className="badge-count mono">{typeCounts[t.id] || 0}</span>
              </button>
            )
          })}

          <span className="text-muted mx-1">|</span>

          <span className="text-xs text-muted">时间:</span>
          {[
            { id: 'all', label: '全部' },
            { id: '1d', label: '今天' },
            { id: '3d', label: '近3天' },
            { id: '7d', label: '近7天' },
          ].map((tf) => (
            <button
              key={tf.id}
              type="button"
              className={`preset-pill ${timeFilter === tf.id ? 'preset-pill-active' : ''}`}
              onClick={() => setTimeFilter(tf.id)}
            >
              {tf.label}
            </button>
          ))}

          {hasActiveFilters && (
            <button
              type="button"
              className="button button-quiet text-rose btn-sm ml-auto"
              onClick={handleClearFilters}
            >
              <X size={14} />
              <span>清空所有筛选</span>
            </button>
          )}
        </div>
      </div>

      {/* 日志清单表格 */}
      <div className="panel p-5">
        <div className="table-responsive">
          <table className="table w-full text-xs">
            <thead>
              <tr>
                <th className="w-24">日志 ID</th>
                <th className="w-36">发生时间</th>
                <th className="w-32">来源 IP</th>
                <th className="w-28">事件类型</th>
                <th>摘要内容</th>
                <th className="text-right w-20">详情</th>
              </tr>
            </thead>
            <tbody>
              {filteredLogs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="text-center py-8 text-muted">
                    未找到匹配当前筛选条件的日志事件
                  </td>
                </tr>
              ) : (
                filteredLogs.map((log) => (
                  <tr key={log.id} className="hover:bg-subtle/50 transition-colors">
                    <td>
                      <button
                        type="button"
                        className="mono text-blue font-bold hover:underline cursor-pointer"
                        onClick={() => setActiveLogDetail(log)}
                      >
                        {log.id}
                      </button>
                    </td>
                    <td className="mono text-muted">{formatAlertTime(log.timestamp)}</td>
                    <td className="mono text-foreground font-medium">{log.ip}</td>
                    <td>
                      <span
                        className={`badge ${
                          log.severity === 'warning'
                            ? 'badge-amber'
                            : log.severity === 'critical'
                            ? 'badge-rose'
                            : 'badge-blue'
                        }`}
                      >
                        {log.typeLabel}
                      </span>
                    </td>
                    <td className="text-foreground leading-snug">
                      <span className="truncate block max-w-xl">{log.summary}</span>
                    </td>
                    <td className="text-right">
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        onClick={() => setActiveLogDetail(log)}
                      >
                        查看
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* 点击日志详情弹窗 */}
      {activeLogDetail && (
        <div className="modal-backdrop" onClick={() => setActiveLogDetail(null)}>
          <div className="modal-card modal-lg" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge"><FileCode size={18} /></span>
                <div>
                  <h3>日志详情 · {activeLogDetail.id}</h3>
                  <p className="modal-subtitle">UUID: {activeLogDetail.uuid}</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setActiveLogDetail(null)}
                aria-label="关闭"
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-3">
              <div className="grid grid-cols-2 gap-3 text-xs bg-subtle p-3 rounded-lg" style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '12px' }}>
                <div>
                  <span className="text-muted block">发生时间 (本地时区)：</span>
                  <strong className="mono">{formatAlertTime(activeLogDetail.timestamp)}</strong>
                </div>
                <div>
                  <span className="text-muted block">来源 IP 地址：</span>
                  <strong className="mono">{activeLogDetail.ip}</strong>
                </div>
                <div>
                  <span className="text-muted block">事件分类：</span>
                  <span className="badge badge-blue">{activeLogDetail.typeLabel}</span>
                </div>
                <div>
                  <span className="text-muted block">客户端 Agent：</span>
                  <span className="text-muted truncate block">{activeLogDetail.userAgent}</span>
                </div>
              </div>

              <div>
                <span className="text-xs font-medium block mb-1 text-muted">完整请求 Payload / 事件堆栈：</span>
                <pre className="p-3 bg-black/90 text-mint rounded-lg text-xs font-mono overflow-x-auto max-h-60">
                  {JSON.stringify(activeLogDetail.payload, null, 2)}
                </pre>
              </div>
            </div>

            <div className="modal-actions justify-end">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setActiveLogDetail(null)}
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
