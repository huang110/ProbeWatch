import { useCallback, useEffect, useState } from 'react'
import { Check, CheckCircle, CircleNotch, FilmStrip, Play, Sparkle, Warning, X } from '@phosphor-icons/react'
import { numeric, safeArray, safeObject, safeText, formatTimeOfDay } from '../lib/format.js'
import { EmptyState } from './Common.jsx'
import { fetchCsrfToken } from '../lib/api.js'

const POPULAR_STREAMING_PLATFORMS = [
  { id: 'chatgpt', label: 'OpenAI / ChatGPT', category: 'ai', host: 'chatgpt.com', path: '/cdn-cgi/trace', regionRules: [{ region: 'US', contains: 'loc=US' }] },
  { id: 'claude', label: 'Claude AI', category: 'ai', host: 'claude.ai', path: '/cdn-cgi/trace', regionRules: [{ region: 'US', contains: 'loc=US' }] },
  { id: 'youtube', label: 'YouTube Premium', category: 'media', host: 'www.youtube.com', path: '/premium', regionRules: [{ region: 'US', contains: 'Premium' }] },
  { id: 'netflix', label: 'Netflix', category: 'media', host: 'www.netflix.com', path: '/title/80018499', regionRules: [{ region: 'US', contains: 'United States' }] },
  { id: 'disney', label: 'Disney+', category: 'media', host: 'www.disneyplus.com', path: '/', regionRules: [] },
  { id: 'tiktok', label: 'TikTok', category: 'media', host: 'www.tiktok.com', path: '/', regionRules: [] },
  { id: 'spotify', label: 'Spotify', category: 'media', host: 'www.spotify.com', path: '/', regionRules: [] },
  { id: 'bilibili', label: 'Bilibili 港澳台', category: 'media', host: 'api.bilibili.com', path: '/pgc/player/web/v2/playurl?cid=144541892&ep_id=234405', regionRules: [{ region: 'TW', contains: '"code":0' }, { region: 'HK', contains: '"code":-10403' }] },
]

const mediaStatusLabel = (status) => ({
  available: '原生解锁',
  unavailable: '未解锁',
  timeout: '请求超时',
  blocked: '被封锁',
  error: '检测异常',
}[status] || (safeText(status) || '—'))

const mediaStatusTone = (status) => {
  if (status === 'available') return 'available'
  if (status === 'unavailable') return 'unavailable'
  if (['timeout', 'blocked', 'error'].includes(status)) return 'warning'
  return 'muted'
}

const formatRelativeTime = (ms) => {
  if (numeric(ms) === null) return '—'
  const seconds = Math.round((Date.now() - ms) / 1000)
  if (seconds < 60) return '刚刚'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  return `${Math.floor(seconds / 86400)} 天前`
}

const normalizeMediaReport = (report) => {
  const source = safeObject(report)
  const result = safeObject(source.result)
  const rawChecked = source.checked_at ?? result.checked_at
  const checkedAtMs = typeof rawChecked === 'number' && Number.isFinite(rawChecked) && rawChecked > 0
    ? (rawChecked < 1e12 ? rawChecked * 1000 : rawChecked)
    : typeof rawChecked === 'string' && rawChecked && !Number.isNaN(new Date(rawChecked).getTime())
      ? new Date(rawChecked).getTime()
      : null
  const detectorId = safeText(source.detector_id || source.target_id)
  return {
    detectorId,
    detector: safeText(result.detector) || detectorId,
    status: safeText(result.status),
    region: safeText(result.region),
    reason: safeText(result.reason),
    latencyMs: numeric(result.latency_ms),
    checkedAtMs,
  }
}

