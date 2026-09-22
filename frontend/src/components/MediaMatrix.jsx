import { useEffect, useState } from 'react'
import { Check, Warning, X } from '@phosphor-icons/react'
import { numeric, safeArray, safeObject, safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

const mediaStatusLabel = (status) => ({ available: '可用', unavailable: '不可用', timeout: '超时', blocked: '被封锁', error: '错误' }[status] || (safeText(status) || '—'))
const mediaStatusTone = (status) => status === 'available' ? 'available' : status === 'unavailable' ? 'unavailable' : ['timeout', 'blocked', 'error'].includes(status) ? 'warning' : 'muted'
const formatRelativeTime = (ms) => { if (numeric(ms) === null) return '—'; const seconds = Math.round((Date.now() - ms) / 1000); if (seconds < 60) return '刚刚'; if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`; if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`; return `${Math.floor(seconds / 86400)} 天前` }
const normalizeMediaReport = (report) => { const source = safeObject(report); const result = safeObject(source.result); const rawChecked = source.checked_at ?? result.checked_at; const checkedAtMs = typeof rawChecked === 'number' && Number.isFinite(rawChecked) && rawChecked > 0 ? (rawChecked < 1e12 ? rawChecked * 1000 : rawChecked) : typeof rawChecked === 'string' && rawChecked && !Number.isNaN(new Date(rawChecked).getTime()) ? new Date(rawChecked).getTime() : null; const detectorId = safeText(source.detector_id); return { detectorId, detector: safeText(result.detector) || detectorId, status: safeText(result.status), region: safeText(result.region), reason: safeText(result.reason), latencyMs: numeric(result.latency_ms), checkedAtMs } }

export function MediaMatrix({ nodes }) {
  const [state, setState] = useState({ loading: true, entries: [] })
  useEffect(() => {
    const controller = new AbortController()
    setState({ loading: true, entries: [] })
    const targets = safeArray(nodes).map((node, index) => { const source = safeObject(node); const uuid = safeText(source.uuid) || safeText(source.id); return { key: uuid || `node-${index}`, name: safeText(source.name, '未命名节点'), uuid } })
    if (!targets.length) { setState({ loading: false, entries: [] }); return () => controller.abort() }
    Promise.all(targets.map((target) => { if (!target.uuid) return Promise.resolve({ ...target, ok: false, reports: [] }); return fetch(`/api/nodes/${encodeURIComponent(target.uuid)}/media`, { credentials: 'same-origin', signal: controller.signal }).then(async (response) => { if (!response.ok) throw new Error(`media:${response.status}`); const json = await response.json(); if (!Array.isArray(json)) throw new Error('media:invalid-json'); return { ...target, ok: true, reports: json.map(normalizeMediaReport) } }).catch((error) => { if (error?.name === 'AbortError') throw error; return { ...target, ok: false, reports: [] } }) })).then((entries) => { if (!controller.signal.aborted) setState({ loading: false, entries }) }).catch(() => { if (!controller.signal.aborted) setState({ loading: false, entries: [] }) })
    return () => controller.abort()
  }, [nodes])
  const entries = state.entries
  const columns = []
  const seen = new Set()
  entries.forEach((entry) => safeArray(entry?.reports).forEach((report) => { const key = safeText(report?.detector) || safeText(report?.detectorId); if (!key || seen.has(key)) return; seen.add(key); columns.push(key) }))
  const hasData = entries.some((entry) => entry?.ok && safeArray(entry.reports).length > 0)
  if (state.loading) return <EmptyState title="正在加载流媒体数据" detail="正在并行拉取各节点最新流媒体检测上报。" />
  if (!hasData || !columns.length) return <EmptyState title="暂无流媒体上报" detail="当前没有任何节点返回流媒体检测数据。" />
  return <div className="table-scroll"><table className="media-table"><thead><tr><th>节点</th>{columns.map((column) => <th key={column}>{column}</th>)}<th>最近检测</th></tr></thead><tbody>{entries.map((entry) => { const reports = safeArray(entry?.reports); const byDetector = new Map(); reports.forEach((report) => { const key = safeText(report?.detector) || safeText(report?.detectorId); if (key && !byDetector.has(key)) byDetector.set(key, report) }); const lastCheckedMs = reports.map((report) => numeric(report?.checkedAtMs)).filter((value) => value !== null).sort((a, b) => b - a)[0] ?? null; return <tr key={safeText(entry?.key)} className={entry?.ok ? '' : 'media-row-unreported'}><td className="media-node-cell">{safeText(entry?.name, '未命名节点')}</td>{entry?.ok ? columns.map((column) => { const report = byDetector.get(column); if (!report) return <td key={column} className="media-cell media-cell-muted">—</td>; const tone = mediaStatusTone(report.status); const label = mediaStatusLabel(report.status); return <td key={column} className={`media-cell media-cell-${tone}`} title={report.reason || label}>{tone === 'available' ? <><Check size={13} weight="fill" />{report.region || label}</> : tone === 'unavailable' ? <><X size={13} weight="fill" />{label}</> : tone === 'warning' ? <><Warning size={13} weight="fill" />{label}</> : label}</td> }) : <td colSpan={columns.length + 1} className="media-cell media-cell-muted media-cell-unreported">暂无上报</td>}<td className="media-checked-cell">{formatRelativeTime(lastCheckedMs)}</td></tr> })}</tbody></table></div>
}
