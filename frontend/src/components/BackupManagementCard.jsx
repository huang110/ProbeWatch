import { useEffect, useState, useRef } from 'react'
import {
  Database,
  DownloadSimple,
  Trash,
  ArrowCounterClockwise,
  UploadSimple,
  CheckCircle,
  Warning,
  ShieldCheck,
  FileArchive,
  Plus,
  HardDrives,
  Clock,
  ArrowClockwise,
  FileText,
  Copy,
} from '@phosphor-icons/react'
import {
  fetchBackups,
  createBackup,
  deleteBackup,
  restoreBackup,
  uploadBackupFile,
} from '../lib/api.js'
import { formatBytes, safeText } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function BackupManagementCard() {
  const [backups, setBackups] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [actionBusy, setActionBusy] = useState(false)
  const [status, setStatus] = useState(null)
  const [confirmRestore, setConfirmRestore] = useState(null)
  const [copiedHash, setCopiedHash] = useState('')
  const fileInputRef = useRef(null)

  const loadList = async () => {
    try {
      setError('')
      const list = await fetchBackups()
      setBackups(Array.isArray(list) ? list : [])
    } catch (err) {
      setError(err?.message || '无法加载备份列表')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadList()
  }, [])

  const handleCreate = async () => {
    setCreating(true)
    setStatus(null)
    try {
      const res = await createBackup()
      setStatus({
        kind: 'ok',
        message: `备份创建成功：${res.filename} (${formatBytes(res.size_bytes)})`,
      })
      await loadList()
    } catch (err) {
      setStatus({ kind: 'error', message: `备份失败: ${err.message}` })
    } finally {
      setCreating(false)
    }
  }

  const handleFileChange = async (e) => {
    const file = e.target.files?.[0]
    if (!file) return
    if (!file.name.endsWith('.db') && !file.name.endsWith('.db.gz') && !file.name.endsWith('.gz')) {
      setStatus({ kind: 'error', message: '只允许上传 .db 或 .db.gz 格式的数据库备份文件。' })
      if (fileInputRef.current) fileInputRef.current.value = ''
      return
    }

    setUploading(true)
    setStatus(null)
    try {
      const res = await uploadBackupFile(file)
      setStatus({
        kind: 'ok',
        message: `备份文件上传成功：${res.filename} (${formatBytes(res.size_bytes)})`,
      })
      await loadList()
    } catch (err) {
      setStatus({ kind: 'error', message: `上传失败: ${err.message}` })
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  const handleDelete = async (filename) => {
    if (!window.confirm(`确定要永久删除备份文件 "${filename}" 吗？此操作不可逆。`)) {
      return
    }
    setActionBusy(true)
    setStatus(null)
    try {
      await deleteBackup(filename)
      setStatus({ kind: 'ok', message: `已成功删除备份：${filename}` })
      await loadList()
    } catch (err) {
      setStatus({ kind: 'error', message: `删除备份失败: ${err.message}` })
    } finally {
      setActionBusy(false)
    }
  }

  const handleRestore = async (filename) => {
    setConfirmRestore(null)
    setActionBusy(true)
    setStatus(null)
    try {
      await restoreBackup(filename)
      setStatus({
        kind: 'ok',
        message: `数据库已成功恢复为 "${filename}"！系统已自动保留恢复前紧急快照。正在自动刷新页面...`,
      })
      setTimeout(() => {
        window.location.reload()
      }, 1600)
    } catch (err) {
      setStatus({ kind: 'error', message: `恢复失败: ${err.message}` })
      setActionBusy(false)
    }
  }

  const copyHash = (hash) => {
    if (!hash) return
    navigator.clipboard?.writeText(hash).then(() => {
      setCopiedHash(hash)
      setTimeout(() => setCopiedHash(''), 2000)
    })
  }

  const formatDate = (isoStr) => {
    if (!isoStr) return '—'
    try {
      const d = new Date(isoStr)
      if (isNaN(d.getTime())) return isoStr
      return d.toLocaleString('zh-CN', { hour12: false })
    } catch {
      return isoStr
    }
  }

  return (
    <div className="panel" style={{ marginTop: '16px' }}>
      <div className="panel-header">
        <div>
          <h2>SQLite 数据库备份与安全热恢复</h2>
          <p>
            基于 SQLite 零停机在线快照（VACUUM INTO）与 Gzip 压缩，支持 SHA-256 完整性检验、一键下载、外部文件上传及原子热切换恢复。
          </p>
        </div>
        <span className="metric-icon metric-icon-mint">
          <Database size={17} weight="duotone" />
        </span>
      </div>

      {status && (
        <div
          className={`api-state api-state-${status.kind === 'ok' ? 'ok' : 'error'}`}
          role="status"
          style={{ marginBottom: '14px' }}
        >
          {status.message}
        </div>
      )}

      {/* 顶部操作区 */}
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: '10px',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: '16px',
        }}
      >
        <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
          <button
            type="button"
            className="button button-primary"
            onClick={handleCreate}
            disabled={creating || uploading || actionBusy}
          >
            {creating ? (
              <>
                <ArrowClockwise className="spin" size={16} />
                <span>正在生成热备份...</span>
              </>
            ) : (
              <>
                <Plus size={16} weight="bold" />
                <span>立即创建备份</span>
              </>
            )}
          </button>

          <label
            className={`button button-quiet ${uploading ? 'button-disabled' : ''}`}
            style={{ cursor: uploading ? 'not-allowed' : 'pointer' }}
          >
            <input
              type="file"
              ref={fileInputRef}
              onChange={handleFileChange}
              accept=".db,.gz,.db.gz"
              style={{ display: 'none' }}
              disabled={uploading || creating || actionBusy}
            />
            {uploading ? (
              <>
                <ArrowClockwise className="spin" size={16} />
                <span>正在上传文件...</span>
              </>
            ) : (
              <>
                <UploadSimple size={16} />
                <span>上传备份文件</span>
              </>
            )}
          </label>
        </div>

        <button
          type="button"
          className="button button-quiet btn-sm"
          onClick={loadList}
          disabled={loading || actionBusy}
          title="刷新备份列表"
        >
          <ArrowClockwise size={15} className={loading ? 'spin' : ''} />
          <span>刷新列表</span>
        </button>
      </div>

      {/* 列表区域 */}
      {error ? (
        <EmptyState title="无法加载备份" detail={error} />
      ) : loading ? (
        <EmptyState title="正在加载备份列表..." />
      ) : backups.length === 0 ? (
        <div className="empty-state-card" style={{ padding: '24px', textAlign: 'center' }}>
          <FileArchive size={36} className="muted" style={{ marginBottom: '8px' }} />
          <p className="muted" style={{ margin: 0 }}>
            暂无历史数据库备份文件。点击上方“立即创建备份”即可生成第一份安全归档。
          </p>
        </div>
      ) : (
        <div className="table-responsive">
          <table className="table">
            <thead>
              <tr>
                <th>备份文件名</th>
                <th>大小</th>
                <th>格式</th>
                <th>SHA-256 校验和</th>
                <th>创建时间</th>
                <th style={{ textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {backups.map((b) => (
                <tr key={b.filename}>
                  <td style={{ fontWeight: 500, fontFamily: 'monospace' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                      <FileText size={16} className="text-mint" />
                      <span>{b.filename}</span>
                    </div>
                  </td>
                  <td>
                    <span className="mono">{formatBytes(b.size_bytes)}</span>
                  </td>
                  <td>
                    {b.is_compressed ? (
                      <span className="badge badge-mint" style={{ fontSize: '11px' }}>
                        GZIP 压缩
                      </span>
                    ) : (
                      <span className="badge badge-gray" style={{ fontSize: '11px' }}>
                        Raw DB
                      </span>
                    )}
                  </td>
                  <td>
                    {b.sha256 ? (
                      <div
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: '4px',
                          cursor: 'pointer',
                          padding: '2px 6px',
                          borderRadius: '4px',
                          background: 'rgba(255,255,255,0.04)',
                        }}
                        onClick={() => copyHash(b.sha256)}
                        title={`完整校验和: ${b.sha256}\n点击复制`}
                      >
                        <span className="mono muted" style={{ fontSize: '12px' }}>
                          {b.sha256.slice(0, 10)}…{b.sha256.slice(-6)}
                        </span>
                        <Copy size={13} className={copiedHash === b.sha256 ? 'text-mint' : 'muted'} />
                        {copiedHash === b.sha256 && (
                          <span style={{ fontSize: '11px', color: 'var(--mint-400)' }}>已复制</span>
                        )}
                      </div>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                  <td className="muted" style={{ fontSize: '13px' }}>
                    {formatDate(b.created_at)}
                  </td>
                  <td style={{ textAlign: 'right' }}>
                    <div
                      style={{
                        display: 'inline-flex',
                        gap: '6px',
                        justifyContent: 'flex-end',
                      }}
                    >
                      <a
                        href={`/api/system/backups/${encodeURIComponent(b.filename)}/download`}
                        download={b.filename}
                        className="button button-quiet btn-sm"
                        title="下载此备份文件到本地"
                        style={{ textDecoration: 'none' }}
                      >
                        <DownloadSimple size={14} />
                        <span>下载</span>
                      </a>
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        style={{ color: 'var(--amber-400)' }}
                        onClick={() => setConfirmRestore(b)}
                        disabled={actionBusy}
                        title="从该备份热恢复数据库"
                      >
                        <ArrowCounterClockwise size={14} />
                        <span>恢复</span>
                      </button>
                      <button
                        type="button"
                        className="button button-quiet btn-sm"
                        style={{ color: 'var(--rose-400)' }}
                        onClick={() => handleDelete(b.filename)}
                        disabled={actionBusy}
                        title="永久删除此备份"
                      >
                        <Trash size={14} />
                        <span>删除</span>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* 恢复确认弹窗 */}
      {confirmRestore && (
        <div
          className="modal-backdrop"
          style={{
            position: 'fixed',
            inset: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.72)',
            backdropFilter: 'blur(4px)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
            padding: '16px',
          }}
          onClick={() => setConfirmRestore(null)}
        >
          <div
            className="modal-card panel"
            style={{
              maxWidth: '520px',
              width: '100%',
              backgroundColor: 'var(--panel-bg, #1a1e24)',
              border: '1px solid rgba(245, 158, 11, 0.4)',
              boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.6)',
              borderRadius: '12px',
              padding: '24px',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '16px' }}>
              <span
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  width: '40px',
                  height: '40px',
                  borderRadius: '8px',
                  backgroundColor: 'rgba(245, 158, 11, 0.15)',
                  color: 'var(--amber-400, #f59e0b)',
                }}
              >
                <Warning size={24} weight="fill" />
              </span>
              <div>
                <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>
                  确认从备份热恢复数据库？
                </h3>
                <p className="muted" style={{ margin: '4px 0 0 0', fontSize: '13px' }}>
                  目标备份：<code className="text-amber">{confirmRestore.filename}</code>
                </p>
              </div>
            </div>

            <div
              style={{
                backgroundColor: 'rgba(255, 255, 255, 0.03)',
                border: '1px solid rgba(255, 255, 255, 0.08)',
                borderRadius: '8px',
                padding: '14px',
                fontSize: '13px',
                lineHeight: '1.6',
                color: 'var(--text-secondary, #cbd5e1)',
                marginBottom: '20px',
              }}
            >
              <p style={{ margin: '0 0 8px 0' }}>
                <b>恢复安全机制说明：</b>
              </p>
              <ul style={{ margin: 0, paddingLeft: '18px' }}>
                <li>系统将在替换前<b>自动对当前活跃数据库进行紧急保护快照</b>（probewatch-pre-restore-*）。</li>
                <li>解压后系统会强制运行 <code>PRAGMA integrity_check</code> 验证数据完整性，校验失败将自动中断恢复。</li>
                <li>恢复完成后将自动热重载数据连接并刷新控制台页面。</li>
              </ul>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setConfirmRestore(null)}
                disabled={actionBusy}
              >
                取消
              </button>
              <button
                type="button"
                className="button"
                style={{
                  backgroundColor: '#dc2626',
                  color: '#fff',
                  border: 'none',
                }}
                onClick={() => handleRestore(confirmRestore.filename)}
                disabled={actionBusy}
              >
                {actionBusy ? '正在热恢复...' : '确认立即恢复'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
