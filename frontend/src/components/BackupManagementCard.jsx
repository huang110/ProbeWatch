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
  CloudArrowUp,
  FloppyDisk,
  Check,
  XCircle,
  CloudCheck,
  Globe,
  Key,
  Shield,
} from '@phosphor-icons/react'
import {
  fetchBackups,
  createBackup,
  deleteBackup,
  restoreBackup,
  uploadBackupFile,
  fetchBackupConfig,
  saveBackupConfig,
  testS3Backup,
  testWebDAVBackup,
  exportBackupNow,
  verifyBackup,
} from '../lib/api.js'
import { formatBytes } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function BackupManagementCard() {
  const [activeTab, setActiveTab] = useState('snapshots') // 'snapshots' | 'cloud'

  // Snapshots State
  const [backups, setBackups] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [actionBusy, setActionBusy] = useState(false)
  const [status, setStatus] = useState(null)
  const [confirmRestore, setConfirmRestore] = useState(null)
  const [copiedHash, setCopiedHash] = useState('')
  const [verifyingFile, setVerifyingFile] = useState(null)
  const [verifyModal, setVerifyModal] = useState(null)
  const fileInputRef = useRef(null)

  // Cloud & Schedule Config State
  const [configLoading, setConfigLoading] = useState(false)
  const [configSaving, setConfigSaving] = useState(false)
  const [testingS3, setTestingS3] = useState(false)
  const [testingWebDAV, setTestingWebDAV] = useState(false)
  const [exportingNow, setExportingNow] = useState(false)
  const [testResultS3, setTestResultS3] = useState(null)
  const [testResultWebDAV, setTestResultWebDAV] = useState(null)
  const [exportReport, setExportReport] = useState(null)

  const [cloudConfig, setCloudConfig] = useState({
    enabled: false,
    interval_hours: 24,
    retention_count: 7,
    s3: {
      enabled: false,
      endpoint: '',
      bucket: '',
      region: 'us-east-1',
      access_key: '',
      secret_key: '',
    },
    webdav: {
      enabled: false,
      url: '',
      username: '',
      password: '',
    },
  })

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

  const loadConfig = async () => {
    setConfigLoading(true)
    try {
      const cfg = await fetchBackupConfig()
      if (cfg) {
        setCloudConfig({
          enabled: !!cfg.enabled,
          interval_hours: cfg.interval_hours || 24,
          retention_count: cfg.retention_count || 7,
          s3: {
            enabled: !!cfg.s3?.enabled,
            endpoint: cfg.s3?.endpoint || '',
            bucket: cfg.s3?.bucket || '',
            region: cfg.s3?.region || 'us-east-1',
            access_key: cfg.s3?.access_key || '',
            secret_key: cfg.s3?.secret_key || '',
          },
          webdav: {
            enabled: !!cfg.webdav?.enabled,
            url: cfg.webdav?.url || '',
            username: cfg.webdav?.username || '',
            password: cfg.webdav?.password || '',
          },
        })
      }
    } catch (err) {
      setStatus({ kind: 'error', message: `加载备份配置失败: ${err.message}` })
    } finally {
      setConfigLoading(false)
    }
  }

  useEffect(() => {
    loadList()
    loadConfig()
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

  const handleVerify = async (filename) => {
    setVerifyingFile(filename)
    try {
      const result = await verifyBackup(filename)
      setVerifyModal(result)
    } catch (err) {
      setStatus({ kind: 'error', message: `灾备演练校验失败: ${err.message}` })
    } finally {
      setVerifyingFile(null)
    }
  }

  const handleSaveConfig = async () => {
    setConfigSaving(true)
    setStatus(null)
    try {
      await saveBackupConfig(cloudConfig)
      setStatus({ kind: 'ok', message: '灾备计划与异地存储配置已成功保存！' })
      await loadConfig()
    } catch (err) {
      setStatus({ kind: 'error', message: `保存配置失败: ${err.message}` })
    } finally {
      setConfigSaving(false)
    }
  }

  const handleTestS3 = async () => {
    setTestingS3(true)
    setTestResultS3(null)
    try {
      const res = await testS3Backup(cloudConfig.s3)
      setTestResultS3({ ok: true, message: res.message || 'S3 存储桶连接并验证通过！' })
    } catch (err) {
      setTestResultS3({ ok: false, message: err.message || 'S3 连接失败' })
    } finally {
      setTestingS3(false)
    }
  }

  const handleTestWebDAV = async () => {
    setTestingWebDAV(true)
    setTestResultWebDAV(null)
    try {
      const res = await testWebDAVBackup(cloudConfig.webdav)
      setTestResultWebDAV({ ok: true, message: res.message || 'WebDAV 端点验证与凭据校验通过！' })
    } catch (err) {
      setTestResultWebDAV({ ok: false, message: err.message || 'WebDAV 连接失败' })
    } finally {
      setTestingWebDAV(false)
    }
  }

  const handleExportNow = async () => {
    setExportingNow(true)
    setExportReport(null)
    setStatus(null)
    try {
      const res = await exportBackupNow()
      setExportReport(res)
      setStatus({
        kind: 'ok',
        message: `云端导出任务执行完毕！耗时 ${res.duration_ms}ms`,
      })
      await loadList()
    } catch (err) {
      setStatus({ kind: 'error', message: `立即导出失败: ${err.message}` })
    } finally {
      setExportingNow(false)
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
          <h2>SQLite 灾难备份与多区域热备 (Disaster Recovery & Hot Standby)</h2>
          <p>
            零停机在线快照（VACUUM INTO）、离线沙箱完整性演练、S3 (Cloudflare R2 / AWS / MinIO) 与 WebDAV 异地云同步。
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

      {/* 选项卡导航 */}
      <div
        style={{
          display: 'flex',
          gap: '8px',
          borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
          paddingBottom: '12px',
          marginBottom: '16px',
        }}
      >
        <button
          type="button"
          className={`button ${activeTab === 'snapshots' ? 'button-primary' : 'button-quiet'}`}
          onClick={() => setActiveTab('snapshots')}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <HardDrives size={16} />
          <span>本地快照与归档 ({backups.length})</span>
        </button>
        <button
          type="button"
          className={`button ${activeTab === 'cloud' ? 'button-primary' : 'button-quiet'}`}
          onClick={() => setActiveTab('cloud')}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <CloudArrowUp size={16} />
          <span>云端灾备与定时同步 (S3 / WebDAV)</span>
        </button>
      </div>

      {/* TAB 1: 本地快照与恢复 */}
      {activeTab === 'snapshots' && (
        <div>
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
                          <button
                            type="button"
                            className="button button-quiet btn-sm"
                            style={{ color: 'var(--mint-400)' }}
                            onClick={() => handleVerify(b.filename)}
                            disabled={actionBusy || verifyingFile === b.filename}
                            title="非破坏性离线沙箱演练完整性校验"
                          >
                            {verifyingFile === b.filename ? (
                              <ArrowClockwise size={14} className="spin" />
                            ) : (
                              <ShieldCheck size={14} />
                            )}
                            <span>校验</span>
                          </button>
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
        </div>
      )}

      {/* TAB 2: 云端灾备与定时同步 */}
      {activeTab === 'cloud' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
          {/* 定时计划配置 */}
          <div
            style={{
              padding: '16px',
              borderRadius: '8px',
              backgroundColor: 'rgba(255, 255, 255, 0.02)',
              border: '1px solid rgba(255, 255, 255, 0.06)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <Clock size={20} className="text-mint" />
                <h3 style={{ margin: 0, fontSize: '15px' }}>自动化调度计划 (Automated Schedule)</h3>
              </div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={cloudConfig.enabled}
                  onChange={(e) => setCloudConfig({ ...cloudConfig, enabled: e.target.checked })}
                />
                <span style={{ fontSize: '13px', fontWeight: 500 }}>
                  {cloudConfig.enabled ? '已开启后台自动调度' : '未开启自动调度'}
                </span>
              </label>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '16px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '6px' }}>
                  备份周期间隔 (小时)
                </label>
                <select
                  className="input"
                  style={{ width: '100%' }}
                  value={cloudConfig.interval_hours}
                  onChange={(e) => setCloudConfig({ ...cloudConfig, interval_hours: parseInt(e.target.value, 10) || 24 })}
                >
                  <option value={1}>每 1 小时 (高频热备)</option>
                  <option value={6}>每 6 小时</option>
                  <option value={12}>每 12 小时</option>
                  <option value={24}>每 24 小时 (每日定时)</option>
                  <option value={48}>每 48 小时</option>
                  <option value={168}>每 7 天 (每周)</option>
                </select>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '6px' }}>
                  本地快照最多保留份数 (超出自动轮转修剪)
                </label>
                <input
                  type="number"
                  min="1"
                  max="100"
                  className="input"
                  style={{ width: '100%' }}
                  value={cloudConfig.retention_count}
                  onChange={(e) => setCloudConfig({ ...cloudConfig, retention_count: parseInt(e.target.value, 10) || 7 })}
                />
              </div>
            </div>
          </div>

          {/* S3 存储桶配置 */}
          <div
            style={{
              padding: '16px',
              borderRadius: '8px',
              backgroundColor: 'rgba(255, 255, 255, 0.02)',
              border: '1px solid rgba(255, 255, 255, 0.06)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <CloudCheck size={20} className="text-mint" />
                <div>
                  <h3 style={{ margin: 0, fontSize: '15px' }}>S3 对象存储 (Cloudflare R2 / AWS / MinIO / OSS)</h3>
                  <p style={{ margin: '2px 0 0 0', fontSize: '12px', color: 'var(--muted)' }}>
                    标准 AWS S3 SigV4 协议，支持上传至任意全球多区域对象存储桶。
                  </p>
                </div>
              </div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={cloudConfig.s3.enabled}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      s3: { ...cloudConfig.s3, enabled: e.target.checked },
                    })
                  }
                />
                <span style={{ fontSize: '13px', fontWeight: 500 }}>
                  {cloudConfig.s3.enabled ? '已启用 S3 同步' : '已禁用'}
                </span>
              </label>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '14px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  S3 Endpoint 接入点
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder="https://<account>.r2.cloudflarestorage.com"
                  value={cloudConfig.s3.endpoint}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      s3: { ...cloudConfig.s3, endpoint: e.target.value },
                    })
                  }
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  存储桶名称 (Bucket)
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder="probewatch-backups"
                  value={cloudConfig.s3.bucket}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      s3: { ...cloudConfig.s3, bucket: e.target.value },
                    })
                  }
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  区域 (Region)
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder="auto / us-east-1"
                  value={cloudConfig.s3.region}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      s3: { ...cloudConfig.s3, region: e.target.value },
                    })
                  }
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  Access Key ID
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder="AKIA..."
                  value={cloudConfig.s3.access_key}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      s3: { ...cloudConfig.s3, access_key: e.target.value },
                    })
                  }
                />
              </div>

              <div style={{ gridColumn: '1 / -1' }}>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  Secret Access Key
                </label>
                <input
                  type="password"
                  className="input"
                  placeholder={cloudConfig.s3.secret_key ? '•••••••• (已保存，留空或带掩码保持不变)' : '请输入密钥'}
                  value={cloudConfig.s3.secret_key}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      s3: { ...cloudConfig.s3, secret_key: e.target.value },
                    })
                  }
                />
              </div>
            </div>

            <div style={{ marginTop: '12px', display: 'flex', alignItems: 'center', gap: '10px' }}>
              <button
                type="button"
                className="button button-quiet btn-sm"
                onClick={handleTestS3}
                disabled={testingS3 || !cloudConfig.s3.bucket}
              >
                {testingS3 ? (
                  <>
                    <ArrowClockwise className="spin" size={14} />
                    <span>正在测试连接...</span>
                  </>
                ) : (
                  <>
                    <ShieldCheck size={14} />
                    <span>测试 S3 连接与权限</span>
                  </>
                )}
              </button>
              {testResultS3 && (
                <span
                  style={{
                    fontSize: '12px',
                    color: testResultS3.ok ? 'var(--mint-400)' : 'var(--rose-400)',
                  }}
                >
                  {testResultS3.message}
                </span>
              )}
            </div>
          </div>

          {/* WebDAV 存储配置 */}
          <div
            style={{
              padding: '16px',
              borderRadius: '8px',
              backgroundColor: 'rgba(255, 255, 255, 0.02)',
              border: '1px solid rgba(255, 255, 255, 0.06)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <Globe size={20} className="text-mint" />
                <div>
                  <h3 style={{ margin: 0, fontSize: '15px' }}>WebDAV 异地备份 (坚果云 / Nextcloud / 群晖 NAS)</h3>
                  <p style={{ margin: '2px 0 0 0', fontSize: '12px', color: 'var(--muted)' }}>
                    通过 WebDAV 协议将备份推送到私有网盘或 NAS 存储。
                  </p>
                </div>
              </div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={cloudConfig.webdav.enabled}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      webdav: { ...cloudConfig.webdav, enabled: e.target.checked },
                    })
                  }
                />
                <span style={{ fontSize: '13px', fontWeight: 500 }}>
                  {cloudConfig.webdav.enabled ? '已启用 WebDAV 同步' : '已禁用'}
                </span>
              </label>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '14px' }}>
              <div style={{ gridColumn: '1 / -1' }}>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  WebDAV 目标目录 URL
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder="https://dav.jianguoyun.com/dav/probewatch/"
                  value={cloudConfig.webdav.url}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      webdav: { ...cloudConfig.webdav, url: e.target.value },
                    })
                  }
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  用户名 / 账号
                </label>
                <input
                  type="text"
                  className="input"
                  placeholder="user@example.com"
                  value={cloudConfig.webdav.username}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      webdav: { ...cloudConfig.webdav, username: e.target.value },
                    })
                  }
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', color: 'var(--muted)', marginBottom: '4px' }}>
                  密码 / 应用授权码
                </label>
                <input
                  type="password"
                  className="input"
                  placeholder={cloudConfig.webdav.password ? '•••••••• (已保存，留空保持不变)' : '请输入密码'}
                  value={cloudConfig.webdav.password}
                  onChange={(e) =>
                    setCloudConfig({
                      ...cloudConfig,
                      webdav: { ...cloudConfig.webdav, password: e.target.value },
                    })
                  }
                />
              </div>
            </div>

            <div style={{ marginTop: '12px', display: 'flex', alignItems: 'center', gap: '10px' }}>
              <button
                type="button"
                className="button button-quiet btn-sm"
                onClick={handleTestWebDAV}
                disabled={testingWebDAV || !cloudConfig.webdav.url}
              >
                {testingWebDAV ? (
                  <>
                    <ArrowClockwise className="spin" size={14} />
                    <span>正在测试连接...</span>
                  </>
                ) : (
                  <>
                    <ShieldCheck size={14} />
                    <span>测试 WebDAV 连接与写入</span>
                  </>
                )}
              </button>
              {testResultWebDAV && (
                <span
                  style={{
                    fontSize: '12px',
                    color: testResultWebDAV.ok ? 'var(--mint-400)' : 'var(--rose-400)',
                  }}
                >
                  {testResultWebDAV.message}
                </span>
              )}
            </div>
          </div>

          {/* 底部操作与执行 */}
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: '12px',
              paddingTop: '8px',
            }}
          >
            <div style={{ display: 'flex', gap: '10px' }}>
              <button
                type="button"
                className="button button-primary"
                onClick={handleSaveConfig}
                disabled={configSaving}
              >
                {configSaving ? (
                  <>
                    <ArrowClockwise className="spin" size={16} />
                    <span>正在保存配置...</span>
                  </>
                ) : (
                  <>
                    <FloppyDisk size={16} weight="bold" />
                    <span>保存云端灾备设置</span>
                  </>
                )}
              </button>
            </div>

            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <button
                type="button"
                className="button button-quiet"
                onClick={handleExportNow}
                disabled={exportingNow}
                title="立即生成本地热快照并同步推送至已启用的 S3 / WebDAV 云端存储"
              >
                {exportingNow ? (
                  <>
                    <ArrowClockwise className="spin" size={16} />
                    <span>正在执行云端异地导出...</span>
                  </>
                ) : (
                  <>
                    <CloudArrowUp size={16} />
                    <span>立即触发云端异地备份 (Export Now)</span>
                  </>
                )}
              </button>
            </div>
          </div>

          {/* 立即导出结果卡片 */}
          {exportReport && (
            <div
              style={{
                marginTop: '12px',
                padding: '16px',
                borderRadius: '8px',
                backgroundColor: 'rgba(56, 189, 248, 0.08)',
                border: '1px solid rgba(56, 189, 248, 0.25)',
              }}
            >
              <h4 style={{ margin: '0 0 10px 0', fontSize: '14px', color: 'var(--mint-400)' }}>
                云端同步执行报告：
              </h4>
              <ul style={{ margin: 0, paddingLeft: '18px', fontSize: '13px', lineHeight: '1.7' }}>
                <li>
                  生成快照：<code>{exportReport.backup_info?.filename}</code> (
                  {formatBytes(exportReport.backup_info?.size_bytes)})
                </li>
                <li>
                  S3 上传：{exportReport.s3_uploaded ? '✅ 成功上传' : exportReport.s3_error ? `❌ 失败: ${exportReport.s3_error}` : '未启用'}
                </li>
                <li>
                  WebDAV 上传：{exportReport.webdav_uploaded ? '✅ 成功上传' : exportReport.webdav_error ? `❌ 失败: ${exportReport.webdav_error}` : '未启用'}
                </li>
                <li>自动修剪历史快照：{exportReport.pruned_count} 份</li>
                <li>执行总耗时：{exportReport.duration_ms} ms</li>
              </ul>
            </div>
          )}
        </div>
      )}

      {/* 校验演练结果弹窗 */}
      {verifyModal && (
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
          onClick={() => setVerifyModal(null)}
        >
          <div
            className="modal-card panel"
            style={{
              maxWidth: '560px',
              width: '100%',
              backgroundColor: 'var(--panel-bg, #1a1e24)',
              border: '1px solid rgba(16, 185, 129, 0.4)',
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
                  backgroundColor: 'rgba(16, 185, 129, 0.15)',
                  color: 'var(--mint-400, #10b981)',
                }}
              >
                <ShieldCheck size={26} weight="fill" />
              </span>
              <div>
                <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>
                  灾备演练与完整性校验通过 (Integrity OK)
                </h3>
                <p className="muted" style={{ margin: '4px 0 0 0', fontSize: '13px' }}>
                  目标备份：<code className="text-mint">{verifyModal.filename}</code>
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
                lineHeight: '1.8',
                color: 'var(--text-secondary, #cbd5e1)',
                marginBottom: '20px',
              }}
            >
              <div style={{ display: 'grid', gridTemplateColumns: '130px 1fr', rowGap: '6px' }}>
                <span className="muted">B-Tree 完整性：</span>
                <span style={{ color: 'var(--mint-400)', fontWeight: 600 }}>
                  {verifyModal.integrity === 'ok' ? 'PRAGMA integrity_check 通过 (OK)' : verifyModal.integrity}
                </span>

                <span className="muted">校验数据表总数：</span>
                <span><b>{verifyModal.tables_count}</b> 个核心数据表</span>

                <span className="muted">归档大小：</span>
                <span>{formatBytes(verifyModal.size_bytes)}</span>

                <span className="muted">校验耗时：</span>
                <span>{verifyModal.duration_ms} ms</span>

                <span className="muted">SHA-256 指纹：</span>
                <span className="mono" style={{ fontSize: '11px', wordBreak: 'break-all' }}>
                  {verifyModal.sha256}
                </span>

                <span className="muted">演练时间：</span>
                <span>{formatDate(verifyModal.verified_at)}</span>
              </div>
            </div>

            <p style={{ margin: '0 0 16px 0', fontSize: '12px', color: 'var(--muted)' }}>
              *
              演练过程在全隔离沙箱内解压并执行零停机结构扫描，证明该归档具有完整的可恢复性与数据自洽性。
            </p>

            <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
              <button
                type="button"
                className="button button-primary"
                onClick={() => setVerifyModal(null)}
              >
                关闭
              </button>
            </div>
          </div>
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
