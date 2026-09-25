import { useEffect, useState } from 'react'
import {
  Fingerprint,
  Plus,
  Trash,
  PencilSimple,
  Check,
  X,
  ShieldCheck,
  Key,
  Laptop,
  WarningCircle,
} from '@phosphor-icons/react'
import {
  isWebAuthnSupported,
  registerPasskey,
  listPasskeys,
  deletePasskey,
  renamePasskey,
} from '../lib/webauthn.js'
import { formatTimeOfDay } from '../lib/format.js'

function formatDateTime(isoOrDate) {
  if (!isoOrDate) return '从未使用'
  const d = new Date(isoOrDate)
  if (isNaN(d.getTime())) return '从未使用'
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function algoName(alg) {
  switch (alg) {
    case -7:
      return 'ES256 (ECC)'
    case -257:
      return 'RS256 (RSA)'
    case -8:
      return 'EdDSA (Ed25519)'
    default:
      return `Alg ${alg}`
  }
}

export function PasskeySettingsCard() {
  const [passkeys, setPasskeys] = useState([])
  const [loading, setLoading] = useState(true)
  const [actionBusy, setActionBusy] = useState(false)
  const [statusMsg, setStatusMsg] = useState(null)
  const [showAddModal, setShowAddModal] = useState(false)
  const [newPasskeyName, setNewPasskeyName] = useState('')
  const [editingId, setEditingId] = useState(null)
  const [editingName, setEditingName] = useState('')

  const supported = isWebAuthnSupported()

  const refreshList = async () => {
    try {
      const data = await listPasskeys()
      setPasskeys(Array.isArray(data) ? data : [])
    } catch (err) {
      console.error('Failed to load passkeys:', err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refreshList()
  }, [])

  const handleRegister = async (e) => {
    e.preventDefault()
    if (!supported) return
    setActionBusy(true)
    setStatusMsg({ kind: 'info', message: '请在系统弹出的生物识别或安全密钥提示中完成验证...' })

    try {
      const defaultName = navigator.userAgent.includes('Windows')
        ? 'Windows Hello'
        : navigator.userAgent.includes('Macintosh')
        ? 'Touch ID'
        : '我的通行密钥'
      const finalName = newPasskeyName.trim() || defaultName
      await registerPasskey(finalName)
      setStatusMsg({ kind: 'success', message: `通行密钥「${finalName}」绑定成功！` })
      setShowAddModal(false)
      setNewPasskeyName('')
      await refreshList()
    } catch (err) {
      setStatusMsg({ kind: 'error', message: err?.message || '通行密钥注册失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const handleDelete = async (id, name) => {
    if (!window.confirm(`确定要移除通行密钥「${name}」吗？移除后将无法使用该密钥登录。`)) {
      return
    }
    setActionBusy(true)
    setStatusMsg(null)
    try {
      await deletePasskey(id)
      setStatusMsg({ kind: 'success', message: `通行密钥「${name}」已移除。` })
      await refreshList()
    } catch (err) {
      setStatusMsg({ kind: 'error', message: err?.message || '移除失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const handleStartRename = (pk) => {
    setEditingId(pk.id)
    setEditingName(pk.name)
  }

  const handleSaveRename = async (id) => {
    if (!editingName.trim()) return
    setActionBusy(true)
    try {
      await renamePasskey(id, editingName.trim())
      setEditingId(null)
      await refreshList()
    } catch (err) {
      setStatusMsg({ kind: 'error', message: err?.message || '重命名失败' })
    } finally {
      setActionBusy(false)
    }
  }

  return (
    <div className="panel totp-card" style={{ position: 'relative' }}>
      <div className="panel-header" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          <div style={{
            width: '32px',
            height: '32px',
            borderRadius: '8px',
            background: 'rgba(56, 189, 248, 0.12)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#38bdf8',
          }}>
            <Fingerprint size={20} weight="bold" />
          </div>
          <div>
            <h2 style={{ margin: 0, fontSize: '15px', fontWeight: 600 }}>通行密钥 (Passkey / WebAuthn)</h2>
            <p style={{ margin: 0, fontSize: '12px', color: 'var(--text-muted)' }}>
              使用 Windows Hello、Touch ID、Face ID 或 FIDO2 硬件密钥免密极速登录
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          {passkeys.length > 0 ? (
            <span className="badge badge-success" style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
              <ShieldCheck size={14} />
              <span>已绑定 {passkeys.length} 个密钥</span>
            </span>
          ) : (
            <span className="badge" style={{ background: 'rgba(255,255,255,0.06)', color: 'var(--text-muted)' }}>
              未绑定
            </span>
          )}

          <button
            type="button"
            className="button button-primary btn-sm"
            onClick={() => {
              setNewPasskeyName('')
              setStatusMsg(null)
              setShowAddModal(true)
            }}
            disabled={actionBusy || !supported}
            style={{ display: 'inline-flex', alignItems: 'center', gap: '5px' }}
          >
            <Plus size={14} weight="bold" />
            <span>添加密钥</span>
          </button>
        </div>
      </div>

      {!supported && (
        <div className="alert-banner alert-warning" style={{ margin: '14px 0 0 0', display: 'flex', alignItems: 'center', gap: '8px', padding: '10px 14px', borderRadius: '8px', background: 'rgba(245, 158, 11, 0.1)', color: '#f59e0b', fontSize: '13px' }}>
          <WarningCircle size={18} weight="fill" />
          <span>当前浏览器环境不支持 WebAuthn 或未在安全上下文 (HTTPS / localhost) 中运行，通行密钥功能已禁用。</span>
        </div>
      )}

      {statusMsg && (
        <div
          style={{
            margin: '12px 0 0 0',
            padding: '8px 12px',
            borderRadius: '6px',
            fontSize: '13px',
            background: statusMsg.kind === 'error' ? 'rgba(244, 63, 94, 0.1)' : statusMsg.kind === 'success' ? 'rgba(34, 197, 94, 0.1)' : 'rgba(56, 189, 248, 0.1)',
            color: statusMsg.kind === 'error' ? '#f43f5e' : statusMsg.kind === 'success' ? '#22c55e' : '#38bdf8',
          }}
        >
          {statusMsg.message}
        </div>
      )}

      <div style={{ marginTop: '14px' }}>
        {loading ? (
          <div style={{ padding: '20px', textAlign: 'center', color: 'var(--text-muted)', fontSize: '13px' }}>
            正在加载通行密钥...
          </div>
        ) : passkeys.length === 0 ? (
          <div style={{
            padding: '24px 16px',
            textAlign: 'center',
            background: 'rgba(255, 255, 255, 0.02)',
            borderRadius: '8px',
            border: '1px dashed rgba(255, 255, 255, 0.08)',
          }}>
            <Key size={28} style={{ opacity: 0.35, marginBottom: '6px' }} />
            <div style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>暂无绑定的通行密钥</div>
            <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
              点击右上角「添加密钥」，为当前设备（指纹 / 面容 / 硬件锁）注册免密凭证。
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            {passkeys.map((pk) => {
              const isEditing = editingId === pk.id
              return (
                <div
                  key={pk.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '10px 14px',
                    borderRadius: '8px',
                    background: 'rgba(255, 255, 255, 0.03)',
                    border: '1px solid rgba(255, 255, 255, 0.05)',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                    <div style={{
                      width: '28px',
                      height: '28px',
                      borderRadius: '6px',
                      background: 'rgba(255, 255, 255, 0.05)',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      color: 'var(--text-secondary)',
                    }}>
                      <Laptop size={16} />
                    </div>

                    <div>
                      {isEditing ? (
                        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                          <input
                            type="text"
                            value={editingName}
                            onChange={(e) => setEditingName(e.target.value)}
                            className="input-field"
                            style={{ height: '26px', padding: '2px 8px', fontSize: '13px' }}
                            autoFocus
                          />
                          <button
                            type="button"
                            className="button button-quiet btn-sm"
                            onClick={() => handleSaveRename(pk.id)}
                            title="保存"
                          >
                            <Check size={14} />
                          </button>
                          <button
                            type="button"
                            className="button button-quiet btn-sm"
                            onClick={() => setEditingId(null)}
                            title="取消"
                          >
                            <X size={14} />
                          </button>
                        </div>
                      ) : (
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          <strong style={{ fontSize: '13px', fontWeight: 600 }}>{pk.name}</strong>
                          <button
                            type="button"
                            className="icon-button"
                            style={{ width: '20px', height: '20px', opacity: 0.6 }}
                            onClick={() => handleStartRename(pk)}
                            title="重命名"
                          >
                            <PencilSimple size={12} />
                          </button>
                        </div>
                      )}

                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '11px', color: 'var(--text-muted)', marginTop: '2px' }}>
                        <span>算法: {algoName(pk.algorithm)}</span>
                        <span>·</span>
                        <span>添加于: {formatDateTime(pk.created_at)}</span>
                        <span>·</span>
                        <span>上次使用: {formatDateTime(pk.last_used_at)}</span>
                      </div>
                    </div>
                  </div>

                  <button
                    type="button"
                    className="button button-quiet btn-sm text-rose"
                    onClick={() => handleDelete(pk.id, pk.name)}
                    disabled={actionBusy}
                    title="移除此通行密钥"
                    style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}
                  >
                    <Trash size={14} />
                    <span>移除</span>
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Add Passkey Modal */}
      {showAddModal && (
        <div className="modal-overlay" onClick={() => !actionBusy && setShowAddModal(false)}>
          <div className="mjj-login-modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '420px' }}>
            <div className="modal-header">
              <div className="modal-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <Fingerprint size={20} className="modal-title-icon" style={{ color: '#38bdf8' }} />
                <span>添加通行密钥</span>
              </div>
              <button
                type="button"
                className="modal-close-btn"
                onClick={() => setShowAddModal(false)}
                disabled={actionBusy}
              >
                <X size={16} />
              </button>
            </div>

            <p className="modal-desc">
              系统将唤起当前设备的 Windows Hello、Touch ID、Face ID 或 USB 安全密钥完成注册。
            </p>

            <form onSubmit={handleRegister}>
              <div className="input-group">
                <label>密钥名称 (可选)</label>
                <div className="input-wrapper">
                  <Key size={18} className="input-icon" />
                  <input
                    type="text"
                    className="modal-input"
                    placeholder="如：MacBook Touch ID / 笔记本 Windows Hello"
                    value={newPasskeyName}
                    onChange={(e) => setNewPasskeyName(e.target.value)}
                    disabled={actionBusy}
                    autoFocus
                  />
                </div>
              </div>

              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px', marginTop: '16px' }}>
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={() => setShowAddModal(false)}
                  disabled={actionBusy}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="button button-primary"
                  disabled={actionBusy}
                  style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
                >
                  <Fingerprint size={16} />
                  <span>{actionBusy ? '正在唤起验证...' : '立即唤起注册'}</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
