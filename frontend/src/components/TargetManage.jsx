import { useCallback, useEffect, useMemo, useState } from 'react'
import { Plus } from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { numeric, safeObject, safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

const TARGET_KINDS = ['tcp', 'http', 'https', 'dns', 'mtr', 'media_http']
const KIND_LABELS = { tcp: 'TCP', http: 'HTTP', https: 'HTTPS', dns: 'DNS', mtr: 'MTR', media_http: '流媒体' }
const KIND_PORT_DEFAULTS = { tcp: 80, http: 80, https: 443, dns: 53, mtr: 80, media_http: 80 }
const HTTP_LIKE_KINDS = ['http', 'https', 'media_http']
const DNS_TYPES = ['A', 'AAAA', 'CNAME']
const REGION_RULE_EXAMPLE = '[{"region":"US","contains":"United States"}]'

const normalizeTarget = (row) => {
  const source = safeObject(row)
  return {
    id: safeText(source.id),
    name: safeText(source.name, '未命名目标'),
    kind: safeText(source.kind, 'tcp'),
    host: safeText(source.host, '—'),
    port: numeric(source.port),
    path: safeText(source.path),
    dnsType: safeText(source.dns_type),
    intervalSeconds: numeric(source.interval_seconds),
    timeoutMs: numeric(source.timeout_ms),
    maxHops: numeric(source.max_hops),
    enabled: source.enabled === true,
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

const emptyForm = (kind = 'tcp') => ({
  kind,
  id: '',
  name: '',
  host: '',
  port: String(KIND_PORT_DEFAULTS[kind] ?? 80),
  path: '',
  dnsType: 'A',
  intervalSeconds: '60',
  timeoutMs: '3000',
  maxHops: '20',
  expectedStatus: '',
  regionRules: '',
})

const validateForm = (form) => {
  const kind = form.kind
  const name = form.name.trim()
  const id = form.id.trim()
  const host = form.host.trim()
  if (!name) return '请输入目标名称。'
  if (name.length > 128) return '目标名称不能超过 128 个字符。'
  if (!id) return '请输入目标 ID。'
  if (id.length > 128) return '目标 ID 不能超过 128 个字符。'
  if (!host) return '请输入主机地址。'
  if (host.length > 253) return '主机地址不能超过 253 个字符。'
  if (/\s/.test(host) || host.includes('://') || /[/?#@\\]/.test(host)) return '主机地址不能包含空格、协议前缀或 / ? # @ \\ 字符。'
  const port = Number(form.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return '端口需为 1-65535 的整数。'
  if (HTTP_LIKE_KINDS.includes(kind)) {
    if (!form.path) return 'HTTP 类目标必须填写路径（以 / 开头）。'
    if (!form.path.startsWith('/')) return '路径必须以 / 开头。'
    if (form.path.length > 2048) return '路径不能超过 2048 个字符。'
    if (/[\\\r\n]/.test(form.path)) return '路径不能包含反斜杠或换行。'
  }
  if (kind === 'dns' && !DNS_TYPES.includes(form.dnsType)) return 'DNS 目标必须选择记录类型（A / AAAA / CNAME）。'
  const interval = Number(form.intervalSeconds)
  if (!Number.isInteger(interval) || interval < 10 || interval > 86400) return '检测间隔需为 10-86400 秒。'
  const timeout = Number(form.timeoutMs)
  if (!Number.isInteger(timeout) || timeout < 100 || timeout > 30000) return '超时时间需为 100-30000 毫秒。'
  if (kind === 'mtr') {
    const hops = Number(form.maxHops)
    if (!Number.isInteger(hops) || hops < 1 || hops > 30) return 'MTR 最大跳数需为 1-30。'
  }
  if (HTTP_LIKE_KINDS.includes(kind) && form.expectedStatus.trim() !== '') {
    const status = Number(form.expectedStatus)
    if (!Number.isInteger(status) || status < 100 || status > 599) return '期望状态码需为 100-599。'
  }
  if (kind === 'media_http' && form.regionRules.trim() !== '') {
    let parsed
    try {
      parsed = JSON.parse(form.regionRules)
    } catch {
      return `region_rules 不是合法 JSON，请输入数组，例如 ${REGION_RULE_EXAMPLE}`
    }
    if (!Array.isArray(parsed)) return `region_rules 需为 JSON 数组，例如 ${REGION_RULE_EXAMPLE}`
    for (const rule of parsed) {
      const item = safeObject(rule)
      if (typeof item.region !== 'string' || typeof item.contains !== 'string' || !item.contains) return 'region_rules 数组每项需包含字符串字段 region 与 contains。'
    }
  }
  return null
}

export function TargetTable({ targets, readOnly = false, loading = false, mutatingId = null, onToggle, onDelete, emptyDetail = '暂无可展示的检测目标。' }) {
  return <div className="table-scroll target-table-wrap">
    <table className="target-table">
      <thead>
        <tr><th>ID</th><th>名称</th><th>类型</th><th>主机</th><th>端口</th><th>路径</th><th>间隔</th><th>启用</th>{!readOnly && <th aria-hidden="true" />}</tr>
      </thead>
      <tbody>
        {targets.map((target, index) => <tr key={target.id || `target-${index}`}>
          <td>{target.id || '—'}</td>
          <td>{target.name}</td>
          <td><span className="target-kind">{KIND_LABELS[target.kind] || target.kind}</span></td>
          <td>{target.host}</td>
          <td>{target.port ?? '—'}</td>
          <td>{target.path || '—'}</td>
          <td>{target.intervalSeconds !== null ? `${target.intervalSeconds}s` : '—'}</td>
          <td>{readOnly
            ? <span className="target-state"><span className={`status-dot status-${target.enabled ? 'online' : 'offline'}`} />{target.enabled ? '已启用' : '已停用'}</span>
            : <button type="button" className={`target-toggle ${target.enabled ? 'target-toggle-on' : ''}`} disabled={mutatingId === target.id} aria-pressed={target.enabled} onClick={() => onToggle(target)}>{mutatingId === target.id ? '切换中…' : target.enabled ? '已启用' : '已停用'}</button>}</td>
          {!readOnly && <td className="target-cell-action"><button type="button" className="text-button target-delete" disabled={mutatingId === target.id} onClick={() => onDelete(target)}>{mutatingId === target.id ? '处理中…' : '删除'}</button></td>}
        </tr>)}
      </tbody>
    </table>
    {!targets.length && <EmptyState title={loading ? '正在加载检测目标' : '暂无检测目标'} detail={loading ? '正在从 API 读取检测目标列表。' : emptyDetail} />}
  </div>
}

// readOnly 时只渲染列表（network/mtr 子页复用），kinds 限定显示的检测类型。
export function TargetManage({ readOnly = false, kinds = null, title = '检测目标' }) {
  const [targets, setTargets] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [status, setStatus] = useState(null)
  const [filter, setFilter] = useState('all')
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState(() => emptyForm())
  const [formError, setFormError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [mutatingId, setMutatingId] = useState(null)

  const load = useCallback(async (signal) => {
    setLoading(true)
    setError('')
    try {
      const response = await fetch('/api/targets', { credentials: 'same-origin', signal })
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error('load')
      const json = await response.json()
      if (!Array.isArray(json)) throw new Error('invalid')
      if (!signal?.aborted) setTargets(json.map(normalizeTarget))
    } catch (caught) {
      if (caught?.name === 'AbortError') return
      if (!signal?.aborted) setError(caught?.message === 'auth' ? '需要登录后才能查看检测目标。' : '暂无 API 数据 · 无法加载检测目标，请稍后重试。')
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  const visible = useMemo(() => {
    if (kinds) return targets.filter((target) => kinds.includes(target.kind))
    if (filter === 'all') return targets
    return targets.filter((target) => target.kind === filter)
  }, [targets, kinds, filter])

  const toggleEnabled = async (target) => {
    setMutatingId(target.id)
    setError('')
    setStatus(null)
    try {
      const csrfToken = await fetchCsrfToken()
      const response = await fetch(`/api/targets/${encodeURIComponent(target.id)}`, { method: 'PATCH', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ enabled: !target.enabled }) })
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error((await readErrorDetail(response)) || `启停失败（${response.status}）`)
      const updated = normalizeTarget(await response.json())
      setTargets((current) => current.map((item) => item.id === updated.id ? updated : item))
    } catch (caught) {
      const messages = { auth: '需要登录后才能修改检测目标。', csrf: '无法获取安全令牌，请刷新后重试。' }
      setError(messages[caught?.message] || caught?.message || '启停失败，请稍后重试。')
    } finally {
      setMutatingId(null)
    }
  }

  const deleteTarget = async (target) => {
    setMutatingId(target.id)
    setError('')
    setStatus(null)
    try {
      const csrfToken = await fetchCsrfToken()
      const response = await fetch(`/api/targets/${encodeURIComponent(target.id)}`, { method: 'DELETE', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken } })
      if (response.status === 401) throw new Error('auth')
      if (!response.ok) throw new Error((await readErrorDetail(response)) || `删除失败（${response.status}）`)
      setTargets((current) => current.filter((item) => item.id !== target.id))
      setStatus({ kind: 'ok', message: `检测目标 ${target.name} 已删除。` })
    } catch (caught) {
      const messages = { auth: '需要登录后才能删除检测目标。', csrf: '无法获取安全令牌，请刷新后重试。' }
      setError(messages[caught?.message] || caught?.message || '删除失败，请稍后重试。')
    } finally {
      setMutatingId(null)
    }
  }

  const setField = (key) => (event) => setForm((current) => ({ ...current, [key]: event.target.value }))
  const changeKind = (event) => {
    const kind = event.target.value
    setForm((current) => ({ ...emptyForm(kind), id: current.id, name: current.name, host: current.host }))
  }

  const submit = async (event) => {
    event.preventDefault()
    const detail = validateForm(form)
    if (detail) {
      setFormError(detail)
      return
    }
    const body = {
      id: form.id.trim(),
      name: form.name.trim(),
      kind: form.kind,
      host: form.host.trim(),
      port: Number(form.port),
      interval_seconds: Number(form.intervalSeconds),
      timeout_ms: Number(form.timeoutMs),
    }
    if (HTTP_LIKE_KINDS.includes(form.kind)) body.path = form.path
    if (form.kind === 'dns') body.dns_type = form.dnsType
    if (form.kind === 'mtr') body.max_hops = Number(form.maxHops)
    if (HTTP_LIKE_KINDS.includes(form.kind) && form.expectedStatus.trim() !== '') body.expected_status = Number(form.expectedStatus)
    if (form.kind === 'media_http' && form.regionRules.trim() !== '') body.region_rules = JSON.parse(form.regionRules)
    setSubmitting(true)
    setFormError('')
    setStatus(null)
    try {
      const csrfToken = await fetchCsrfToken()
      const response = await fetch('/api/targets', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(body) })
      if (response.status === 401) throw new Error('auth')
      if (response.status === 403) throw new Error('forbidden')
      if (!response.ok) throw new Error((await readErrorDetail(response)) || `创建失败（${response.status}）`)
      const created = normalizeTarget(await response.json())
      setTargets((current) => [...current, created])
      setForm(emptyForm())
      setStatus({ kind: 'ok', message: `检测目标 ${created.name} 已创建。` })
    } catch (caught) {
      const messages = { auth: '需要登录后才能创建检测目标。', csrf: '无法获取安全令牌，请刷新后重试。', forbidden: '创建检测目标被拒绝，请刷新后重试。' }
      setFormError(messages[caught?.message] || caught?.message || '创建失败，请稍后重试。')
    } finally {
      setSubmitting(false)
    }
  }

  if (readOnly) {
    return <div className="panel target-panel">
      <div className="panel-header">
        <div><h2>{title}</h2><p>只读视图 · 共 {visible.length} 个目标</p></div>
      </div>
      {error && <div className="api-state api-state-error" role="status">{error}</div>}
      <TargetTable targets={visible} readOnly loading={loading} emptyDetail="暂无此类检测目标。" />
    </div>
  }

  return <>
    <div className="panel target-panel">
      <div className="panel-header">
        <div><h2>全部检测目标</h2><p>共 {targets.length} 个目标{filter === 'all' ? '' : ` · 筛选 ${KIND_LABELS[filter] || filter}`}</p></div>
        <div className="target-actions">
          <div className="filter-group" aria-label="按类型筛选">
            <button type="button" className={filter === 'all' ? 'filter-active' : ''} aria-pressed={filter === 'all'} onClick={() => setFilter('all')}>全部</button>
            {TARGET_KINDS.map((kind) => <button key={kind} type="button" className={filter === kind ? 'filter-active' : ''} aria-pressed={filter === kind} onClick={() => setFilter(kind)}>{KIND_LABELS[kind]}</button>)}
          </div>
          <button type="button" className={`button ${formOpen ? 'button-quiet' : 'button-primary'}`} onClick={() => { setFormOpen((open) => !open); setFormError('') }}><Plus size={14} weight="bold" />{formOpen ? '收起表单' : '新建目标'}</button>
        </div>
      </div>
      {(error || status) && <div className={`api-state ${error ? 'api-state-error' : 'api-state-ok'}`} role="status">{error || status?.message}</div>}
      <TargetTable targets={visible} loading={loading} mutatingId={mutatingId} onToggle={toggleEnabled} onDelete={deleteTarget} emptyDetail={filter === 'all' ? '还没有检测目标，点击“新建目标”添加第一个探测项。' : '当前类型下没有检测目标。'} />
    </div>
    {formOpen && <div className="panel target-form-panel">
      <div className="panel-header">
        <div><h2>新建检测目标</h2><p>按类型填写探测参数，提交后立即生效。</p></div>
      </div>
      {formError && <div className="api-state api-state-error" role="alert">{formError}</div>}
      <form className="target-form" onSubmit={submit} noValidate>
        <div className="target-form-grid">
          <label className="field"><span className="field-label">类型 *</span>
            <select className="field-select" value={form.kind} onChange={changeKind}>
              {TARGET_KINDS.map((kind) => <option key={kind} value={kind}>{KIND_LABELS[kind]}（{kind}）</option>)}
            </select>
          </label>
          <label className="field"><span className="field-label">名称 *</span><input className="field-input" value={form.name} onChange={setField('name')} maxLength={128} placeholder="例如：上海 HTTP 探测" /></label>
          <label className="field"><span className="field-label">ID *</span><input className="field-input" value={form.id} onChange={setField('id')} maxLength={128} placeholder="例如：http-sh-01" /></label>
          <label className="field"><span className="field-label">主机 *</span><input className="field-input" value={form.host} onChange={setField('host')} maxLength={253} placeholder="域名或 IP，不含协议" /></label>
          <label className="field"><span className="field-label">端口</span><input className="field-input" type="number" min={1} max={65535} value={form.port} onChange={setField('port')} /></label>
          {HTTP_LIKE_KINDS.includes(form.kind) && <label className="field"><span className="field-label">路径 *</span><input className="field-input" value={form.path} onChange={setField('path')} maxLength={2048} placeholder="/health" /></label>}
          {form.kind === 'dns' && <label className="field"><span className="field-label">记录类型 *</span>
            <select className="field-select" value={form.dnsType} onChange={setField('dnsType')}>
              {DNS_TYPES.map((type) => <option key={type} value={type}>{type}</option>)}
            </select>
          </label>}
          {form.kind === 'mtr' && <label className="field"><span className="field-label">最大跳数</span><input className="field-input" type="number" min={1} max={30} value={form.maxHops} onChange={setField('maxHops')} /></label>}
          <label className="field"><span className="field-label">间隔（秒）</span><input className="field-input" type="number" min={10} max={86400} value={form.intervalSeconds} onChange={setField('intervalSeconds')} /></label>
          <label className="field"><span className="field-label">超时（毫秒）</span><input className="field-input" type="number" min={100} max={30000} value={form.timeoutMs} onChange={setField('timeoutMs')} /></label>
          {HTTP_LIKE_KINDS.includes(form.kind) && <label className="field"><span className="field-label">期望状态码</span><input className="field-input" type="number" min={100} max={599} value={form.expectedStatus} onChange={setField('expectedStatus')} placeholder="可选，例如 200" /></label>}
          {form.kind === 'media_http' && <label className="field field-wide"><span className="field-label">区域规则（JSON 数组，可选）</span><textarea className="field-textarea" value={form.regionRules} onChange={setField('regionRules')} rows={4} placeholder={REGION_RULE_EXAMPLE} /><span className="field-hint">提交前会做 JSON 校验，每项需包含 region 与 contains 字段。</span></label>}
        </div>
        <div className="target-form-actions">
          <button className="button button-primary" type="submit" disabled={submitting}>{submitting ? '正在提交…' : '创建目标'}</button>
          <span className="field-hint">带 * 为必填；提交前先做本地校验，服务端错误会原样展示。</span>
        </div>
      </form>
    </div>}
  </>
}
