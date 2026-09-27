import React, { useState, useEffect, useRef, useCallback } from 'react'
import {
  Terminal as TerminalIcon,
  Play,
  ArrowClockwise,
  CheckCircle,
  WarningCircle,
  ShieldCheck,
  Cpu,
  Globe,
  HardDrive,
  Copy,
  Check,
  ArrowsOut,
  ArrowsIn,
  Trash,
  StopCircle,
} from '@phosphor-icons/react'
import { fetchTerminalStatus, execTerminalCommand } from '../lib/api'

const QUICK_PRESETS = [
  {
    id: 'sysinfo',
    title: '系统概况与负载',
    icon: Cpu,
    cmd: 'uname -a && uptime && free -h && df -h',
    desc: '操作系统版本、开机时间、内存与磁盘空间',
  },
  {
    id: 'network',
    title: '网络接口与路由',
    icon: Globe,
    cmd: 'ip a && ip route',
    desc: '本地 IP 分配、网络掩码与默认网关路由表',
  },
  {
    id: 'ports',
    title: '端口与监听服务',
    icon: TerminalIcon,
    cmd: 'ss -tulpn 2>/dev/null || netstat -tulpn 2>/dev/null',
    desc: '查看当前主机所有正在监听的 TCP/UDP 端口及进程',
  },
  {
    id: 'top-proc',
    title: 'CPU 占用 Top 10',
    icon: Cpu,
    cmd: 'ps aux --sort=-%cpu | head -n 11',
    desc: '列出消耗 CPU 最多的前 10 个进程',
  },
  {
    id: 'docker',
    title: 'Docker 容器状态',
    icon: TerminalIcon,
    cmd: 'docker ps -a --format "table {{.Names}}\\t{{.Status}}\\t{{.Ports}}" 2>/dev/null || echo "Docker not available"',
    desc: '枚举所有运行中与停止的容器及映射端口',
  },
  {
    id: 'disk-io',
    title: '磁盘结构与挂载',
    icon: HardDrive,
    cmd: 'lsblk 2>/dev/null || df -hT',
    desc: '查看块存储设备挂载点与文件系统类型',
  },
]

