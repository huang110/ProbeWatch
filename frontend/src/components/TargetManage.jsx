import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowClockwise,
  Broadcast,
  CaretDown,
  CaretRight,
  Check,
  CheckCircle,
  CircleNotch,
  Crosshair,
  Funnel,
  Globe,
  Lightning,
  MagnifyingGlass,
  PencilSimple,
  Plus,
  Power,
  ShieldWarning,
  Tag,
  Trash,
  WarningCircle,
  X,
} from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { detectRegionAndFlag, numeric, safeObject, safeText } from '../lib/format.js'
import {
  TARGET_QUICK_TEMPLATES,
  applyTargetQuickTemplate,
  buildTargetFormFromRecord,
  buildTargetSubmitPayload,
  executeWithCsrfRetry,
  extractAvailableNodeTags,
  formatTargetScopeDisplay,
  nodeMatchesTarget,
} from '../lib/targetScope.js'

const TARGET_KINDS = ['tcp', 'http', 'https', 'dns', 'mtr', 'media_http']
const CREATE_TARGET_KINDS = ['http', 'https', 'tcp', 'dns', 'mtr']
const KIND_LABELS = {
  tcp: 'TCP',
  http: 'HTTP',
  https: 'HTTPS',
  dns: 'DNS',
  mtr: 'MTR',
  media_http: '历史流媒体',
}
const FILTER_TABS = [
  { key: 'all', label: '全部' },
  { key: 'enabled', label: '已启用' },
  { key: 'disabled', label: '已停用' },
  { key: 'http_all', label: 'HTTP/S' },
  { key: 'tcp', label: 'TCP' },
  { key: 'dns', label: 'DNS' },
  { key: 'mtr', label: 'MTR' },
  { key: 'media_http', label: '历史流媒体目标', muted: true },
]
const KIND_PORT_DEFAULTS = { tcp: 80, http: 80, https: 443, dns: 53, mtr: 80, media_http: 80 }
const HTTP_LIKE_KINDS = ['http', 'https', 'media_http']
const DNS_TYPES = ['A', 'AAAA', 'CNAME']
const REGION_RULE_EXAMPLE = '[{"region":"US","contains":"United States"}]'

export const normalizeTarget = (row) => {
  const source = safeObject(row)
  const rawNodeIds = Array.isArray(source.node_ids)
    ? source.node_ids
    : Array.isArray(source.nodeIds)
      ? source.nodeIds
      : []
  const rawNodeTags = Array.isArray(source.node_tags)
    ? source.node_tags
    : Array.isArray(source.nodeTags)
      ? source.nodeTags
      : []
  const rawRegionRules = Array.isArray(source.region_rules)
    ? source.region_rules
    : Array.isArray(source.regionRules)
      ? source.regionRules
      : []
  return {
    id: safeText(source.id),
    name: safeText(source.name, '未命名目标'),
    kind: safeText(source.kind, 'tcp'),
    host: safeText(source.host, '—'),
    port: numeric(source.port),
    path: safeText(source.path),
    dns_type: safeText(source.dns_type || source.dnsType),
    dnsType: safeText(source.dns_type || source.dnsType),
    expected_status: numeric(source.expected_status ?? source.expectedStatus),
    expectedStatus: numeric(source.expected_status ?? source.expectedStatus),
    interval_seconds: numeric(source.interval_seconds ?? source.intervalSeconds),
    intervalSeconds: numeric(source.interval_seconds ?? source.intervalSeconds),
    timeout_ms: numeric(source.timeout_ms ?? source.timeoutMs),
    timeoutMs: numeric(source.timeout_ms ?? source.timeoutMs),
    max_hops: numeric(source.max_hops ?? source.maxHops),
    maxHops: numeric(source.max_hops ?? source.maxHops),
    enabled: source.enabled === true,
    node_ids: rawNodeIds.map((v) => String(v || '').trim()).filter(Boolean),
    nodeIds: rawNodeIds.map((v) => String(v || '').trim()).filter(Boolean),
    node_tags: rawNodeTags.map((v) => String(v || '').trim()).filter(Boolean),
    nodeTags: rawNodeTags.map((v) => String(v || '').trim()).filter(Boolean),
    region_rules: rawRegionRules,
    regionRules: rawRegionRules,
  }
}

// 服务端错误响应统一为 {"error": "..."}，原样展示文案方便定位。
const readErrorDetail = async (response) => {
  try {
    return safeText(safeObject(await response.json()).error)
  } catch {
    return ''
  }
}

export const emptyTargetForm = (kind = 'http') => ({
  kind,
  id: '',
  name: '',
  host: '',
  port: String(KIND_PORT_DEFAULTS[kind] ?? 80),
  path: HTTP_LIKE_KINDS.includes(kind) ? '/health' : '',
  dnsType: 'A',
  intervalSeconds: '60',
  timeoutMs: '3000',
  maxHops: '20',
  expectedStatus: '',
  enabled: true,
  scopeMode: 'all', // 'all' | 'nodes' | 'tags' | 'custom'
  nodeIds: [],
  nodeTags: [],
  regionRulesList: [],
  regionRules: '',
  regionRulesCleared: false,
})

const parseLegacyRegionRulesJson = (rawText) => {
  if (!rawText || !String(rawText).trim()) return []
  const parsed = JSON.parse(rawText)
  if (!Array.isArray(parsed)) throw new Error('not_array')
  return parsed
}

