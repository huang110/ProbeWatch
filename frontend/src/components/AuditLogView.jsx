import { useEffect, useState } from 'react'
import {
  ShieldCheck,
  ArrowClockwise,
  CircleNotch,
  MagnifyingGlass,
  WarningCircle,
  CaretLeft,
  CaretRight,
  User,
  Key,
  Database,
  SlidersHorizontal,
  HardDrives,
  LockKey,
} from '@phosphor-icons/react'
import { fetchAuditLogs } from '../lib/api.js'
import { EmptyState } from './Common.jsx'

export function AuditLogView() {
  const [logs, setLogs] = useState([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [offset, setOffset] = useState(0)
  const limit = 25

  const loadLogs = async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetchAuditLogs({
        limit,
        offset,
        action: actionFilter.trim(),
      })
      setLogs(Array.isArray(res?.logs) ? res.logs : [])
      setTotal(typeof res?.total === 'number' ? res.total : 0)
    } catch (err) {
      setError(err?.message || '获取安全审计日志失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadLogs()
  }, [offset])

  const handleSearchSubmit = (e) => {
    e.preventDefault()
    setOffset(0)
    loadLogs()
  }

  const renderActionBadge = (action) => {
    let color = '#38bdf8'
    let bg = 'rgba(56, 189, 248, 0.1)'
    let border = 'rgba(56, 189, 248, 0.25)'

    if (action.startsWith('auth.')) {
      color = '#a855f7'
      bg = 'rgba(168, 85, 247, 0.1)'
      border = 'rgba(168, 85, 247, 0.25)'
    } else if (action.startsWith('token.')) {
      color = '#eab308'
      bg = 'rgba(234, 179, 8, 0.1)'
      border = 'rgba(234, 179, 8, 0.25)'
    } else if (action.startsWith('node.')) {
      color = '#10b981'
      bg = 'rgba(16, 185, 129, 0.1)'
      border = 'rgba(16, 185, 129, 0.25)'
    } else if (action.startsWith('backup.')) {
      color = '#ec4899'
      bg = 'rgba(236, 72, 153, 0.1)'
      border = 'rgba(236, 72, 153, 0.25)'
    }

    return (
      <span
        style={{
          display: 'inline-block',
          padding: '2px 8px',
          borderRadius: '4px',
          fontSize: '11px',
          fontWeight: '600',
          fontFamily: 'monospace',
          color,
          background: bg,
          border: `1px solid ${border}`,
        }}
      >
        {action}
      </span>
    )
  }

  const totalPages = Math.ceil(total / limit) || 1
  const currentPage = Math.floor(offset / limit) + 1

  return (
    <div className="card audit-card" style={{ padding: '24px', borderRadius: '12px' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: '16px', marginBottom: '20px' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            <div style={{ width: '36px', height: '36px', borderRadius: '8px', background: 'rgba(56, 189, 248, 0.12)', color: '#38bdf8', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <ShieldCheck size={22} weight="bold" />
            </div>
            <div>
              <h2 style={{ fontSize: '18px', fontWeight: '600', margin: 0 }}>安全与管理操作审计中心 (Audit Logs)</h2>
              <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-secondary, #94a3b8)' }}>
                不可篡改的操作审计流水：全量记录会话登录、团队成员变更、API 令牌签发、节点维护与云端灾备操作。
              </p>
            </div>
          </div>
        </div>

        <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
          <form onSubmit={handleSearchSubmit} style={{ display: 'flex', gap: '6px' }}>
            <input
              type="text"
              className="modal-input"
              style={{ width: '180px', padding: '6px 10px', fontSize: '12px' }}
              placeholder="按行为筛选 (例: token)"
              value={actionFilter}
              onChange={(e) => setActionFilter(e.target.value)}
            />
            <button type="submit" className="button button-quiet btn-sm" title="搜索">
              <MagnifyingGlass size={15} />
            </button>
          </form>

          <button
            type="button"
            className="button button-quiet"
            onClick={loadLogs}
            disabled={loading}
            title="刷新审计记录"
          >
            <ArrowClockwise size={16} className={loading ? 'spin' : ''} />
            <span>刷新</span>
          </button>
        </div>
      </div>

      {/* Table */}
      {loading && logs.length === 0 ? (
        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', padding: '40px' }}>
          <CircleNotch size={24} className="spin" style={{ color: 'var(--accent-color, #6366f1)' }} />
          <span style={{ marginLeft: '10px', fontSize: '14px', color: 'var(--text-secondary)' }}>正在载入安全审计日志…</span>
        </div>
      ) : error ? (
        <EmptyState
          icon={WarningCircle}
          title="加载审计日志失败"
          description={error}
          actionText="重新加载"
          onAction={loadLogs}
        />
      ) : logs.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title="暂无审计事件"
          description="系统各项操作均将自动在此留痕。当前无匹配的审计记录。"
        />
      ) : (
        <div className="table-responsive" style={{ overflowX: 'auto' }}>
          <table className="table" style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color, rgba(255, 255, 255, 0.08))', color: 'var(--text-secondary)' }}>
                <th style={{ padding: '10px 14px' }}>时间 (本地)</th>
                <th style={{ padding: '10px 14px' }}>操作者</th>
                <th style={{ padding: '10px 14px' }}>操作类型</th>
                <th style={{ padding: '10px 14px' }}>详情与说明</th>
                <th style={{ padding: '10px 14px' }}>来源 IP</th>
                <th style={{ padding: '10px 14px', textAlign: 'right' }}>状态</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log) => {
                const isSuccess = log.status_code >= 200 && log.status_code < 300
                return (
                  <tr
                    key={log.id}
                    style={{
                      borderBottom: '1px solid var(--border-color, rgba(255, 255, 255, 0.05))',
                    }}
                  >
                    <td style={{ padding: '12px 14px', whiteSpace: 'nowrap', fontSize: '12px', color: 'var(--text-secondary)' }}>
                      {new Date(log.created_at).toLocaleString()}
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                        {log.actor_type === 'token' ? (
                          <Key size={14} style={{ color: '#eab308' }} />
                        ) : (
                          <User size={14} style={{ color: '#818cf8' }} />
                        )}
                        <strong style={{ fontSize: '12px' }}>{log.actor_name || log.actor_id}</strong>
                      </div>
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {renderActionBadge(log.action)}
                    </td>
                    <td style={{ padding: '12px 14px', fontSize: '12px', color: 'var(--text-secondary)', maxWidth: '300px' }}>
                      {log.detail || '—'}
                    </td>
                    <td style={{ padding: '12px 14px', fontSize: '12px', color: 'var(--text-secondary)', fontFamily: 'monospace' }}>
                      {log.ip_address || '—'}
                    </td>
                    <td style={{ padding: '12px 14px', textAlign: 'right' }}>
                      <span
                        style={{
                          fontSize: '11px',
                          fontWeight: '600',
                          padding: '2px 6px',
                          borderRadius: '4px',
                          color: isSuccess ? '#10b981' : '#ef4444',
                          background: isSuccess ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                          border: `1px solid ${isSuccess ? 'rgba(16, 185, 129, 0.2)' : 'rgba(239, 68, 68, 0.2)'}`,
                        }}
                      >
                        {log.status_code}
                      </span>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Pagination Footer */}
      {total > limit && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '16px', paddingTop: '12px', borderTop: '1px solid var(--border-color, rgba(255, 255, 255, 0.05))', fontSize: '12px', color: 'var(--text-secondary)' }}>
          <div>
            共 {total} 条记录 · 第 {currentPage} / {totalPages} 页
          </div>
          <div style={{ display: 'flex', gap: '8px' }}>
            <button
              type="button"
              className="button button-quiet btn-sm"
              disabled={offset === 0 || loading}
              onClick={() => setOffset(Math.max(0, offset - limit))}
              style={{ display: 'flex', alignItems: 'center', gap: '4px' }}
            >
              <CaretLeft size={14} />
              <span>上一页</span>
            </button>
            <button
              type="button"
              className="button button-quiet btn-sm"
              disabled={offset + limit >= total || loading}
              onClick={() => setOffset(offset + limit)}
              style={{ display: 'flex', alignItems: 'center', gap: '4px' }}
            >
              <span>下一页</span>
              <CaretRight size={14} />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
