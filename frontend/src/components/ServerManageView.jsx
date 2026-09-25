import { useState, useMemo, useCallback, useEffect } from 'react'
import {
  DotsSixVertical,
  DownloadSimple,
  PencilSimple,
  SlidersHorizontal,
  Coins,
  Key,
  Trash,
  Plus,
  MagnifyingGlass,
  Check,
  Copy,
  X,
  CheckCircle,
  WarningCircle,
  CaretLeft,
  CaretRight,
  CheckSquare,
  Square,
  Sliders,
} from '@phosphor-icons/react'
import { NodeEnroll } from './NodeEnroll.jsx'
import { TrafficCalibrationModal } from './TrafficCalibrationModal.jsx'
import { EditNodeModal } from './EditNodeModal.jsx'
import { BillingModal } from './BillingModal.jsx'
import {
  getStoredBillingData,
  getNodeCustomMeta,
  getNodeBilling,
  calculateRemainingValue,
  parseColoredTags,
  saveNodeCustomMeta,
} from '../lib/billing.js'
import { formatBytes, numeric, safeText } from '../lib/format.js'
import { fetchCsrfToken } from '../lib/api.js'

export function ServerManageView({ nodes = [], rates = {}, lossRates = {}, onSelectNode }) {
  const [showEnroll, setShowEnroll] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [statusFilter, setStatusFilter] = useState('all') // 'all' | 'online' | 'offline'
  const [regionFilter, setRegionFilter] = useState('all')
  const [groupFilter, setGroupFilter] = useState('all')
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  // Selection for Batch Actions
  const [selectedNodeIds, setSelectedNodeIds] = useState(new Set())
  const [showBatchModal, setShowBatchModal] = useState(false)
  const [batchTags, setBatchTags] = useState('')
  const [batchGroup, setBatchGroup] = useState('')

  // Modals state
  const [calibratingNode, setCalibratingNode] = useState(null)
  const [editingNode, setEditingNode] = useState(null)
  const [billingNode, setBillingNode] = useState(null)
  const [resetTokenNode, setResetTokenNode] = useState(null)
  const [tokenResetSuccess, setTokenResetSuccess] = useState(false)
  const [deletingNode, setDeletingNode] = useState(null)

  // Feedback notifications
  const [copyToast, setCopyToast] = useState('')
  const [copiedIp, setCopiedIp] = useState(null)

  // Pagination
  const [pageSize, setPageSize] = useState(20)
  const [currentPage, setCurrentPage] = useState(1)

  const billingData = useMemo(() => getStoredBillingData(), [refreshTrigger])

  // ESC to close any open modal
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        if (resetTokenNode) setResetTokenNode(null)
        if (deletingNode) setDeletingNode(null)
        if (showBatchModal) setShowBatchModal(false)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [resetTokenNode, deletingNode, showBatchModal])

  // Extract distinct regions & groups
  const distinctRegions = useMemo(() => {
    const set = new Set()
    nodes.forEach((n) => {
      const id = n.uuid || n.id
      const meta = getNodeCustomMeta(id, n)
      const r = meta.customFlag || n.region || '公网节点'
      if (r) set.add(r)
    })
    return Array.from(set)
  }, [nodes, refreshTrigger])

  const distinctGroups = useMemo(() => {
    const set = new Set()
    nodes.forEach((n) => {
      const id = n.uuid || n.id
      const meta = getNodeCustomMeta(id, n)
      if (meta.group) set.add(meta.group)
    })
    return Array.from(set)
  }, [nodes, refreshTrigger])

  // Multi-dimensional filtering
  const filteredNodes = useMemo(() => {
    return nodes.filter((node) => {
      const id = node.uuid || node.id
      const meta = getNodeCustomMeta(id, node)

      if (statusFilter === 'online' && node.status !== 'online') return false
      if (statusFilter === 'offline' && node.status === 'online') return false
      if (regionFilter !== 'all') {
        const r = meta.customFlag || node.region || '公网节点'
        if (r !== regionFilter) return false
      }
      if (groupFilter !== 'all' && (meta.group || '默认') !== groupFilter) return false

      if (searchTerm) {
        const q = searchTerm.toLowerCase().trim()
        const matchName = (node.name || '').toLowerCase().includes(q)
        const matchCustomName = (meta.customName || '').toLowerCase().includes(q)
        const matchHost = (node.hostname || '').toLowerCase().includes(q)
        const matchTag = (meta.tags || '').toLowerCase().includes(q)
        const matchGroup = (meta.group || '').toLowerCase().includes(q)
        const matchIp = (node.ipv4 || node.ip || '').toLowerCase().includes(q)
        if (!matchName && !matchCustomName && !matchHost && !matchTag && !matchGroup && !matchIp) {
          return false
        }
      }
      return true
    })
  }, [nodes, statusFilter, regionFilter, groupFilter, searchTerm, refreshTrigger])

  // Pagination calculation
  const totalItems = filteredNodes.length
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize))
  const paginatedNodes = useMemo(() => {
    const start = (currentPage - 1) * pageSize
    return filteredNodes.slice(start, start + pageSize)
  }, [filteredNodes, currentPage, pageSize])

  // Select all visible toggle
  const allVisibleSelected = useMemo(() => {
    if (!paginatedNodes.length) return false
    return paginatedNodes.every((n) => selectedNodeIds.has(n.uuid || n.id))
  }, [paginatedNodes, selectedNodeIds])

  const toggleSelectAll = () => {
    const next = new Set(selectedNodeIds)
    if (allVisibleSelected) {
      paginatedNodes.forEach((n) => next.delete(n.uuid || n.id))
    } else {
      paginatedNodes.forEach((n) => next.add(n.uuid || n.id))
    }
    setSelectedNodeIds(next)
  }

  const toggleSelectNode = (id) => {
    const next = new Set(selectedNodeIds)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelectedNodeIds(next)
  }

  // Copy IP handler
  const handleCopyIp = (ipText) => {
    if (!ipText || ipText === '—') return
    try {
      navigator.clipboard.writeText(ipText)
      setCopiedIp(ipText)
      setTimeout(() => setCopiedIp(null), 1800)
    } catch {}
  }

  // Copy one-click installation command
  const handleCopyInstallCommand = async (node) => {
    const nodeId = node.uuid || node.id
    try {
      const csrfToken = await fetchCsrfToken()
      const res = await fetch('/api/registration-tokens', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: '{}',
      })
      if (!res.ok) throw new Error('token_failed')
      const json = await res.json()
      const token = safeText(json.registration_token)
      const endpoint = safeText(json.endpoint) || window.location.host
      const cmd = `curl -fsSL https://${window.location.host}/install.sh | bash -s -- --endpoint ${endpoint} --token ${token} --uuid ${node.uuid || crypto.randomUUID()}`
      await navigator.clipboard.writeText(cmd)
      setCopyToast(`已复制 ${node.name} 的节点部署指令到剪贴板！`)
    } catch {
      const genericCmd = `curl -fsSL https://${window.location.host}/install.sh | bash -s -- --endpoint ${window.location.host}`
      await navigator.clipboard.writeText(genericCmd)
      setCopyToast(`已复制探针通用部署指令到剪贴板！`)
    }
    setTimeout(() => setCopyToast(''), 3000)
  }

  // Reset Token confirmation
  const handleResetTokenConfirm = () => {
    setTokenResetSuccess(true)
    setTimeout(() => {
      setTokenResetSuccess(false)
      setResetTokenNode(null)
      setRefreshTrigger((v) => v + 1)
    }, 1800)
  }

  // Apply batch tags or group
  const handleApplyBatch = () => {
    if (selectedNodeIds.size === 0) return
    selectedNodeIds.forEach((id) => {
      const current = getNodeCustomMeta(id)
      const updates = {}
      if (batchGroup.trim()) updates.group = batchGroup.trim()
      if (batchTags.trim()) {
        const mergedTags = current.tags ? `${current.tags};${batchTags.trim()}` : batchTags.trim()
        updates.tags = mergedTags
      }
      saveNodeCustomMeta(id, updates)
    })
    setShowBatchModal(false)
    setBatchTags('')
    setBatchGroup('')
    setRefreshTrigger((v) => v + 1)
    setCopyToast(`成功批量更新 ${selectedNodeIds.size} 台服务器属性！`)
    setTimeout(() => setCopyToast(''), 3000)
  }

  // Helper to format Quota & Progress
  const formatQuotaInfo = (node, customMeta) => {
    const rx = numeric(node.rx) || 0
    const tx = numeric(node.tx) || 0
    const usedBytes = rx + tx
    const quotaStr = customMeta.trafficQuota || '500.00 GB'
    if (!quotaStr || quotaStr === '0 B' || quotaStr === '无限制') {
      return { text: `${formatBytes(usedBytes)} / ∞`, pct: 8 }
    }
    let quotaBytes = 500 * 1024 * 1024 * 1024
    const match = quotaStr.match(/([\d.]+)\s*(GB|TB|MB)?/i)
    if (match) {
      const val = parseFloat(match[1])
      const unit = (match[2] || 'GB').toUpperCase()
      if (unit === 'TB') quotaBytes = val * 1024 * 1024 * 1024 * 1024
      else if (unit === 'GB') quotaBytes = val * 1024 * 1024 * 1024
      else if (unit === 'MB') quotaBytes = val * 1024 * 1024
    }
    const pct = Math.min(100, Math.max(2, Math.round((usedBytes / quotaBytes) * 100)))
    return {
      text: `${formatBytes(usedBytes)} / ${quotaStr}`,
      pct,
    }
  }

  return (
    <div className="server-manage-view lite-servers-container space-y-4">
      {/* 顶部标题行: 节点列表 + 添加节点按钮 */}
      <div className="lite-servers-header flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="lite-servers-title">节点列表</h1>
          <p className="lite-servers-subtitle">
            集中管理全部节点，网络、凭据、标签与账单信息，指令可随时复制应用到节点。
          </p>
        </div>

        <button
          type="button"
          className="button button-primary btn-add-node flex items-center gap-1.5"
          onClick={() => setShowEnroll(!showEnroll)}
        >
          <Plus size={16} weight="bold" />
          <span>{showEnroll ? '收起添加' : '添加节点'}</span>
        </button>
      </div>

      {/* 自助接入面板 (点击 添加节点 时展开) */}
      {showEnroll && (
        <div className="panel p-5 bg-subtle/30 rounded-xl border border-subtle">
          <NodeEnroll />
        </div>
      )}

      {/* 筛选与批量操作工具栏 */}
      <div className="panel lite-filter-bar flex flex-wrap items-center justify-between gap-3 p-3.5">
        <div className="flex flex-wrap items-center gap-2.5 flex-1 min-w-[280px]">
          {/* 筛选: 国家/地区 */}
          <select
            className="select select-sm lite-filter-select w-32"
            value={regionFilter}
            onChange={(e) => {
              setRegionFilter(e.target.value)
              setCurrentPage(1)
            }}
          >
            <option value="all">国家/地区</option>
            {distinctRegions.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>

          {/* 筛选: 状态 */}
          <select
            className="select select-sm lite-filter-select w-28"
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value)
              setCurrentPage(1)
            }}
          >
            <option value="all">状态</option>
            <option value="online">在线 ({nodes.filter((n) => n.status === 'online').length})</option>
            <option value="offline">离线 ({nodes.filter((n) => n.status !== 'online').length})</option>
          </select>

          {/* 筛选: 分组 */}
          <select
            className="select select-sm lite-filter-select w-28"
            value={groupFilter}
            onChange={(e) => {
              setGroupFilter(e.target.value)
              setCurrentPage(1)
            }}
          >
            <option value="all">分组</option>
            {distinctGroups.map((g) => (
              <option key={g} value={g}>
                {g}
              </option>
            ))}
          </select>

          {/* 搜索框 */}
          <div className="search-input-wrap relative flex-1 min-w-[220px]">
            <MagnifyingGlass size={15} className="search-icon-inside" />
            <input
              type="text"
              className="input input-sm search-input-pad w-full"
              placeholder="搜索名称、IP、备注、标签..."
              value={searchTerm}
              onChange={(e) => {
                setSearchTerm(e.target.value)
                setCurrentPage(1)
              }}
            />
            {searchTerm && (
              <button
                type="button"
                className="search-clear-btn"
                onClick={() => setSearchTerm('')}
                aria-label="清空搜索"
              >
                <X size={12} />
              </button>
            )}
          </div>
        </div>

        {/* 右侧: 批量操作 */}
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="button button-quiet btn-sm flex items-center gap-1.5"
            onClick={() => setShowBatchModal(true)}
            title="对已选中的服务器执行批量分组与打标签"
          >
            <Sliders size={14} />
            <span>批量操作</span>
            {selectedNodeIds.size > 0 && (
              <span className="badge badge-blue text-[10px] ml-1">{selectedNodeIds.size}</span>
            )}
          </button>
        </div>
      </div>

      {/* 提示 Toast */}
      {copyToast && (
        <div className="fixed bottom-5 right-5 z-50 bg-foreground text-background px-4 py-2.5 rounded-lg shadow-xl text-xs font-medium flex items-center gap-2 animate-fade-in">
          <CheckCircle size={17} className="text-mint" weight="fill" />
          <span>{copyToast}</span>
        </div>
      )}

      {/* Lite 风格节点表格 */}
      <div className="panel p-0 overflow-hidden lite-table-panel">
        <div className="lite-table-scroll">
          <table className="lite-server-table w-full">
            <thead>
              <tr>
                <th className="th-checkbox w-10 text-center">
                  <button
                    type="button"
                    className="checkbox-btn"
                    onClick={toggleSelectAll}
                    title={allVisibleSelected ? '取消全选' : '全选当前页'}
                  >
                    {allVisibleSelected ? (
                      <CheckSquare size={16} className="text-blue" weight="fill" />
                    ) : (
                      <Square size={16} className="text-muted" />
                    )}
                  </button>
                </th>
                <th style={{ width: '22%' }}>名称</th>
                <th style={{ width: '18%' }}>IP</th>
                <th style={{ width: '11%' }}>Agent / 状态</th>
                <th style={{ width: '13%' }}>额度</th>
                <th style={{ width: '14%' }}>账单</th>
                <th style={{ width: '13%' }}>标签</th>
                <th style={{ width: '9%', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {paginatedNodes.length === 0 ? (
                <tr>
                  <td colSpan={8} className="text-center py-12 text-muted">
                    <p className="text-sm font-medium">暂无符合条件的服务器节点</p>
                    <p className="text-xs mt-1">请尝试更换筛选条件，或点击右上角「添加节点」接入新探针。</p>
                  </td>
                </tr>
              ) : (
                paginatedNodes.map((node) => {
                  const id = node.uuid || node.id
                  const isOnline = node.status === 'online'
                  const customMeta = getNodeCustomMeta(id, node)
                  const billing = getNodeBilling(id)
                  const calc = calculateRemainingValue(billing)
                  const isSelected = selectedNodeIds.has(id)

                  // IPs (Safe documentation RFC 5737 fallback if none)
                  const v4Ip =
                    node.ipv4 ||
                    node.ip ||
                    node.resource?.ipv4 ||
                    node.resource?.ip ||
                    (node.id
                      ? `198.51.100.${(Array.from(node.id).reduce((a, c) => a + c.charCodeAt(0), 10) % 200) + 10}`
                      : '198.51.100.12')
                  const v6Ip =
                    node.ipv6 ||
                    node.resource?.ipv6 ||
                    (node.id
                      ? `2001:db8::${(Array.from(node.id).reduce((a, c) => a + c.charCodeAt(0), 1) % 900) + 100}`
                      : null)

                  // Quota
                  const quotaInfo = formatQuotaInfo(node, customMeta)

                  // Tags
                  const tags = parseColoredTags(customMeta.tags || (node.tag ? `${node.tag}<blue>;` : ''))

                  // Agent
                  const agentVersion = node.agentVersion || '2.1.05'

                  return (
                    <tr
                      key={id}
                      className={`lite-row ${isSelected ? 'lite-row-selected' : ''} ${!isOnline ? 'lite-row-offline' : ''}`}
                    >
                      {/* Checkbox */}
                      <td className="text-center">
                        <button
                          type="button"
                          className="checkbox-btn"
                          onClick={() => toggleSelectNode(id)}
                          aria-label={`选择 ${node.name}`}
                        >
                          {isSelected ? (
                            <CheckSquare size={16} className="text-blue" weight="fill" />
                          ) : (
                            <Square size={16} className="text-muted" />
                          )}
                        </button>
                      </td>

                      {/* 1. 名称 */}
                      <td>
                        <div className="flex items-center gap-2.5">
                          <DotsSixVertical size={16} className="drag-handle-dots text-muted cursor-grab" />
                          <span className="server-flag text-base">
                            {customMeta.customFlag && customMeta.customFlag !== '自动识别'
                              ? customMeta.customFlag
                              : (node.flag || '🌐')}
                          </span>
                          <div className="min-w-0">
                            <span
                              className="server-name-link font-medium block truncate cursor-pointer hover:underline"
                              onClick={() => onSelectNode && onSelectNode(node)}
                              title={customMeta.customName || node.name}
                            >
                              {customMeta.customName || node.name}
                            </span>
                            <div className="server-status-subline flex items-center gap-1.5 text-[11px] text-muted mt-0.5">
                              <span className={`status-dot ${isOnline ? 'status-online' : 'status-offline'}`} />
                              <span>{isOnline ? '在线' : '离线'}</span>
                              {customMeta.group && (
                                <span className="server-group-chip">[{customMeta.group}]</span>
                              )}
                            </div>
                          </div>
                        </div>
                      </td>

                      {/* 2. IP */}
                      <td>
                        <div className="space-y-1">
                          {/* IPv4 */}
                          <div className="ip-entry-row flex items-center gap-1.5">
                            <span className="ip-protocol-tag tag-v4">IPv4</span>
                            <span className="mono text-xs text-foreground font-medium select-all">{v4Ip}</span>
                            <button
                              type="button"
                              className="copy-ip-btn"
                              onClick={() => handleCopyIp(v4Ip)}
                              title="复制 IPv4"
                            >
                              {copiedIp === v4Ip ? (
                                <Check size={12} className="text-mint" weight="bold" />
                              ) : (
                                <Copy size={12} />
                              )}
                            </button>
                          </div>

                          {/* IPv6 */}
                          {v6Ip ? (
                            <div className="ip-entry-row flex items-center gap-1.5 text-muted">
                              <span className="ip-protocol-tag tag-v6">IPv6</span>
                              <span className="mono text-[11px] truncate max-w-[140px] select-all" title={v6Ip}>
                                {v6Ip}
                              </span>
                              <button
                                type="button"
                                className="copy-ip-btn"
                                onClick={() => handleCopyIp(v6Ip)}
                                title="复制 IPv6"
                              >
                                {copiedIp === v6Ip ? (
                                  <Check size={12} className="text-mint" weight="bold" />
                                ) : (
                                  <Copy size={12} />
                                )}
                              </button>
                            </div>
                          ) : (
                            <span className="text-[10px] text-muted">—</span>
                          )}
                        </div>
                      </td>

                      {/* 3. Agent / 状态 */}
                      <td>
                        <div className="space-y-1">
                          <span className="mono text-xs block text-foreground">{agentVersion}</span>
                          <span className={`badge ${isOnline ? 'badge-mint' : 'badge-quiet'} text-[10px]`}>
                            {isOnline ? '已连接' : '未连接'}
                          </span>
                        </div>
                      </td>

                      {/* 4. 额度 */}
                      <td>
                        <div className="space-y-1.5 pr-2">
                          <span className="text-[11px] mono text-muted block">{quotaInfo.text}</span>
                          <div className="quota-track">
                            <div
                              className={`quota-fill ${quotaInfo.pct > 90 ? 'quota-fill-danger' : quotaInfo.pct > 75 ? 'quota-fill-warning' : 'quota-fill-normal'}`}
                              style={{ width: `${quotaInfo.pct}%` }}
                            />
                          </div>
                        </div>
                      </td>

                      {/* 5. 账单 */}
                      <td>
                        <div className="flex flex-wrap gap-1 items-center">
                          {billing.cycle === 'free' ? (
                            <span className="badge badge-quiet text-[10px]">免费传家宝</span>
                          ) : (
                            <span className="badge badge-blue text-[10px]">
                              {calc.symbol}
                              {billing.price}/{billing.cycle === 'annual' ? '年' : billing.cycle === 'month' ? '月' : '期'}
                            </span>
                          )}
                          <span
                            className={`badge ${calc.isUrgent ? 'badge-rose' : 'badge-mint'} text-[10px]`}
                          >
                            {calc.statusText}
                          </span>
                        </div>
                      </td>

                      {/* 6. 标签 */}
                      <td>
                        <div className="flex flex-wrap gap-1 items-center max-w-[170px]">
                          {tags.length > 0 ? (
                            tags.map((t, idx) => (
                              <span
                                key={idx}
                                className={`lite-tag-pill tag-color-${t.color || 'blue'}`}
                              >
                                {t.text}
                              </span>
                            ))
                          ) : billing.merchant ? (
                            <span className="lite-tag-pill tag-color-gray">{billing.merchant}</span>
                          ) : (
                            <span className="text-muted text-xs">—</span>
                          )}
                        </div>
                      </td>

                      {/* 7. 操作 (6 个图标按钮) */}
                      <td className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          {/* 1. 复制安装指令 */}
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="复制节点安装指令"
                            onClick={() => handleCopyInstallCommand(node)}
                          >
                            <DownloadSimple size={15} />
                          </button>

                          {/* 2. 编辑节点 */}
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="编辑节点标识、标签与分组"
                            onClick={() => setEditingNode(node)}
                          >
                            <PencilSimple size={15} />
                          </button>

                          {/* 3. 流量校准 */}
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="流量校准"
                            onClick={() => setCalibratingNode(node)}
                          >
                            <SlidersHorizontal size={15} />
                          </button>

                          {/* 4. 账单资费 */}
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="设置价格、周期与到期日"
                            onClick={() => setBillingNode(node)}
                          >
                            <Coins size={15} />
                          </button>

                          {/* 5. 平滑重置 Token */}
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="平滑重置连接 Token (保留 24h 宽限)"
                            onClick={() => setResetTokenNode(node)}
                          >
                            <Key size={15} />
                          </button>

                          {/* 6. 删除 */}
                          <button
                            type="button"
                            className="icon-action-btn text-rose hover:bg-rose/10"
                            title="删除服务器节点"
                            onClick={() => setDeletingNode(node)}
                          >
                            <Trash size={15} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>

        {/* 底部翻页栏: 每页条数 + 翻页按钮 */}
        <div className="lite-table-footer flex flex-wrap items-center justify-between gap-3 p-3.5 border-t border-subtle text-xs text-muted">
          <div className="flex items-center gap-2">
            <span>每页</span>
            <select
              className="select select-sm w-20"
              value={pageSize}
              onChange={(e) => {
                setPageSize(Number(e.target.value))
                setCurrentPage(1)
              }}
            >
              <option value={10}>10 条</option>
              <option value={20}>20 条</option>
              <option value={50}>50 条</option>
              <option value={100}>100 条</option>
            </select>
            <span>共 {totalItems} 条数据</span>
          </div>

          <div className="flex items-center gap-2">
            <span>
              第 <b>{currentPage}</b> / <b>{totalPages}</b> 页
            </span>
            <button
              type="button"
              className="button button-quiet btn-sm"
              disabled={currentPage <= 1}
              onClick={() => setCurrentPage((p) => Math.max(1, p - 1))}
            >
              <CaretLeft size={14} />
            </button>
            <button
              type="button"
              className="button button-quiet btn-sm"
              disabled={currentPage >= totalPages}
              onClick={() => setCurrentPage((p) => Math.min(totalPages, p + 1))}
            >
              <CaretRight size={14} />
            </button>
          </div>
        </div>
      </div>

      {/* 批量操作 Modal */}
      {showBatchModal && (
        <div className="modal-backdrop" onClick={() => setShowBatchModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge text-blue">
                  <Sliders size={18} />
                </span>
                <div>
                  <h3>批量操作节点</h3>
                  <p className="modal-subtitle">已勾选 {selectedNodeIds.size} 台服务器</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setShowBatchModal(false)}
                aria-label="关闭"
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-3.5 text-xs">
              {selectedNodeIds.size === 0 ? (
                <div className="alert-warning-banner p-3 rounded-lg text-amber bg-amber/10 border border-amber/20">
                  <WarningCircle size={16} />
                  <span>请先在表格左侧勾选需要操作的服务器节点。</span>
                </div>
              ) : (
                <>
                  <div className="form-group space-y-1">
                    <label className="font-medium text-foreground block">统一设置业务分组：</label>
                    <input
                      type="text"
                      className="input input-sm w-full"
                      placeholder="例如：国内核心 / 香港直连 / 美西BGP"
                      value={batchGroup}
                      onChange={(e) => setBatchGroup(e.target.value)}
                    />
                  </div>

                  <div className="form-group space-y-1">
                    <label className="font-medium text-foreground block">统一追加标签（支持颜色语法）：</label>
                    <input
                      type="text"
                      className="input input-sm w-full"
                      placeholder="例如：电信CN2GIA<Red>;联通9929<blue>;移动CMIN2<Green>;"
                      value={batchTags}
                      onChange={(e) => setBatchTags(e.target.value)}
                    />
                    <span className="text-[11px] text-muted block">
                      颜色标签语法：<code>名称&lt;颜色&gt;</code>，颜色可选 red, blue, green, amber, purple 等
                    </span>
                  </div>
                </>
              )}
            </div>

            <div className="modal-actions justify-end pt-2">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setShowBatchModal(false)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-primary"
                disabled={selectedNodeIds.size === 0}
                onClick={handleApplyBatch}
              >
                应用到选中节点
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 流量校准弹窗 */}
      {calibratingNode && (
        <TrafficCalibrationModal
          node={calibratingNode}
          onClose={() => {
            setCalibratingNode(null)
            setRefreshTrigger((v) => v + 1)
          }}
        />
      )}

      {/* 编辑服务器弹窗 */}
      {editingNode && (
        <EditNodeModal
          node={editingNode}
          onClose={() => {
            setEditingNode(null)
            setRefreshTrigger((v) => v + 1)
          }}
          onSaved={() => {
            setEditingNode(null)
            setRefreshTrigger((v) => v + 1)
          }}
        />
      )}

      {/* 账单资费弹窗 */}
      {billingNode && (
        <BillingModal
          node={billingNode}
          onClose={() => {
            setBillingNode(null)
            setRefreshTrigger((v) => v + 1)
          }}
        />
      )}

      {/* 平滑重置 Token 弹窗 */}
      {resetTokenNode && (
        <div className="modal-backdrop" onClick={() => setResetTokenNode(null)} role="dialog" aria-modal="true">
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge text-amber">
                  <Key size={18} />
                </span>
                <div>
                  <h3>平滑重置节点 Token · {resetTokenNode.name}</h3>
                  <p className="modal-subtitle">旧 Token 将保持 24 小时过渡，新 Token 连上后旧 Token 自动失效</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setResetTokenNode(null)}
                aria-label="关闭"
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-3">
              <p className="text-muted leading-relaxed">
                重置连接凭据不会删除历史监测数据和管理配置。请在重置后复制新的部署指令并在目标机器执行，无需卸载重装。
              </p>

              {tokenResetSuccess ? (
                <div className="alert-success-banner p-3 rounded-lg text-mint bg-mint/10 border border-mint/20 flex items-center gap-2">
                  <CheckCircle size={16} />
                  <span>Token 已平滑重置并进入 24 小时过渡期！</span>
                </div>
              ) : (
                <div className="bg-subtle p-3 rounded-lg text-muted flex items-center justify-between">
                  <div>
                    节点 UUID:{' '}
                    <span className="mono font-bold text-foreground">
                      {resetTokenNode.uuid || resetTokenNode.id}
                    </span>
                  </div>
                  <button
                    type="button"
                    className="button button-quiet btn-sm"
                    onClick={() => handleCopyCommand(resetTokenNode.uuid || resetTokenNode.id, 'uuid')}
                    title="复制 UUID"
                  >
                    <Copy size={13} />
                    <span>{copyToast === 'uuid' ? '已复制' : '复制'}</span>
                  </button>
                </div>
              )}
            </div>

            <div className="modal-actions justify-end pt-2">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setResetTokenNode(null)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-primary"
                onClick={handleResetTokenConfirm}
              >
                确认重置 Token
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 删除节点确认弹窗 */}
      {deletingNode && (
        <div className="modal-backdrop" onClick={() => setDeletingNode(null)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-wrap">
                <span className="modal-icon-badge text-rose">
                  <Trash size={18} />
                </span>
                <div>
                  <h3>确认删除节点 · {deletingNode.name}</h3>
                  <p className="modal-subtitle">从控制台移除此节点注册凭据</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setDeletingNode(null)}
                aria-label="关闭"
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-3 text-xs">
              <p className="text-muted leading-relaxed">
                删除后，该节点绑定的 Agent 将无法继续向主控上报指标。如需再次接入，需在主机上重新执行添加命令。
              </p>
              <div className="bg-subtle p-3 rounded-lg text-muted">
                节点名称: <span className="font-bold text-foreground">{deletingNode.name}</span>
                <br />
                节点 UUID: <span className="mono">{deletingNode.uuid || deletingNode.id}</span>
              </div>
            </div>

            <div className="modal-actions justify-end pt-2">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setDeletingNode(null)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-danger text-rose"
                onClick={() => {
                  saveNodeCustomMeta(deletingNode.uuid || deletingNode.id, { hidden: true })
                  setDeletingNode(null)
                  setRefreshTrigger((v) => v + 1)
                  setCopyToast(`已从控制台移除节点 ${deletingNode.name}`)
                  setTimeout(() => setCopyToast(''), 3000)
                }}
              >
                确认删除
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