export function MediaMatrix({ nodes = [] }) {
  const [state, setState] = useState({ loading: true, entries: [] })
  const [addingPreset, setAddingPreset] = useState(false)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [categoryFilter, setCategoryFilter] = useState('all')

  // Fetch media reports from all nodes
  const fetchAllMedia = useCallback(() => {
    const controller = new AbortController()
    setState((prev) => ({ ...prev, loading: true }))
    const targets = safeArray(nodes).map((node, index) => {
      const source = safeObject(node)
      const uuid = safeText(source.uuid) || safeText(source.id)
      return {
        key: uuid || `node-${index}`,
        name: safeText(source.name, '未命名节点'),
        flag: source.flag || '🌐',
        region: source.region || '公网',
        uuid,
      }
    })

    if (!targets.length) {
      setState({ loading: false, entries: [] })
      return () => controller.abort()
    }

    Promise.all(
      targets.map((target) => {
        if (!target.uuid) return Promise.resolve({ ...target, ok: false, reports: [] })
        return fetch(`/api/nodes/${encodeURIComponent(target.uuid)}/media`, {
          credentials: 'same-origin',
          signal: controller.signal,
        })
          .then(async (response) => {
            if (!response.ok) throw new Error(`media:${response.status}`)
            const json = await response.json()
            if (!Array.isArray(json)) throw new Error('media:invalid-json')
            return { ...target, ok: true, reports: json.map(normalizeMediaReport) }
          })
          .catch((error) => {
            if (error?.name === 'AbortError') throw error
            return { ...target, ok: false, reports: [] }
          })
      })
    )
      .then((entries) => {
        if (!controller.signal.aborted) setState({ loading: false, entries })
      })
      .catch(() => {
        if (!controller.signal.aborted) setState({ loading: false, entries: [] })
      })

    return () => controller.abort()
  }, [nodes])

  useEffect(() => {
    fetchAllMedia()
    const timer = setInterval(fetchAllMedia, 45000)
    return () => clearInterval(timer)
  }, [fetchAllMedia, refreshTrigger])

  // Handle adding preset media targets via atomic backend seed
  const handleAddMediaPresets = async () => {
    setAddingPreset(true)
    try {
      const csrfToken = await fetchCsrfToken()
      const resp = await fetch('/api/targets/seed-media', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: '{}',
      })
      if (!resp.ok) {
        // Fallback to individual target creation
        for (const p of POPULAR_STREAMING_PLATFORMS) {
          try {
            await fetch('/api/targets', {
              method: 'POST',
              credentials: 'same-origin',
              headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
              body: JSON.stringify({
                id: `media-${p.id}`,
                name: p.label,
                kind: 'media_http',
                host: p.host,
                port: 443,
                path: p.path,
                interval_seconds: 60,
                timeout_ms: 5000,
                enabled: true,
                region_rules: p.regionRules,
              }),
            })
          } catch {
            // ignore
          }
        }
      }
      setRefreshTrigger((v) => v + 1)
    } catch {
      // ignore
    } finally {
      setAddingPreset(false)
    }
  }

  const entries = state.entries
  const columns = []
  const seen = new Set()

  entries.forEach((entry) =>
    safeArray(entry?.reports).forEach((report) => {
      const key = safeText(report?.detector) || safeText(report?.detectorId)
      if (!key || seen.has(key)) return
      seen.add(key)
      columns.push(key)
    })
  )

  const isAI = (key) => /chatgpt|claude|openai|gemini|anthropic/i.test(key)
  const displayColumns = columns.filter((col) => {
    if (categoryFilter === 'ai') return isAI(col)
    if (categoryFilter === 'media') return !isAI(col)
    return true
  })

  const hasData = entries.some((entry) => entry?.ok && safeArray(entry.reports).length > 0)

  return (
    <div className="media-module-view">
      {/* 顶部服务商平台介绍卡片条 */}
      <div className="media-top-banner panel">
        <div className="banner-left">
          <div className="banner-icon-box">
            <FilmStrip size={26} weight="duotone" className="text-mint" />
          </div>
          <div>
            <h3>全球流媒体与 AI 服务解锁能力雷达</h3>
            <p>基于受控节点出站发起真实 HTTP 请求并匹配区域路由，支持 OpenAI/ChatGPT, Claude, Netflix, YouTube Premium, Disney+, TikTok, Spotify, Bilibili 等主流平台。</p>
          </div>
        </div>

        <div className="banner-actions">
          <button
            type="button"
            className="button button-primary btn-sm"
            onClick={handleAddMediaPresets}
            disabled={addingPreset}
          >
            {addingPreset ? <CircleNotch size={14} className="spin" /> : <Sparkle size={14} />}
            <span>一键下发流媒体与 AI 检测规则</span>
          </button>
        </div>
      </div>

      {/* 矩阵表格 */}
      <div className="panel media-matrix-panel">
        <div className="panel-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '12px' }}>
          <div>
            <h2>节点流媒体与 AI 解锁检测矩阵</h2>
            <p>绿色 = 原生完整解锁 · 蓝色 = 仅自制剧/DNS解锁 · 红色/灰色 = 未解锁或节点阻断</p>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <div style={{ display: 'inline-flex', background: 'var(--bg-subtle, rgba(255,255,255,0.06))', padding: '2px', borderRadius: '6px' }}>
              <button
                type="button"
                style={{ padding: '4px 10px', fontSize: '12px', border: 'none', borderRadius: '4px', cursor: 'pointer', background: categoryFilter === 'all' ? 'var(--primary, #0284c7)' : 'transparent', color: categoryFilter === 'all' ? '#fff' : 'inherit' }}
                onClick={() => setCategoryFilter('all')}
              >
                全部 ({columns.length})
              </button>
              <button
                type="button"
                style={{ padding: '4px 10px', fontSize: '12px', border: 'none', borderRadius: '4px', cursor: 'pointer', background: categoryFilter === 'ai' ? 'var(--primary, #0284c7)' : 'transparent', color: categoryFilter === 'ai' ? '#fff' : 'inherit' }}
                onClick={() => setCategoryFilter('ai')}
              >
                AI 生产力
              </button>
              <button
                type="button"
                style={{ padding: '4px 10px', fontSize: '12px', border: 'none', borderRadius: '4px', cursor: 'pointer', background: categoryFilter === 'media' ? 'var(--primary, #0284c7)' : 'transparent', color: categoryFilter === 'media' ? '#fff' : 'inherit' }}
                onClick={() => setCategoryFilter('media')}
              >
                影音流媒体
              </button>
            </div>
            <button type="button" className="button button-quiet btn-sm" onClick={fetchAllMedia}>
              刷新矩阵
            </button>
          </div>
        </div>

        {state.loading && !entries.length ? (
          <EmptyState title="正在加载流媒体检测矩阵…" detail="正在并行获取各计算节点最新探测状态。" />
        ) : hasData && displayColumns.length > 0 ? (
          <div className="table-scroll">
            <table className="node-table media-table">
              <thead>
                <tr>
                  <th style={{ minWidth: '180px' }}>服务器节点</th>
                  {displayColumns.map((column) => (
                    <th key={column} className="text-center">{column}</th>
                  ))}
                  <th style={{ width: '120px' }}>最近检测</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => {
                  const reports = safeArray(entry?.reports)
                  const byDetector = new Map()
                  reports.forEach((report) => {
                    const key = safeText(report?.detector) || safeText(report?.detectorId)
                    if (key && !byDetector.has(key)) byDetector.set(key, report)
                  })

                  const lastCheckedMs = reports
                    .map((report) => numeric(report?.checkedAtMs))
                    .filter((value) => value !== null)
                    .sort((a, b) => b - a)[0] ?? null

                  return (
                    <tr key={safeText(entry?.key)} className={entry?.ok ? '' : 'media-row-unreported'}>
                      <td>
                        <div className="media-node-cell">
                          <span className="node-flag">{entry.flag || '🌐'}</span>
                          <div>
                            <strong>{safeText(entry?.name, '未命名节点')}</strong>
                            <small className="muted">{entry.region || '公网'}</small>
                          </div>
                        </div>
                      </td>

                      {entry?.ok ? (
                        displayColumns.map((column) => {
                          const report = byDetector.get(column)
                          if (!report) return <td key={column} className="media-cell media-cell-muted text-center">—</td>
                          const tone = mediaStatusTone(report.status)
                          const label = mediaStatusLabel(report.status)

                          return (
                            <td key={column} className="text-center">
                              <span
                                className={`media-status-badge media-badge-${tone}`}
                                title={report.reason || label}
                              >
                                {tone === 'available' ? (
                                  <>
                                    <Check size={12} weight="bold" />
                                    <span>{report.region ? `${report.region} 解锁` : label}</span>
                                  </>
                                ) : tone === 'unavailable' ? (
                                  <>
                                    <X size={12} weight="bold" />
                                    <span>{label}</span>
                                  </>
                                ) : (
                                  <>
                                    <Warning size={12} weight="bold" />
                                    <span>{label}</span>
                                  </>
                                )}
                              </span>
                            </td>
                          )
                        })
                      ) : (
                        <td colSpan={displayColumns.length} className="media-cell media-cell-muted text-center">
                          暂无上报
                        </td>
                      )}

                      <td className="mono muted text-center">
                        {formatRelativeTime(lastCheckedMs)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="media-empty-guide">
            <EmptyState
              title="暂无流媒体上报"
              detail="当前尚未配置流媒体检测规则，请点击上方“一键生成全球主流流媒体检测规则”按钮，主控将自动下发 Netflix, YouTube, OpenAI 等主流检测项至所有探针。"
            />
            <div className="text-center" style={{ marginTop: '16px' }}>
              <button
                type="button"
                className="button button-primary"
                onClick={handleAddMediaPresets}
                disabled={addingPreset}
              >
                {addingPreset ? <CircleNotch size={15} className="spin" /> : <Sparkle size={15} />}
                <span>立即一键下发流媒体检测项</span>
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