export const validateTargetForm = (form) => {
  const kind = form.kind
  const name = String(form.name || '').trim()
  const id = String(form.id || '').trim()
  const host = String(form.host || '').trim()
  if (!name) return '请输入目标名称。'
  if (name.length > 128) return '目标名称不能超过 128 个字符。'
  if (!id) return '请输入目标 ID。'
  if (id.length > 128) return '目标 ID 不能超过 128 个字符。'
  if (!host) return '请输入主机地址。'
  if (host.length > 253) return '主机地址不能超过 253 个字符。'
  if (/\s/.test(host) || host.includes('://') || /[/?#@\\]/.test(host)) {
    return '主机地址不能包含空格、协议前缀或 / ? # @ \\ 字符。'
  }
  const port = Number(form.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return '端口需为 1-65535 的整数。'
  if (HTTP_LIKE_KINDS.includes(kind)) {
    if (!form.path) return 'HTTP 类目标必须填写路径（以 / 开头）。'
    if (!form.path.startsWith('/')) return '路径必须以 / 开头。'
    if (form.path.length > 2048) return '路径不能超过 2048 个字符。'
    if (/[\\\r\n]/.test(form.path)) return '路径不能包含反斜杠或换行。'
  }
  if (kind === 'dns' && !DNS_TYPES.includes(form.dnsType)) {
    return 'DNS 目标必须选择记录类型（A / AAAA / CNAME）。'
  }
  const interval = Number(form.intervalSeconds)
  if (!Number.isInteger(interval) || interval < 10 || interval > 86400) {
    return '检测间隔需为 10-86400 秒。'
  }
  const timeout = Number(form.timeoutMs)
  if (!Number.isInteger(timeout) || timeout < 100 || timeout > 30000) {
    return '超时时间需为 100-30000 毫秒。'
  }
  if (kind === 'mtr') {
    const hops = Number(form.maxHops)
    if (!Number.isInteger(hops) || hops < 1 || hops > 30) return 'MTR 最大跳数需为 1-30。'
  }
  if (HTTP_LIKE_KINDS.includes(kind) && String(form.expectedStatus || '').trim() !== '') {
    const status = Number(form.expectedStatus)
    if (!Number.isInteger(status) || status < 100 || status > 599) return '期望状态码需为 100-599。'
  }
  if (Array.isArray(form.regionRulesList) && form.regionRulesList.length > 0) {
    for (const rule of form.regionRulesList) {
      const r = safeObject(rule)
      const hasAny = String(r.region || '').trim() !== '' || String(r.contains || '').trim() !== ''
      if (hasAny && (!String(r.region || '').trim() || !String(r.contains || '').trim())) {
        return '区域规则每项必须同时填写地区代码 (region) 与匹配内容 (contains)。'
      }
    }
  } else if (kind === 'media_http' && String(form.regionRules || '').trim() !== '') {
    let parsed
    try {
      parsed = parseLegacyRegionRulesJson(form.regionRules)
    } catch {
      return `region_rules 不是合法 JSON，请输入数组，例如 ${REGION_RULE_EXAMPLE}`
    }
    for (const rule of parsed) {
      const item = safeObject(rule)
      if (typeof item.region !== 'string' || typeof item.contains !== 'string' || !item.contains) {
        return 'region_rules 数组每项需包含字符串字段 region 与 contains。'
      }
    }
  }
  return null
}

function formatCheckTimeShort(rawTime) {
  if (!rawTime) return '暂无数据'
  const d = new Date(rawTime)
  if (Number.isNaN(d.getTime())) return '暂无数据'
  const pad = (n) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function resolveTargetRecentStatus(target, telemetryMap) {
  const isMedia = target.kind === 'media_http' || String(target.id).startsWith('media-')
  if (isMedia && !target.enabled) {
    return {
      tone: 'disabled',
      label: 'IPQA 接管',
      detail: '周期探测已停用，由节点 IPQA 归档统一提供',
      lastCheckedAt: null,
      isFailed: false,
    }
  }
  const entry = telemetryMap?.get(target.id)
  if (!target.enabled) {
    return {
      tone: 'disabled',
      label: '已停用',
      detail: entry?.lastCheckedAt ? `上次采样 ${formatCheckTimeShort(entry.lastCheckedAt)}` : '任务已停用',
      lastCheckedAt: entry?.lastCheckedAt || null,
      isFailed: false,
    }
  }
  if (!entry || !entry.hasSample) {
    return {
      tone: 'unknown',
      label: '暂无数据',
      detail: '等待探针首次周期采样回报',
      lastCheckedAt: entry?.lastCheckedAt || null,
      isFailed: false,
    }
  }
  if (
    (entry.latestFailedNodes > 0 && (entry.latestSuccessNodes || 0) === 0) ||
    (entry.failedNodes > 0 && entry.successNodes === 0)
  ) {
    return {
      tone: 'danger',
      label: '失败',
      detail: entry.failureReason || `连续失败 · 丢包率 ${Math.round(entry.maxLossRate * 100)}%`,
      lastCheckedAt: entry.lastCheckedAt,
      isFailed: true,
    }
  }
  if (entry.latestFailedNodes > 0 || entry.failedNodes > 0 || entry.maxLossRate >= 0.2) {
    return {
      tone: 'warning',
      label: '部分异常',
      detail: entry.failureReason || `最大丢包 ${Math.round(entry.maxLossRate * 100)}%${entry.avgLatencyMs ? ` · 均延 ${Math.round(entry.avgLatencyMs)}ms` : ''}`,
      lastCheckedAt: entry.lastCheckedAt,
      isFailed: true,
    }
  }
  return {
    tone: 'online',
    label: '正常',
    detail: entry.avgLatencyMs ? `平均延迟 ${Math.round(entry.avgLatencyMs)}ms` : '采样正常',
    lastCheckedAt: entry.lastCheckedAt,
    isFailed: false,
  }
}

export function TargetTable({
  targets,
  nodes = [],
  telemetryMap = new Map(),
  readOnly = false,
  loading = false,
  mutatingId = null,
  selectedIds = new Set(),
  onSelectToggle,
  onSelectAll,
  onEdit,
  onToggle,
  onDelete,
  onQuickCreate,
  onOpenCreate,
  emptyDetail = '添加一个 HTTP、TCP 或 DNS 目标后，探针会按执行范围开始采样。',
}) {
  const [expandedScopeId, setExpandedScopeId] = useState(null)
  const [expandedMobileIds, setExpandedMobileIds] = useState(() => new Set())

  const toggleMobileCardExpand = (id) => {
    setExpandedMobileIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (!loading && targets.length === 0) {
    return (
      <div className="target-empty-state-compact" data-testid="target-empty-state">
        <div className="target-empty-icon-wrap">
          <Crosshair size={22} weight="duotone" />
        </div>
        <div className="target-empty-title">还没有检测目标</div>
        <p className="target-empty-desc">{emptyDetail}</p>
        {!readOnly && (
          <div className="target-empty-actions">
            {onOpenCreate && (
              <button
                type="button"
                className="button button-primary target-empty-btn-primary"
                onClick={() => onOpenCreate()}
              >
                <Plus size={14} weight="bold" />
                新建目标
              </button>
            )}
            {onQuickCreate && (
              <div className="target-quick-template-group" role="group" aria-label="快捷创建模板">
                <button
                  type="button"
                  className="button button-quiet target-quick-template-btn"
                  data-template="http"
                  onClick={() => onQuickCreate('http')}
                  title="快速创建 HTTP（仅预填表单，不直接写库）"
                >
                  <Lightning size={13} weight="fill" />
                  HTTP 健康检查
                </button>
                <button
                  type="button"
                  className="button button-quiet target-quick-template-btn"
                  data-template="tcp"
                  onClick={() => onQuickCreate('tcp')}
                  title="快速创建 TCP（仅预填表单，不直接写库）"
                >
                  <Lightning size={13} weight="fill" />
                  TCP 端口检查
                </button>
                <button
                  type="button"
                  className="button button-quiet target-quick-template-btn"
                  data-template="dns"
                  onClick={() => onQuickCreate('dns')}
                  title="快速创建 DNS（仅预填表单，不直接写库）"
                >
                  <Lightning size={13} weight="fill" />
                  DNS 解析检查
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    )
  }

  const allSelected = targets.length > 0 && targets.every((t) => selectedIds.has(t.id))

  return (
    <div className="target-list-container">
      {/* 桌面端高密度表格 */}
      <div className="table-scroll target-table-wrap target-desktop-table-wrap">
        <table className="target-table mjj-target-table">
          <thead>
            <tr>
              {!readOnly && onSelectAll && (
                <th className="target-col-check">
                  <input
                    type="checkbox"
                    className="lite-checkbox"
                    checked={allSelected}
                    onChange={(e) => onSelectAll(e.target.checked)}
                    aria-label="全选当前列表目标"
                  />
                </th>
              )}
              <th>名称 / ID</th>
              <th>类型</th>
              <th>主机 / 端口</th>
              <th>路径 / 记录类型</th>
              <th>执行范围</th>
              <th>检查间隔</th>
              <th>最近状态</th>
              <th>最近检测时间</th>
              <th>启用状态</th>
              {!readOnly && <th className="target-col-actions">操作</th>}
            </tr>
          </thead>
          <tbody>
            {targets.map((target, index) => {
              const isMedia = target.kind === 'media_http' || String(target.id).startsWith('media-')
              const isMediaDisabled = isMedia && !target.enabled
              const scopeInfo = formatTargetScopeDisplay(target, nodes)
              const recentStatus = resolveTargetRecentStatus(target, telemetryMap)
              const isScopePopoverOpen = expandedScopeId === target.id

              const pathOrDnsDisplay =
                target.kind === 'dns'
                  ? `记录: ${target.dnsType || 'A'}`
                  : HTTP_LIKE_KINDS.includes(target.kind)
                    ? `${target.path || '/'}${target.expectedStatus ? ` (HTTP ${target.expectedStatus})` : ''}`
                    : target.kind === 'mtr'
                      ? `最大 ${target.maxHops || 20} 跳`
                      : '—'

              return (
                <tr
                  key={target.id || `target-${index}`}
                  data-target-id={target.id || undefined}
                  className={isMediaDisabled ? 'target-row-media-disabled' : undefined}
                >
                  {!readOnly && onSelectToggle && (
                    <td className="target-col-check">
                      <input
                        type="checkbox"
                        className="lite-checkbox"
                        checked={selectedIds.has(target.id)}
                        onChange={() => onSelectToggle(target.id)}
                        aria-label={`选择目标 ${target.name}`}
                      />
                    </td>
                  )}
                  <td>
                    <div className="target-name-stack">
                      <div className="target-name-line">
                        <span className="target-name-cell">{target.name}</span>
                        {isMedia && (
                          <span
                            className="target-tag-ipqa-muted"
                            title="周期探测已由 IPQA 历史归档接管"
                          >
                            已由 IPQA 接管
                          </span>
                        )}
                      </div>
                      <span className="target-id-sub mono">{target.id || '—'}</span>
                    </div>
                  </td>
                  <td>
                    <span className={`target-kind target-kind-${target.kind}`}>
                      {KIND_LABELS[target.kind] || target.kind}
                    </span>
                  </td>
                  <td>
                    <span className="target-host-port mono">
                      {target.host}
                      {target.port ? <span className="target-port-pill">:{target.port}</span> : null}
                    </span>
                  </td>
                  <td>
                    <span className="target-path-cell mono" title={pathOrDnsDisplay}>
                      {pathOrDnsDisplay}
                    </span>
                  </td>
                  <td>
                    <div className="target-scope-cell">
                      <button
                        type="button"
                        className={`target-scope-badge scope-${scopeInfo.type}`}
                        title={scopeInfo.detail}
                        onClick={() =>
                          scopeInfo.nodeCount > 0 || scopeInfo.tags.length > 0
                            ? setExpandedScopeId(isScopePopoverOpen ? null : target.id)
                            : undefined
                        }
                      >
                        <span>{scopeInfo.label}</span>
                      </button>
                      {scopeInfo.hasRegionRules && (
                        <span
                          className="target-region-rule-badge"
                          title={`包含 ${scopeInfo.regionRulesCount} 条区域匹配规则`}
                        >
                          区域规则
                        </span>
                      )}
                      {isScopePopoverOpen && (scopeInfo.nodeNames.length > 0 || scopeInfo.tags.length > 0) && (
                        <div className="target-scope-popover" role="dialog" aria-label="执行范围详情">
                          <div className="target-scope-popover-head">
                            <span>执行范围明细</span>
                            <button
                              type="button"
                              className="target-scope-popover-close"
                              onClick={() => setExpandedScopeId(null)}
                            >
                              <X size={12} />
                            </button>
                          </div>
                          {scopeInfo.nodeNames.length > 0 && (
                            <div className="target-scope-popover-group">
                              <span className="group-lbl">指定节点 ({scopeInfo.nodeNames.length})：</span>
                              <span className="group-val">{scopeInfo.nodeNames.join('、')}</span>
                            </div>
                          )}
                          {scopeInfo.tags.length > 0 && (
                            <div className="target-scope-popover-group">
                              <span className="group-lbl">匹配标签：</span>
                              <span className="group-val">{scopeInfo.tagsJoined}</span>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  </td>
                  <td className="mono">
                    <span title={`超时: ${target.timeoutMs || 3000}ms`}>
                      {target.intervalSeconds !== null ? `${target.intervalSeconds}s` : '—'}
                    </span>
                  </td>
                  <td>
                    <div className="target-recent-status-cell" title={recentStatus.detail}>
                      <span className={`target-status-pill status-tone-${recentStatus.tone}`}>
                        <span className="status-dot-mini" />
                        {recentStatus.label}
                      </span>
                      {recentStatus.detail && recentStatus.tone !== 'unknown' && recentStatus.tone !== 'disabled' && (
                        <span className="target-status-reason">{recentStatus.detail}</span>
                      )}
                    </div>
                  </td>
                  <td className="mono target-checked-time-cell">
                    {formatCheckTimeShort(recentStatus.lastCheckedAt)}
                  </td>
                  <td>
                    {readOnly || (isMedia && !target.enabled) ? (
                      <span
                        className="target-state target-state-ipqa-disabled"
                        title={isMedia ? '流媒体探测已停用，由 IPQA 历史归档统一提供' : undefined}
                      >
                        <span className={`status-dot ${target.enabled ? 'status-online' : 'status-offline'}`} />
                        {target.enabled ? '已启用' : isMedia ? '已停用 · IPQA 接管' : '已停用'}
                      </span>
                    ) : (
                      <button
                        type="button"
                        className={`target-toggle ${target.enabled ? 'target-toggle-on' : ''}`}
                        disabled={mutatingId === target.id}
                        aria-pressed={target.enabled}
                        onClick={() => onToggle?.(target)}
                        title={isMedia ? '停用此历史流媒体目标（停用后由 IPQA 接管）' : '点击切换启用/停用状态'}
                      >
                        {mutatingId === target.id ? '切换中…' : target.enabled ? '已启用' : '已停用'}
                      </button>
                    )}
                  </td>
                  {!readOnly && (
                    <td className="target-cell-action">
                      <div className="target-row-btn-group">
                        {onEdit && (
                          <button
                            type="button"
                            className="text-button target-edit-btn"
                            aria-label={`${isMedia ? '查看' : '编辑'}检测目标 ${target.id || target.name}`}
                            disabled={mutatingId === target.id}
                            onClick={() => onEdit(target)}
                          >
                            {isMedia ? '查看' : '编辑'}
                          </button>
                        )}
                        <button
                          type="button"
                          className="text-button target-delete"
                          aria-label={`删除检测目标 ${target.id || target.name}`}
                          disabled={mutatingId === target.id}
                          onClick={(event) => {
                            event.preventDefault()
                            event.stopPropagation()
                            onDelete?.(target)
                          }}
                        >
                          {mutatingId === target.id ? '处理中…' : '删除'}
                        </button>
                      </div>
                    </td>
                  )}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* 移动端紧凑卡片列表 (<=768px) */}
      <div className="target-mobile-card-list">
        {targets.map((target, index) => {
          const isMedia = target.kind === 'media_http' || String(target.id).startsWith('media-')
          const scopeInfo = formatTargetScopeDisplay(target, nodes)
          const recentStatus = resolveTargetRecentStatus(target, telemetryMap)
          const isExpanded = expandedMobileIds.has(target.id)
          return (
            <div
              key={target.id || `mobile-target-${index}`}
              className={`target-mobile-card ${!target.enabled ? 'is-disabled' : ''}`}
              data-target-id={target.id || undefined}
            >
              <div className="target-mobile-card-top">
                <div className="target-mobile-title-wrap">
                  <span className={`target-kind target-kind-${target.kind}`}>
                    {KIND_LABELS[target.kind] || target.kind}
                  </span>
                  <strong className="target-mobile-name">{target.name}</strong>
                  {isMedia && <span className="target-tag-ipqa-muted">已由 IPQA 接管</span>}
                </div>
                <span className={`target-status-pill status-tone-${recentStatus.tone}`}>
                  <span className="status-dot-mini" />
                  {recentStatus.label}
                </span>
              </div>

              <div className="target-mobile-card-main">
                <div className="target-mobile-kv">
                  <span className="kv-label">主机</span>
                  <span className="kv-val mono">{target.host}</span>
                </div>
                <div className="target-mobile-kv">
                  <span className="kv-label">执行范围</span>
                  <span className="kv-val">
                    {scopeInfo.label}
                    {scopeInfo.hasRegionRules ? ' · 区域规则' : ''}
                  </span>
                </div>
                <div className="target-mobile-kv">
                  <span className="kv-label">启用状态</span>
                  <span className="kv-val">
                    {target.enabled ? '已启用' : isMedia ? '已停用 · IPQA 接管' : '已停用'}
                  </span>
                </div>
                {recentStatus.detail && recentStatus.tone !== 'unknown' && recentStatus.tone !== 'disabled' && (
                  <div className="target-mobile-kv">
                    <span className="kv-label">最近结果</span>
                    <span className="kv-val target-status-reason">{recentStatus.detail}</span>
                  </div>
                )}
              </div>

              {isExpanded && (
                <div className="target-mobile-card-extra">
                  <div className="target-mobile-kv">
                    <span className="kv-label">目标 ID</span>
                    <span className="kv-val mono">{target.id}</span>
                  </div>
                  <div className="target-mobile-kv">
                    <span className="kv-label">端口</span>
                    <span className="kv-val mono">{target.port ?? '—'}</span>
                  </div>
                  <div className="target-mobile-kv">
                    <span className="kv-label">路径 / 记录</span>
                    <span className="kv-val mono">
                      {target.kind === 'dns' ? target.dnsType || 'A' : target.path || '—'}
                    </span>
                  </div>
                  <div className="target-mobile-kv">
                    <span className="kv-label">检查间隔 / 超时</span>
                    <span className="kv-val mono">
                      {target.intervalSeconds || 60}s / {target.timeoutMs || 3000}ms
                    </span>
                  </div>
                  <div className="target-mobile-kv">
                    <span className="kv-label">最近检测时间</span>
                    <span className="kv-val mono">{formatCheckTimeShort(recentStatus.lastCheckedAt)}</span>
                  </div>
                  {scopeInfo.nodeNames.length > 0 && (
                    <div className="target-mobile-kv">
                      <span className="kv-label">指定节点</span>
                      <span className="kv-val">{scopeInfo.nodeNames.join('、')}</span>
                    </div>
                  )}
                </div>
              )}

              <div className="target-mobile-card-actions">
                <button
                  type="button"
                  className="target-mobile-btn"
                  onClick={() => toggleMobileCardExpand(target.id)}
                >
                  {isExpanded ? '收起详情' : '展开详情'}
                </button>
                {!readOnly && (
                  <>
                    {onEdit && (
                      <button
                        type="button"
                        className="target-mobile-btn"
                        onClick={() => onEdit(target)}
                      >
                        {isMedia ? '查看' : '编辑'}
                      </button>
                    )}
                    {!(isMedia && !target.enabled) && (
                      <button
                        type="button"
                        className="target-mobile-btn"
                        disabled={mutatingId === target.id}
                        onClick={() => onToggle?.(target)}
                      >
                        {target.enabled ? '停用' : '启用'}
                      </button>
                    )}
                    <button
                      type="button"
                      className="target-mobile-btn is-danger"
                      disabled={mutatingId === target.id}
                      onClick={() => onDelete?.(target)}
                    >
                      删除
                    </button>
                  </>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// readOnly 时只渲染列表（network/mtr 子页或 Guest 模式复用），kinds 限定显示的检测类型。
export function TargetManage({
  readOnly = false,
  kinds = null,
  title = '检测目标',
  nodes: propNodes = [],
  onBack = null,
}) {
  const [targets, setTargets] = useState([])
  const [nodes, setNodes] = useState(() => (Array.isArray(propNodes) ? propNodes : []))
  const [telemetryMap, setTelemetryMap] = useState(() => new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [status, setStatus] = useState(null)

  // 筛选状态
  const [filter, setFilter] = useState('all')
  const [scopeFilter, setScopeFilter] = useState('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedIds, setSelectedIds] = useState(() => new Set())

  // 抽屉状态（默认不展开创建表单！）
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [drawerMode, setDrawerMode] = useState('create') // 'create' | 'edit'
  const [editingTarget, setEditingTarget] = useState(null)
  const [form, setForm] = useState(() => emptyTargetForm('http'))
  const [formError, setFormError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [mutatingId, setMutatingId] = useState(null)
  const [batchRunning, setBatchRunning] = useState(false)

  // 抽屉内节点搜索、自定义标签输入与高级折叠
  const [nodeSearch, setNodeSearch] = useState('')
  const [customTagInput, setCustomTagInput] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)

  // 删除二次确认状态
  const [confirmDeleteTarget, setConfirmDeleteTarget] = useState(null)

  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  // 同步外部传入的节点列表，若为空则主动拉取 /api/nodes
  useEffect(() => {
    if (Array.isArray(propNodes) && propNodes.length > 0) {
      setNodes(propNodes)
      return undefined
    }
    const controller = new AbortController()
    fetch('/api/nodes', { credentials: 'same-origin', signal: controller.signal })
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => {
        if (Array.isArray(list) && !controller.signal.aborted) {
          setNodes(list)
        }
      })
      .catch(() => {})
    return () => controller.abort()
  }, [propNodes])

  // 加载目标列表（失败时保留旧 targets 数据，不清空列表！）
  const load = useCallback(async (signal) => {
    setLoading(true)
    setError('')
    try {
      const response = await fetch('/api/targets', { credentials: 'same-origin', signal })
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error('load')
      const json = await response.json()
      if (!Array.isArray(json)) throw new Error('invalid')
      if (!signal?.aborted) {
        setTargets(json.map(normalizeTarget))
      }
    } catch (caught) {
      if (caught?.name === 'AbortError') return
      if (!signal?.aborted) {
        setError(
          caught?.message === 'auth'
            ? '需要登录后才能查看检测目标。'
            : '无法刷新检测目标列表，已保留最近一次加载数据，请点击重试。'
        )
      }
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  // 聚合在线节点的真实采样结果 (/api/nodes/{uuid}/checks, /checks/summary & /mtr)，绝不伪造数值
  useEffect(() => {
    if (!Array.isArray(nodes) || nodes.length === 0 || targets.length === 0) return undefined
    const controller = new AbortController()
    const sampleNodes = nodes
      .filter((n) => n && (n.uuid || n.id))
      .slice(0, 6)

    if (sampleNodes.length === 0) return undefined

    Promise.all(
      sampleNodes.map(async (node) => {
        const uuid = node.uuid || node.id
        const [latestChecksRes, checksRes, mtrRes] = await Promise.all([
          fetch(`/api/nodes/${encodeURIComponent(uuid)}/checks`, {
            credentials: 'same-origin',
            signal: controller.signal,
          })
            .then((r) => (r.ok ? r.json() : []))
            .catch(() => []),
          fetch(`/api/nodes/${encodeURIComponent(uuid)}/checks/summary`, {
            credentials: 'same-origin',
            signal: controller.signal,
          })
            .then((r) => (r.ok ? r.json() : []))
            .catch(() => []),
          fetch(`/api/nodes/${encodeURIComponent(uuid)}/mtr`, {
            credentials: 'same-origin',
            signal: controller.signal,
          })
            .then((r) => (r.ok ? r.json() : []))
            .catch(() => []),
        ])
        return {
          latestChecks: Array.isArray(latestChecksRes) ? latestChecksRes : [],
          checks: Array.isArray(checksRes) ? checksRes : [],
          mtr: Array.isArray(mtrRes) ? mtrRes : [],
        }
      })
    )
      .then((results) => {
        if (controller.signal.aborted) return
        const nextMap = new Map()
        const ensureEntry = (tId) => {
          if (!nextMap.has(tId)) {
            nextMap.set(tId, {
              hasSample: false,
              successNodes: 0,
              failedNodes: 0,
              latestSuccessNodes: 0,
              latestFailedNodes: 0,
              maxLossRate: 0,
              latencySum: 0,
              latencyCount: 0,
              avgLatencyMs: null,
              lastCheckedAt: null,
              failureReason: '',
            })
          }
          return nextMap.get(tId)
        }

        for (const { latestChecks, checks, mtr } of results) {
          // 1. 最新单次检测结果 (/api/nodes/{uuid}/checks -> network_results_latest)
          for (const item of latestChecks) {
            const tId = item?.target_id || item?.result?.target_id
            if (!tId) continue
            const entry = ensureEntry(tId)
            const checkedAt = item.checked_at || item?.result?.checked_at
            if (checkedAt) {
              if (!entry.lastCheckedAt || new Date(checkedAt) > new Date(entry.lastCheckedAt)) {
                entry.lastCheckedAt = checkedAt
              }
            }
            const res = item?.result || item
            if (res && (res.status || res.error || typeof res.latency_ms === 'number')) {
              entry.hasSample = true
              const isDown = res.status === 'down' || res.status === 'failed' || Boolean(res.error)
              if (isDown) {
                entry.latestFailedNodes += 1
                entry.failedNodes += 1
                const errDetail = String(res.error || '').trim()
                if (errDetail) {
                  entry.failureReason = errDetail
                } else if (res.status_code) {
                  entry.failureReason = `HTTP 状态码异常 (${res.status_code})`
                } else if (!entry.failureReason) {
                  entry.failureReason = '最近一次探测失败'
                }
              } else {
                entry.latestSuccessNodes += 1
                entry.successNodes += 1
                if (typeof res.latency_ms === 'number' && res.latency_ms > 0 && entry.avgLatencyMs === null) {
                  entry.avgLatencyMs = res.latency_ms
                }
              }
            }
          }

          // 2. 窗口期采样聚合 (/api/nodes/{uuid}/checks/summary)
          for (const c of checks) {
            const tId = c?.target_id
            if (!tId) continue
            const entry = ensureEntry(tId)
            if (c.last_checked_at) {
              if (!entry.lastCheckedAt || new Date(c.last_checked_at) > new Date(entry.lastCheckedAt)) {
                entry.lastCheckedAt = c.last_checked_at
              }
            }
            const total = Number(c.total ?? 0)
            const failure = Number(c.failure ?? 0)
            const success = Number(c.success ?? 0)
            if (total > 0 || c.sample_count > 0) {
              entry.hasSample = true
              const loss = typeof c.loss_rate === 'number' ? c.loss_rate : total > 0 ? failure / total : 0
              if (loss > entry.maxLossRate) entry.maxLossRate = loss
              if (failure > 0 && success === 0) {
                entry.failedNodes += 1
                if (!entry.failureReason) {
                  entry.failureReason = '探测失败 (100% 丢包/超时)'
                }
              } else if (failure > 0) {
                entry.failedNodes += 1
                entry.successNodes += 1
                if (!entry.failureReason) {
                  entry.failureReason = `采样丢包 ${Math.round(loss * 100)}%`
                }
              } else {
                entry.successNodes += 1
              }
              if (typeof c.latency_avg_ms === 'number' && c.latency_avg_ms > 0) {
                entry.latencySum += c.latency_avg_ms
                entry.latencyCount += 1
                entry.avgLatencyMs = entry.latencySum / entry.latencyCount
              }
            }
          }

          // 3. MTR 路由追踪最新结果 (/api/nodes/{uuid}/mtr)
          for (const m of mtr) {
            const tId = m?.target_id || m?.id || m?.result?.target_id
            if (!tId) continue
            const entry = ensureEntry(tId)
            const checkedAt = m.checked_at || m.timestamp
            if (checkedAt) {
              const iso = typeof checkedAt === 'number' ? new Date(checkedAt * 1000).toISOString() : checkedAt
              if (!entry.lastCheckedAt || new Date(iso) > new Date(entry.lastCheckedAt)) {
                entry.lastCheckedAt = iso
              }
            }
            entry.hasSample = true
            const mRes = m?.result || m
            if (mRes.reached === false || m.success === false || mRes.error || m.error) {
              entry.failedNodes += 1
              entry.failureReason = mRes.error || m.error || 'MTR 路由追踪未达目标'
            } else {
              entry.successNodes += 1
            }
          }
        }

        setTelemetryMap(nextMap)
      })
      .catch(() => {})

    return () => controller.abort()
  }, [nodes, targets])

  // 顶部 4 个统计卡片真实计算（已停用目标绝不计入失败）
  const summaryStats = useMemo(() => {
    const baseList = kinds ? targets.filter((t) => kinds.includes(t.kind)) : targets
    const total = baseList.length
    const enabled = baseList.filter((t) => t.enabled).length
    const disabled = total - enabled
    let failedCount = 0
    let sampledCount = 0
    for (const t of baseList) {
      if (!t.enabled) continue
      const st = resolveTargetRecentStatus(t, telemetryMap)
      if (telemetryMap.get(t.id)?.hasSample) sampledCount += 1
      if (st.isFailed) failedCount += 1
    }
    return {
      total,
      enabled,
      disabled,
      failedCount,
      sampledCount,
      hasTelemetry: sampledCount > 0,
    }
  }, [targets, kinds, telemetryMap])

  // 筛选后的可见目标列表
  const visible = useMemo(() => {
    let list = kinds ? targets.filter((target) => kinds.includes(target.kind)) : targets
    if (!kinds) {
      if (filter === 'enabled') list = list.filter((t) => t.enabled)
      else if (filter === 'disabled') list = list.filter((t) => !t.enabled)
      else if (filter === 'http_all') list = list.filter((t) => t.kind === 'http' || t.kind === 'https')
      else if (filter === 'network') list = list.filter((t) => t.kind !== 'media_http')
      else if (filter !== 'all') list = list.filter((t) => t.kind === filter)
    }
    if (scopeFilter !== 'all') {
      list = list.filter((t) => {
        const sc = formatTargetScopeDisplay(t, nodes)
        if (scopeFilter === 'global') return sc.type === 'global'
        if (scopeFilter === 'nodes') return sc.type === 'nodes' || sc.type === 'hybrid'
        if (scopeFilter === 'tags') return sc.type === 'tags' || sc.type === 'hybrid'
        if (scopeFilter === 'region') return sc.hasRegionRules
        return true
      })
    }
    if (searchQuery.trim()) {
      const q = searchQuery.trim().toLowerCase()
      list = list.filter((t) => {
        const sc = formatTargetScopeDisplay(t, nodes)
        return (
          t.name.toLowerCase().includes(q) ||
          t.id.toLowerCase().includes(q) ||
          t.host.toLowerCase().includes(q) ||
          sc.label.toLowerCase().includes(q) ||
          sc.detail.toLowerCase().includes(q)
        )
      })
    }
    return list
  }, [targets, kinds, filter, scopeFilter, searchQuery, nodes])

  const hasActiveMediaTargets = useMemo(
    () => targets.some((t) => t.kind === 'media_http' && t.enabled),
    [targets]
  )
  const hasAnyMediaTargets = useMemo(
    () => targets.some((t) => t.kind === 'media_http' || String(t.id).startsWith('media-')),
    [targets]
  )

  // 从当前节点真实标签生成可选标签列表
  const availableTags = useMemo(() => extractAvailableNodeTags(nodes), [nodes])

  // 抽屉内节点搜索过滤
  const filteredDrawerNodes = useMemo(() => {
    if (!nodeSearch.trim()) return nodes
    const q = nodeSearch.trim().toLowerCase()
    return nodes.filter((n) => {
      const name = String(n?.name || n?.hostname || '').toLowerCase()
      const region = String(n?.region || n?.country || '').toLowerCase()
      const tagsStr = Array.isArray(n?.tags) ? n.tags.join(' ').toLowerCase() : String(n?.tags || '').toLowerCase()
      return name.includes(q) || region.includes(q) || tagsStr.includes(q)
    })
  }, [nodes, nodeSearch])

  // 实时计算当前表单执行范围匹配的节点列表（方便 MJJ 创建时直接看出将在哪些探针执行）
  const matchedNodesPreview = useMemo(() => {
    const draftPayload = buildTargetSubmitPayload(form, editingTarget)
    const matched = nodes.filter((n) => nodeMatchesTarget(n, draftPayload))
    return {
      count: matched.length,
      total: nodes.length,
      names: matched.map((n) => String(n?.name || n?.hostname || '未命名节点').trim()).filter(Boolean),
    }
  }, [form, editingTarget, nodes])

  // 打开新建抽屉
  const openCreateDrawer = useCallback((initialKind = 'http') => {
    setDrawerMode('create')
    setEditingTarget(null)
    setForm(emptyTargetForm(initialKind))
    setFormError('')
    setNodeSearch('')
    setCustomTagInput('')
    setAdvancedOpen(false)
    setDrawerOpen(true)
  }, [])

  // 快捷模板打开抽屉（仅预填表单，绝不直接写库）
  const openQuickTemplateDrawer = useCallback((templateKey) => {
    setDrawerMode('create')
    setEditingTarget(null)
    setForm(applyTargetQuickTemplate(templateKey, emptyTargetForm()))
    setFormError('')
    setNodeSearch('')
    setCustomTagInput('')
    setAdvancedOpen(false)
    setDrawerOpen(true)
  }, [])

  // 打开编辑/查看抽屉（复用同一个抽屉，完整保留 node_ids、node_tags、region_rules）
  const openEditDrawer = useCallback((target) => {
    if (!target) return
    setDrawerMode('edit')
    setEditingTarget(target)
    const built = buildTargetFormFromRecord(target)
    setForm(built || emptyTargetForm(target.kind))
    setFormError('')
    setNodeSearch('')
    setCustomTagInput('')
    setAdvancedOpen(Array.isArray(built?.regionRulesList) && built.regionRulesList.length > 0)
    setDrawerOpen(true)
  }, [])

  const closeDrawer = useCallback(() => {
    if (submitting) return
    setDrawerOpen(false)
    setFormError('')
  }, [submitting])

  const disableAllMediaTargets = async () => {
    setMutatingId('batch-disable-media')
    setError('')
    setStatus({ kind: 'info', message: '正在停用历史流媒体检测目标…' })
    try {
      const response = await executeWithCsrfRetry(
        (csrfToken) =>
          fetch('/api/targets/disable-media', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          }),
        fetchCsrfToken
      )
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error((await readErrorDetail(response)) || `停用失败（${response.status}）`)
      const res = await response.json()
      setTargets((current) =>
        current.map((item) => (item.kind === 'media_http' ? { ...item, enabled: false } : item))
      )
      setStatus({
        kind: 'ok',
        message: `已成功停用 ${res.disabled || 0} 个历史流媒体目标，流媒体状态已交由 IPQA 归档统一接管。`,
      })
    } catch (caught) {
      const messages = {
        auth: '需要登录后才能停用检测目标。',
        csrf: '无法获取安全令牌，请刷新后重试。',
      }
      setError(messages[caught?.message] || caught?.message || '停用失败，请稍后重试。')
    } finally {
      setMutatingId(null)
    }
  }

  const toggleEnabled = async (target) => {
    const isMedia = target.kind === 'media_http' || String(target.id).startsWith('media-')
    if (isMedia && !target.enabled) {
      setError('流媒体检测已统一由节点 IPQA 历史归档接管，不再支持重新启用独立 media-* 周期探测。')
      return
    }
    setMutatingId(target.id)
    setError('')
    setStatus(null)
    try {
      const response = await executeWithCsrfRetry(
        (csrfToken) =>
          fetch(`/api/targets/${encodeURIComponent(target.id)}`, {
            method: 'PATCH',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
            body: JSON.stringify({ enabled: !target.enabled }),
          }),
        fetchCsrfToken
      )
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error((await readErrorDetail(response)) || `启停失败（${response.status}）`)
      const updated = normalizeTarget(await response.json())
      setTargets((current) => current.map((item) => (item.id === updated.id ? updated : item)))
      setStatus({
        kind: 'ok',
        message: `检测目标「${updated.name}」已${updated.enabled ? '启用' : '停用'}。`,
      })
    } catch (caught) {
      const messages = {
        auth: '需要登录后才能修改检测目标。',
        csrf: '无法获取安全令牌，请刷新后重试。',
      }
      setError(messages[caught?.message] || caught?.message || '启停失败，请稍后重试。')
    } finally {
      setMutatingId(null)
    }
  }

  // 批量串行启停（严格串行执行，避免并发 CSRF 轮换冲突）
  const handleBatchToggle = async (enableState) => {
    if (batchRunning || selectedIds.size === 0) return
    const targetList = targets.filter(
      (t) =>
        selectedIds.has(t.id) &&
        t.enabled !== enableState &&
        !(enableState && (t.kind === 'media_http' || String(t.id).startsWith('media-')))
    )
    if (targetList.length === 0) return

    setBatchRunning(true)
    setError('')
    setStatus({ kind: 'info', message: `正在串行${enableState ? '启用' : '停用'} ${targetList.length} 个目标…` })

    let successCount = 0
    let failCount = 0

    for (const target of targetList) {
      try {
        const response = await executeWithCsrfRetry(
          (csrfToken) =>
            fetch(`/api/targets/${encodeURIComponent(target.id)}`, {
              method: 'PATCH',
              credentials: 'same-origin',
              headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
              body: JSON.stringify({ enabled: enableState }),
            }),
          fetchCsrfToken
        )
        if (!response.ok) throw new Error('patch_failed')
        const updated = normalizeTarget(await response.json())
        setTargets((current) => current.map((item) => (item.id === updated.id ? updated : item)))
        successCount += 1
      } catch {
        failCount += 1
      }
    }

    setBatchRunning(false)
    setSelectedIds(new Set())
    if (failCount > 0) {
      setError(`批量操作完成：成功 ${successCount} 个，失败 ${failCount} 个（失败项已保留原状态）。`)
    } else {
      setStatus({ kind: 'ok', message: `已批量${enableState ? '启用' : '停用'} ${successCount} 个检测目标。` })
    }
  }

  // 请求删除目标（先打开二次确认弹窗）
  const requestDeleteTarget = (target) => {
    if (!target?.id) return
    setConfirmDeleteTarget(target)
  }

  // 确认删除目标（删除失败绝不从列表乐观移除）
  const confirmAndDeleteTarget = async (targetToDelete = confirmDeleteTarget) => {
    const target = targetToDelete
    if (!target?.id) return
    setConfirmDeleteTarget(null)
    setMutatingId(target.id)
    setError('')
    setStatus({ kind: 'info', message: '正在删除检测目标…' })
    try {
      const response = await executeWithCsrfRetry(
        (csrfToken) =>
          fetch(`/api/targets/${encodeURIComponent(target.id)}`, {
            method: 'DELETE',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          }),
        fetchCsrfToken
      )
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error((await readErrorDetail(response)) || `删除失败（${response.status}）`)
      // 仅在服务端成功返回后移除
      setTargets((current) => current.filter((item) => item.id !== target.id))
      setSelectedIds((prev) => {
        const next = new Set(prev)
        next.delete(target.id)
        return next
      })
      setStatus({ kind: 'ok', message: `检测目标 ${target.name} 已删除。` })
    } catch (caught) {
      const messages = {
        auth: '需要登录后才能删除检测目标。',
        csrf: '无法获取安全令牌，请刷新后重试。',
      }
      setError(messages[caught?.message] || caught?.message || '删除失败，目标已保留在列表中。')
    } finally {
      setMutatingId(null)
    }
  }

  const setField = (key) => (event) => {
    const val = event.target.value
    setForm((current) => ({ ...current, [key]: val }))
  }

  const handleHostInput = (event) => {
    const rawVal = event.target.value
    setForm((current) => {
      const next = { ...current, host: rawVal }
      const trimmed = rawVal.trim()
      if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
        try {
          const u = new URL(trimmed)
          next.kind = u.protocol === 'https:' ? 'https' : 'http'
          next.host = u.hostname
          next.port = u.port || (u.protocol === 'https:' ? '443' : '80')
          if (u.pathname && u.pathname !== '/') next.path = u.pathname
        } catch {}
      }
      return next
    })
  }

  const changeKind = (event) => {
    const kind = event.target.value
    setForm((current) => ({
      ...current,
      kind,
      port: String(KIND_PORT_DEFAULTS[kind] ?? 80),
      path: HTTP_LIKE_KINDS.includes(kind) ? current.path || '/health' : '',
    }))
  }

  // 节点范围切换
  const handleScopeModeChange = (mode) => {
    setForm((current) => ({
      ...current,
      scopeMode: mode,
      nodeIds: mode === 'all' || mode === 'tags' ? [] : current.nodeIds,
      nodeTags: mode === 'all' || mode === 'nodes' ? [] : current.nodeTags,
    }))
  }

  const toggleNodeIdInForm = (nodeUuid) => {
    if (!nodeUuid) return
    setForm((current) => {
      const set = new Set(current.nodeIds)
      if (set.has(nodeUuid)) set.delete(nodeUuid)
      else set.add(nodeUuid)
      const nextIds = Array.from(set)
      const hasTags = current.nodeTags.length > 0
      const nextMode =
        nextIds.length > 0 && hasTags
          ? 'custom'
          : nextIds.length > 0
            ? 'nodes'
            : hasTags
              ? 'tags'
              : current.scopeMode === 'all'
                ? 'nodes'
                : current.scopeMode
      return {
        ...current,
        scopeMode: nextMode,
        nodeIds: nextIds,
      }
    })
  }

  const selectAllFilteredNodes = () => {
    const allIds = filteredDrawerNodes.map((n) => n.uuid || n.id).filter(Boolean)
    setForm((current) => ({
      ...current,
      scopeMode: current.nodeTags.length > 0 ? 'custom' : 'nodes',
      nodeIds: Array.from(new Set([...current.nodeIds, ...allIds])),
    }))
  }

  const clearSelectedNodes = () => {
    setForm((current) => ({
      ...current,
      nodeIds: [],
    }))
  }

  const toggleTagInForm = (tag) => {
    const clean = String(tag || '').trim()
    if (!clean) return
    setForm((current) => {
      const set = new Set(current.nodeTags)
      if (set.has(clean)) set.delete(clean)
      else set.add(clean)
      const nextTags = Array.from(set)
      const hasNodes = current.nodeIds.length > 0
      const nextMode =
        hasNodes && nextTags.length > 0
          ? 'custom'
          : nextTags.length > 0
            ? 'tags'
            : hasNodes
              ? 'nodes'
              : current.scopeMode === 'all'
                ? 'tags'
                : current.scopeMode
      return {
        ...current,
        scopeMode: nextMode,
        nodeTags: nextTags,
      }
    })
  }

  const handleAddCustomTag = () => {
    const raw = customTagInput.trim()
    if (!raw) return
    const tagsToAdd = raw
      .split(/[,\s]+/)
      .map((t) => t.trim())
      .filter(Boolean)
    if (tagsToAdd.length === 0) return
    setForm((current) => {
      const nextTags = Array.from(new Set([...current.nodeTags, ...tagsToAdd]))
      return {
        ...current,
        scopeMode: current.nodeIds.length > 0 ? 'custom' : 'tags',
        nodeTags: nextTags,
      }
    })
    setCustomTagInput('')
  }

  // 区域规则表单化增删改
  const addRegionRuleRow = () => {
    setForm((current) => ({
      ...current,
      regionRulesCleared: false,
      regionRulesList: [...(current.regionRulesList || []), { region: '', contains: '' }],
    }))
  }

  const updateRegionRuleRow = (idx, key, val) => {
    setForm((current) => {
      const list = [...(current.regionRulesList || [])]
      if (!list[idx]) return current
      list[idx] = { ...list[idx], [key]: val }
      return {
        ...current,
        regionRulesList: list,
      }
    })
  }

  const removeRegionRuleRow = (idx) => {
    setForm((current) => {
      const list = (current.regionRulesList || []).filter((_, i) => i !== idx)
      return {
        ...current,
        regionRulesList: list,
        regionRulesCleared: list.length === 0,
      }
    })
  }

  // 提交新建或编辑
  const submit = async (event) => {
    event.preventDefault()
    if (submitting) return

    const isEditing = drawerMode === 'edit' && Boolean(editingTarget)
    const isMedia = form.kind === 'media_http' || (editingTarget && editingTarget.kind === 'media_http')

    if (!isEditing && isMedia) {
      setFormError('流媒体检测已转由节点 IPQA 历史归档统一提供，不再支持新建 media_http 检测目标。')
      return
    }
    if (isEditing && isMedia) {
      setFormError('历史流媒体目标已由 IPQA 接管，处于只读状态。')
      return
    }

    const detail = validateTargetForm(form)
    if (detail) {
      setFormError(detail)
      return
    }

    const body = buildTargetSubmitPayload(form, isEditing ? editingTarget : null)
    // 非 media_http 新建目标若未配置有效区域规则，不附带 region_rules 字段以符合后端协议校验
    if (body.kind !== 'media_http' && (!isEditing || !editingTarget?.region_rules?.length)) {
      delete body.region_rules
    }

    setSubmitting(true)
    setFormError('')
    setStatus(null)

    try {
      const url = isEditing
        ? `/api/targets/${encodeURIComponent(editingTarget.id)}`
        : '/api/targets'
      const method = isEditing ? 'PATCH' : 'POST'

      const response = await executeWithCsrfRetry(
        (csrfToken) =>
          fetch(url, {
            method,
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
            body: JSON.stringify(body),
          }),
        fetchCsrfToken
      )
      if (response.status === 401) throw new Error('auth')
      if (response.status === 403) throw new Error('forbidden')
      if (!response.ok) {
        throw new Error(
          (await readErrorDetail(response)) || `${isEditing ? '更新' : '创建'}失败（${response.status}）`
        )
      }
      const saved = normalizeTarget(await response.json())
      if (isEditing) {
        setTargets((current) => current.map((item) => (item.id === saved.id ? saved : item)))
        setStatus({ kind: 'ok', message: `检测目标「${saved.name}」已更新。` })
      } else {
        setTargets((current) => [...current, saved])
        setStatus({ kind: 'ok', message: `检测目标「${saved.name}」已创建。` })
      }
      setDrawerOpen(false)
      setEditingTarget(null)
      setForm(emptyTargetForm('http'))
    } catch (caught) {
      const messages = {
        auth: `需要登录后才能${isEditing ? '编辑' : '创建'}检测目标。`,
        csrf: '无法获取安全令牌，请刷新后重试。',
        forbidden: `${isEditing ? '更新' : '创建'}检测目标被拒绝，请刷新后重试。`,
      }
      setFormError(messages[caught?.message] || caught?.message || `${isEditing ? '更新' : '创建'}失败，请稍后重试。`)
    } finally {
      setSubmitting(false)
    }
  }

  // 只读视图（network/mtr 子页或 Guest 权限复用）
  if (readOnly) {
    return (
      <div className="target-manage-shell">
        <div className="panel target-panel">
          <div className="panel-header target-compact-header">
            <div>
              <h2>{title}</h2>
              <p>只读视图 · 共 {visible.length} 个目标</p>
            </div>
            <div className="target-header-actions">
              <button type="button" className="button button-quiet" onClick={() => load()}>
                <ArrowClockwise size={14} />
                刷新
              </button>
            </div>
          </div>
          {error && (
            <div className="api-state api-state-error target-error-banner" role="status">
              <span>{error}</span>
              <button type="button" className="button button-quiet target-retry-btn" onClick={() => load()}>
                重试
              </button>
            </div>
          )}
          <TargetTable
            targets={visible}
            nodes={nodes}
            telemetryMap={telemetryMap}
            readOnly
            loading={loading}
            emptyDetail="当前没有可展示的检测目标。"
          />
        </div>
      </div>
    )
  }

  const isDrawerReadOnlyMedia =
    drawerMode === 'edit' &&
    editingTarget &&
    (editingTarget.kind === 'media_http' || String(editingTarget.id).startsWith('media-'))

  return (
    <div className="target-manage-shell">
      {/* 1. 顶部紧凑标题与主操作栏 */}
      <div className="target-top-bar">
        <div className="target-top-bar-left">
          <div className="target-eyebrow">PROBEWATCH · 探测调度中心</div>
          <div className="target-title-row">
            <h1 className="target-main-title">检测目标</h1>
            <span className="target-subtitle-inline">
              管理 HTTP / TCP / DNS / MTR 探测目标、指定探针与标签分发规则
            </span>
          </div>
        </div>
        <div className="target-top-bar-actions">
          <button
            type="button"
            className="button button-quiet target-btn-refresh"
            onClick={() => load()}
            disabled={loading}
            title="刷新检测目标与采样状态"
          >
            <ArrowClockwise size={14} className={loading ? 'spin' : ''} />
            刷新
          </button>
          {hasActiveMediaTargets && (
            <button
              type="button"
              className="button button-warning target-btn-disable-media"
              onClick={disableAllMediaTargets}
              disabled={mutatingId === 'batch-disable-media'}
              title="一键停用所有历史流媒体检测目标，由 IPQA 归档统一接管"
            >
              {mutatingId === 'batch-disable-media' ? '正在停用…' : '停用流媒体目标'}
            </button>
          )}
          {onBack && (
            <button type="button" className="button button-quiet" onClick={onBack}>
              返回仪表盘
            </button>
          )}
          <button
            type="button"
            className="button button-primary target-btn-create"
            onClick={() => openCreateDrawer('http')}
          >
            <Plus size={14} weight="bold" />
            新建目标
          </button>
        </div>
      </div>

      {/* 2. 顶部 4 个小型统计卡片 */}
      <div className="target-stats-grid-4" data-testid="target-stats-grid">
        <div className="target-stat-mini-card">
          <div className="stat-mini-head">
            <span className="stat-mini-label">全部目标</span>
            <Crosshair size={15} className="text-blue" />
          </div>
          <div className="stat-mini-val mono">{summaryStats.total}</div>
          <div className="stat-mini-sub">
            覆盖 {nodes.length} 台已注册节点
          </div>
        </div>

        <div className="target-stat-mini-card is-mint">
          <div className="stat-mini-head">
            <span className="stat-mini-label">已启用</span>
            <CheckCircle size={15} className="text-mint" />
          </div>
          <div className="stat-mini-val mono text-mint">{summaryStats.enabled}</div>
          <div className="stat-mini-sub">探针按执行范围周期采样</div>
        </div>

        <div className="target-stat-mini-card is-muted">
          <div className="stat-mini-head">
            <span className="stat-mini-label">已停用</span>
            <Power size={15} className="text-muted" />
          </div>
          <div className="stat-mini-val mono">{summaryStats.disabled}</div>
          <div className="stat-mini-sub">已暂停探针下发调度</div>
        </div>

        <div className={`target-stat-mini-card ${summaryStats.failedCount > 0 ? 'is-rose' : ''}`}>
          <div className="stat-mini-head">
            <span className="stat-mini-label">最近失败</span>
            <WarningCircle size={15} className={summaryStats.failedCount > 0 ? 'text-rose' : 'text-muted'} />
          </div>
          <div className={`stat-mini-val mono ${summaryStats.failedCount > 0 ? 'text-rose' : ''}`}>
            {summaryStats.hasTelemetry ? summaryStats.failedCount : '暂无数据'}
          </div>
          <div className="stat-mini-sub">
            {summaryStats.hasTelemetry
              ? summaryStats.failedCount > 0
                ? `${summaryStats.failedCount} 个目标存在异常或丢包`
                : '近期已采样目标全部正常'
              : '暂无数据 · 等待首次采样回报'}
          </div>
        </div>
      </div>

      {/* 3. 筛选工具栏与列表主面板 */}
      <div className="panel target-panel">
        <div className="target-toolbar">
          <div className="target-filter-tabs" role="tablist" aria-label="按类型或状态筛选">
            {FILTER_TABS.filter((tab) => tab.key !== 'media_http' || hasAnyMediaTargets || filter === 'media_http').map(
              (tab) => (
                <button
                  key={tab.key}
                  type="button"
                  className={`target-filter-tab ${filter === tab.key ? 'filter-active' : ''} ${
                    tab.muted ? 'filter-tab-readonly-muted' : ''
                  }`}
                  aria-pressed={filter === tab.key}
                  onClick={() => setFilter(tab.key)}
                >
                  {tab.label}
                </button>
              )
            )}
          </div>

          <div className="target-toolbar-right">
            {selectedIds.size > 0 && (
              <div className="target-batch-bar">
                <span className="batch-count mono">已选 {selectedIds.size} 项</span>
                <button
                  type="button"
                  className="button button-quiet batch-btn"
                  disabled={batchRunning}
                  onClick={() => handleBatchToggle(true)}
                >
                  批量启用
                </button>
                <button
                  type="button"
                  className="button button-quiet batch-btn"
                  disabled={batchRunning}
                  onClick={() => handleBatchToggle(false)}
                >
                  批量停用
                </button>
              </div>
            )}

            <div className="target-scope-select-wrap">
              <Funnel size={13} className="text-muted" />
              <select
                className="target-scope-filter-select"
                value={scopeFilter}
                onChange={(e) => setScopeFilter(e.target.value)}
                aria-label="按执行范围筛选"
              >
                <option value="all">全部范围</option>
                <option value="global">全部节点</option>
                <option value="nodes">指定节点</option>
                <option value="tags">节点标签</option>
                <option value="region">含区域规则</option>
              </select>
            </div>

            <div className="target-search-box">
              <MagnifyingGlass size={13} className="text-muted" />
              <input
                type="text"
                className="target-search-input"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="搜索名称 / ID / 主机 / 标签…"
                aria-label="搜索检测目标"
              />
              {searchQuery && (
                <button
                  type="button"
                  className="target-search-clear"
                  onClick={() => setSearchQuery('')}
                  aria-label="清空搜索"
                >
                  <X size={12} />
                </button>
              )}
            </div>
          </div>
        </div>

        {(error || status) && (
          <div
            className={`api-state ${error ? 'api-state-error' : 'api-state-ok'} target-error-banner`}
            role="status"
          >
            <span>{error || status?.message}</span>
            {error && (
              <button type="button" className="button button-quiet target-retry-btn" onClick={() => load()}>
                重试
              </button>
            )}
          </div>
        )}

        {hasAnyMediaTargets && (filter === 'all' || filter === 'disabled' || filter === 'media_http') && (
          <div className="target-media-notice">
            <span>
              流媒体探测已转由节点 IPQA 历史归档统一提供，历史 <code>media_http</code> 目标显示为「已由 IPQA 接管」（仅支持查看、停用或删除，不再支持新建）。
            </span>
          </div>
        )}

        <TargetTable
          targets={visible}
          nodes={nodes}
          telemetryMap={telemetryMap}
          loading={loading}
          mutatingId={mutatingId}
          selectedIds={selectedIds}
          onSelectToggle={(id) =>
            setSelectedIds((prev) => {
              const next = new Set(prev)
              if (next.has(id)) next.delete(id)
              else next.add(id)
              return next
            })
          }
          onSelectAll={(checked) => {
            if (checked) {
              setSelectedIds(new Set(visible.map((t) => t.id).filter(Boolean)))
            } else {
              setSelectedIds(new Set())
            }
          }}
          onEdit={openEditDrawer}
          onToggle={toggleEnabled}
          onDelete={requestDeleteTarget}
          onOpenCreate={() => openCreateDrawer('http')}
          onQuickCreate={openQuickTemplateDrawer}
          emptyDetail={
            targets.length === 0
              ? '添加一个 HTTP、TCP 或 DNS 目标后，探针会按执行范围开始采样。'
              : '当前筛选条件下没有匹配的检测目标。'
          }
        />
      </div>

      {/* 4. 右侧创建 / 编辑抽屉 (Drawer) */}
      {drawerOpen && (
        <div
          className="target-drawer-backdrop"
          onClick={closeDrawer}
          role="presentation"
        >
          <aside
            className="target-drawer-panel target-form-panel"
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-label={drawerMode === 'edit' ? '编辑检测目标' : '新建检测目标'}
          >
            <div className="target-drawer-header">
              <div>
                <h2 className="target-drawer-title">
                  {isDrawerReadOnlyMedia
                    ? '查看历史流媒体目标（只读）'
                    : drawerMode === 'edit'
                      ? `编辑目标 · ${editingTarget?.name || ''}`
                      : '新建检测目标'}
                </h2>
                <p className="target-drawer-subtitle">
                  {isDrawerReadOnlyMedia
                    ? '已由 IPQA 接管，仅展示历史配置与区域规则。'
                    : '配置探测协议参数、检查频率与探针执行范围。'}
                </p>
              </div>
              <button
                type="button"
                className="target-drawer-close"
                onClick={closeDrawer}
                aria-label="关闭抽屉"
              >
                <X size={18} />
              </button>
            </div>

            {formError && (
              <div className="api-state api-state-error target-drawer-error" role="alert">
                {formError}
              </div>
            )}

            <form className="target-drawer-form target-form" onSubmit={submit} noValidate>
              <div className="target-drawer-body">
                {/* 分组 A: 基本信息 */}
                <section className="target-form-section">
                  <div className="target-section-title">A. 基本信息</div>
                  <div className="target-form-grid">
                    <label className="field">
                      <span className="field-label">类型 *</span>
                      <select
                        className="field-select"
                        value={form.kind}
                        onChange={changeKind}
                        disabled={drawerMode === 'edit'}
                      >
                        {(isDrawerReadOnlyMedia ? TARGET_KINDS : CREATE_TARGET_KINDS).map((kind) => (
                          <option key={kind} value={kind}>
                            {KIND_LABELS[kind]}（{kind}）
                          </option>
                        ))}
                      </select>
                    </label>

                    <label className="field">
                      <span className="field-label">名称 *</span>
                      <input
                        className="field-input"
                        value={form.name}
                        onChange={setField('name')}
                        maxLength={128}
                        disabled={isDrawerReadOnlyMedia}
                        placeholder="例如：上海 HTTP 探测"
                      />
                    </label>

                    <label className="field">
                      <span className="field-label">ID *</span>
                      <input
                        className="field-input mono"
                        value={form.id}
                        onChange={setField('id')}
                        maxLength={128}
                        disabled={drawerMode === 'edit'}
                        placeholder="例如：http-sh-01"
                      />
                    </label>

                    <label className="field">
                      <span className="field-label">主机 *</span>
                      <input
                        className="field-input mono"
                        value={form.host}
                        onChange={handleHostInput}
                        maxLength={253}
                        disabled={isDrawerReadOnlyMedia}
                        placeholder="域名或 IP，不含协议"
                      />
                    </label>

                    <label className="field">
                      <span className="field-label">端口 *</span>
                      <input
                        className="field-input mono"
                        type="number"
                        min={1}
                        max={65535}
                        value={form.port}
                        onChange={setField('port')}
                        disabled={isDrawerReadOnlyMedia}
                      />
                    </label>

                    {HTTP_LIKE_KINDS.includes(form.kind) && (
                      <label className="field">
                        <span className="field-label">路径 *</span>
                        <input
                          className="field-input mono"
                          value={form.path}
                          onChange={setField('path')}
                          maxLength={2048}
                          disabled={isDrawerReadOnlyMedia}
                          placeholder="/health"
                        />
                      </label>
                    )}

                    {form.kind === 'dns' && (
                      <label className="field">
                        <span className="field-label">DNS 记录类型 *</span>
                        <select
                          className="field-select mono"
                          value={form.dnsType}
                          onChange={setField('dnsType')}
                          disabled={isDrawerReadOnlyMedia}
                        >
                          {DNS_TYPES.map((type) => (
                            <option key={type} value={type}>
                              {type}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                  </div>
                </section>

                {/* 分组 B: 检查策略 */}
                <section className="target-form-section">
                  <div className="target-section-title">B. 检查策略</div>
                  <div className="target-form-grid">
                    <label className="field">
                      <span className="field-label">间隔（秒）</span>
                      <input
                        className="field-input mono"
                        type="number"
                        min={10}
                        max={86400}
                        value={form.intervalSeconds}
                        onChange={setField('intervalSeconds')}
                        disabled={isDrawerReadOnlyMedia}
                      />
                    </label>

                    <label className="field">
                      <span className="field-label">超时（毫秒）</span>
                      <input
                        className="field-input mono"
                        type="number"
                        min={100}
                        max={30000}
                        value={form.timeoutMs}
                        onChange={setField('timeoutMs')}
                        disabled={isDrawerReadOnlyMedia}
                      />
                    </label>

                    {form.kind === 'mtr' && (
                      <label className="field">
                        <span className="field-label">最大跳数</span>
                        <input
                          className="field-input mono"
                          type="number"
                          min={1}
                          max={30}
                          value={form.maxHops}
                          onChange={setField('maxHops')}
                          disabled={isDrawerReadOnlyMedia}
                        />
                      </label>
                    )}

                    {HTTP_LIKE_KINDS.includes(form.kind) && (
                      <label className="field">
                        <span className="field-label">期望状态码</span>
                        <input
                          className="field-input mono"
                          type="number"
                          min={100}
                          max={599}
                          value={form.expectedStatus}
                          onChange={setField('expectedStatus')}
                          disabled={isDrawerReadOnlyMedia}
                          placeholder="可选，例如 200"
                        />
                      </label>
                    )}
                  </div>
                </section>

                {/* 分组 C: 执行范围 */}
                <section className="target-form-section">
                  <div className="target-section-head-row">
                    <span className="target-section-title">C. 执行范围</span>
                    <span className="target-scope-or-hint">
                      node_ids 与 node_tags 按 OR 规则匹配
                    </span>
                  </div>

                  {/* 范围模式切换 */}
                  <div className="target-scope-mode-tabs" role="radiogroup" aria-label="执行范围模式">
                    {[
                      { key: 'all', label: '全部节点' },
                      { key: 'nodes', label: `指定节点${form.nodeIds.length ? ` (${form.nodeIds.length})` : ''}` },
                      { key: 'tags', label: `节点标签${form.nodeTags.length ? ` (${form.nodeTags.length})` : ''}` },
                      { key: 'custom', label: '节点 + 标签组合' },
                    ].map((tab) => (
                      <button
                        key={tab.key}
                        type="button"
                        className={`target-scope-mode-btn ${form.scopeMode === tab.key ? 'is-active' : ''}`}
                        disabled={isDrawerReadOnlyMedia}
                        onClick={() => handleScopeModeChange(tab.key)}
                      >
                        {tab.label}
                      </button>
                    ))}
                  </div>

                  {/* 实时匹配节点预览条 */}
                  <div className="target-matched-preview-bar">
                    <span className="preview-badge mono">
                      匹配探针：{matchedNodesPreview.count} / {matchedNodesPreview.total} 台
                    </span>
                    <span className="preview-names" title={matchedNodesPreview.names.join('、')}>
                      {form.scopeMode === 'all' && form.nodeIds.length === 0 && form.nodeTags.length === 0
                        ? '将在全部已接入及未来新增探针上执行'
                        : matchedNodesPreview.names.length > 0
                          ? `执行节点：${matchedNodesPreview.names.join('、')}`
                          : '当前没有探针命中该范围条件'}
                    </span>
                  </div>

                  {/* 指定节点选择器 */}
                  {(form.scopeMode === 'nodes' || form.scopeMode === 'custom') && (
                    <div className="target-node-picker-box">
                      <div className="target-node-picker-toolbar">
                        <div className="picker-search">
                          <MagnifyingGlass size={13} className="text-muted" />
                          <input
                            type="text"
                            value={nodeSearch}
                            onChange={(e) => setNodeSearch(e.target.value)}
                            placeholder="搜索节点名称、地区或标签…"
                            aria-label="搜索节点"
                          />
                        </div>
                        <div className="picker-actions">
                          <span className="picker-selected-count mono">
                            已选 {form.nodeIds.length} 台
                          </span>
                          {!isDrawerReadOnlyMedia && (
                            <>
                              <button
                                type="button"
                                className="picker-link-btn"
                                onClick={selectAllFilteredNodes}
                              >
                                全选
                              </button>
                              <button
                                type="button"
                                className="picker-link-btn"
                                onClick={clearSelectedNodes}
                              >
                                清空
                              </button>
                            </>
                          )}
                        </div>
                      </div>

                      <div className="target-node-picker-list">
                        {filteredDrawerNodes.length === 0 ? (
                          <div className="target-node-picker-empty">未找到匹配的探针节点</div>
                        ) : (
                          filteredDrawerNodes.map((node, idx) => {
                            const nodeKey = node.uuid || node.id || `node-${idx}`
                            const checked = form.nodeIds.includes(nodeKey)
                            const isOnline =
                              node.status === 'online' ||
                              node.online === true ||
                              node.state === 'online'
                            const regionInfo = detectRegionAndFlag(node)
                            return (
                              <label
                                key={nodeKey}
                                className={`target-node-picker-item ${checked ? 'is-checked' : ''}`}
                              >
                                <input
                                  type="checkbox"
                                  className="lite-checkbox"
                                  checked={checked}
                                  disabled={isDrawerReadOnlyMedia}
                                  onChange={() => toggleNodeIdInForm(nodeKey)}
                                />
                                <span
                                  className={`node-online-dot ${isOnline ? 'is-online' : 'is-offline'}`}
                                  title={isOnline ? '在线' : '离线'}
                                />
                                <span className="node-picker-name">
                                  {node.name || node.hostname || '未命名探针'}
                                </span>
                                <span className="node-picker-region">
                                  {regionInfo?.flag ? `${regionInfo.flag} ` : ''}
                                  {node.region || node.country || regionInfo?.region || '未知地区'}
                                </span>
                                <span className={`node-picker-status ${isOnline ? 'online' : 'offline'}`}>
                                  {isOnline ? '在线' : '离线'}
                                </span>
                              </label>
                            )
                          })
                        )}
                      </div>
                    </div>
                  )}

                  {/* 节点标签选择器 */}
                  {(form.scopeMode === 'tags' || form.scopeMode === 'custom') && (
                    <div className="target-tag-picker-box">
                      <div className="target-tag-picker-head">
                        <span className="tag-head-label">可选节点标签（点击多选）：</span>
                        <span className="tag-head-note">node_ids 与 node_tags 按 OR 规则匹配</span>
                      </div>

                      <div className="target-tag-chips-wrap">
                        {availableTags.length === 0 && form.nodeTags.length === 0 ? (
                          <span className="tag-empty-hint">当前节点暂无预设标签，可在下方输入新标签</span>
                        ) : (
                          Array.from(new Set([...availableTags, ...form.nodeTags])).map((tag) => {
                            const active = form.nodeTags.includes(tag)
                            return (
                              <button
                                key={tag}
                                type="button"
                                className={`target-tag-chip ${active ? 'is-active' : ''}`}
                                disabled={isDrawerReadOnlyMedia}
                                onClick={() => toggleTagInForm(tag)}
                              >
                                <Tag size={11} />
                                <span>{tag}</span>
                                {active && <Check size={11} weight="bold" />}
                              </button>
                            )
                          })
                        )}
                      </div>

                      {!isDrawerReadOnlyMedia && (
                        <div className="target-tag-custom-row">
                          <input
                            type="text"
                            className="field-input target-tag-custom-input"
                            value={customTagInput}
                            onChange={(e) => setCustomTagInput(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault()
                                handleAddCustomTag()
                              }
                            }}
                            placeholder="输入新标签（如 cn2、hk-edge），回车添加"
                            aria-label="输入新标签"
                          />
                          <button
                            type="button"
                            className="button button-quiet target-tag-add-btn"
                            onClick={handleAddCustomTag}
                          >
                            添加标签
                          </button>
                        </div>
                      )}
                    </div>
                  )}
                </section>

                {/* 高级设置：区域规则（默认折叠，表单化编辑并保留旧目标 region_rules） */}
                <section className="target-form-section target-advanced-section">
                  <button
                    type="button"
                    className="target-advanced-toggle"
                    onClick={() => setAdvancedOpen((prev) => !prev)}
                    aria-expanded={advancedOpen}
                  >
                    <span className="adv-toggle-left">
                      {advancedOpen ? <CaretDown size={13} /> : <CaretRight size={13} />}
                      <span>高级设置 · 区域规则 (region_rules)</span>
                      {form.regionRulesList?.length > 0 && (
                        <span className="adv-count-pill mono">{form.regionRulesList.length} 条</span>
                      )}
                    </span>
                    <span className="adv-toggle-right">{advancedOpen ? '收起' : '展开'}</span>
                  </button>

                  {advancedOpen && (
                    <div className="target-advanced-body">
                      <p className="target-advanced-hint">
                        按响应特征匹配归属地区（表单化维护，编辑旧目标时自动保留原有 <code>region_rules</code>，无需手写 JSON）。
                      </p>

                      {Array.isArray(form.regionRulesList) && form.regionRulesList.length > 0 ? (
                        <div className="target-region-rules-list">
                          {form.regionRulesList.map((rule, idx) => (
                            <div key={`rule-${idx}`} className="target-region-rule-row">
                              <input
                                type="text"
                                className="field-input mono"
                                value={rule.region}
                                disabled={isDrawerReadOnlyMedia}
                                onChange={(e) => updateRegionRuleRow(idx, 'region', e.target.value)}
                                placeholder="地区代码 (如 US / SG)"
                                aria-label={`区域规则 ${idx + 1} 地区代码`}
                              />
                              <input
                                type="text"
                                className="field-input mono"
                                value={rule.contains}
                                disabled={isDrawerReadOnlyMedia}
                                onChange={(e) => updateRegionRuleRow(idx, 'contains', e.target.value)}
                                placeholder="匹配响应内容 (如 United States)"
                                aria-label={`区域规则 ${idx + 1} 匹配内容`}
                              />
                              {!isDrawerReadOnlyMedia && (
                                <button
                                  type="button"
                                  className="target-rule-remove-btn"
                                  onClick={() => removeRegionRuleRow(idx)}
                                  aria-label={`删除区域规则 ${idx + 1}`}
                                >
                                  <Trash size={13} />
                                </button>
                              )}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="target-region-rules-empty">暂未配置区域规则</div>
                      )}

                      {!isDrawerReadOnlyMedia && (
                        <button
                          type="button"
                          className="button button-quiet target-add-rule-btn"
                          onClick={addRegionRuleRow}
                        >
                          <Plus size={12} />
                          添加区域规则
                        </button>
                      )}
                    </div>
                  )}
                </section>
              </div>

              <div className="target-drawer-footer target-form-actions">
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={closeDrawer}
                  disabled={submitting}
                >
                  {isDrawerReadOnlyMedia ? '关闭' : '取消'}
                </button>
                {!isDrawerReadOnlyMedia && (
                  <button
                    className="button button-primary"
                    type="submit"
                    disabled={submitting}
                  >
                    {submitting
                      ? '正在提交…'
                      : drawerMode === 'edit'
                        ? '保存修改'
                        : '创建目标'}
                  </button>
                )}
              </div>
            </form>
          </aside>
        </div>
      )}

      {/* 5. 删除目标二次确认弹窗 */}
      {confirmDeleteTarget && (
        <div
          className="target-confirm-modal-backdrop"
          onClick={() => setConfirmDeleteTarget(null)}
          role="presentation"
        >
          <div
            className="target-confirm-modal"
            onClick={(e) => e.stopPropagation()}
            role="alertdialog"
            aria-modal="true"
            aria-label="确认删除检测目标"
          >
            <div className="confirm-modal-head">
              <WarningCircle size={18} className="text-rose" />
              <h3>确认删除检测目标？</h3>
            </div>
            <p className="confirm-modal-desc">
              即将删除检测目标 <strong>{confirmDeleteTarget.name}</strong>（ID:{' '}
              <code className="mono">{confirmDeleteTarget.id}</code>）。删除后各探针将停止对该目标的采样调度。
            </p>
            <div className="confirm-modal-actions">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setConfirmDeleteTarget(null)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-danger target-confirm-delete-btn"
                onClick={() => confirmAndDeleteTarget(confirmDeleteTarget)}
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
