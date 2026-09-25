import { useEffect, useState, useMemo } from 'react'
import {
  Users,
  UserPlus,
  ShieldCheck,
  Eye,
  SlidersHorizontal,
  PencilSimple,
  Trash,
  CheckCircle,
  XCircle,
  WarningCircle,
  ArrowClockwise,
  CircleNotch,
  Key,
  HardDrives,
  User,
  X,
  Check,
} from '@phosphor-icons/react'
import { fetchUsers, createUser, updateUser, deleteUser } from '../lib/api.js'
import { safeArray, safeText, formatTimeOfDay } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function UserManagementCard({ currentUser, nodes = [] }) {
  const [users, setUsers] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionBusy, setActionBusy] = useState(false)
  const [statusMsg, setStatusMsg] = useState(null) // { kind: 'success' | 'error', text: '' }

  // Modals state
  const [showAddModal, setShowAddModal] = useState(false)
  const [editUser, setEditUser] = useState(null)
  const [deleteConfirmUser, setDeleteConfirmUser] = useState(null)

  // Form states for Add Member
  const [newUsername, setNewUsername] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newDisplayName, setNewDisplayName] = useState('')
  const [newRole, setNewRole] = useState('operator')
  const [newNodeScopeType, setNewNodeScopeType] = useState('all') // 'all' | 'custom'
  const [newSelectedNodes, setNewSelectedNodes] = useState([])

  // Form states for Edit Member
  const [editDisplayName, setEditDisplayName] = useState('')
  const [editRole, setEditRole] = useState('operator')
  const [editPassword, setEditPassword] = useState('')
  const [editDisabled, setEditDisabled] = useState(false)
  const [editNodeScopeType, setEditNodeScopeType] = useState('all')
  const [editSelectedNodes, setEditSelectedNodes] = useState([])

  const isAdmin = currentUser?.is_admin || currentUser?.role === 'admin'

  const loadUsers = async () => {
    setLoading(true)
    setError('')
    try {
      const data = await fetchUsers()
      if (Array.isArray(data)) {
        setUsers(data)
      } else if (Array.isArray(data?.users)) {
        setUsers(data.users)
      } else {
        setUsers([])
      }
    } catch (err) {
      setError(err?.message || '无法获取团队成员列表')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadUsers()
  }, [])

  // Count active admins to prevent deleting/demoting last admin
  const activeAdminsCount = useMemo(() => {
    return users.filter((u) => u.role === 'admin' && !u.disabled).length
  }, [users])

  const openAddModal = () => {
    setNewUsername('')
    setNewPassword('')
    setNewDisplayName('')
    setNewRole('operator')
    setNewNodeScopeType('all')
    setNewSelectedNodes([])
    setStatusMsg(null)
    setShowAddModal(true)
  }

  const openEditModal = (u) => {
    setEditUser(u)
    setEditDisplayName(u.display_name || '')
    setEditRole(u.role || 'operator')
    setEditPassword('')
    setEditDisabled(!!u.disabled)
    if (!u.allowed_nodes || u.allowed_nodes === '*' || u.allowed_nodes === '') {
      setEditNodeScopeType('all')
      setEditSelectedNodes([])
    } else {
      setEditNodeScopeType('custom')
      const arr = u.allowed_nodes.split(',').map((s) => s.trim()).filter(Boolean)
      setEditSelectedNodes(arr)
    }
    setStatusMsg(null)
  }

  const handleCreateSubmit = async (e) => {
    e.preventDefault()
    if (!newUsername.trim() || !newPassword) {
      setStatusMsg({ kind: 'error', text: '请填写登录用户名与初始登录密码' })
      return
    }
    if (newPassword.length < 6) {
      setStatusMsg({ kind: 'error', text: '密码长度不能少于 6 位' })
      return
    }

    const allowedNodes = newNodeScopeType === 'all' ? '*' : newSelectedNodes.join(',')
    setActionBusy(true)
    setStatusMsg(null)
    try {
      await createUser({
        login: newUsername.trim(),
        username: newUsername.trim(),
        password: newPassword,
        display_name: newDisplayName.trim(),
        role: newRole,
        allowed_nodes: allowedNodes,
      })
      setStatusMsg({ kind: 'success', text: `成员【${newUsername.trim()}】创建成功！` })
      setShowAddModal(false)
      loadUsers()
    } catch (err) {
      setStatusMsg({ kind: 'error', text: err?.message || '创建用户失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const handleEditSubmit = async (e) => {
    e.preventDefault()
    if (!editUser) return

    // If editing self or last admin
    if (editUser.role === 'admin' && editRole !== 'admin' && activeAdminsCount <= 1) {
      setStatusMsg({ kind: 'error', text: '不可降级系统中唯一的超级管理员' })
      return
    }
    if (editUser.role === 'admin' && editDisabled && activeAdminsCount <= 1) {
      setStatusMsg({ kind: 'error', text: '不可停用系统中唯一的超级管理员' })
      return
    }

    const allowedNodes = editNodeScopeType === 'all' ? '*' : editSelectedNodes.join(',')
    setActionBusy(true)
    setStatusMsg(null)
    try {
      const payload = {
        role: editRole,
        display_name: editDisplayName.trim(),
        allowed_nodes: allowedNodes,
        disabled: editDisabled,
      }
      if (editPassword) {
        if (editPassword.length < 6) {
          setStatusMsg({ kind: 'error', text: '新密码长度不能少于 6 位' })
          setActionBusy(false)
          return
        }
        payload.password = editPassword
      }
      await updateUser(editUser.id, payload)
      setStatusMsg({ kind: 'success', text: `成员【${editUser.provider_user_id || editUser.display_name}】权限已更新！` })
      setEditUser(null)
      loadUsers()
    } catch (err) {
      setStatusMsg({ kind: 'error', text: err?.message || '修改用户失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const handleDeleteSubmit = async () => {
    if (!deleteConfirmUser) return
    if (deleteConfirmUser.role === 'admin' && activeAdminsCount <= 1) {
      setStatusMsg({ kind: 'error', text: '不可删除系统中唯一的有效超级管理员' })
      setDeleteConfirmUser(null)
      return
    }

    setActionBusy(true)
    setStatusMsg(null)
    try {
      await deleteUser(deleteConfirmUser.id)
      setStatusMsg({ kind: 'success', text: `成员【${deleteConfirmUser.login || deleteConfirmUser.provider_user_id}】已成功移除` })
      setDeleteConfirmUser(null)
      loadUsers()
    } catch (err) {
      setStatusMsg({ kind: 'error', text: err?.message || '删除用户失败' })
    } finally {
      setActionBusy(false)
    }
  }

  const toggleNodeSelection = (uuid, isEdit = false) => {
    if (isEdit) {
      setEditSelectedNodes((prev) =>
        prev.includes(uuid) ? prev.filter((id) => id !== uuid) : [...prev, uuid]
      )
    } else {
      setNewSelectedNodes((prev) =>
        prev.includes(uuid) ? prev.filter((id) => id !== uuid) : [...prev, uuid]
      )
    }
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
    <div className="card rbac-card" style={{ padding: '24px', borderRadius: '12px' }}>
      {/* Card Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: '16px', marginBottom: '20px' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            <div style={{ width: '36px', height: '36px', borderRadius: '8px', background: 'rgba(99, 102, 241, 0.12)', color: '#6366f1', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Users size={22} weight="bold" />
            </div>
            <div>
              <h2 style={{ fontSize: '18px', fontWeight: '600', margin: 0 }}>多租户团队成员与权限协作 (RBAC)</h2>
              <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-secondary, #94a3b8)' }}>
                细粒度管理团队成员协作权限：超级管理员 (全权限)、运维操作员 (节点/告警维护)、只读观察员 (监控总览) 及指定节点访问白名单。
              </p>
            </div>
          </div>
        </div>

        <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
          <button
            type="button"
            className="button button-quiet"
            onClick={loadUsers}
            disabled={loading}
            title="刷新团队列表"
          >
            <ArrowClockwise size={16} className={loading ? 'spin' : ''} />
            <span>刷新</span>
          </button>

          {isAdmin && (
            <button
              type="button"
              className="button button-primary"
              onClick={openAddModal}
              style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
            >
              <UserPlus size={16} weight="bold" />
              <span>添加团队成员</span>
            </button>
          )}
        </div>
      </div>

      {/* Status Notifications */}
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

      {/* Current User Role Notice */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '10px 16px',
          background: 'var(--bg-secondary, rgba(255, 255, 255, 0.03))',
          borderRadius: '8px',
          border: '1px solid var(--border-color, rgba(255, 255, 255, 0.08))',
          marginBottom: '20px',
          fontSize: '13px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <User size={16} style={{ color: 'var(--text-secondary)' }} />
          <span>当前登录身份：<strong>{currentUser?.display_name || currentUser?.login || '本地管理员'}</strong></span>
          {renderRoleBadge(currentUser?.role || (isAdmin ? 'admin' : 'operator'))}
        </div>
        <div style={{ color: 'var(--text-secondary)', fontSize: '12px' }}>
          {currentUser?.allowed_nodes && currentUser.allowed_nodes !== '*' ? (
            <span>限定访问 {currentUser.allowed_nodes.split(',').length} 个指定节点</span>
          ) : (
            <span>可全域访问所有探针节点 (*)</span>
          )}
        </div>
      </div>

      {/* Users Table */}
      {loading && users.length === 0 ? (
        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', padding: '40px' }}>
          <CircleNotch size={24} className="spin" style={{ color: 'var(--accent-color, #6366f1)' }} />
          <span style={{ marginLeft: '10px', fontSize: '14px', color: 'var(--text-secondary)' }}>正在载入团队成员列表…</span>
        </div>
      ) : error ? (
        <EmptyState
          icon={WarningCircle}
          title="获取团队成员失败"
          description={error}
          actionText="重新加载"
          onAction={loadUsers}
        />
      ) : users.length === 0 ? (
        <EmptyState
          icon={Users}
          title="暂无独立团队成员"
          description="当前系统仅由根管理员 (PROBEWATCH_ADMIN_PASSWORD) 维护。可点击右上角添加运维或只读成员。"
          actionText={isAdmin ? "添加第一个团队成员" : undefined}
          onAction={isAdmin ? openAddModal : undefined}
        />
      ) : (
        <div className="table-responsive" style={{ overflowX: 'auto' }}>
          <table className="table" style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color, rgba(255, 255, 255, 0.08))', color: 'var(--text-secondary)' }}>
                <th style={{ padding: '10px 14px' }}>账号 / 昵称</th>
                <th style={{ padding: '10px 14px' }}>角色权限</th>
                <th style={{ padding: '10px 14px' }}>节点作用域</th>
                <th style={{ padding: '10px 14px' }}>状态</th>
                <th style={{ padding: '10px 14px' }}>创建时间</th>
                {isAdmin && <th style={{ padding: '10px 14px', textAlign: 'right' }}>操作</th>}
              </tr>
            </thead>
            <tbody>
              {users.map((u) => {
                const username = u.login || u.provider_user_id
                const isSelf = currentUser && (currentUser.user_id === u.id || currentUser.id === u.id || currentUser.login === username)
                const isOnlyAdmin = u.role === 'admin' && activeAdminsCount <= 1
                return (
                  <tr
                    key={u.id}
                    style={{
                      borderBottom: '1px solid var(--border-color, rgba(255, 255, 255, 0.05))',
                      opacity: u.disabled ? 0.6 : 1,
                    }}
                  >
                    <td style={{ padding: '12px 14px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <div
                          style={{
                            width: '28px',
                            height: '28px',
                            borderRadius: '50%',
                            background: u.role === 'admin' ? 'rgba(168, 85, 247, 0.2)' : 'rgba(59, 130, 246, 0.2)',
                            color: u.role === 'admin' ? '#a855f7' : '#3b82f6',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            fontWeight: '600',
                            fontSize: '12px',
                          }}
                        >
                          {(u.display_name || username || 'U').slice(0, 1).toUpperCase()}
                        </div>
                        <div>
                          <div style={{ fontWeight: '500' }}>
                            {username}
                            {isSelf && <span style={{ marginLeft: '6px', fontSize: '11px', color: '#10b981' }}>(您)</span>}
                          </div>
                          {u.display_name && (
                            <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>{u.display_name}</div>
                          )}
                        </div>
                      </div>
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {renderRoleBadge(u.role)}
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {!u.allowed_nodes || u.allowed_nodes === '*' ? (
                        <span style={{ color: 'var(--text-secondary)' }}>全部节点 (*)</span>
                      ) : (
                        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', maxWidth: '240px' }}>
                          {u.allowed_nodes.split(',').map((id) => (
                            <span
                              key={id}
                              style={{
                                fontSize: '11px',
                                background: 'var(--bg-secondary, rgba(255, 255, 255, 0.06))',
                                padding: '2px 6px',
                                borderRadius: '4px',
                                border: '1px solid var(--border-color, rgba(255, 255, 255, 0.08))',
                              }}
                              title={id}
                            >
                              {getNodeName(id)}
                            </span>
                          ))}
                        </div>
                      )}
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      {u.disabled ? (
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', color: '#ef4444', fontSize: '12px' }}>
                          <XCircle size={14} weight="fill" />
                          已停用
                        </span>
                      ) : (
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', color: '#10b981', fontSize: '12px' }}>
                          <CheckCircle size={14} weight="fill" />
                          正常
                        </span>
                      )}
                    </td>
                    <td style={{ padding: '12px 14px', color: 'var(--text-secondary)', fontSize: '12px' }}>
                      {u.created_at ? new Date(u.created_at).toLocaleDateString() : '—'}
                    </td>
                    {isAdmin && (
                      <td style={{ padding: '12px 14px', textAlign: 'right' }}>
                        <div style={{ display: 'inline-flex', gap: '8px', alignItems: 'center' }}>
                          <button
                            type="button"
                            className="button button-quiet btn-sm"
                            onClick={() => openEditModal(u)}
                            title="编辑权限与修改密码"
                            style={{ padding: '4px 8px' }}
                          >
                            <PencilSimple size={14} />
                            <span>编辑</span>
                          </button>
                          <button
                            type="button"
                            className="button button-quiet btn-sm text-rose"
                            onClick={() => setDeleteConfirmUser(u)}
                            disabled={isOnlyAdmin}
                            title={isOnlyAdmin ? '唯一管理员不可删除' : '删除成员'}
                            style={{ padding: '4px 8px', opacity: isOnlyAdmin ? 0.4 : 1 }}
                          >
                            <Trash size={14} />
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
      )}

      {/* Add Member Modal */}
      {showAddModal && (
        <div className="modal-overlay" onClick={() => !actionBusy && setShowAddModal(false)}>
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '480px', width: '90%', margin: 'auto', padding: '24px', borderRadius: '12px', background: 'var(--card-bg, #18181b)', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <UserPlus size={20} style={{ color: '#6366f1' }} />
                <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600' }}>添加团队成员</h3>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setShowAddModal(false)}
                disabled={actionBusy}
              >
                <X size={16} />
              </button>
            </div>

            <form onSubmit={handleCreateSubmit}>
              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>登录账号 / 用户名 *</label>
                <input
                  type="text"
                  className="modal-input"
                  placeholder="例: devops_tom (字母、数字、下划线)"
                  value={newUsername}
                  onChange={(e) => setNewUsername(e.target.value)}
                  required
                  autoFocus
                />
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>初始登录密码 * (至少 6 位)</label>
                <input
                  type="password"
                  className="modal-input"
                  placeholder="请输入初始强密码"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  required
                />
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>显示昵称 (可选)</label>
                <input
                  type="text"
                  className="modal-input"
                  placeholder="例: 广州机房运维-小张"
                  value={newDisplayName}
                  onChange={(e) => setNewDisplayName(e.target.value)}
                />
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>协作角色权限 *</label>
                <select
                  className="modal-input"
                  value={newRole}
                  onChange={(e) => setNewRole(e.target.value)}
                  style={{ background: 'var(--input-bg, #27272a)' }}
                >
                  <option value="operator">运维操作员 (Operator) - 可管理维护节点、设置告警与测试网络，无权管理团队用户</option>
                  <option value="viewer">只读观察员 (Viewer) - 仅可查看监控大屏、路由跟踪与矩阵状态，禁止写操作</option>
                  <option value="admin">超级管理员 (Admin) - 完整控制权限，可增删团队用户与全局灾备配置</option>
                </select>
              </div>

              <div style={{ marginBottom: '20px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>探针节点访问白名单</label>
                <div style={{ display: 'flex', gap: '16px', marginBottom: '8px', fontSize: '13px' }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="newNodeScope"
                      checked={newNodeScopeType === 'all'}
                      onChange={() => setNewNodeScopeType('all')}
                    />
                    全部节点 (*)
                  </label>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="newNodeScope"
                      checked={newNodeScopeType === 'custom'}
                      onChange={() => setNewNodeScopeType('custom')}
                    />
                    限制特定节点
                  </label>
                </div>

                {newNodeScopeType === 'custom' && (
                  <div style={{ maxHeight: '140px', overflowY: 'auto', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))', borderRadius: '6px', padding: '8px', background: 'rgba(0,0,0,0.2)' }}>
                    {nodes.length === 0 ? (
                      <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>暂无可选择的探针节点</div>
                    ) : (
                      nodes.map((n) => {
                        const uuid = n.uuid || n.id
                        const checked = newSelectedNodes.includes(uuid)
                        return (
                          <label key={uuid} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '4px 0', fontSize: '12px', cursor: 'pointer' }}>
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={() => toggleNodeSelection(uuid, false)}
                            />
                            <span>{n.customName || n.name}</span>
                            <span style={{ color: 'var(--text-secondary)', fontSize: '11px' }}>({uuid.slice(0, 8)})</span>
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
                  onClick={() => setShowAddModal(false)}
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
                  <span>确认添加</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Edit Member Modal */}
      {editUser && (
        <div className="modal-overlay" onClick={() => !actionBusy && setEditUser(null)}>
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '480px', width: '90%', margin: 'auto', padding: '24px', borderRadius: '12px', background: 'var(--card-bg, #18181b)', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <PencilSimple size={20} style={{ color: '#3b82f6' }} />
                <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600' }}>
                  编辑成员：{editUser.login || editUser.provider_user_id}
                </h3>
              </div>
              <button
                type="button"
                className="icon-button"
                onClick={() => setEditUser(null)}
                disabled={actionBusy}
              >
                <X size={16} />
              </button>
            </div>

            <form onSubmit={handleEditSubmit}>
              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>显示昵称</label>
                <input
                  type="text"
                  className="modal-input"
                  placeholder="例: 香港机房技术支持"
                  value={editDisplayName}
                  onChange={(e) => setEditDisplayName(e.target.value)}
                />
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>协作角色权限</label>
                <select
                  className="modal-input"
                  value={editRole}
                  onChange={(e) => setEditRole(e.target.value)}
                  style={{ background: 'var(--input-bg, #27272a)' }}
                >
                  <option value="operator">运维操作员 (Operator) - 节点/告警维护，无权管理团队用户</option>
                  <option value="viewer">只读观察员 (Viewer) - 监控只读总览，无写权限</option>
                  <option value="admin">超级管理员 (Admin) - 全局控制与用户管理</option>
                </select>
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>重置密码 (留空则保持原密码不变)</label>
                <input
                  type="password"
                  className="modal-input"
                  placeholder="输入新密码 (至少 6 位)"
                  value={editPassword}
                  onChange={(e) => setEditPassword(e.target.value)}
                />
              </div>

              <div style={{ marginBottom: '14px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '500', marginBottom: '6px', color: 'var(--text-secondary)' }}>探针节点访问白名单</label>
                <div style={{ display: 'flex', gap: '16px', marginBottom: '8px', fontSize: '13px' }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="editNodeScope"
                      checked={editNodeScopeType === 'all'}
                      onChange={() => setEditNodeScopeType('all')}
                    />
                    全部节点 (*)
                  </label>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer' }}>
                    <input
                      type="radio"
                      name="editNodeScope"
                      checked={editNodeScopeType === 'custom'}
                      onChange={() => setEditNodeScopeType('custom')}
                    />
                    限制特定节点
                  </label>
                </div>

                {editNodeScopeType === 'custom' && (
                  <div style={{ maxHeight: '140px', overflowY: 'auto', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))', borderRadius: '6px', padding: '8px', background: 'rgba(0,0,0,0.2)' }}>
                    {nodes.length === 0 ? (
                      <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>暂无可选择的探针节点</div>
                    ) : (
                      nodes.map((n) => {
                        const uuid = n.uuid || n.id
                        const checked = editSelectedNodes.includes(uuid)
                        return (
                          <label key={uuid} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '4px 0', fontSize: '12px', cursor: 'pointer' }}>
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={() => toggleNodeSelection(uuid, true)}
                            />
                            <span>{n.customName || n.name}</span>
                            <span style={{ color: 'var(--text-secondary)', fontSize: '11px' }}>({uuid.slice(0, 8)})</span>
                          </label>
                        )
                      })
                    )}
                  </div>
                )}
              </div>

              <div style={{ marginBottom: '20px', padding: '10px 12px', borderRadius: '6px', background: 'rgba(255, 255, 255, 0.03)' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '13px' }}>
                  <input
                    type="checkbox"
                    checked={editDisabled}
                    onChange={(e) => setEditDisabled(e.target.checked)}
                  />
                  <span>停用此成员账号 (停用后将无法登录系统)</span>
                </label>
              </div>

              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={() => setEditUser(null)}
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
                  <span>保存变更</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deleteConfirmUser && (
        <div className="modal-overlay" onClick={() => !actionBusy && setDeleteConfirmUser(null)}>
          <div className="card" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '420px', width: '90%', margin: 'auto', padding: '24px', borderRadius: '12px', background: 'var(--card-bg, #18181b)', border: '1px solid var(--border-color, rgba(255, 255, 255, 0.1))' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '14px', color: '#ef4444' }}>
              <WarningCircle size={24} weight="bold" />
              <h3 style={{ margin: 0, fontSize: '16px', fontWeight: '600', color: 'var(--text-primary)' }}>确认删除团队成员？</h3>
            </div>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', lineHeight: '1.5', margin: '0 0 20px 0' }}>
              您确定要彻底删除成员【<strong>{deleteConfirmUser.login || deleteConfirmUser.provider_user_id}</strong>】({deleteConfirmUser.display_name || deleteConfirmUser.role}) 吗？
              删除后该成员将立即失去所有访问权限。此操作不可逆。
            </p>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setDeleteConfirmUser(null)}
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
                <span>确认删除</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