// Simple ANSI color code parser to React styled spans
function renderANSI(text) {
  if (!text) return null
  const regex = /\x1b\[([0-9;]*)m/g
  const parts = []
  let lastIdx = 0
  let curStyle = {}

  let match
  while ((match = regex.exec(text)) !== null) {
    if (match.index > lastIdx) {
      parts.push({
        text: text.substring(lastIdx, match.index),
        style: { ...curStyle },
      })
    }
    const codes = match[1] ? match[1].split(';').map(Number) : [0]
    for (const code of codes) {
      switch (code) {
        case 0:
          curStyle = {}
          break
        case 1:
          curStyle.fontWeight = 'bold'
          break
        case 2:
          curStyle.opacity = 0.7
          break
        case 30:
          curStyle.color = '#4b5563'
          break
        case 31:
          curStyle.color = '#ef4444'
          break
        case 32:
          curStyle.color = '#10b981'
          break
        case 33:
          curStyle.color = '#f59e0b'
          break
        case 34:
          curStyle.color = '#3b82f6'
          break
        case 35:
          curStyle.color = '#ec4899'
          break
        case 36:
          curStyle.color = '#06b6d4'
          break
        case 37:
          curStyle.color = '#f3f4f6'
          break
        case 90:
          curStyle.color = '#6b7280'
          break
        case 91:
          curStyle.color = '#f87171'
          break
        case 92:
          curStyle.color = '#34d399'
          break
        case 93:
          curStyle.color = '#fbbf24'
          break
        case 94:
          curStyle.color = '#60a5fa'
          break
        case 95:
          curStyle.color = '#f472b6'
          break
        case 96:
          curStyle.color = '#22d3ee'
          break
        case 97:
          curStyle.color = '#ffffff'
          break
      }
    }
    lastIdx = regex.lastIndex
  }
  if (lastIdx < text.length) {
    parts.push({
      text: text.substring(lastIdx),
      style: { ...curStyle },
    })
  }

  return parts.map((p, i) => (
    <span key={i} style={p.style}>
      {p.text}
    </span>
  ))
}

export default function TerminalView({ initialNodeId = null }) {
  const [nodes, setNodes] = useState([])
  const [selectedNodeId, setSelectedNodeId] = useState(initialNodeId || '')
  const [selectedNodeIds, setSelectedNodeIds] = useState(initialNodeId ? [initialNodeId] : [])
  const [nodeSearch, setNodeSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [activeTab, setActiveTab] = useState('interactive') // 'interactive' | 'runner'

  // Interactive Terminal State
  const [termLines, setTermLines] = useState([
    { type: 'sys', text: 'ProbeWatch Agent Web Terminal Initialized' },
    { type: 'sys', text: 'Connecting reverse PTY tunnel...' },
  ])
  const [inputVal, setInputVal] = useState('')
  const [wsConnected, setWsConnected] = useState(false)
  const [history, setHistory] = useState([])
  const [historyIdx, setHistoryIdx] = useState(-1)
  const [fullscreen, setFullscreen] = useState(false)
  const wsRef = useRef(null)
  const termEndRef = useRef(null)
  const inputRef = useRef(null)

  // Command Runner State
  const [customCmd, setCustomCmd] = useState('')
  const [timeoutSec, setTimeoutSec] = useState(30)
  const [executing, setExecuting] = useState(false)
  const [execResult, setExecResult] = useState(null)
  const [batchResults, setBatchResults] = useState([])
  const [copied, setCopied] = useState(false)

  const selectedNode = nodes.find((n) => n.id === selectedNodeId || n.uuid === selectedNodeId)
  const filteredNodes = nodes.filter((node) => {
    const query = nodeSearch.trim().toLowerCase()
    const matchesQuery = !query || [node.name, node.id, node.uuid].filter(Boolean).some((value) => String(value).toLowerCase().includes(query))
    const matchesStatus = statusFilter === 'all' || (statusFilter === 'online' ? node.terminal_online : !node.terminal_online)
    return matchesQuery && matchesStatus
  })

  // Fetch node terminal statuses
  const loadStatus = useCallback(async () => {
    try {
      setLoading(true)
      const data = await fetchTerminalStatus()
      setNodes(data.nodes || [])
      if (selectedNodeId) {
        const matchingNode = (data.nodes || []).find((node) => node.id === selectedNodeId || node.uuid === selectedNodeId)
        if (matchingNode && matchingNode.id !== selectedNodeId) setSelectedNodeId(matchingNode.id)
      }
      if (!selectedNodeId && data.nodes && data.nodes.length > 0) {
        setSelectedNodeId(data.nodes[0].id)
      }
      setSelectedNodeIds((current) => {
        const available = new Map((data.nodes || []).flatMap((node) => [[node.id, node.id], [node.uuid, node.id]]))
        const kept = Array.from(new Set(current.map((id) => available.get(id)).filter(Boolean)))
        if (kept.length > 0) return kept
        return data.nodes?.[0] ? [data.nodes[0].id] : []
      })
      setError(null)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [selectedNodeId])

  useEffect(() => {
    loadStatus()
  }, [])

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') loadStatus()
    }, 8000)
    return () => window.clearInterval(timer)
  }, [loadStatus])

  // Auto-scroll terminal output
  useEffect(() => {
    if (activeTab === 'interactive') {
      termEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [termLines, activeTab])

  // Establish Interactive WebSocket connection to node
  const connectWebSocket = useCallback(() => {
    if (!selectedNode) return

    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }

    setTermLines([
      { type: 'sys', text: `Connecting to ${selectedNode.name || selectedNode.uuid}...` },
    ])
    setWsConnected(false)

    const loc = window.location
    const proto = loc.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${proto}//${loc.host}/api/admin/terminal/ws?node_id=${encodeURIComponent(selectedNode.id)}&cols=100&rows=30`

    try {
      const ws = new WebSocket(wsUrl)
      wsRef.current = ws

      ws.onopen = () => {
        setWsConnected(true)
        setTermLines((prev) => [
          ...prev,
          { type: 'sys', text: '✓ Tunnel established. Shell interactive session ready.' },
        ])
        inputRef.current?.focus()
      }

      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          if (msg.type === 'data') {
            setTermLines((prev) => [...prev, { type: 'out', text: msg.data }])
          } else if (msg.type === 'session_started') {
            setTermLines((prev) => [
              ...prev,
              { type: 'sys', text: `Session attached (${msg.shell || 'bash'}). Type commands below.` },
            ])
          } else if (msg.type === 'session_end') {
            setTermLines((prev) => [
              ...prev,
              { type: 'sys', text: `Session disconnected: ${msg.reason || 'closed'}` },
            ])
            setWsConnected(false)
          } else if (msg.type === 'error') {
            setTermLines((prev) => [
              ...prev,
              { type: 'err', text: `Error: ${msg.error}` },
            ])
          }
        } catch {
          setTermLines((prev) => [...prev, { type: 'out', text: event.data }])
        }
      }

      ws.onerror = () => {
        setTermLines((prev) => [
          ...prev,
          { type: 'err', text: 'WebSocket connection error or node offline' },
        ])
        setWsConnected(false)
      }

      ws.onclose = () => {
        setWsConnected(false)
      }
    } catch (err) {
      setTermLines((prev) => [...prev, { type: 'err', text: `Connect failed: ${err.message}` }])
    }
  }, [selectedNode])

  useEffect(() => {
    if (activeTab === 'interactive' && selectedNode && selectedNode.terminal_online) {
      connectWebSocket()
    }
    return () => {
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [selectedNodeId, activeTab])

  // Handle keystrokes in interactive terminal
  const handleKeyDown = (e) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      const cmd = inputVal
      if (!cmd.trim() && !wsConnected) return

      if (cmd.trim()) {
        setHistory((prev) => [...prev, cmd])
        setHistoryIdx(-1)
      }

      setTermLines((prev) => [...prev, { type: 'in', text: cmd }])

      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(
          JSON.stringify({
            type: 'data',
            data: cmd + '\n',
          })
        )
      }

      setInputVal('')
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (history.length === 0) return
      const nextIdx = historyIdx === -1 ? history.length - 1 : Math.max(0, historyIdx - 1)
      setHistoryIdx(nextIdx)
      setInputVal(history[nextIdx] || '')
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (historyIdx === -1) return
      const nextIdx = historyIdx + 1
      if (nextIdx >= history.length) {
        setHistoryIdx(-1)
        setInputVal('')
      } else {
        setHistoryIdx(nextIdx)
        setInputVal(history[nextIdx] || '')
      }
    } else if (e.key === 'c' && e.ctrlKey) {
      e.preventDefault()
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(
          JSON.stringify({
            type: 'data',
            data: '\x03',
          })
        )
      }
      setTermLines((prev) => [...prev, { type: 'sys', text: '^C' }])
      setInputVal('')
    } else if (e.key === 'l' && e.ctrlKey) {
      e.preventDefault()
      setTermLines([])
    }
  }

  // Handle command runner execution
  const runCommand = async (cmdText) => {
    const toRun = cmdText || customCmd
    const targetIds = selectedNodeIds.length > 0 ? selectedNodeIds : (selectedNode ? [selectedNode.id] : [])
    const targetNodes = targetIds.map((id) => nodes.find((node) => node.id === id || node.uuid === id)).filter(Boolean)
    if (!toRun.trim() || targetNodes.length === 0) return

    setExecuting(true)
    setExecResult(null)
    setBatchResults(targetNodes.map((node) => ({ node, status: 'pending' })))
    setError(null)

    const results = []
    for (const node of targetNodes) {
      setBatchResults((current) => current.map((item) => item.node.id === node.id ? { ...item, status: 'running' } : item))
      try {
        const res = await execTerminalCommand({
          nodeId: node.id,
          command: toRun,
          timeoutSec: Number(timeoutSec),
        })
        const result = { ...res, node, status: res.exit_code === 0 ? 'success' : 'failed', command: toRun, timestamp: new Date().toLocaleTimeString() }
        results.push(result)
        setBatchResults((current) => current.map((item) => item.node.id === node.id ? result : item))
        setExecResult(result)
      } catch (err) {
        const result = { node, status: 'failed', command: toRun, error: err.message, timestamp: new Date().toLocaleTimeString() }
        results.push(result)
        setBatchResults((current) => current.map((item) => item.node.id === node.id ? result : item))
      }
    }
    setExecuting(false)
  }

  const toggleNode = (nodeId) => {
    setSelectedNodeIds((current) => current.includes(nodeId) ? current.filter((id) => id !== nodeId) : [...current, nodeId])
  }

  const toggleVisibleNodes = () => {
    const visibleIds = filteredNodes.map((node) => node.id)
    const allSelected = visibleIds.length > 0 && visibleIds.every((id) => selectedNodeIds.includes(id))
    setSelectedNodeIds((current) => allSelected ? current.filter((id) => !visibleIds.includes(id)) : Array.from(new Set([...current, ...visibleIds])))
  }

  const handleCopy = () => {
    const output = batchResults.map((item) => `${item.node.name || item.node.uuid}:\n${item.stdout || item.error || '(无输出)'}`).join('\n\n')
    if (!output) return
    navigator.clipboard.writeText(output)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={`space-y-6 ${fullscreen ? 'fixed inset-0 z-50 bg-[#0f172a] p-6 overflow-auto' : ''}`}>
      {/* Top Header & Node Selector */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 p-5 rounded-2xl bg-white/[0.03] border border-white/10 backdrop-blur-md">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-emerald-500/20 to-cyan-500/20 border border-emerald-500/30 flex items-center justify-center text-emerald-400">
            <TerminalIcon size={24} weight="duotone" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-white tracking-wide flex items-center gap-2">
              Agent 远程终端与受控执行
              <span className="text-xs px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-mono">
                v0.5.9
              </span>
            </h2>
            <p className="text-xs text-slate-400">
              基于反向隧道穿透 NAT/防火墙，支持全交互式 Web PTY 与安全受控一键诊断命令
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {/* Node Picker */}
          <div className="flex items-center gap-2 bg-slate-900/80 border border-white/10 rounded-xl px-3 py-1.5">
            <span className="text-xs text-slate-400 font-medium">目标节点:</span>
            <select
              value={selectedNodeId}
              onChange={(e) => setSelectedNodeId(e.target.value)}
              className="bg-transparent text-sm text-slate-200 outline-none cursor-pointer font-medium"
            >
              {nodes.map((n) => (
                <option key={n.id} value={n.id} className="bg-slate-900 text-slate-200">
                  {n.name || n.uuid.slice(0, 8)} {n.terminal_online ? '● 在线' : '○ 离线'}
                </option>
              ))}
            </select>
            {selectedNode && (
              <span
                className={`inline-block w-2.5 h-2.5 rounded-full ${
                  selectedNode.terminal_online
                    ? 'bg-emerald-400 shadow-[0_0_8px_#34d399]'
                    : 'bg-amber-500/60'
                }`}
                title={selectedNode.terminal_online ? 'Agent 隧道已在线' : 'Agent 终端未连接'}
              />
            )}
          </div>

          <button
            onClick={loadStatus}
            disabled={loading}
            className="p-2 rounded-xl bg-white/5 hover:bg-white/10 text-slate-300 transition-colors border border-white/5"
            title="刷新节点状态"
          >
            <ArrowClockwise size={18} className={loading ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {/* Mode Navigation Tabs */}
      <div className="flex border-b border-white/10 gap-2">
        <button
          onClick={() => setActiveTab('interactive')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium transition-all relative ${
            activeTab === 'interactive'
              ? 'text-emerald-400 border-b-2 border-emerald-400'
              : 'text-slate-400 hover:text-slate-200'
          }`}
        >
          <TerminalIcon size={18} />
          交互式终端 (Web PTY)
          {selectedNode?.terminal_online && (
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
          )}
        </button>
        <button
          onClick={() => setActiveTab('runner')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium transition-all relative ${
            activeTab === 'runner'
              ? 'text-cyan-400 border-b-2 border-cyan-400'
              : 'text-slate-400 hover:text-slate-200'
          }`}
        >
          <Play size={18} />
          远程执行
        </button>
      </div>

      {error && (
        <div className="p-3 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-400 text-xs flex items-center gap-2">
          <WarningCircle size={16} />
          {error}
        </div>
      )}

      {/* Offline Alert */}
      {selectedNode && !selectedNode.terminal_online && (
        <div className="p-4 rounded-xl bg-amber-500/10 border border-amber-500/20 text-amber-300 text-sm flex items-start gap-3">
          <WarningCircle size={20} className="shrink-0 mt-0.5 text-amber-400" />
          <div>
            <div className="font-semibold">当前节点 Agent 远程终端未在线</div>
            <div className="text-xs text-amber-400/80 mt-1 leading-relaxed">
              请确保目标节点已升级至最新探针版本（v0.5.9），且在运行环境中启用了终端功能（环境变量
              <code className="mx-1 px-1 py-0.5 rounded bg-black/30">PROBEWATCH_AGENT_ENABLE_TERMINAL=true</code>
              或保持默认开启状态）。节点将自动通过安全反向 WebSocket 连接至控制面。
            </div>
          </div>
        </div>
      )}

      {/* Tab 1: Interactive Terminal */}
      {activeTab === 'interactive' && (
        <div className="rounded-2xl border border-white/10 bg-[#090d16] shadow-2xl overflow-hidden font-mono flex flex-col h-[580px]">
          {/* Terminal Window Header Bar */}
          <div className="flex items-center justify-between px-4 py-2.5 bg-slate-900/90 border-b border-white/10 select-none">
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded-full bg-rose-500/80" />
              <div className="w-3 h-3 rounded-full bg-amber-500/80" />
              <div className="w-3 h-3 rounded-full bg-emerald-500/80" />
              <span className="text-xs text-slate-400 ml-2 font-mono">
                {selectedNode ? `terminal@${selectedNode.name || selectedNode.uuid.slice(0, 8)}` : 'terminal'}
              </span>
              <span
                className={`text-[10px] px-2 py-0.2 rounded-full font-sans ${
                  wsConnected
                    ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                    : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                }`}
              >
                {wsConnected ? 'Connected' : 'Disconnected'}
              </span>
            </div>

            <div className="flex items-center gap-1">
              <button
                onClick={() => setTermLines([])}
                className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-white/5 transition-colors"
                title="清屏 (Ctrl+L)"
              >
                <Trash size={15} />
              </button>
              <button
                onClick={connectWebSocket}
                className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-white/5 transition-colors"
                title="重新连接"
              >
                <ArrowClockwise size={15} />
              </button>
              <button
                onClick={() => setFullscreen(!fullscreen)}
                className="p-1.5 rounded-lg text-slate-400 hover:text-white hover:bg-white/5 transition-colors"
                title={fullscreen ? '退出全屏' : '全屏显示'}
              >
                {fullscreen ? <ArrowsIn size={15} /> : <ArrowsOut size={15} />}
              </button>
            </div>
          </div>

          {/* Terminal Screen Body */}
          <div
            onClick={() => inputRef.current?.focus()}
            className="flex-1 p-4 overflow-y-auto space-y-1 text-xs text-slate-200 leading-relaxed cursor-text selection:bg-emerald-500/30"
          >
            {termLines.map((line, i) => (
              <div key={i} className="whitespace-pre-wrap break-all">
                {line.type === 'in' && (
                  <span className="text-emerald-400 font-semibold">$ {line.text}</span>
                )}
                {line.type === 'out' && renderANSI(line.text)}
                {line.type === 'sys' && (
                  <span className="text-cyan-400/80 italic"># {line.text}</span>
                )}
                {line.type === 'err' && (
                  <span className="text-rose-400 font-medium">! {line.text}</span>
                )}
              </div>
            ))}
            <div ref={termEndRef} />
          </div>

          {/* Terminal Input Line */}
          <div className="p-3 bg-slate-900/60 border-t border-white/10 flex items-center gap-2">
            <span className="text-emerald-400 font-bold select-none text-sm">$</span>
            <input
              ref={inputRef}
              type="text"
              value={inputVal}
              onChange={(e) => setInputVal(e.target.value)}
              onKeyDown={handleKeyDown}
              disabled={!wsConnected}
              placeholder={wsConnected ? '输入 shell 命令... (回车发送，↑/↓ 查看历史，Ctrl+C 中断)' : '等待终端连接...'}
              className="flex-1 bg-transparent text-xs text-white placeholder-slate-500 outline-none font-mono"
            />
            {wsConnected && (
              <span className="text-[10px] text-slate-500 font-mono hidden sm:inline">
                Enter ↵
              </span>
            )}
          </div>
        </div>
      )}

      {/* Tab 2: Controlled Command Runner */}
      {activeTab === 'runner' && (
        <div className="ssh-runner-page">
          <section className="ssh-runner-card ssh-command-section">
            <div className="ssh-section-heading"><div><span className="ssh-eyebrow">COMMAND RUNNER</span><h3>远程执行</h3><p>通过受控 Agent 通道向选中的节点执行命令，结果会逐节点返回并写入审计记录。</p></div><span className="ssh-live-badge"><span /> 状态每 8 秒自动同步</span></div>
            <textarea value={customCmd} onChange={(e) => setCustomCmd(e.target.value)} placeholder="输入要执行的 Shell 命令，例如：docker ps 或 systemctl status probewatch" className="ssh-command-editor" rows={4} />
            <div className="ssh-command-meta"><div className="ssh-preset-list">{QUICK_PRESETS.slice(0, 4).map((preset) => { const Icon = preset.icon; return <button key={preset.id} type="button" className="ssh-preset-chip" onClick={() => setCustomCmd(preset.cmd)} title={preset.desc}><Icon size={14} /> {preset.title}</button> })}</div><label className="ssh-timeout-control">超时<select value={timeoutSec} onChange={(e) => setTimeoutSec(Number(e.target.value))}><option value={10}>10 秒</option><option value={30}>30 秒</option><option value={60}>60 秒</option><option value={120}>120 秒</option></select></label></div>
          </section>
          <section className="ssh-runner-card ssh-target-section"><div className="ssh-section-heading ssh-target-heading"><div><span className="ssh-eyebrow">TARGET NODES</span><h3>选择目标节点</h3><p>仅显示当前账号可访问的 Agent 节点，在线状态来自实时终端隧道。</p></div><strong className="ssh-selection-count">已选 {selectedNodeIds.length} / {nodes.length}</strong></div><div className="ssh-node-toolbar"><button type="button" className="ssh-toolbar-button" onClick={toggleVisibleNodes}>{filteredNodes.length > 0 && filteredNodes.every((node) => selectedNodeIds.includes(node.id)) ? '取消全选' : '全选当前'}</button><select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="ssh-filter-select"><option value="all">全部状态</option><option value="online">仅在线</option><option value="offline">仅离线</option></select><div className="ssh-search-box"><span>⌕</span><input value={nodeSearch} onChange={(e) => setNodeSearch(e.target.value)} placeholder="搜索节点名称或 UUID" /></div></div><div className="ssh-node-table-wrap"><table className="ssh-node-table"><thead><tr><th aria-label="选择" /><th>节点</th><th>终端状态</th><th>节点标识</th><th>执行权限</th></tr></thead><tbody>{filteredNodes.map((node) => { const checked = selectedNodeIds.includes(node.id); return <tr key={node.id} className={checked ? 'is-selected' : ''} onClick={() => toggleNode(node.id)}><td><input type="checkbox" checked={checked} onChange={() => toggleNode(node.id)} onClick={(e) => e.stopPropagation()} /></td><td><strong>{node.name || node.uuid?.slice(0, 8) || '未命名节点'}</strong></td><td><span className={node.terminal_online ? 'ssh-status-pill online' : 'ssh-status-pill offline'}><span />{node.terminal_online ? '在线' : '离线'}</span></td><td><code>{node.uuid || node.id}</code></td><td><span className="ssh-access-pill">受控执行</span></td></tr> })}{filteredNodes.length === 0 && <tr><td colSpan={5} className="ssh-empty-row">没有匹配的节点</td></tr>}</tbody></table></div></section>
          <section className="ssh-runner-footer"><div className="ssh-runner-note"><ShieldCheck size={18} /><span>命令通过受控接口执行，保留权限校验、CSRF 防护与审计流水。</span></div><button type="button" onClick={() => runCommand()} disabled={executing || !customCmd.trim() || selectedNodeIds.length === 0 || selectedNodeIds.every((id) => !nodes.find((node) => node.id === id)?.terminal_online)} className="ssh-execute-button">{executing ? <><ArrowClockwise size={17} className="spin" /> 执行中...</> : <><Play size={17} weight="fill" /> 执行命令</>}</button></section>
          {batchResults.length > 0 && <section className="ssh-result-section"><div className="ssh-result-heading"><div><span className="ssh-eyebrow">EXECUTION RESULTS</span><h3>执行结果</h3></div><button type="button" className="ssh-toolbar-button" onClick={handleCopy}><Copy size={14} /> 复制全部输出</button></div><div className="ssh-result-grid">{batchResults.map((result) => <article className={'ssh-result-card ' + result.status} key={result.node.id}><header><strong>{result.node.name || result.node.uuid?.slice(0, 8)}</strong><span>{result.status === 'running' ? '执行中' : result.status === 'pending' ? '等待中' : result.status === 'success' ? '成功' : '失败'}</span></header>{result.status === 'success' || result.status === 'failed' ? <><div className="ssh-result-meta">退出码 {result.exit_code ?? '—'} · {result.duration_ms ?? '—'} ms</div><pre>{result.stdout || result.stderr || result.error || '(无输出)'}</pre></> : <div className="ssh-result-pending">等待执行...</div>}</article>)}</div></section>}
        </div>
      )}
      {/* Security & Audit Compliance Footer */}
      <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5 flex items-center justify-between text-xs text-slate-400">
        <div className="flex items-center gap-2">
          <ShieldCheck size={18} className="text-emerald-400 shrink-0" />
          <span>
            受控安全审计已启用：所有终端交互会话与命令执行均由管理员鉴权并通过不可篡改的 SQLite 审计流水追溯。
          </span>
        </div>
        <span className="text-[11px] text-slate-500 font-mono hidden md:inline">
          table: audit_events
        </span>
      </div>
    </div>
  )
}
