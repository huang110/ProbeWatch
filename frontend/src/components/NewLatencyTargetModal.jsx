import { useEffect, useMemo, useState } from 'react'
import {
  CaretDown,
  CaretRight,
  Check,
  CircleNotch,
  MagnifyingGlass,
  Plus,
  WifiHigh,
  X,
} from '@phosphor-icons/react'
import { fetchCsrfToken } from '../lib/api.js'
import { detectRegionAndFlag } from '../lib/format.js'

const PROTOCOL_OPTIONS = [
  { value: 'icmp', label: 'ICMP' },
  { value: 'tcp', label: 'TCP' },
  { value: 'http', label: 'HTTP' },
  { value: 'https', label: 'HTTPS' },
  { value: 'dns', label: 'DNS' },
]

export function NewLatencyTargetModal({
  isOpen,
  onClose,
  onCreated,
  initialData = null,
  nodes = [],
}) {
  const isEdit = Boolean(initialData)

  const [name, setName] = useState('')
  const [kind, setKind] = useState('icmp')
  const [targetInput, setTargetInput] = useState('')
  const [port, setPort] = useState('80')
  const [path, setPath] = useState('/')
  const [dnsType, setDnsType] = useState('A')
  const [nameserver, setNameserver] = useState('')
  const [expectedStatus, setExpectedStatus] = useState('')
  const [keyword, setKeyword] = useState('')
  const [checkTls, setCheckTls] = useState(false)
  const [ipMode, setIpMode] = useState('auto') // 'auto' | 'ipv4' | 'ipv6'
  const [intervalSeconds, setIntervalSeconds] = useState(60)
  const [timeoutMs, setTimeoutMs] = useState(3000)
  const [enabled, setEnabled] = useState(true)

  // 服务器选择状态
  const [serverList, setServerList] = useState([])
  const [selectedServers, setSelectedServers] = useState('ALL') // 'ALL' or Set of node UUIDs
  const [showServerPicker, setShowServerPicker] = useState(false)
  const [serverSearch, setServerSearch] = useState('')
  const [tempSelectedServers, setTempSelectedServers] = useState('ALL')
  const [nodeTagsInput, setNodeTagsInput] = useState('')

  // 高级选项折叠
  const [showAdvanced, setShowAdvanced] = useState(false)

  const [loading, setLoading] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')

  // 从 serverList 提取可用的标签列表
  const availableTags = useMemo(() => {
    const set = new Set()
    serverList.forEach((s) => {
      if (Array.isArray(s.tags)) {
        s.tags.forEach((t) => {
          const clean = String(t || '').trim()
          if (clean) set.add(clean)
        })
      } else if (typeof s.tags === 'string' && s.tags.trim()) {
        s.tags.split(/[,\s]+/).forEach((t) => {
          const clean = t.trim()
          if (clean) set.add(clean)
        })
      }
    })
    return Array.from(set)
  }, [serverList])

  // 解析当前输入的标签数组
  const parsedTags = useMemo(() => {
    return nodeTagsInput
      .split(/[,\s]+/)
      .map((t) => t.trim().toLowerCase())
      .filter(Boolean)
  }, [nodeTagsInput])

  const toggleTag = (tag) => {
    const cleanTag = tag.trim().toLowerCase()
    const set = new Set(parsedTags)
    if (set.has(cleanTag)) {
      set.delete(cleanTag)
    } else {
      set.add(cleanTag)
    }
    setNodeTagsInput(Array.from(set).join(', '))
  }

  // 初始化节点列表
  useEffect(() => {
    if (nodes && nodes.length > 0) {
      setServerList(nodes)
    } else if (isOpen) {
      fetch('/api/nodes', { credentials: 'same-origin' })
        .then((r) => (r.ok ? r.json() : []))
        .then((data) => {
          if (Array.isArray(data)) setServerList(data)
        })
        .catch(() => {})
    }
  }, [nodes, isOpen])

  // ESC 键关闭
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape' && isOpen) {
        if (showServerPicker) {
          handleCancelServerPicker()
        } else {
          onClose()
        }
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, showServerPicker, onClose, selectedServers])

  // 当传入编辑数据时填充表单
  useEffect(() => {
    if (initialData) {
      setName(initialData.name || '')
      setKind(initialData.kind || 'icmp')
      setTargetInput(initialData.host || '')
      setPort(String(initialData.port || (initialData.kind === 'https' ? 443 : initialData.kind === 'dns' ? 53 : 80)))
      setPath(initialData.path || '/')
      setDnsType(initialData.dns_type || 'A')
      setNameserver(initialData.nameserver || '')
      setIntervalSeconds(initialData.interval_seconds || 60)
      setTimeoutMs(initialData.timeout_ms || 3000)
      setExpectedStatus(initialData.expected_status ? String(initialData.expected_status) : '')
      setKeyword(initialData.keyword || '')
      setCheckTls(Boolean(initialData.check_tls))
      setEnabled(initialData.enabled !== false)

      if (Array.isArray(initialData.node_tags) && initialData.node_tags.length > 0) {
        setNodeTagsInput(initialData.node_tags.join(', '))
      } else {
        setNodeTagsInput('')
      }

      if (Array.isArray(initialData.node_ids) && initialData.node_ids.length > 0) {
        setSelectedServers(new Set(initialData.node_ids))
      } else {
        setSelectedServers('ALL')
      }
    } else {
      setName('')
      setKind('icmp')
      setTargetInput('')
      setPort('80')
      setPath('/')
      setDnsType('A')
      setNameserver('')
      setIntervalSeconds(60)
      setTimeoutMs(3000)
      setExpectedStatus('')
      setKeyword('')
      setCheckTls(false)
      setIpMode('auto')
      setEnabled(true)
      setSelectedServers('ALL')
      setNodeTagsInput('')
      setShowAdvanced(false)
    }
    setErrorMsg('')
  }, [initialData, isOpen])

  // 协议切换智能重设默认值
  const handleKindChange = (newKind) => {
    setKind(newKind)
    if (newKind === 'https') setPort('443')
    else if (newKind === 'dns') setPort('53')
    else if (newKind === 'http' || newKind === 'tcp') setPort('80')
    else setPort('0')
  }

  // 智能目标输入解析：自动推导 host, port, path
  const handleTargetInputChange = (val) => {
    setTargetInput(val)
    const trimmed = val.trim()
    if (!trimmed) return

    // 如果粘贴完整 URL: http://host:port/path 或 https://host:port/path
    if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
      try {
        const parsed = new URL(trimmed)
        setKind(parsed.protocol === 'https:' ? 'https' : 'http')
        setPort(parsed.port || (parsed.protocol === 'https:' ? '443' : '80'))
        if (parsed.pathname && parsed.pathname !== '/') setPath(parsed.pathname)
      } catch {}
    } else if (trimmed.includes(':') && !trimmed.includes('::')) {
      // 类似 host:port (排除 IPv6)
      const parts = trimmed.split(':')
      if (parts.length === 2 && /^\d+$/.test(parts[1])) {
        setPort(parts[1])
      }
    }
  }

  // 计算提取出来的 Clean Host
  const parseCleanHost = (input) => {
    let clean = input.trim()
    if (clean.startsWith('http://')) clean = clean.slice(7)
    if (clean.startsWith('https://')) clean = clean.slice(8)
    const slashIdx = clean.indexOf('/')
    if (slashIdx !== -1) clean = clean.slice(0, slashIdx)
    const colonIdx = clean.lastIndexOf(':')
    if (colonIdx !== -1 && !clean.includes(']')) {
      // 不是带括号的 IPv6 [::1]:80
      clean = clean.slice(0, colonIdx)
    }
    return clean
  }

  // 打开服务器选择器时同步当前选择
  const handleOpenServerPicker = () => {
    setTempSelectedServers(
      selectedServers === 'ALL'
        ? 'ALL'
        : new Set(selectedServers)
    )
    setServerSearch('')
    setShowServerPicker(true)
  }

  // 过滤服务器搜索列表
  const filteredServers = useMemo(() => {
    if (!serverSearch.trim()) return serverList
    const q = serverSearch.trim().toLowerCase()
    return serverList.filter(
      (s) =>
        (s.name && s.name.toLowerCase().includes(q)) ||
        (s.hostname && s.hostname.toLowerCase().includes(q)) ||
        (s.uuid && s.uuid.toLowerCase().includes(q)) ||
        (s.tags && s.tags.toLowerCase().includes(q)) ||
        (s.ip && s.ip.toLowerCase().includes(q)) ||
        (s.region && s.region.toLowerCase().includes(q))
    )
  }, [serverList, serverSearch])

  // 服务器选择文本标签
  const serverSelectionLabel = useMemo(() => {
    if (selectedServers === 'ALL') return '全部服务器'
    const count = selectedServers.size
    if (count === 0) return '0选择'
    if (count === serverList.length && serverList.length > 0) return '全部服务器'
    return `${count}已选择`
  }, [selectedServers, serverList])

  const tempSelectedCount = useMemo(() => {
    if (tempSelectedServers === 'ALL') return serverList.length
    return tempSelectedServers.size
  }, [tempSelectedServers, serverList])

  const toggleTempServer = (uuid) => {
    const cur =
      tempSelectedServers === 'ALL'
        ? new Set(serverList.map((s) => s.uuid || s.id))
        : new Set(tempSelectedServers)
    if (cur.has(uuid)) {
      cur.delete(uuid)
    } else {
      cur.add(uuid)
    }
    setTempSelectedServers(cur)
  }

  const handleSelectAll = () => {
    if (tempSelectedCount === serverList.length) {
      setTempSelectedServers(new Set())
    } else {
      setTempSelectedServers('ALL')
    }
  }

  const handleApplyServerPicker = () => {
    if (tempSelectedServers === 'ALL') {
      setSelectedServers('ALL')
    } else {
      setSelectedServers(new Set(tempSelectedServers))
    }
    setShowServerPicker(false)
  }

  const handleCancelServerPicker = () => {
    setTempSelectedServers(
      selectedServers === 'ALL' ? 'ALL' : new Set(selectedServers)
    )
    setServerSearch('')
    setShowServerPicker(false)
  }

  // 提交处理
  const handleSubmit = async (e) => {
    e.preventDefault()
    setErrorMsg('')

    const trimmedName = name.trim()
    const trimmedTarget = targetInput.trim()
    const cleanHost = parseCleanHost(trimmedTarget)

    if (!trimmedName) {
      setErrorMsg('请填写监测任务名称')
      return
    }
    if (trimmedName.length > 128) {
      setErrorMsg('任务名称不能超过 128 个字符')
      return
    }
    if (!cleanHost) {
      setErrorMsg('请填写监测目标（IP 或域名）')
      return
    }
    if (cleanHost.length > 253) {
      setErrorMsg('目标主机地址不能超过 253 个字符')
      return
    }

    // 校验端口
    if (kind !== 'icmp') {
      const portNum = Number(port)
      if (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) {
        setErrorMsg('端口必须为 1 到 65535 之间的有效整数')
        return
      }
    }

    // HTTP / HTTPS 路径与期望码校验
    if (kind === 'http' || kind === 'https') {
      if (!path || !path.startsWith('/')) {
        setErrorMsg('HTTP/HTTPS 探测路径必须以 / 开头')
        return
      }
      if (expectedStatus.trim()) {
        const code = Number(expectedStatus)
        if (!Number.isInteger(code) || code < 100 || code > 599) {
          setErrorMsg('期望状态码需为 100 到 599 之间的有效数值')
          return
        }
      }
    }

    // DNS 类型校验
    if (kind === 'dns' && !['A', 'AAAA', 'CNAME'].includes(dnsType)) {
      setErrorMsg('DNS 记录类型必须为 A、AAAA 或 CNAME')
      return
    }

    // 周期与超时校验
    const intervalNum = Number(intervalSeconds) || 60
    if (intervalNum < 10 || intervalNum > 86400) {
      setErrorMsg('探测周期需在 10 到 86400 秒之间')
      return
    }
    const timeoutNum = Number(timeoutMs) || 3000
    if (timeoutNum < 100 || timeoutNum > 30000) {
      setErrorMsg('超时时间需在 100 到 30000 毫秒之间')
      return
    }

    // 服务器关联校验与后端真正字段绑定
    let finalNodeIds = []
    if (selectedServers === 'ALL') {
      // 全部服务器时提交空数组，后端存储并在 agentConfig 匹配全部节点
      finalNodeIds = []
    } else {
      finalNodeIds = Array.from(selectedServers)
      if (finalNodeIds.length === 0) {
        setErrorMsg('请至少选择一台关联服务器，或选择全部服务器。')
        return
      }
    }

    setLoading(true)
    try {
      const csrfToken = await fetchCsrfToken()
      const targetId = isEdit ? initialData.id : ''

      const payload = {
        name: trimmedName,
        kind,
        host: cleanHost,
        interval_seconds: intervalNum,
        timeout_ms: timeoutNum,
        enabled: Boolean(enabled),
        node_ids: finalNodeIds,
      }

      if (kind === 'tcp' || kind === 'http' || kind === 'https' || kind === 'dns') {
        payload.port = Number(port) || (kind === 'https' ? 443 : kind === 'dns' ? 53 : 80)
      }
      if (kind === 'http' || kind === 'https') {
        payload.path = path.startsWith('/') ? path : `/${path}`
        if (expectedStatus.trim()) {
          payload.expected_status = Number(expectedStatus)
        }
        if (keyword.trim()) {
          payload.keyword = keyword.trim()
        }
      }
      if (kind === 'https' || kind === 'tcp') {
        if (checkTls) {
          payload.check_tls = true
        }
      }
      if (kind === 'dns') {
        payload.dns_type = dnsType
        if (nameserver.trim()) {
          payload.nameserver = nameserver.trim()
        }
      }
      // 提交 node_tags: 若用户配置了标签则下发，编辑时若清空标签则置空
      if (parsedTags.length > 0) {
        payload.node_tags = parsedTags
      } else if (isEdit) {
        payload.node_tags = []
      }
      // 保持已有任务的 region_rules
      if (Array.isArray(initialData?.region_rules) && initialData.region_rules.length > 0) {
        payload.region_rules = initialData.region_rules
      }

      const url = isEdit ? `/api/targets/${encodeURIComponent(targetId)}` : '/api/targets'
      const method = isEdit ? 'PATCH' : 'POST'

      const res = await fetch(url, {
        method,
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify(payload),
      })

      if (res.status === 409) {
        throw new Error('目标标识生成冲突，请重试')
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `${isEdit ? '修改' : '创建'}监测任务失败（HTTP ${res.status}）`)
      }

      const result = await res.json().catch(() => ({}))
      if (onCreated) onCreated(result || payload)
      onClose()
    } catch (err) {
      setErrorMsg(err.message || '网络异常，提交失败')
    } finally {
      setLoading(false)
    }
  }

  if (!isOpen) return null

  // 动态 placeholder (使用 RFC 5737 文档保留地址)
  const targetPlaceholder =
    kind === 'icmp'
      ? '198.51.100.1 | 2001:db8::1 | dns.google'
      : kind === 'tcp'
      ? '198.51.100.1:80 | example.com:443'
      : kind === 'http'
      ? 'http://198.51.100.1:8080/health | example.com'
      : kind === 'https'
      ? 'https://example.com/api | 198.51.100.1'
      : 'dns.google | 198.51.100.1'

  return (
    <div className="modal-backdrop" onClick={onClose} role="dialog" aria-modal="true">
      <div className="lite-add-modal" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="lite-modal-header">
          <h3 className="lite-modal-title">{isEdit ? '编辑监测任务' : '添加'}</h3>
          <button
            type="button"
            className="icon-button lite-modal-close"
            onClick={onClose}
            aria-label="关闭"
          >
            <X size={18} />
          </button>
        </div>

        {errorMsg && (
          <div className="alert-banner-error" style={{ margin: '12px 18px 0' }}>
            <span>{errorMsg}</span>
          </div>
        )}

        <form onSubmit={handleSubmit} noValidate>
          <div className="lite-modal-body">
            {/* 1. 名称 */}
            <div className="lite-field">
              <label className="lite-field-label">名称</label>
              <input
                type="text"
                className="lite-input"
                placeholder="如: 重庆移动 / AWS 新加坡延迟"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={128}
                autoFocus
                required
              />
            </div>

            {/* 2. 类型 */}
            <div className="lite-field">
              <label className="lite-field-label">类型</label>
              <select
                className="lite-select"
                value={kind}
                onChange={(e) => handleKindChange(e.target.value)}
              >
                {PROTOCOL_OPTIONS.map((p) => (
                  <option key={p.value} value={p.value}>
                    {p.label}
                  </option>
                ))}
              </select>
            </div>

            {/* 3. 目标 */}
            <div className="lite-field">
              <label className="lite-field-label">目标</label>
              <input
                type="text"
                className="lite-input mono"
                placeholder={targetPlaceholder}
                value={targetInput}
                onChange={(e) => handleTargetInputChange(e.target.value)}
                required
              />
            </div>

            {/* 4. 服务器执行范围 (Scheme B: 支持同时配置服务器与标签，后端按 OR 规则执行) */}
            <div className="lite-field">
              <label className="lite-field-label">执行范围</label>

              {/* 服务器选择 */}
              <div className="lite-server-select-row">
                <button
                  type="button"
                  className="lite-btn-server-pick"
                  onClick={handleOpenServerPicker}
                >
                  选择服务器
                </button>
                <span className="lite-server-count-badge">{serverSelectionLabel}</span>
              </div>

              {/* 节点标签限定输入 */}
              <div style={{ marginTop: '10px' }}>
                <label className="lite-field-label" style={{ fontSize: '12px' }}>
                  限定探针标签 (可选，支持多标签)
                </label>
                <input
                  type="text"
                  className="lite-input"
                  placeholder="多个标签以逗号或空格隔开，如: ap, hk, edge"
                  value={nodeTagsInput}
                  onChange={(e) => setNodeTagsInput(e.target.value)}
                />
                {availableTags.length > 0 && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '5px', flexWrap: 'wrap', marginTop: '6px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>快捷填入集群标签:</span>
                    {availableTags.map((tag) => {
                      const active = parsedTags.includes(tag.toLowerCase())
                      return (
                        <button
                          key={tag}
                          type="button"
                          className={`badge ${active ? 'badge-primary' : 'badge-subtle'}`}
                          style={{
                            cursor: 'pointer',
                            fontSize: '11px',
                            padding: '2px 8px',
                            background: active ? 'rgba(2, 132, 199, 0.2)' : undefined,
                            borderColor: active ? '#0284c7' : undefined,
                            color: active ? '#0284c7' : undefined,
                          }}
                          onClick={() => toggleTag(tag)}
                        >
                          {active ? `✓ ${tag}` : `+ ${tag}`}
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>

              {/* 规则说明与范围提示 */}
              <div style={{ marginTop: '8px' }}>
                {selectedServers !== 'ALL' && parsedTags.length > 0 ? (
                  <div
                    style={{
                      padding: '8px 12px',
                      borderRadius: '6px',
                      background: 'rgba(2, 132, 199, 0.08)',
                      border: '1px solid rgba(2, 132, 199, 0.2)',
                      fontSize: '12px',
                      color: '#0284c7',
                      lineHeight: 1.5,
                    }}
                  >
                    ℹ️ <b>规则说明</b>：已同时配置指定服务器与限定标签。后端执行规则为 <b>OR (满足任一条件即执行)</b>：满足选定服务器列表（共 {selectedServers.size} 台），或探针自身带有上述任一指定标签（[{parsedTags.join(', ')}]）的服务器，均会执行此探测任务。
                  </div>
                ) : selectedServers === 'ALL' && parsedTags.length === 0 ? (
                  <p className="lite-field-subtext" style={{ margin: 0 }}>
                    全部服务器：集群中当前及未来所有加入的服务器探针均自动下发执行。
                  </p>
                ) : selectedServers !== 'ALL' && parsedTags.length === 0 ? (
                  <p className="lite-field-subtext" style={{ margin: 0 }}>
                    指定服务器：仅向选定的 {selectedServers.size} 台服务器探针下发探测任务。
                  </p>
                ) : (
                  <p className="lite-field-subtext" style={{ margin: 0 }}>
                    标签限定：仅向具备 [{parsedTags.join(', ')}] 标签的探针下发探测任务。
                  </p>
                )}
              </div>
            </div>

            {/* 5. 启用任务 */}
            <div className="lite-field lite-default-enable-row">
              <label className="lite-checkbox-label">
                <input
                  type="checkbox"
                  className="lite-checkbox"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                />
                <span className="lite-checkbox-text">启用任务</span>
              </label>
              <p className="lite-field-subtext">
                开启后，所有在执行范围内的服务器探针将立即执行此探测；关闭则全部暂停探测。
              </p>
            </div>

            {/* 6. 间隔 (秒) */}
            <div className="lite-field">
              <label className="lite-field-label">间隔 (秒)</label>
              <input
                type="number"
                className="lite-input mono"
                min={10}
                max={86400}
                value={intervalSeconds}
                onChange={(e) => setIntervalSeconds(e.target.value)}
                required
              />
            </div>

            {/* 7. 高级选项折叠栏 */}
            <div className="lite-advanced-section">
              <button
                type="button"
                className="lite-advanced-toggle"
                onClick={() => setShowAdvanced(!showAdvanced)}
              >
                <span>高级选项 (超时、端口、路径、状态码、DNS等)</span>
                {showAdvanced ? <CaretDown size={14} /> : <CaretRight size={14} />}
              </button>

              {showAdvanced && (
                <div className="lite-advanced-panel">
                  <div className="lite-grid-2">
                    <div className="lite-field">
                      <label className="lite-field-label">超时时间 (毫秒)</label>
                      <input
                        type="number"
                        className="lite-input mono"
                        min={100}
                        max={30000}
                        value={timeoutMs}
                        onChange={(e) => setTimeoutMs(e.target.value)}
                      />
                    </div>

                    {kind !== 'icmp' && (
                      <div className="lite-field">
                        <label className="lite-field-label">端口</label>
                        <input
                          type="number"
                          className="lite-input mono"
                          min={1}
                          max={65535}
                          value={port}
                          onChange={(e) => setPort(e.target.value)}
                        />
                      </div>
                    )}
                  </div>

                  {(kind === 'http' || kind === 'https') && (
                    <>
                      <div className="lite-grid-2">
                        <div className="lite-field">
                          <label className="lite-field-label">请求路径</label>
                          <input
                            type="text"
                            className="lite-input mono"
                            placeholder="/"
                            value={path}
                            onChange={(e) => setPath(e.target.value)}
                          />
                        </div>
                        <div className="lite-field">
                          <label className="lite-field-label">期望状态码 (可选)</label>
                          <input
                            type="number"
                            className="lite-input mono"
                            placeholder="200"
                            value={expectedStatus}
                            onChange={(e) => setExpectedStatus(e.target.value)}
                          />
                        </div>
                      </div>

                      <div className="lite-grid-2">
                        <div className="lite-field">
                          <label className="lite-field-label">响应内容关键字 (可选)</label>
                          <input
                            type="text"
                            className="lite-input"
                            placeholder="如: ok 或 success"
                            value={keyword}
                            onChange={(e) => setKeyword(e.target.value)}
                          />
                        </div>
                        {kind === 'https' && (
                          <div className="lite-field lite-default-enable-row" style={{ marginTop: '22px' }}>
                            <label className="lite-checkbox-label">
                              <input
                                type="checkbox"
                                className="lite-checkbox"
                                checked={checkTls}
                                onChange={(e) => setCheckTls(e.target.checked)}
                              />
                              <span className="lite-checkbox-text">校验 TLS 证书</span>
                            </label>
                          </div>
                        )}
                      </div>
                    </>
                  )}

                  {kind === 'dns' && (
                    <div className="lite-grid-2">
                      <div className="lite-field">
                        <label className="lite-field-label">DNS 记录类型</label>
                        <select
                          className="lite-select"
                          value={dnsType}
                          onChange={(e) => setDnsType(e.target.value)}
                        >
                          <option value="A">A 记录 (IPv4)</option>
                          <option value="AAAA">AAAA 记录 (IPv6)</option>
                          <option value="CNAME">CNAME 别名记录</option>
                        </select>
                      </div>
                      <div className="lite-field">
                        <label className="lite-field-label">DNS 服务器 (可选)</label>
                        <input
                          type="text"
                          className="lite-input mono"
                          placeholder="系统默认 或 198.51.100.53"
                          value={nameserver}
                          onChange={(e) => setNameserver(e.target.value)}
                        />
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* Footer Actions */}
          <div className="lite-modal-footer">
            <button
              type="button"
              className="button button-quiet lite-btn-cancel"
              onClick={onClose}
              disabled={loading}
            >
              取消
            </button>
            <button
              type="submit"
              className="button button-primary lite-btn-submit"
              disabled={loading}
            >
              {loading ? (
                <>
                  <CircleNotch size={14} className="spin" />
                  <span>提交中…</span>
                </>
              ) : (
                <span>{isEdit ? '保存' : '添加'}</span>
              )}
            </button>
          </div>
        </form>

        {/* 服务器多选浮层 (Image 4 样式) */}
        {showServerPicker && (
          <div
            className="lite-server-picker-overlay"
            onClick={handleCancelServerPicker}
          >
            <div
              className="lite-server-picker-card"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="lite-server-picker-header">
                <h4>选择</h4>
                <button
                  type="button"
                  className="icon-button"
                  onClick={handleCancelServerPicker}
                  aria-label="关闭选择器"
                >
                  <X size={16} />
                </button>
              </div>

              <div className="lite-server-picker-subheader">
                <span className="lite-picker-sub-info">
                  已选 {tempSelectedCount} / 共 {serverList.length}
                </span>
                <button
                  type="button"
                  className="button button-quiet btn-xs lite-picker-btn-all"
                  onClick={handleSelectAll}
                >
                  {tempSelectedCount === serverList.length ? '清空' : '全选'}
                </button>
              </div>

              <div className="lite-server-picker-searchbar">
                <div className="lite-picker-search-wrap">
                  <MagnifyingGlass size={14} />
                  <input
                    type="text"
                    className="lite-picker-search-input"
                    placeholder="搜索"
                    value={serverSearch}
                    onChange={(e) => setServerSearch(e.target.value)}
                    autoFocus
                  />
                  {serverSearch && (
                    <button
                      type="button"
                      className="icon-button"
                      onClick={() => setServerSearch('')}
                    >
                      <X size={12} />
                    </button>
                  )}
                </div>
              </div>

              <div className="lite-server-picker-table-header">
                <span>服务器</span>
              </div>

              <div className="lite-server-picker-list">
                {filteredServers.length === 0 ? (
                  <div className="p-4 text-center text-muted text-xs">
                    未找到匹配的服务器
                  </div>
                ) : (
                  filteredServers.map((s) => {
                    const sid = s.uuid || s.id
                    const isChecked =
                      tempSelectedServers === 'ALL' ||
                      (tempSelectedServers instanceof Set && tempSelectedServers.has(sid))
                    const isOnline = s.status === 'online' || s.online === true
                    const meta = detectRegionAndFlag(s.name, s.hostname || s.region || '')
                    const flag = s.flag || meta.flag

                    return (
                      <div
                        key={sid}
                        className={`lite-server-picker-item ${
                          isChecked ? 'is-selected' : ''
                        } ${!isOnline ? 'is-offline' : ''}`}
                        onClick={() => toggleTempServer(sid)}
                      >
                        <input
                          type="checkbox"
                          className="lite-checkbox"
                          checked={isChecked}
                          onChange={() => {}}
                        />
                        <div className="lite-picker-item-info">
                          <div className="lite-picker-item-name">
                            <span className="lite-flag-icon">{flag}</span>
                            <span
                              className={`status-dot ${
                                isOnline ? 'status-online' : 'status-offline'
                              }`}
                            />
                            <strong>{s.name || s.hostname || sid}</strong>
                          </div>
                          <div className="lite-picker-item-extra">
                            {!isOnline && (
                              <span className="text-muted text-xs mr-2">[离线]</span>
                            )}
                            {(s.region || s.tags) && (
                              <span className="lite-picker-item-tag">
                                {s.region || s.tags}
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                    )
                  })
                )}
              </div>

              <div className="lite-server-picker-footer">
                <span className="lite-picker-footer-count">
                  {tempSelectedCount === serverList.length ? '全部服务器' : `${tempSelectedCount}选择`}
                </span>
                <div className="lite-picker-footer-btns">
                  <button
                    type="button"
                    className="button button-quiet btn-sm"
                    onClick={handleCancelServerPicker}
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    className="button button-primary btn-sm"
                    onClick={handleApplyServerPicker}
                  >
                    完成
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
