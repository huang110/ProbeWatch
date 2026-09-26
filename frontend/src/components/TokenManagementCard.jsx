import { useEffect, useState } from 'react'
import {
  Key,
  Plus,
  Trash,
  CheckCircle,
  XCircle,
  WarningCircle,
  ArrowClockwise,
  CircleNotch,
  Copy,
  Check,
  X,
  ShieldCheck,
  SlidersHorizontal,
  Eye,
  BookOpen,
  Calendar,
  Clock,
  HardDrives,
} from '@phosphor-icons/react'
import { fetchTokens, createToken, updateToken, deleteToken } from '../lib/api.js'
import { EmptyState } from './Common.jsx'

export function TokenManagementCard({ currentUser, nodes = [] }) {
  const [tokens, setTokens] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionBusy, setActionBusy] = useState(false)
  const [statusMsg, setStatusMsg] = useState(null)

  // Modals
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newRawTokenModal, setNewRawTokenModal] = useState(null) // { name: '', rawToken: '' }
  const [deleteConfirmToken, setDeleteConfirmToken] = useState(null)
  const [copied, setCopied] = useState(false)

  // Form states for creation
  const [tokenName, setTokenName] = useState('')
  const [tokenRole, setTokenRole] = useState('operator')
  const [scopeType, setScopeType] = useState('all') // 'all' | 'custom'
  const [customScopes, setCustomScopes] = useState(['read:nodes', 'read:metrics'])
  const [nodeScopeType, setNodeScopeType] = useState('all') // 'all' | 'custom'
  const [selectedNodes, setSelectedNodes] = useState([])
  const [expiresInDays, setExpiresInDays] = useState(90) // 7, 30, 90, 365, 0 (never)

  const isAdmin = currentUser?.is_admin || currentUser?.role === 'admin'

  const loadTokens = async () => {
    setLoading(true)
    setError('')
    try {
      const data = await fetchTokens(!isAdmin)
      setTokens(Array.isArray(data) ? data : [])
    } catch (err) {
      setError(err?.message || '获取令牌列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadTokens()
  }, [])

  const openCreateModal = () => {
    setTokenName('')
    setTokenRole(isAdmin ? 'operator' : (currentUser?.role || 'viewer'))
    setScopeType('all')
    setCustomScopes(['read:nodes', 'read:metrics'])
    setNodeScopeType('all')
    setSelectedNodes([])
    setExpiresInDays(90)
    setStatusMsg(null)
    setShowCreateModal(true)
  }

  const handleCreateSubmit = async (e) => {
    e.preventDefault()
    if (!tokenName.trim()) {
      setStatusMsg({ kind: 'error', text: '请输入令牌名称' })
      return
    }

    const scopes = scopeType === 'all' ? '*' : customScopes.join(',')
    const allowedNodes = nodeScopeType === 'all' ? '*' : selectedNodes.join(',')

    setActionBusy(true)
    setStatusMsg(null)
    try {
      const res = await createToken({
        name: tokenName.trim(),
        role: tokenRole,
        scopes,
        allowed_nodes: allowedNodes,
        expires_in_days: Number(expiresInDays),
      })

      setShowCreateModal(false)
      if (res?.raw_token) {
        setNewRawTokenModal({
          name: tokenName.trim(),
          rawToken: res.raw_token,
        })
      }
      loadTokens()
    } catch (err) {
      setStatusMsg({ kind: 'error', text: err?.message || '创建令牌失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const handleToggleDisabled = async (token) => {
    setActionBusy(true)
    setStatusMsg(null)
    try {
      await updateToken(token.id, { disabled: !token.disabled })
      setStatusMsg({ kind: 'success', text: `令牌【${token.name}】已${token.disabled ? '启用' : '停用'}` })
      loadTokens()
    } catch (err) {
      setStatusMsg({ kind: 'error', text: err?.message || '更新令牌状态失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const handleDeleteSubmit = async () => {
    if (!deleteConfirmToken) return
    setActionBusy(true)
    setStatusMsg(null)
    try {
      await deleteToken(deleteConfirmToken.id)
      setStatusMsg({ kind: 'success', text: `令牌【${deleteConfirmToken.name}】已彻底撤销` })
      setDeleteConfirmToken(null)
      loadTokens()
    } catch (err) {
      setStatusMsg({ kind: 'error', text: err?.message || '删除令牌失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const copyToClipboard = (text) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2500)
    })
  }

  const toggleCustomScope = (scope) => {
    setCustomScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope]
    )
  }

  const toggleNodeSelection = (uuid) => {
    setSelectedNodes((prev) =>
      prev.includes(uuid) ? prev.filter((id) => id !== uuid) : [...prev, uuid]
    )
  }

  const renderRoleBadge = (role) => {
    switch (role) {
      case 'admin':
        return (
          <span className="badge badge-purple" style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
            <ShieldCheck size={13} weight="bold" />
            超级管理员
          </span>
        )
      case 'operator':
        return (
          <span className="badge badge-blue" style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
            <SlidersHorizontal size={13} weight="bold" />
            运维操作员
          </span>
        )
      case 'viewer':
      default:
        return (
          <span className="badge badge-gray" style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
            <Eye size={13} weight="bold" />
            只读观察员
          </span>
        )
    }
  }

  const getNodeName = (uuid) => {
    const n = nodes.find((item) => (item.uuid || item.id) === uuid)
    return n ? (n.customName || n.name) : uuid.slice(0, 8)
  }

  return (
    <div className="card token-card" style={{ padding: '24px', borderRadius: '12px' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: '16px', marginBottom: '20px' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            <div style={{ width: '36px', height: '36px', borderRadius: '8px', background: 'rgba(234, 179, 8, 0.12)', color: '#eab308', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Key size={22} weight="bold" />
            </div>
            <div>
              <h2 style={{ fontSize: '18px', fontWeight: '600', margin: 0 }}>开发者 API 访问令牌 (Personal Access Tokens / PAT)</h2>
              <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-secondary, #94a3b8)' }}>
                长效自动化访问凭证，支持角色继承、细粒度调用作用域与节点白名单，安全免除浏览器 CSRF 依赖。
              </p>
            </div>
          </div>
        </div>

        <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
          <a
            href="/docs"
            target="_blank"
            rel="noopener noreferrer"
            className="button button-quiet"
            style={{ display: 'flex', alignItems: 'center', gap: '6px', textDecoration: 'none' }}
            title="打开 OpenAPI / Swagger 交互式文档中心"
          >
            <BookOpen size={16} />
            <span>API 交互文档 (Swagger)</span>
          </a>

          <button
            type="button"
            className="button button-quiet"
            onClick={loadTokens}
            disabled={loading}
            title="刷新令牌列表"
          >
            <ArrowClockwise size={16} className={loading ? 'spin' : ''} />
            <span>刷新</span>
          </button>

          <button
            type="button"
            className="button button-primary"
            onClick={openCreateModal}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Plus size={16} weight="bold" />
            <span>生成新令牌</span>
          </button>
        </div>
      </div>

      {/* Status Notice */}
      {statusMsg && (
        <div
          style={{
            padding: '12px 16px',
            borderRadius: '8px',
            marginBottom: '16px',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            fontSize: '13px',
            background: statusMsg.kind === 'error' ? 'rgba(239, 68, 68, 0.12)' : 'rgba(16, 185, 129, 0.12)',
            color: statusMsg.kind === 'error' ? '#ef4444' : '#10b981',
            border: `1px solid ${statusMsg.kind === 'error' ? 'rgba(239, 68, 68, 0.25)' : 'rgba(16, 185, 129, 0.25)'}`,
          }}
        >
          {statusMsg.kind === 'error' ? <WarningCircle size={18} /> : <CheckCircle size={18} />}
          <span>{statusMsg.text}</span>
          <button
            type="button"
            className="icon-button"
            style={{ marginLeft: 'auto', background: 'transparent', border: 'none', cursor: 'pointer', color: 'inherit' }}
            onClick={() => setStatusMsg(null)}
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Table */}
      {loading && tokens.length === 0 ? (
        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', padding: '40px' }}>
          <CircleNotch size={24} className="spin" style={{ color: 'var(--accent-color, #6366f1)' }} />
          <span style={{ marginLeft: '10px', fontSize: '14px', color: 'var(--text-secondary)' }}>正在载入 API 令牌列表…</span>
        </div>
      ) : error ? (
        <EmptyState
          icon={WarningCircle}
          title="加载失败"
          description={error}
          actionText="重新加载"
          onAction={loadTokens}
        />
      ) : tokens.length === 0 ? (
        <EmptyState
          icon={Key}
          title="暂无任何 API 访问令牌"
          description="点击右上角「生成新令牌」，可为自动化部署、Prometheus 监控系统或外部脚本创建安全的访问凭据。"
          actionText="生成第一个 API 令牌"
          onAction={openCreateModal}
        />
      ) : (
        <div className="table-responsive" style={{ overflowX: 'auto' }}>
          <table className="table" style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color, rgba(255, 255, 255, 0.08))', color: 'var(--text-secondary)' }}>
                <th style={{ padding: '10px 14px' }}>令牌名称</th>
                <th style={{ padding: '10px 14px' }}>标识前缀</th>
                <th style={{ padding: '10px 14px' }}>角色</th>
                <th style={{ padding: '10px 14px' }}>权限作用域</th>
                <th style={{ padding: '10px 14px' }}>有效期</th>
                <th style={{ padding: '10px 14px' }}>最后使用</th>
                <th style={{ padding: '10px 14px' }}>状态</th>
                <th style={{ padding: '10px 14px', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((tok) => {
                const isExpired = tok.expires_at && new Date(tok.expires_at) < new Date()
                return (
                  <tr
                    key={tok.id}
                    style={{
                      borderBottom: '1px solid var(--border-color, rgba(255, 255, 255, 0.05))',
                      opacity: tok.disabled || isExpired ? 0.6 : 1,
                    }}
                  >
                    <td style={{ padding: '12px 14px' }}>
                      <strong style={{ color: 'var(--text-primary)' }}>{tok.name}</strong>
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      <code style={{ background: 'rgba(255, 255, 255, 0.05)', padding: '2px 6px', borderRadius: '4px', fontSize: '12px' }}>
                        {tok.token_prefix}
                      </code>
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {renderRoleBadge(tok.role)}
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {tok.scopes === '*' || !tok.scopes ? (
                        <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>全域权限 (*)</span>
                      ) : (
                        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', maxWidth: '200px' }}>
                          {tok.scopes.split(',').map((sc) => (
                            <span
                              key={sc}
                              style={{
                                fontSize: '11px',
                                background: 'rgba(99, 102, 241, 0.1)',
                                color: '#818cf8',
                                padding: '2px 6px',
                                borderRadius: '4px',
                                border: '1px solid rgba(99, 102, 241, 0.2)',
                              }}
                            >
                              {sc.trim()}
                            </span>
                          ))}
                        </div>
                      )}
                    </td>
                    <td style={{ padding: '12px 14px', fontSize: '12px' }}>
                      {tok.expires_at ? (
                        <span style={{ color: isExpired ? '#ef4444' : 'var(--text-secondary)' }}>
                          {isExpired ? '已过期 (' : ''}
                          {new Date(tok.expires_at).toLocaleDateString()}
                          {isExpired ? ')' : ''}
                        </span>
                      ) : (
                        <span style={{ color: 'var(--text-secondary)' }}>永久有效</span>
                      )}
                    </td>
                    <td style={{ padding: '12px 14px', fontSize: '12px', color: 'var(--text-secondary)' }}>
                      {tok.last_used_at ? new Date(tok.last_used_at).toLocaleDateString() : '从未'}
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {tok.disabled ? (
                        <span style={{ color: '#ef4444', fontSize: '12px', display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                          <XCircle size={14} weight="fill" />
                          已停用
                        </span>
                      ) : isExpired ? (
                        <span style={{ color: '#f59e0b', fontSize: '12px', display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                          <Clock size={14} weight="fill" />
                          已过期
                        </span>
                      ) : (
                        <span style={{ color: '#10b981', fontSize: '12px', display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                          <CheckCircle size={14} weight="fill" />
                          正常
                        </span>
                      )}
                    </td>
                    <td style={{ padding: '12px 14px', textAlign: 'right' }}>
                      <div style={{ display: 'inline-flex', gap: '8px', alignItems: 'center' }}>
                        <button
                          type="button"
                          className="button button-quiet btn-sm"
                          onClick={() => handleToggleDisabled(tok)}
                          title={tok.disabled ? '启用令牌' : '停用令牌'}
                          style={{ padding: '4px 8px' }}
                        >
                          {tok.disabled ? '启用' : '停用'}
                        </button>
                        <button
                          type="button"
                          className="button button-quiet btn-sm text-rose"
                          onClick={() => setDeleteConfirmToken(tok)}
                          title="彻底删除此令牌"
                          style={{ padding: '4px 8px' }}
                        >
                          <Trash size={14} />
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Create Token Modal */}
      {showCreateModal && (
        <div className="modal-overlay" onClick={() => !actionBusy && setShowCreateModal(false)}>
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '500px', width: '90%', margin: 'auto', padding: '24px', borderRadius: '12px', background: 'var(--card-bg, #18181b)', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <Key size={20} style={{ color: '#eab308' }} />
                <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600' }}>生成新的 API 访问令牌</h3>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setShowCreateModal(false)}
                disabled={actionBusy}
              >
                <X size={16} />
              </button>
            </div>

            <form onSubmit={handleCreateSubmit}>
              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>令牌名称 *</label>
                <input
                  type="text"
                  className="modal-input"
                  placeholder="例: Prometheus-Node-Exporter"
                  value={tokenName}
                  onChange={(e) => setTokenName(e.target.value)}
                  required
                  autoFocus
                />
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>令牌角色权限</label>
                <select
                  className="modal-input"
                  value={tokenRole}
                  onChange={(e) => setTokenRole(e.target.value)}
                  style={{ background: 'var(--input-bg, #27272a)' }}
                >
                  {isAdmin && <option value="admin">超级管理员 (Admin) - 全权调用所有控制面接口</option>}
                  <option value="operator">运维操作员 (Operator) - 可维护节点、触发网络测试与告警操作</option>
                  <option value="viewer">只读观察员 (Viewer) - 仅允许读取监控与指标数据，禁止任何写操作</option>
                </select>
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>有效期</label>
                <select
                  className="modal-input"
                  value={expiresInDays}
                  onChange={(e) => setExpiresInDays(Number(e.target.value))}
                  style={{ background: 'var(--input-bg, #27272a)' }}
                >
                  <option value={7}>7 天</option>
                  <option value={30}>30 天</option>
                  <option value={90}>90 天 (推荐)</option>
                  <option value={365}>1 年</option>
                  <option value={0}>永久有效 (不推荐长期服务无到期)</option>
                </select>
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>调用作用域 (Scopes)</label>
                <div style={{ display: 'flex', gap: '16px', marginBottom: '8px', fontSize: '13px' }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="scopeType"
                      checked={scopeType === 'all'}
                      onChange={() => setScopeType('all')}
                    />
                    全域权限 (*)
                  </label>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="scopeType"
                      checked={scopeType === 'custom'}
                      onChange={() => setScopeType('custom')}
                    />
                    指定权限细项
                  </label>
                </div>

                {scopeType === 'custom' && (
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px', padding: '8px', borderRadius: '6px', background: 'rgba(0,0,0,0.2)', border: '1px solid var(--border-color, rgba(255,255,255,0.08))' }}>
                    {[
                      { id: 'read:nodes', label: '读取探针与状态' },
                      { id: 'write:nodes', label: '修改与下线探针' },
                      { id: 'read:metrics', label: '读取延迟与历史指标' },
                      { id: 'read:alerts', label: '读取告警中枢' },
                      { id: 'write:alerts', label: '确认告警与规则' },
                      { id: 'write:backups', label: '触发灾备快照' },
                    ].map((sc) => (
                      <label key={sc.id} style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', cursor: 'pointer' }}>
                        <input
                          type="checkbox"
                          checked={customScopes.includes(sc.id)}
                          onChange={() => toggleCustomScope(sc.id)}
                        />
                        <span>{sc.label}</span>
                      </label>
                    ))}
                  </div>
                )}
              </div>

              <div style={{ marginBottom: '20px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>节点访问范围</label>
                <div style={{ display: 'flex', gap: '16px', marginBottom: '8px', fontSize: '13px' }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="nodeScopeType"
                      checked={nodeScopeType === 'all'}
                      onChange={() => setNodeScopeType('all')}
                    />
                    全部节点 (*)
                  </label>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="nodeScopeType"
                      checked={nodeScopeType === 'custom'}
                      onChange={() => setNodeScopeType('custom')}
                    />
                    限定特定节点
                  </label>
                </div>

                {nodeScopeType === 'custom' && (
                  <div style={{ maxHeight: '120px', overflowY: 'auto', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))', borderRadius: '6px', padding: '8px', background: 'rgba(0,0,0,0.2)' }}>
                    {nodes.length === 0 ? (
                      <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>暂无节点可选</div>
                    ) : (
                      nodes.map((n) => {
                        const uuid = n.uuid || n.id
                        return (
                          <label key={uuid} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '3px 0', fontSize: '12px', cursor: 'pointer' }}>
                            <input
                              type="checkbox"
                              checked={selectedNodes.includes(uuid)}
                              onChange={() => toggleNodeSelection(uuid)}
                            />
                            <span>{n.customName || n.name}</span>
                          </label>
                        )
                      })
                    )}
                  </div>
                )}
              </div>

              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={() => setShowCreateModal(false)}
                  disabled={actionBusy}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="button button-primary"
                  disabled={actionBusy}
                  style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
                >
                  {actionBusy ? <CircleNotch size={16} className="spin" /> : <Check size={16} />}
                  <span>生成令牌</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Show-Once Raw Token Modal */}
      {newRawTokenModal && (
        <div className="modal-overlay" onClick={() => setNewRawTokenModal(null)}>
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '520px', width: '90%', margin: 'auto', padding: '24px', borderRadius: '12px', background: 'var(--card-bg, #18181b)', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: '#10b981', marginBottom: '14px' }}>
              <CheckCircle size={22} weight="bold" />
              <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600', color: 'var(--text-primary)' }}>
                API 访问令牌生成成功！
              </h3>
            </div>

            <div style={{ padding: '12px 14px', borderRadius: '8px', background: 'rgba(234, 179, 8, 0.12)', border: '1px solid rgba(234, 179, 8, 0.25)', color: '#eab308', fontSize: '13px', lineHeight: '1.5', marginBottom: '16px' }}>
              <strong>安全注意：</strong> 出于最高安全准则，此明文令牌<strong>仅展示一次</strong>！一旦关闭当前窗口，系统数据库仅保留不可逆散列密文，将无法再次获取此明文。请立即复制并妥善保管。
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '6px' }}>
                令牌【{newRawTokenModal.name}】明文：
              </label>
              <div style={{ display: 'flex', gap: '8px' }}>
                <input
                  type="text"
                  readOnly
                  className="modal-input"
                  style={{ fontFamily: 'monospace', fontSize: '12px', background: 'rgba(0,0,0,0.4)', color: '#38bdf8' }}
                  value={newRawTokenModal.rawToken}
                />
                <button
                  type="button"
                  className="button button-primary"
                  onClick={() => copyToClipboard(newRawTokenModal.rawToken)}
                  style={{ display: 'flex', alignItems: 'center', gap: '6px', whiteSpace: 'nowrap' }}
                >
                  {copied ? <Check size={16} /> : <Copy size={16} />}
                  <span>{copied ? '已复制' : '复制'}</span>
                </button>
              </div>
            </div>

            <div style={{ background: 'rgba(255,255,255,0.03)', padding: '10px 12px', borderRadius: '6px', fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '20px' }}>
              <div style={{ fontWeight: '500', marginBottom: '4px', color: 'var(--text-primary)' }}>快速调用示例 (cURL)：</div>
              <code style={{ wordBreak: 'break-all', display: 'block', color: '#94a3b8' }}>
                curl -H "Authorization: Bearer {newRawTokenModal.rawToken}" {window.location.origin}/api/me
              </code>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
              <button
                type="button"
                className="button button-primary"
                onClick={() => setNewRawTokenModal(null)}
              >
                我已安全保存，关闭弹窗
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deleteConfirmToken && (
        <div className="modal-overlay" onClick={() => !actionBusy && setDeleteConfirmToken(null)}>
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '420px', width: '90%', margin: 'auto', padding: '24px', borderRadius: '12px', background: 'var(--card-bg, #18181b)', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '14px', color: '#ef4444' }}>
              <WarningCircle size={24} weight="bold" />
              <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600', color: 'var(--text-primary)' }}>确认撤销 API 令牌？</h3>
            </div>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', lineHeight: '1.5', margin: '0 0 20px 0' }}>
              您确定要彻底删除令牌【<strong>{deleteConfirmToken.name}</strong>】吗？撤销后，依赖此令牌的所有外部自动化脚本与集成将立即被拒绝访问。此操作不可逆。
            </p>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setDeleteConfirmToken(null)}
                disabled={actionBusy}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-primary"
                onClick={handleDeleteSubmit}
                disabled={actionBusy}
                style={{ background: '#ef4444', borderColor: '#ef4444', display: 'flex', alignItems: 'center', gap: '6px' }}
              >
                {actionBusy ? <CircleNotch size={16} className="spin" /> : <Trash size={16} />}
                <span>确认撤销</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
