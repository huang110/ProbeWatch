import { useEffect, useState } from 'react'
import {
  Sparkle,
  Robot,
  ShieldCheck,
  Warning,
  WarningCircle,
  Coins,
  Cpu,
  Broadcast,
  PaperPlaneRight,
  ArrowsClockwise,
  Copy,
  Check,
  Key,
  Eye,
  EyeSlash,
  Calendar,
  HardDrives,
  Gear,
} from '@phosphor-icons/react'
import {
  fetchAIDiagnosis,
  sendAIChatPrompt,
  fetchAISettings,
  saveAISettings,
  regenerateMCPToken,
  fetchMCPConfig,
} from '../lib/api.js'

export function AICopilotView({ nodes = [], rates = {}, lossRates = {}, onNavigate }) {
  const [activeTab, setActiveTab] = useState('health') // 'health', 'chat', 'mcp', 'settings'
  const [loading, setLoading] = useState(false)
  const [report, setReport] = useState(null)
  const [error, setError] = useState(null)
  const [copiedId, setCopiedId] = useState(null)

  // Chat state
  const [chatMessages, setChatMessages] = useState([
    {
      sender: 'ai',
      text: '您好！我是 ProbeWatch 智能基础设施诊断 Copilot。我已全面同步全集群的节点遥测、回程路由 MTR、流量配额及云服务器账单。请问您需要了解哪方面的诊断或建议？',
      time: new Date().toLocaleTimeString(),
    },
  ])
  const [chatInput, setChatInput] = useState('')
  const [chatLoading, setChatLoading] = useState(false)

  // Filter for findings
  const [findingCategory, setFindingCategory] = useState('all')

  // MCP state
  const [mcpConfig, setMcpConfig] = useState(null)
  const [showMcpToken, setShowMcpToken] = useState(false)
  const [regeneratingToken, setRegeneratingToken] = useState(false)

  // Settings state
  const [settings, setSettings] = useState({
    enabled: false,
    provider: 'local',
    api_key: '',
    api_endpoint: 'https://api.deepseek.com/v1',
    model: 'deepseek-chat',
    has_api_key: false,
  })
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [settingsSuccess, setSettingsSuccess] = useState(false)

  // Load initial report
  useEffect(() => {
    loadReport()
  }, [])

  async function loadReport() {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchAIDiagnosis()
      setReport(data)
    } catch (err) {
      setError(err.message || '获取体检诊断数据失败')
    } finally {
      setLoading(false)
    }
  }

  // Load MCP config when tab opened
  useEffect(() => {
    if (activeTab === 'mcp') {
      loadMCPConfig()
    } else if (activeTab === 'settings') {
      loadSettings()
    }
  }, [activeTab])

  async function loadMCPConfig() {
    try {
      const data = await fetchMCPConfig()
      setMcpConfig(data)
    } catch (err) {
      console.error('Failed to load MCP config:', err)
    }
  }

  async function loadSettings() {
    try {
      const data = await fetchAISettings()
      setSettings(data)
    } catch (err) {
      console.error('Failed to load AI settings:', err)
    }
  }

  async function handleSaveSettings(e) {
    e.preventDefault()
    setSettingsSaving(true)
    setSettingsSuccess(false)
    try {
      await saveAISettings(settings)
      setSettingsSuccess(true)
      setTimeout(() => setSettingsSuccess(false), 3000)
      loadSettings()
    } catch (err) {
      alert(err.message || '保存设置失败')
    } finally {
      setSettingsSaving(false)
    }
  }

  async function handleRegenerateToken() {
    if (!window.confirm('确定要重新生成 MCP 访问 Token 吗？已连接的外部客户端需要同步更新该密钥。')) return
    setRegeneratingToken(true)
    try {
      const res = await regenerateMCPToken()
      if (res?.token && mcpConfig) {
        setMcpConfig((prev) => ({
          ...prev,
          token: res.token,
          endpoint: prev.endpoint.replace(/token=[^&]*/, `token=${res.token}`),
        }))
      }
    } catch (err) {
      alert(err.message || '重新生成密钥失败')
    } finally {
      setRegeneratingToken(false)
    }
  }

  async function handleSendMessage(customPrompt) {
    const promptToSend = customPrompt || chatInput
    if (!promptToSend || !promptToSend.trim() || chatLoading) return

    const userMsg = {
      sender: 'user',
      text: promptToSend.trim(),
      time: new Date().toLocaleTimeString(),
    }
    setChatMessages((prev) => [...prev, userMsg])
    if (!customPrompt) setChatInput('')
    setChatLoading(true)

    try {
      const res = await sendAIChatPrompt(promptToSend.trim())
      setChatMessages((prev) => [
        ...prev,
        {
          sender: 'ai',
          text: res.reply || '已完成分析。',
          provider: res.provider,
          time: new Date().toLocaleTimeString(),
        },
      ])
    } catch (err) {
      setChatMessages((prev) => [
        ...prev,
        {
          sender: 'ai',
          text: `⚠️ 分析出错: ${err.message || '网络请求异常'}`,
          time: new Date().toLocaleTimeString(),
        },
      ])
    } finally {
      setChatLoading(false)
    }
  }

  function copyToClipboard(text, id) {
    if (!text) return
    navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    })
  }

  const filteredFindings = report?.findings?.filter((f) => {
    if (findingCategory === 'all') return true
    return f.category === findingCategory
  }) || []

  const score = report?.health_score ?? 100
  let scoreColorClass = 'text-emerald-500'
  let scoreBgClass = 'rgba(16, 185, 129, 0.12)'
  let scoreBorderClass = 'rgba(16, 185, 129, 0.35)'
  if (score < 50) {
    scoreColorClass = 'text-rose-500'
    scoreBgClass = 'rgba(244, 63, 94, 0.12)'
    scoreBorderClass = 'rgba(244, 63, 94, 0.35)'
  } else if (score < 75) {
    scoreColorClass = 'text-amber-500'
    scoreBgClass = 'rgba(245, 158, 11, 0.12)'
    scoreBorderClass = 'rgba(245, 158, 11, 0.35)'
  } else if (score < 90) {
    scoreColorClass = 'text-blue-500'
    scoreBgClass = 'rgba(59, 130, 246, 0.12)'
    scoreBorderClass = 'rgba(59, 130, 246, 0.35)'
  }

  return (
    <div className="subpage-view ai-copilot-view" style={{ maxWidth: '1440px', margin: '0 auto', padding: '16px 20px 48px' }}>
      {/* 顶部标题区 */}
      <div className="flex flex-wrap items-center justify-between gap-4 mb-6" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '24px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
          <div
            style={{
              width: '46px',
              height: '46px',
              borderRadius: '12px',
              background: 'linear-gradient(135deg, rgba(99, 102, 241, 0.25), rgba(168, 85, 247, 0.25))',
              border: '1px solid rgba(168, 85, 247, 0.4)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxShadow: '0 4px 16px rgba(139, 92, 246, 0.2)',
            }}
          >
            <Sparkle size={24} weight="fill" style={{ color: '#a855f7' }} />
          </div>
          <div>
            <h1 style={{ fontSize: '20px', fontWeight: 'bold', margin: 0, display: 'flex', alignItems: 'center', gap: '8px' }}>
              ProbeWatch AI 智能体检与 Copilot 诊断
              <span className="badge badge-emerald" style={{ fontSize: '11px', padding: '2px 8px' }}>v0.5.8</span>
            </h1>
            <p style={{ fontSize: '13px', color: 'var(--text-muted, #888)', margin: '4px 0 0 0' }}>
              回程路由 MTR 根因分析 · 闲置 VPS 降本审计 · Model Context Protocol (MCP) 深度集成
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', gap: '10px' }}>
          <button
            type="button"
            className="button button-primary"
            onClick={loadReport}
            disabled={loading}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <ArrowsClockwise size={16} className={loading ? 'animate-spin' : ''} />
            <span>{loading ? '正在全站深度体检...' : '重新体检'}</span>
          </button>
        </div>
      </div>

      {/* 评分与指标概览看板 */}
      {report && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
            gap: '16px',
            marginBottom: '24px',
          }}
        >
          {/* 健康评分卡片 */}
          <div
            className="card"
            style={{
              padding: '20px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: `1px solid ${scoreBorderClass}`,
              display: 'flex',
              alignItems: 'center',
              gap: '18px',
            }}
          >
            <div
              style={{
                width: '68px',
                height: '68px',
                borderRadius: '50%',
                background: scoreBgClass,
                border: `2px solid ${scoreBorderClass}`,
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                fontWeight: '900',
                fontSize: '24px',
              }}
              className={scoreColorClass}
            >
              <span>{score}</span>
              <span style={{ fontSize: '9px', fontWeight: 'bold', opacity: 0.8 }}>SCORE</span>
            </div>
            <div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted, #888)', marginBottom: '4px' }}>全站健康评分</div>
              <div style={{ fontSize: '16px', fontWeight: 'bold' }}>
                {report.health_status === 'HEALTHY' && '✅ 健康稳定'}
                {report.health_status === 'WARNING' && '⚠️ 需关注隐患'}
                {report.health_status === 'DEGRADED' && '⚡ 亚健康降级'}
                {report.health_status === 'CRITICAL' && '🚨 紧急告警'}
              </div>
              <div style={{ fontSize: '11px', color: 'var(--text-muted, #888)', marginTop: '2px' }}>
                检测到 {report.findings?.length || 0} 个优化项
              </div>
            </div>
          </div>

          {/* 节点运行状态 */}
          <div
            className="card"
            style={{
              padding: '20px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px', color: 'var(--text-muted, #888)', marginBottom: '6px' }}>
              <HardDrives size={16} />
              <span>服务器节点</span>
            </div>
            <div style={{ fontSize: '22px', fontWeight: 'bold', display: 'flex', alignItems: 'baseline', gap: '6px' }}>
              <span>{report.summary?.onlineNodes || 0}</span>
              <span style={{ fontSize: '13px', color: 'var(--text-muted, #888)', fontWeight: 'normal' }}>/ {report.summary?.totalNodes || 0} 在线</span>
            </div>
            <div style={{ fontSize: '11px', marginTop: '4px' }}>
              {report.summary?.offlineNodes > 0 ? (
                <span className="text-rose-500 font-bold">⚠️ {report.summary.offlineNodes} 台机器离线超时</span>
              ) : (
                <span className="text-emerald-500">所有节点正常上报</span>
              )}
            </div>
          </div>

          {/* 异常链路与告警 */}
          <div
            className="card"
            style={{
              padding: '20px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px', color: 'var(--text-muted, #888)', marginBottom: '6px' }}>
              <Broadcast size={16} />
              <span>网络与告警</span>
            </div>
            <div style={{ fontSize: '22px', fontWeight: 'bold', display: 'flex', alignItems: 'baseline', gap: '6px' }}>
              <span>{report.summary?.networkIssues || 0}</span>
              <span style={{ fontSize: '13px', color: 'var(--text-muted, #888)', fontWeight: 'normal' }}>链路异常</span>
            </div>
            <div style={{ fontSize: '11px', color: 'var(--text-muted, #888)', marginTop: '4px' }}>
              当前开放告警: <strong style={{ color: report.summary?.activeAlerts > 0 ? '#f43f5e' : 'inherit' }}>{report.summary?.activeAlerts || 0}</strong> 项
            </div>
          </div>

          {/* 成本与闲置优化 */}
          <div
            className="card"
            style={{
              padding: '20px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px', color: 'var(--text-muted, #888)', marginBottom: '6px' }}>
              <Coins size={16} />
              <span>云成本审计</span>
            </div>
            <div style={{ fontSize: '22px', fontWeight: 'bold' }}>
              {report.cost_audit?.total_monthly_cost ? `¥ ${report.cost_audit.total_monthly_cost.toFixed(1)}` : '—'}
              <span style={{ fontSize: '12px', fontWeight: 'normal', color: 'var(--text-muted, #888)', marginLeft: '4px' }}>/ 月</span>
            </div>
            <div style={{ fontSize: '11px', marginTop: '4px' }}>
              {report.cost_audit?.potential_monthly_savings > 0 ? (
                <span className="text-amber-500 font-bold">💡 建议闲置优化: 节约 ¥{report.cost_audit.potential_monthly_savings.toFixed(1)}/月</span>
              ) : (
                <span className="text-emerald-500">未发现闲置资源浪费</span>
              )}
            </div>
          </div>
        </div>
      )}

      {/* 选项卡导航 */}
      <div
        style={{
          display: 'flex',
          borderBottom: '1px solid var(--border-color, rgba(255,255,255,0.1))',
          marginBottom: '20px',
          gap: '24px',
        }}
      >
        <button
          type="button"
          onClick={() => setActiveTab('health')}
          style={{
            padding: '10px 0',
            borderBottom: activeTab === 'health' ? '2px solid #a855f7' : '2px solid transparent',
            color: activeTab === 'health' ? '#a855f7' : 'var(--text-muted, #888)',
            fontWeight: activeTab === 'health' ? 'bold' : 'normal',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            fontSize: '14px',
          }}
        >
          <ShieldCheck size={18} />
          <span>全站体检与根因 ({report?.findings?.length || 0})</span>
        </button>

        <button
          type="button"
          onClick={() => setActiveTab('chat')}
          style={{
            padding: '10px 0',
            borderBottom: activeTab === 'chat' ? '2px solid #a855f7' : '2px solid transparent',
            color: activeTab === 'chat' ? '#a855f7' : 'var(--text-muted, #888)',
            fontWeight: activeTab === 'chat' ? 'bold' : 'normal',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            fontSize: '14px',
          }}
        >
          <Robot size={18} />
          <span>AI Copilot 智能问诊</span>
        </button>

        <button
          type="button"
          onClick={() => setActiveTab('mcp')}
          style={{
            padding: '10px 0',
            borderBottom: activeTab === 'mcp' ? '2px solid #a855f7' : '2px solid transparent',
            color: activeTab === 'mcp' ? '#a855f7' : 'var(--text-muted, #888)',
            fontWeight: activeTab === 'mcp' ? 'bold' : 'normal',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            fontSize: '14px',
          }}
        >
          <Broadcast size={18} />
          <span>MCP 客户端接入 (Claude & Cursor)</span>
        </button>

        <button
          type="button"
          onClick={() => setActiveTab('settings')}
          style={{
            padding: '10px 0',
            borderBottom: activeTab === 'settings' ? '2px solid #a855f7' : '2px solid transparent',
            color: activeTab === 'settings' ? '#a855f7' : 'var(--text-muted, #888)',
            fontWeight: activeTab === 'settings' ? 'bold' : 'normal',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            fontSize: '14px',
          }}
        >
          <Gear size={18} />
          <span>LLM 引擎配置</span>
        </button>
      </div>

      {/* Tab 1: 全站体检与根因 */}
      {activeTab === 'health' && (
        <div>
          {/* 筛选标签 */}
          <div style={{ display: 'flex', gap: '8px', marginBottom: '16px' }}>
            {[
              { id: 'all', label: '全部发现' },
              { id: 'network', label: '网络与回程路由' },
              { id: 'cost', label: '成本闲置与续费' },
              { id: 'system', label: '硬件资源瓶颈' },
            ].map((tab) => (
              <button
                key={tab.id}
                type="button"
                className={`button btn-sm ${findingCategory === tab.id ? 'button-primary' : 'button-quiet'}`}
                onClick={() => setFindingCategory(tab.id)}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {filteredFindings.length === 0 ? (
            <div
              className="card"
              style={{
                padding: '40px',
                textAlign: 'center',
                borderRadius: '16px',
                background: 'var(--card-bg, rgba(255,255,255,0.03))',
                border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
              }}
            >
              <ShieldCheck size={48} className="text-emerald-500" style={{ margin: '0 auto 12px' }} />
              <h3 style={{ fontSize: '16px', fontWeight: 'bold', margin: '0 0 6px 0' }}>未检测到此分类下的异常发现</h3>
              <p style={{ fontSize: '13px', color: 'var(--text-muted, #888)', margin: 0 }}>
                所有探测目标与系统指标均处于健康安全范围。
              </p>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              {filteredFindings.map((finding) => {
                let badgeStyle = { background: 'rgba(59, 130, 246, 0.15)', color: '#60a5fa', border: '1px solid rgba(59, 130, 246, 0.3)' }
                let badgeText = '需注意'
                if (finding.severity === 'critical') {
                  badgeStyle = { background: 'rgba(244, 63, 94, 0.15)', color: '#f43f5e', border: '1px solid rgba(244, 63, 94, 0.3)' }
                  badgeText = '紧急'
                } else if (finding.severity === 'optimize') {
                  badgeStyle = { background: 'rgba(245, 158, 11, 0.15)', color: '#fbbf24', border: '1px solid rgba(245, 158, 11, 0.3)' }
                  badgeText = '优化建议'
                }

                return (
                  <div
                    key={finding.id}
                    className="card"
                    style={{
                      padding: '16px 20px',
                      borderRadius: '14px',
                      background: 'var(--card-bg, rgba(255,255,255,0.03))',
                      border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '8px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                        <span
                          style={{
                            fontSize: '11px',
                            fontWeight: 'bold',
                            padding: '2px 8px',
                            borderRadius: '6px',
                            ...badgeStyle,
                          }}
                        >
                          {badgeText}
                        </span>
                        <strong style={{ fontSize: '15px' }}>{finding.title}</strong>
                      </div>
                      {finding.node_name && (
                        <span className="badge badge-quiet" style={{ fontSize: '12px' }}>
                          节点: {finding.node_name}
                        </span>
                      )}
                    </div>

                    <div style={{ fontSize: '13px', lineHeight: '1.6', marginBottom: '8px', color: 'var(--text-color, #e5e7eb)' }}>
                      {finding.summary}
                    </div>

                    <div
                      style={{
                        padding: '10px 14px',
                        borderRadius: '8px',
                        background: 'rgba(0,0,0,0.18)',
                        fontSize: '12px',
                        lineHeight: '1.6',
                        display: 'flex',
                        flexDirection: 'column',
                        gap: '4px',
                        borderLeft: '3px solid #a855f7',
                      }}
                    >
                      <div>
                        <strong style={{ color: '#c084fc' }}>根因剖析: </strong>
                        <span>{finding.root_cause}</span>
                      </div>
                      <div>
                        <strong style={{ color: '#34d399' }}>处置建议: </strong>
                        <span>{finding.recommendation}</span>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {/* 完整诊断 Markdown 报告预览 */}
          {report?.markdown_report && (
            <div style={{ marginTop: '32px' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
                <h3 style={{ fontSize: '15px', fontWeight: 'bold', margin: 0 }}>📋 诊断审计 Executive Summary</h3>
                <button
                  type="button"
                  className="button button-quiet btn-sm"
                  onClick={() => copyToClipboard(report.markdown_report, 'report-md')}
                  style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
                >
                  {copiedId === 'report-md' ? <Check size={14} className="text-emerald-500" /> : <Copy size={14} />}
                  <span>{copiedId === 'report-md' ? '已复制' : '复制 Markdown 报告'}</span>
                </button>
              </div>
              <pre
                style={{
                  padding: '16px',
                  borderRadius: '12px',
                  background: 'rgba(0,0,0,0.25)',
                  border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
                  fontSize: '12px',
                  lineHeight: '1.6',
                  fontFamily: 'monospace',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  maxHeight: '380px',
                  overflowY: 'auto',
                }}
              >
                {report.markdown_report}
              </pre>
            </div>
          )}
        </div>
      )}

      {/* Tab 2: AI Copilot 智能问诊 */}
      {activeTab === 'chat' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
          {/* 快捷提问气泡 */}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
            <span style={{ fontSize: '12px', color: 'var(--text-muted, #888)', display: 'flex', alignItems: 'center', marginRight: '4px' }}>
              常用问诊:
            </span>
            {[
              '🌐 分析全网丢包与回程链路根因',
              '💰 哪些服务器处于闲置状态？如何降本？',
              '📅 接下来 7 天有哪些云服务器即将到期？',
              '⚡ 哪些节点当前存在内存或磁盘容量风险？',
            ].map((preset, idx) => (
              <button
                key={idx}
                type="button"
                className="button button-quiet btn-sm"
                onClick={() => handleSendMessage(preset)}
                disabled={chatLoading}
                style={{ fontSize: '12px', padding: '4px 10px', borderRadius: '20px' }}
              >
                {preset}
              </button>
            ))}
          </div>

          {/* 聊天对话区域 */}
          <div
            className="card"
            style={{
              height: '480px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.02))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
              display: 'flex',
              flexDirection: 'column',
              overflow: 'hidden',
            }}
          >
            {/* 消息滚动区 */}
            <div style={{ flex: 1, padding: '20px', overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '16px' }}>
              {chatMessages.map((msg, index) => {
                const isUser = msg.sender === 'user'
                return (
                  <div
                    key={index}
                    style={{
                      display: 'flex',
                      flexDirection: isUser ? 'row-reverse' : 'row',
                      gap: '12px',
                      alignItems: 'flex-start',
                    }}
                  >
                    <div
                      style={{
                        width: '34px',
                        height: '34px',
                        borderRadius: '50%',
                        background: isUser ? '#3b82f6' : 'linear-gradient(135deg, #a855f7, #6366f1)',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        flexShrink: 0,
                        color: '#fff',
                        fontWeight: 'bold',
                        fontSize: '13px',
                      }}
                    >
                      {isUser ? 'ME' : <Sparkle size={18} weight="fill" />}
                    </div>

                    <div
                      style={{
                        maxWidth: '82%',
                        padding: '12px 16px',
                        borderRadius: isUser ? '16px 4px 16px 16px' : '4px 16px 16px 16px',
                        background: isUser ? 'rgba(59, 130, 246, 0.18)' : 'rgba(255,255,255,0.05)',
                        border: isUser ? '1px solid rgba(59, 130, 246, 0.35)' : '1px solid rgba(255,255,255,0.08)',
                        fontSize: '13px',
                        lineHeight: '1.6',
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-word',
                      }}
                    >
                      {msg.text}
                      <div
                        style={{
                          fontSize: '10px',
                          color: 'var(--text-muted, #777)',
                          marginTop: '6px',
                          display: 'flex',
                          justifyContent: isUser ? 'flex-end' : 'space-between',
                          gap: '10px',
                        }}
                      >
                        {!isUser && msg.provider && <span>引擎: {msg.provider}</span>}
                        <span>{msg.time}</span>
                      </div>
                    </div>
                  </div>
                )
              })}
              {chatLoading && (
                <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
                  <div
                    style={{
                      width: '34px',
                      height: '34px',
                      borderRadius: '50%',
                      background: 'linear-gradient(135deg, #a855f7, #6366f1)',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      color: '#fff',
                    }}
                  >
                    <Sparkle size={18} weight="fill" className="animate-spin" />
                  </div>
                  <div style={{ padding: '8px 14px', borderRadius: '12px', background: 'rgba(255,255,255,0.05)', fontSize: '13px', color: '#a855f7' }}>
                    ProbeWatch Copilot 正在推理全网指标中...
                  </div>
                </div>
              )}
            </div>

            {/* 输入发送栏 */}
            <form
              onSubmit={(e) => {
                e.preventDefault()
                handleSendMessage()
              }}
              style={{
                padding: '12px 16px',
                borderTop: '1px solid var(--border-color, rgba(255,255,255,0.08))',
                background: 'rgba(0,0,0,0.2)',
                display: 'flex',
                gap: '10px',
              }}
            >
              <input
                type="text"
                className="input"
                placeholder="询问关于全网丢包、骨干网延迟、闲置机器或成本审计的问题..."
                value={chatInput}
                onChange={(e) => setChatInput(e.target.value)}
                disabled={chatLoading}
                style={{ flex: 1 }}
              />
              <button
                type="submit"
                className="button button-primary"
                disabled={chatLoading || !chatInput.trim()}
                style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
              >
                <PaperPlaneRight size={16} weight="bold" />
                <span>发送</span>
              </button>
            </form>
          </div>
        </div>
      )}

      {/* Tab 3: MCP 客户端接入 (Claude & Cursor) */}
      {activeTab === 'mcp' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
          <div
            className="card"
            style={{
              padding: '24px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '8px' }}>
              <Broadcast size={20} className="text-purple-400" />
              <h2 style={{ fontSize: '16px', fontWeight: 'bold', margin: 0 }}>Model Context Protocol (MCP) 统一服务入口</h2>
            </div>
            <p style={{ fontSize: '13px', color: 'var(--text-muted, #888)', lineHeight: '1.6', margin: '0 0 20px 0' }}>
              ProbeWatch 原生兼容 Anthropic Model Context Protocol 规范（支持 Direct HTTP JSON-RPC 2.0 与 SSE 传输）。
              AI 客户端可在授权后以<strong>完全只读</strong>形式调取全集群硬件利用率、MTR 链路数据、闲置成本及根因诊断。
            </p>

            {/* MCP 服务端点 */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
              <div>
                <label style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--text-muted, #888)', display: 'block', marginBottom: '6px' }}>
                  MCP 代理服务地址 (含安全访问 Token)
                </label>
                <div style={{ display: 'flex', gap: '10px' }}>
                  <input
                    type="text"
                    readOnly
                    className="input font-mono"
                    value={mcpConfig?.endpoint || '正在生成...'}
                    style={{ flex: 1, fontSize: '13px' }}
                  />
                  <button
                    type="button"
                    className="button button-primary btn-sm"
                    onClick={() => copyToClipboard(mcpConfig?.endpoint, 'mcp-url')}
                    style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
                  >
                    {copiedId === 'mcp-url' ? <Check size={14} /> : <Copy size={14} />}
                    <span>{copiedId === 'mcp-url' ? '已复制' : '复制服务地址'}</span>
                  </button>
                </div>
              </div>

              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                  <label style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--text-muted, #888)' }}>
                    专用只读 MCP 访问 Token
                  </label>
                  <button
                    type="button"
                    className="button button-quiet btn-sm text-rose"
                    onClick={handleRegenerateToken}
                    disabled={regeneratingToken}
                    style={{ fontSize: '11px', padding: '2px 8px' }}
                  >
                    重新生成密钥
                  </button>
                </div>
                <div style={{ display: 'flex', gap: '10px' }}>
                  <input
                    type={showMcpToken ? 'text' : 'password'}
                    readOnly
                    className="input font-mono"
                    value={mcpConfig?.token || ''}
                    style={{ flex: 1, fontSize: '13px' }}
                  />
                  <button
                    type="button"
                    className="button button-quiet btn-sm"
                    onClick={() => setShowMcpToken(!showMcpToken)}
                  >
                    {showMcpToken ? <EyeSlash size={16} /> : <Eye size={16} />}
                  </button>
                  <button
                    type="button"
                    className="button button-quiet btn-sm"
                    onClick={() => copyToClipboard(mcpConfig?.token, 'mcp-token')}
                  >
                    {copiedId === 'mcp-token' ? <Check size={14} /> : <Copy size={14} />}
                  </button>
                </div>
              </div>
            </div>
          </div>

          {/* 客户端接入配置代码块 */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: '16px' }}>
            {/* Claude Desktop 配置 */}
            <div
              className="card"
              style={{
                padding: '20px',
                borderRadius: '16px',
                background: 'var(--card-bg, rgba(255,255,255,0.03))',
                border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
                <strong style={{ fontSize: '14px' }}>Claude Desktop (`claude_desktop_config.json`)</strong>
                <button
                  type="button"
                  className="button button-quiet btn-sm"
                  onClick={() => copyToClipboard(JSON.stringify(mcpConfig?.claude_config, null, 2), 'claude-cfg')}
                  style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
                >
                  {copiedId === 'claude-cfg' ? <Check size={14} className="text-emerald-500" /> : <Copy size={14} />}
                  <span>{copiedId === 'claude-cfg' ? '已复制' : '复制代码'}</span>
                </button>
              </div>
              <pre
                style={{
                  padding: '12px',
                  borderRadius: '10px',
                  background: 'rgba(0,0,0,0.25)',
                  fontSize: '12px',
                  lineHeight: '1.5',
                  fontFamily: 'monospace',
                  overflowX: 'auto',
                }}
              >
                {JSON.stringify(mcpConfig?.claude_config, null, 2)}
              </pre>
            </div>

            {/* Cursor IDE 配置 */}
            <div
              className="card"
              style={{
                padding: '20px',
                borderRadius: '16px',
                background: 'var(--card-bg, rgba(255,255,255,0.03))',
                border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
                <strong style={{ fontSize: '14px' }}>Cursor IDE (`.cursor/mcp.json`)</strong>
                <button
                  type="button"
                  className="button button-quiet btn-sm"
                  onClick={() => copyToClipboard(JSON.stringify(mcpConfig?.cursor_config, null, 2), 'cursor-cfg')}
                  style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
                >
                  {copiedId === 'cursor-cfg' ? <Check size={14} className="text-emerald-500" /> : <Copy size={14} />}
                  <span>{copiedId === 'cursor-cfg' ? '已复制' : '复制代码'}</span>
                </button>
              </div>
              <pre
                style={{
                  padding: '12px',
                  borderRadius: '10px',
                  background: 'rgba(0,0,0,0.25)',
                  fontSize: '12px',
                  lineHeight: '1.5',
                  fontFamily: 'monospace',
                  overflowX: 'auto',
                }}
              >
                {JSON.stringify(mcpConfig?.cursor_config, null, 2)}
              </pre>
            </div>
          </div>

          {/* 开放的 5 项只读 MCP 工具说明 */}
          <div
            className="card"
            style={{
              padding: '20px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
            }}
          >
            <h3 style={{ fontSize: '14px', fontWeight: 'bold', margin: '0 0 12px 0' }}>📦 开放的 5 项只读 MCP 工具能力</h3>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '10px' }}>
              {[
                { name: 'probewatch_get_overview', desc: '查询全站实时健康评分、在线服务器状态及活跃告警' },
                { name: 'probewatch_get_node_metrics', desc: '按节点提取 CPU、内存、磁盘利用率、流量及系统规格' },
                { name: 'probewatch_get_network_diagnosis', desc: '读取 MTR 回程路由逐跳延迟及丢包根因分析' },
                { name: 'probewatch_get_fleet_cost_audit', desc: '检索闲置 VPS、月度流量配额超限及到期续费清单' },
                { name: 'probewatch_run_ai_diagnosis', desc: '触发全站综合根因体检并输出 Executive 诊断 Markdown' },
              ].map((tool) => (
                <div
                  key={tool.name}
                  style={{
                    padding: '10px 14px',
                    borderRadius: '10px',
                    background: 'rgba(255,255,255,0.02)',
                    border: '1px solid var(--border-color, rgba(255,255,255,0.06))',
                  }}
                >
                  <code style={{ fontSize: '12px', color: '#c084fc', fontWeight: 'bold' }}>{tool.name}</code>
                  <p style={{ fontSize: '12px', color: 'var(--text-muted, #888)', margin: '4px 0 0 0' }}>{tool.desc}</p>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Tab 4: LLM 引擎配置 */}
      {activeTab === 'settings' && (
        <form onSubmit={handleSaveSettings} style={{ maxWidth: '680px' }}>
          <div
            className="card"
            style={{
              padding: '24px',
              borderRadius: '16px',
              background: 'var(--card-bg, rgba(255,255,255,0.03))',
              border: '1px solid var(--border-color, rgba(255,255,255,0.08))',
              display: 'flex',
              flexDirection: 'column',
              gap: '18px',
            }}
          >
            <div>
              <h2 style={{ fontSize: '16px', fontWeight: 'bold', margin: '0 0 4px 0' }}>LLM 深度推理大模型接入</h2>
              <p style={{ fontSize: '13px', color: 'var(--text-muted, #888)', margin: 0 }}>
                ProbeWatch 默认内置高效的本地启发式规则引擎（0 API 费用、100% 本地计算）。配置外部 LLM Key 后，Copilot 可提供深入的多节点因果链条推理。
              </p>
            </div>

            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', background: 'rgba(255,255,255,0.02)', borderRadius: '10px' }}>
              <div>
                <strong style={{ fontSize: '14px' }}>启用外部大模型深度问诊</strong>
                <p style={{ fontSize: '12px', color: 'var(--text-muted, #888)', margin: '2px 0 0 0' }}>
                  未开启或 API 耗尽时，系统将无缝回退至本地规则引擎。
                </p>
              </div>
              <input
                type="checkbox"
                checked={settings.enabled}
                onChange={(e) => setSettings({ ...settings, enabled: e.target.checked })}
                style={{ width: '18px', height: '18px', cursor: 'pointer' }}
              />
            </div>

            <div>
              <label style={{ fontSize: '12px', fontWeight: 'bold', display: 'block', marginBottom: '6px' }}>
                模型服务提供商 (Provider)
              </label>
              <select
                className="input"
                value={settings.provider}
                onChange={(e) => {
                  const prov = e.target.value
                  let ep = settings.api_endpoint
                  let mod = settings.model
                  if (prov === 'deepseek') {
                    ep = 'https://api.deepseek.com/v1'
                    mod = 'deepseek-chat'
                  } else if (prov === 'openai') {
                    ep = 'https://api.openai.com/v1'
                    mod = 'gpt-4o-mini'
                  } else if (prov === 'ollama') {
                    ep = 'http://localhost:11434/v1'
                    mod = 'llama3'
                  }
                  setSettings({ ...settings, provider: prov, api_endpoint: ep, model: mod })
                }}
              >
                <option value="local">内置本地启发式引擎 (Local Zero-Config · 免费)</option>
                <option value="deepseek">DeepSeek (性价比最高，推荐)</option>
                <option value="openai">OpenAI (GPT-4o / GPT-4o-mini)</option>
                <option value="ollama">Ollama (本地私有部署)</option>
                <option value="custom">自定义 OpenAI 兼容接口</option>
              </select>
            </div>

            {settings.provider !== 'local' && (
              <>
                <div>
                  <label style={{ fontSize: '12px', fontWeight: 'bold', display: 'block', marginBottom: '6px' }}>
                    API Endpoint 服务端点
                  </label>
                  <input
                    type="text"
                    className="input font-mono"
                    value={settings.api_endpoint}
                    onChange={(e) => setSettings({ ...settings, api_endpoint: e.target.value })}
                    placeholder="https://api.deepseek.com/v1"
                  />
                </div>

                <div>
                  <label style={{ fontSize: '12px', fontWeight: 'bold', display: 'block', marginBottom: '6px' }}>
                    API Key 访问密钥
                  </label>
                  <input
                    type="password"
                    className="input font-mono"
                    value={settings.api_key || ''}
                    onChange={(e) => setSettings({ ...settings, api_key: e.target.value })}
                    placeholder={settings.has_api_key ? '•••••••••••••••• (已保存，留空保持原样)' : 'sk-xxxxxxxx'}
                  />
                </div>

                <div>
                  <label style={{ fontSize: '12px', fontWeight: 'bold', display: 'block', marginBottom: '6px' }}>
                    模型名称 (Model)
                  </label>
                  <input
                    type="text"
                    className="input font-mono"
                    value={settings.model}
                    onChange={(e) => setSettings({ ...settings, model: e.target.value })}
                    placeholder="deepseek-chat"
                  />
                </div>
              </>
            )}

            <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginTop: '10px' }}>
              <button
                type="submit"
                className="button button-primary"
                disabled={settingsSaving}
              >
                {settingsSaving ? '正在保存...' : '保存 AI 设置'}
              </button>
              {settingsSuccess && (
                <span className="text-emerald-500 font-bold" style={{ fontSize: '13px', display: 'flex', alignItems: 'center', gap: '4px' }}>
                  <Check size={16} /> 设置已成功保存
                </span>
              )}
            </div>
          </div>
        </form>
      )}
    </div>
  )
}
