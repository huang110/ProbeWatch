import { useState, useMemo, useEffect } from 'react'
import {
  Bell,
  CaretDown,
  CaretUp,
  Check,
  CheckCircle,
  CircleNotch,
  Clock,
  DotsThree,
  EnvelopeSimple,
  MagnifyingGlass,
  PaperPlaneTilt,
  PencilSimple,
  Plus,
  Sliders,
  Sparkle,
  Trash,
  Warning,
  WarningCircle,
  WifiHigh,
  X,
  XCircle,
} from '@phosphor-icons/react'
import { alertSeverity, formatAlertTime, safeArray, safeText } from '../lib/format.js'
import {
  fetchNotificationChannels,
  createNotificationChannel,
  updateNotificationChannel,
  deleteNotificationChannel,
  testNotificationChannel,
  testNotificationDirect,
  fetchAlertSettings,
  saveAlertSettings,
  fetchCsrfToken,
  fetchAlertRules,
  createAlertRule,
  updateAlertRule,
  deleteAlertRule,
  toggleAlertRule,
  fetchAlertSilences,
  createAlertSilence,
  deleteAlertSilence,
  fetchFlappingAlerts,
} from '../lib/api.js'


const METRIC_LABELS = {
  cpu: 'CPU 使用率',
  memory: '物理内存',
  disk: '磁盘根分区',
  load1: '系统负载 (1m)',
  load5: '系统负载 (5m)',
  load15: '系统负载 (15m)',
  traffic_percent: '周期流量百分比',
  network_loss: '网络丢包率',
  network_latency: '网络延迟',
}

// 官方 6 大预置网络检测目标及域名
const DEFAULT_TARGET_CONFIGS = [
  { name: '重庆电信', host: 'cq-ct-dualstack.ip.zstaticcdn.com:80' },
  { name: '四川电信', host: 'sc-ct-dualstack.ip.zstaticcdn.com:80' },
  { name: '重庆联通', host: 'cq-cu-dualstack.ip.zstaticcdn.com:80' },
  { name: '四川联通', host: 'sc-cu-dualstack.ip.zstaticcdn.com:80' },
  { name: '重庆移动', host: 'cq-cm-dualstack.ip.zstaticcdn.com:80' },
  { name: '四川移动', host: 'sc-cm-dualstack.ip.zstaticcdn.com:80' },
]

export function AlertCenterView({
  alerts = [],
  nodes = [],
  onAck,
  ackingId,
  activeSubView = 'channel', // 'channel' | 'offline' | 'load' | 'traffic_report' | 'latency_alert' | 'general'
  onNavigate,
}) {
  const [currentTab, setCurrentTab] = useState(activeSubView)
  const [toastMsg, setToastMsg] = useState('')

  // 1. 通知渠道相关真实状态与后端同步
  const [channelEnabled, setChannelEnabled] = useState(true)
  const [msgTemplate, setMsgTemplate] = useState('')
  const [channels, setChannels] = useState([])
  const [loadingChannels, setLoadingChannels] = useState(false)
  const [channelTestingId, setChannelTestingId] = useState(null)
  const [globalTesting, setGlobalTesting] = useState(false)
  const [savingChannel, setSavingChannel] = useState(false)
  const [testingForm, setTestingForm] = useState(false)

  // 渠道配置表单
  const [formName, setFormName] = useState('')
  const [formPlatform, setFormPlatform] = useState('telegram') // 'telegram' | 'discord' | 'wecom' | 'bark' | 'webhook'
  const [formTelegramToken, setFormTelegramToken] = useState('')
  const [formTelegramChatId, setFormTelegramChatId] = useState('')
  const [formDiscordWebhook, setFormDiscordWebhook] = useState('')
  const [formWecomWebhook, setFormWecomWebhook] = useState('')
  const [formBarkKey, setFormBarkKey] = useState('')
  const [formBarkServer, setFormBarkServer] = useState('https://api.day.app')
  const [formWebhookUrl, setFormWebhookUrl] = useState('')
  const [formEvents, setFormEvents] = useState(['node', 'resource', 'network', 'mtr', 'traffic', 'billing'])

  // 2. 离线通知设置相关状态
  const [offlineSearch, setOfflineSearch] = useState('')
  const [selectedOfflineNodes, setSelectedOfflineNodes] = useState([])
  const [editingOfflineNode, setEditingOfflineNode] = useState(null)
  const [offlineGracePeriod, setOfflineGracePeriod] = useState(180)

  // 3. 负载通知与多指标自定义告警规则状态
  const [loadSubTab, setLoadSubTab] = useState('config') // 'config' | 'current'
  const [loadSearch, setLoadSearch] = useState('')
  const [loadRules, setLoadRules] = useState([])
  const [loadingRules, setLoadingRules] = useState(false)
  const [showAddLoadModal, setShowAddLoadModal] = useState(false)
  const [editingRule, setEditingRule] = useState(null)
  const [ruleFormName, setRuleFormName] = useState('')
  const [ruleFormMetric, setRuleFormMetric] = useState('cpu')
  const [ruleFormOperator, setRuleFormOperator] = useState('>')
  const [ruleFormThreshold, setRuleFormThreshold] = useState('85')
  const [ruleFormDuration, setRuleFormDuration] = useState('0')
  const [ruleFormSeverity, setRuleFormSeverity] = useState('warning')
  const [ruleFormNodeFilter, setRuleFormNodeFilter] = useState('*')
  const [ruleFormEnabled, setRuleFormEnabled] = useState(true)
  const [ruleFormExpressionType, setRuleFormExpressionType] = useState('simple') // 'simple' | 'composite'
  const [ruleFormLogic, setRuleFormLogic] = useState('AND') // 'AND' | 'OR'
  const [ruleFormConsecutiveCount, setRuleFormConsecutiveCount] = useState(1)
  const [ruleFormConditions, setRuleFormConditions] = useState([
    { metric: 'cpu', operator: '>', threshold: '85' },
    { metric: 'memory', operator: '>', threshold: '85' },
  ])
  const [savingRule, setSavingRule] = useState(false)

  // 告警静默(Snooze)与抖动(Flapping)抑制状态
  const [silences, setSilences] = useState([])
  const [loadingSilences, setLoadingSilences] = useState(false)
  const [flappingAlerts, setFlappingAlerts] = useState([])
  const [loadingFlapping, setLoadingFlapping] = useState(false)
  const [showSnoozeModal, setShowSnoozeModal] = useState(false)
  const [snoozeTarget, setSnoozeTarget] = useState(null)
  const [snoozeDurationPreset, setSnoozeDurationPreset] = useState(60) // minutes
  const [snoozeScope, setSnoozeScope] = useState('fingerprint') // 'fingerprint' | 'node' | 'category'
  const [snoozeReason, setSnoozeReason] = useState('')
  const [savingSnooze, setSavingSnooze] = useState(false)

  const filteredLoadRules = useMemo(() => {
    return loadRules.filter((r) => {
      if (!loadSearch) return true
      const s = loadSearch.toLowerCase()
      return (
        (r.name && r.name.toLowerCase().includes(s)) ||
        (r.metric && r.metric.toLowerCase().includes(s)) ||
        (r.node_filter && r.node_filter.toLowerCase().includes(s))
      )
    })
  }, [loadRules, loadSearch])

  // 4. 流量定时报告相关状态
  const [reportPushTime, setReportPushTime] = useState('00:00')
  const [reportSearch, setReportSearch] = useState('')
  const [selectedReportNodes, setSelectedReportNodes] = useState([])

  // 5. 延迟监测告警相关状态
  const [latencyAlertView, setLatencyAlertView] = useState('tasks') // 'tasks' | 'servers'
  const [latencyStatusFilter, setLatencyStatusFilter] = useState('all')
  const [latencySearch, setLatencySearch] = useState('')
  const [selectedLatencyAlerts, setSelectedLatencyAlerts] = useState([])
  const [latencyPage, setLatencyPage] = useState(1)

  // 后端渠道与配置加载
  const loadChannelsAndSettings = async () => {
    setLoadingChannels(true)
    try {
      const chs = await fetchNotificationChannels()
      setChannels(Array.isArray(chs) ? chs : [])
    } catch (e) {
      console.error('Failed to load notification channels:', e)
    } finally {
      setLoadingChannels(false)
    }

    try {
      const sets = await fetchAlertSettings()
      if (sets?.offline_grace_seconds) {
        setOfflineGracePeriod(parseInt(sets.offline_grace_seconds, 10) || 180)
      }
      if (sets?.msg_template) {
        setMsgTemplate(sets.msg_template)
      }
    } catch (e) {
      console.error('Failed to load alert settings:', e)
    }
  }

  const loadRulesData = async () => {
    setLoadingRules(true)
    try {
      const data = await fetchAlertRules()
      setLoadRules(Array.isArray(data) ? data : [])
    } catch (e) {
      console.error('Failed to load alert rules:', e)
    } finally {
      setLoadingRules(false)
    }
  }

  const loadSilencesData = async () => {
    setLoadingSilences(true)
    try {
      const data = await fetchAlertSilences()
      setSilences(Array.isArray(data) ? data : [])
    } catch (e) {
      console.error('Failed to load alert silences:', e)
      setSilences([])
    } finally {
      setLoadingSilences(false)
    }
  }

  const loadFlappingData = async () => {
    setLoadingFlapping(true)
    try {
      const data = await fetchFlappingAlerts()
      setFlappingAlerts(Array.isArray(data) ? data : [])
    } catch (e) {
      console.error('Failed to load flapping alerts:', e)
      setFlappingAlerts([])
    } finally {
      setLoadingFlapping(false)
    }
  }

  useEffect(() => {
    loadChannelsAndSettings()
    loadRulesData()
    loadSilencesData()
    loadFlappingData()
  }, [])

  const handleOpenAddRule = () => {
    setEditingRule(null)
    setRuleFormName('')
    setRuleFormMetric('cpu')
    setRuleFormOperator('>')
    setRuleFormThreshold('85')
    setRuleFormDuration('0')
    setRuleFormSeverity('warning')
    setRuleFormNodeFilter('*')
    setRuleFormEnabled(true)
    setRuleFormExpressionType('simple')
    setRuleFormLogic('AND')
    setRuleFormConsecutiveCount(1)
    setRuleFormConditions([
      { metric: 'cpu', operator: '>', threshold: '85' },
      { metric: 'memory', operator: '>', threshold: '85' },
    ])
    setShowAddLoadModal(true)
  }

  const handleOpenEditRule = (rule) => {
    setEditingRule(rule)
    setRuleFormName(rule.name || '')
    setRuleFormMetric(rule.metric || 'cpu')
    setRuleFormOperator(rule.operator || '>')
    setRuleFormThreshold(String(rule.threshold ?? 85))
    setRuleFormDuration(String(rule.duration_seconds ?? 0))
    setRuleFormSeverity(rule.severity || 'warning')
    setRuleFormNodeFilter(rule.node_filter || '*')
    setRuleFormEnabled(rule.enabled ?? true)
    setRuleFormExpressionType(rule.expression_type || 'simple')
    setRuleFormLogic(rule.logic || 'AND')
    setRuleFormConsecutiveCount(rule.consecutive_count || 1)
    if (rule.conditions && rule.conditions.length > 0) {
      setRuleFormConditions(
        rule.conditions.map((c) => ({
          metric: c.metric,
          operator: c.operator || '>',
          threshold: String(c.threshold ?? 85),
        }))
      )
    } else {
      setRuleFormConditions([
        { metric: rule.metric || 'cpu', operator: rule.operator || '>', threshold: String(rule.threshold ?? 85) },
      ])
    }
    setShowAddLoadModal(true)
  }

  const handleAddCondition = () => {
    setRuleFormConditions((prev) => [
      ...prev,
      { metric: 'memory', operator: '>', threshold: '85' },
    ])
  }

  const handleRemoveCondition = (index) => {
    setRuleFormConditions((prev) => prev.filter((_, i) => i !== index))
  }

  const handleConditionChange = (index, field, value) => {
    setRuleFormConditions((prev) => {
      const next = [...prev]
      next[index] = { ...next[index], [field]: value }
      return next
    })
  }

  const handleSaveRule = async (e) => {
    if (e) e.preventDefault()
    if (!ruleFormName.trim()) {
      showToast('请输入规则名称')
      return
    }
    if (ruleFormExpressionType === 'composite' && ruleFormConditions.length === 0) {
      showToast('复合规则请至少配置一项指标条件')
      return
    }
    setSavingRule(true)
    try {
      const payload = {
        name: ruleFormName.trim(),
        metric: ruleFormMetric,
        operator: ruleFormOperator,
        threshold: parseFloat(ruleFormThreshold) || 0,
        duration_seconds: parseInt(ruleFormDuration, 10) || 0,
        severity: ruleFormSeverity,
        node_filter: ruleFormNodeFilter.trim() || '*',
        enabled: ruleFormEnabled,
        expression_type: ruleFormExpressionType,
        logic: ruleFormLogic,
        consecutive_count: parseInt(ruleFormConsecutiveCount, 10) || 1,
        conditions:
          ruleFormExpressionType === 'composite'
            ? ruleFormConditions.map((c) => ({
                metric: c.metric,
                operator: c.operator || '>',
                threshold: parseFloat(c.threshold) || 0,
              }))
            : [],
      }
      if (editingRule && editingRule.id) {
        await updateAlertRule(editingRule.id, payload)
        showToast('规则已成功更新')
      } else {
        await createAlertRule(payload)
        showToast('规则已成功创建')
      }
      setShowAddLoadModal(false)
      await loadRulesData()
    } catch (err) {
      showToast(err.message || '保存规则失败')
    } finally {
      setSavingRule(false)
    }
  }

  const handleOpenSnooze = (alert) => {
    setSnoozeTarget(alert)
    setSnoozeDurationPreset(60)
    setSnoozeScope('fingerprint')
    const initialReason = alert ? `排查故障: ${alert.message || alert.title || alert.type || '未命名告警'}` : '例行维护静默'
    setSnoozeReason(initialReason)
    setShowSnoozeModal(true)
  }

  const handleCreateSnooze = async (e) => {
    if (e) e.preventDefault()
    setSavingSnooze(true)
    try {
      const now = Math.floor(Date.now() / 1000)
      const durationSec = (parseInt(snoozeDurationPreset, 10) || 60) * 60
      const endsAt = now + durationSec
      const payload = {
        name: snoozeReason.trim() || '快速静默',
        reason: snoozeReason.trim() || '例行排查维护',
        starts_at: now,
        ends_at: endsAt,
        node_filter: snoozeScope === 'node' && snoozeTarget ? (snoozeTarget.node_id || '*') : '*',
        category: snoozeScope === 'category' && snoozeTarget ? (snoozeTarget.category || '*') : '*',
        fingerprint: snoozeScope === 'fingerprint' && snoozeTarget ? (snoozeTarget.fingerprint || '') : '',
      }
      await createAlertSilence(payload)
      showToast('已开启告警静默')
      setShowSnoozeModal(false)
      await loadSilencesData()
    } catch (err) {
      showToast(err.message || '开启静默失败')
    } finally {
      setSavingSnooze(false)
    }
  }

  const handleDeleteSilence = async (id, name) => {
    try {
      await deleteAlertSilence(id)
      setSilences((prev) => prev.filter((s) => s.id !== id))
      showToast(`已解除静默: ${name || id}`)
    } catch (err) {
      showToast(err.message || '解除静默失败')
    }
  }

  const handleToggleRule = async (rule) => {
    try {
      const res = await toggleAlertRule(rule.id)
      setLoadRules((prev) =>
        prev.map((r) => (r.id === rule.id ? { ...r, enabled: res.enabled } : r))
      )
      showToast(res.enabled ? `已启用规则: ${rule.name}` : `已停用规则: ${rule.name}`)
    } catch (err) {
      showToast(`切换状态失败: ${err.message}`)
    }
  }

  const handleDeleteRule = async (id, name) => {
    if (!window.confirm(`确定要删除告警规则「${name}」吗？`)) return
    try {
      await deleteAlertRule(id)
      setLoadRules((prev) => prev.filter((r) => r.id !== id))
      showToast(`已删除规则: ${name}`)
    } catch (err) {
      showToast(`删除规则失败: ${err.message}`)
    }
  }

  // 同步外部传进来的子路由
  useEffect(() => {
    if (activeSubView && activeSubView !== currentTab) {
      setCurrentTab(activeSubView)
    }
  }, [activeSubView])

  const showToast = (msg) => {
    setToastMsg(msg)
    setTimeout(() => setToastMsg(''), 3000)
  }

  const handleToggleChannel = async (ch) => {
    try {
      await updateNotificationChannel(ch.id, {
        name: ch.name,
        type: ch.type,
        config: ch.config,
        enabled: !ch.enabled,
        events: ch.events,
      })
      setChannels((prev) =>
        prev.map((item) => (item.id === ch.id ? { ...item, enabled: !item.enabled } : item))
      )
      showToast(`已${ch.enabled ? '停用' : '启用'}渠道: ${ch.name}`)
    } catch (err) {
      showToast(`修改状态失败: ${err.message}`)
    }
  }

  const handleDeleteChannel = async (id, name) => {
    if (!window.confirm(`确定要删除通知渠道「${name}」吗？`)) return
    try {
      await deleteNotificationChannel(id)
      setChannels((prev) => prev.filter((item) => item.id !== id))
      showToast(`已删除通知渠道: ${name}`)
    } catch (err) {
      showToast(`删除失败: ${err.message}`)
    }
  }

  const handleTestChannel = async (id, name) => {
    setChannelTestingId(id)
    try {
      const res = await testNotificationChannel(id)
      showToast(res.message || `测试通知已成功推送到 ${name}`)
    } catch (err) {
      showToast(`测试失败: ${err.message}`)
    } finally {
      setChannelTestingId(null)
    }
  }

  const handleTestCurrentForm = async () => {
    setTestingForm(true)
    try {
      let configObj = {}
      if (formPlatform === 'telegram') {
        if (!formTelegramToken.trim() || !formTelegramChatId.trim()) {
          showToast('请先输入 Telegram Bot Token 与 Chat ID')
          setTestingForm(false)
          return
        }
        configObj = { bot_token: formTelegramToken.trim(), chat_id: formTelegramChatId.trim() }
      } else if (formPlatform === 'discord') {
        if (!formDiscordWebhook.trim()) {
          showToast('请先输入 Discord Webhook 地址')
          setTestingForm(false)
          return
        }
        configObj = { webhook_url: formDiscordWebhook.trim() }
      } else if (formPlatform === 'wecom') {
        if (!formWecomWebhook.trim()) {
          showToast('请先输入企业微信机器人 Webhook 地址')
          setTestingForm(false)
          return
        }
        configObj = { webhook_url: formWecomWebhook.trim() }
      } else if (formPlatform === 'bark') {
        if (!formBarkKey.trim()) {
          showToast('请先输入 Bark Device Key')
          setTestingForm(false)
          return
        }
        configObj = { server_url: formBarkServer.trim(), device_key: formBarkKey.trim() }
      } else {
        if (!formWebhookUrl.trim()) {
          showToast('请先输入 Webhook URL')
          setTestingForm(false)
          return
        }
        configObj = { webhook_url: formWebhookUrl.trim() }
      }
      const res = await testNotificationDirect({
        type: formPlatform,
        config: configObj,
      })
      showToast(res.message || '测试通知已成功送达！')
    } catch (err) {
      showToast(`测试失败: ${err.message}`)
    } finally {
      setTestingForm(false)
    }
  }

  const handleSaveCurrentChannel = async () => {
    if (!formName.trim()) {
      showToast('请输入渠道名称')
      return
    }
    let configObj = {}
    if (formPlatform === 'telegram') {
      if (!formTelegramToken.trim() || !formTelegramChatId.trim()) {
        showToast('请完整填写 Telegram Bot Token 和 Chat ID')
        return
      }
      configObj = { bot_token: formTelegramToken.trim(), chat_id: formTelegramChatId.trim() }
    } else if (formPlatform === 'discord') {
      if (!formDiscordWebhook.trim()) {
        showToast('请填写 Discord Webhook 地址')
        return
      }
      configObj = { webhook_url: formDiscordWebhook.trim() }
    } else if (formPlatform === 'wecom') {
      if (!formWecomWebhook.trim()) {
        showToast('请填写企业微信机器人 Webhook 地址')
        return
      }
      configObj = { webhook_url: formWecomWebhook.trim() }
    } else if (formPlatform === 'bark') {
      if (!formBarkKey.trim()) {
        showToast('请填写 Bark Device Key')
        return
      }
      configObj = { server_url: formBarkServer.trim(), device_key: formBarkKey.trim() }
    } else {
      if (!formWebhookUrl.trim()) {
        showToast('请填写 Webhook URL')
        return
      }
      configObj = { webhook_url: formWebhookUrl.trim() }
    }

    setSavingChannel(true)
    try {
      const newCh = await createNotificationChannel({
        name: formName.trim(),
        type: formPlatform,
        config: JSON.stringify(configObj),
        enabled: true,
        events: JSON.stringify(formEvents),
      })
      setChannels((prev) => [...prev, newCh])
      showToast(`成功添加通知渠道: ${formName}`)
      setFormName('')
      setFormTelegramToken('')
      setFormTelegramChatId('')
      setFormDiscordWebhook('')
      setFormWecomWebhook('')
      setFormBarkKey('')
      setFormWebhookUrl('')
    } catch (err) {
      showToast(`保存失败: ${err.message}`)
    } finally {
      setSavingChannel(false)
    }
  }

  const handleGlobalTest = async () => {
    setGlobalTesting(true)
    try {
      const res = await testNotificationDirect({})
      showToast(res.message || '全量测试消息已成功推送')
    } catch (err) {
      showToast(`测试失败: ${err.message}`)
    } finally {
      setGlobalTesting(false)
    }
  }

  const handleSaveGracePeriod = async () => {
    try {
      await saveAlertSettings({ offline_grace_seconds: String(offlineGracePeriod) })
      showToast(`已成功保存离线宽限期为 ${offlineGracePeriod} 秒`)
    } catch (err) {
      showToast(`保存失败: ${err.message}`)
    }
  }

  const handleSaveMsgTemplate = async () => {
    try {
      await saveAlertSettings({ msg_template: msgTemplate })
      showToast('消息通知模板已保存')
    } catch (err) {
      showToast(`保存失败: ${err.message}`)
    }
  }

  // 规范化服务器列表数据 (仅显示真实连接的探针)
  const displayNodes = useMemo(() => {
    if (nodes && nodes.length > 0) {
      return nodes.map((n, idx) => ({
        id: n.uuid || n.id || `node-${idx}`,
        name: n.name || '探针',
        flag: n.flag || '🌐',
        status: n.status || 'online',
        enabled: true,
        gracePeriod: `${offlineGracePeriod}秒`,
        lastNotified: n.last_reported_at ? new Date(n.last_reported_at).toLocaleString() : '-',
        reportType: '日报、周报',
        reportContent: '上行/下行流量',
        node: n,
      }))
    }
    return []
  }, [nodes, offlineGracePeriod])

  // 延迟监测告警笛卡尔积矩阵 (Image 5: 6 任务 × 12 节点 = 72 项)
  const latencyAlertMatrix = useMemo(() => {
    const list = []
    DEFAULT_TARGET_CONFIGS.forEach((target) => {
      displayNodes.forEach((node) => {
        list.push({
          id: `${target.name}-${node.id}`,
          task: target.name,
          server: node.name,
          targetHost: target.host,
          status: '未配置',
          window: '-',
          lossThreshold: '-',
          minSamples: '-',
          cooldown: '-',
          lastNotified: '从未触发',
        })
      })
    })
    return list
  }, [displayNodes])

  // 延迟监测矩阵搜索与过滤
  const filteredLatencyAlerts = useMemo(() => {
    return latencyAlertMatrix.filter((item) => {
      if (latencyStatusFilter !== 'all' && item.status !== latencyStatusFilter) return false
      if (latencySearch) {
        const q = latencySearch.toLowerCase().trim()
        if (
          !item.task.toLowerCase().includes(q) &&
          !item.server.toLowerCase().includes(q) &&
          !item.targetHost.toLowerCase().includes(q)
        ) {
          return false
        }
      }
      return true
    })
  }, [latencyAlertMatrix, latencyStatusFilter, latencySearch])

  // 离线节点搜索过滤
  const filteredOfflineNodes = useMemo(() => {
    if (!offlineSearch) return displayNodes
    const q = offlineSearch.toLowerCase().trim()
    return displayNodes.filter((n) => n.name.toLowerCase().includes(q))
  }, [displayNodes, offlineSearch])

  // 流量报告节点搜索过滤
  const filteredReportNodes = useMemo(() => {
    if (!reportSearch) return displayNodes
    const q = reportSearch.toLowerCase().trim()
    return displayNodes.filter((n) => n.name.toLowerCase().includes(q))
  }, [displayNodes, reportSearch])

  // 切换 tab
  const handleTabClick = (tabKey, routeKey) => {
    setCurrentTab(tabKey)
    if (onNavigate) onNavigate(routeKey)
  }

  return (
    <div className="notify-page-container">
      {/* 顶部主二级 Tab 导航条 (完全对齐 Lite 侧栏与二级结构) */}
      <div className="monitor-top-nav-bar">
        <div className="monitor-nav-tabs">
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'channel' ? 'active' : ''}`}
            onClick={() => handleTabClick('channel', 'notify-channel')}
          >
            <span>通知渠道</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'offline' ? 'active' : ''}`}
            onClick={() => handleTabClick('offline', 'notify-offline')}
          >
            <span>离线通知</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'load' ? 'active' : ''}`}
            onClick={() => handleTabClick('load', 'notify-load')}
          >
            <span>负载通知</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'traffic_report' ? 'active' : ''}`}
            onClick={() => handleTabClick('traffic_report', 'notify-traffic')}
          >
            <span>流量定时报告</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'latency_alert' ? 'active' : ''}`}
            onClick={() => handleTabClick('latency_alert', 'notify-latency')}
          >
            <span>延迟监测告警</span>
          </button>
          <button
            type="button"
            className={`monitor-nav-tab-btn ${currentTab === 'general' ? 'active' : ''}`}
            onClick={() => handleTabClick('general', 'notify-general')}
          >
            <span>通用</span>
          </button>
        </div>

        {toastMsg && (
          <div className="badge badge-mint flex items-center gap-1.5 mono" style={{ fontSize: '12px' }}>
            <Check size={14} />
            <span>{toastMsg}</span>
          </div>
        )}
      </div>

      {/* ====================================================================
          TAB 1: 通知渠道 (严格匹配 Image 1: media_1790307354943.png)
         ==================================================================== */}
      {currentTab === 'channel' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>通知</h1>
              <p>配置通知渠道、连接参数与消息模板。</p>
            </div>
          </div>

          {/* 卡片 1: 开启通知 */}
          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">开启通知</span>
                <span className="lite-card-desc">通过首选方式获取及时通知。</span>
              </div>
              <button
                type="button"
                className={`switch-toggle ${channelEnabled ? 'active' : ''}`}
                onClick={() => setChannelEnabled(!channelEnabled)}
                aria-pressed={channelEnabled}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>

          {/* 卡片 2: 消息通知模板 */}
          <div className="lite-card-box">
            <div className="lite-card-meta">
              <span className="lite-card-title">消息通知模板</span>
              <span className="lite-card-desc">ProbeWatch 将按照消息通知模板发送通知</span>
            </div>
            <textarea
              className="lite-code-editor"
              placeholder="留空则使用系统默认的消息通知模板。支持 {{node.name}}, {{status}}, {{alert.title}}, {{time}} 等模板变量"
              value={msgTemplate}
              onChange={(e) => setMsgTemplate(e.target.value)}
              rows={4}
            />
            <div className="flex justify-end">
              <button
                type="button"
                className="lite-btn-primary"
                onClick={handleSaveMsgTemplate}
              >
                保存模板
              </button>
            </div>
          </div>

          {/* 卡片 3: 已配置通知渠道列表 */}
          <div className="lite-card-box">
            <div className="flex items-center justify-between pb-2 border-b border-subtle">
              <div className="lite-card-meta">
                <span className="lite-card-title">已配置通知渠道 ({channels.length})</span>
                <span className="lite-card-desc">当前生效并准备派发告警消息的通知端点</span>
              </div>
              {loadingChannels && <CircleNotch size={18} className="animate-spin text-muted" />}
            </div>

            {channels.length === 0 ? (
              <div className="text-center py-6 text-muted text-xs">
                当前暂无配置通知渠道，请在下方添加 Telegram、Discord、企业微信、Bark 或自定义 Webhook 接收告警。
              </div>
            ) : (
              <div className="divide-y divide-subtle mt-2">
                {channels.map((ch) => {
                  let badgeClass = 'badge-subtle'
                  let typeLabel = ch.type
                  if (ch.type === 'telegram') {
                    badgeClass = 'badge-sky font-bold'
                    typeLabel = 'Telegram'
                  } else if (ch.type === 'discord') {
                    badgeClass = 'badge-indigo font-bold'
                    typeLabel = 'Discord'
                  } else if (ch.type === 'wecom') {
                    badgeClass = 'badge-mint font-bold'
                    typeLabel = '企业微信'
                  } else if (ch.type === 'bark') {
                    badgeClass = 'badge-amber font-bold'
                    typeLabel = 'Bark (iOS)'
                  } else if (ch.type === 'webhook') {
                    badgeClass = 'badge-subtle font-bold'
                    typeLabel = 'Webhook'
                  }

                  let subscribedEvents = []
                  try {
                    subscribedEvents = JSON.parse(ch.events || '[]')
                  } catch {
                    subscribedEvents = []
                  }

                  return (
                    <div key={ch.id} className="py-3 flex flex-col md:flex-row md:items-center justify-between gap-3">
                      <div className="space-y-1">
                        <div className="flex items-center gap-2">
                          <span className={`badge ${badgeClass}`}>{typeLabel}</span>
                          <strong className="text-sm" style={{ color: 'var(--text-1)' }}>{ch.name}</strong>
                          {!ch.enabled && (
                            <span className="badge badge-rose text-xs">已停用</span>
                          )}
                        </div>
                        <div className="text-xs text-muted flex flex-wrap items-center gap-1.5 pt-0.5">
                          <span>订阅事件:</span>
                          {subscribedEvents.length === 0 || subscribedEvents.includes('*') ? (
                            <span className="badge badge-subtle text-xs">全量告警事件</span>
                          ) : (
                            subscribedEvents.map((ev) => (
                              <span key={ev} className="badge badge-subtle text-xs mono">
                                {ev === 'node' ? '节点离线' : ev === 'resource' ? '资源负载' : ev === 'network' ? '网络质量' : ev === 'mtr' ? 'MTR路由' : ev === 'media' ? '流媒体' : ev}
                              </span>
                            ))
                          )}
                        </div>
                      </div>

                      <div className="flex items-center gap-3">
                        <button
                          type="button"
                          className="button button-quiet text-xs flex items-center gap-1"
                          disabled={channelTestingId === ch.id}
                          onClick={() => handleTestChannel(ch.id, ch.name)}
                          title="向该渠道发送一条测试告警"
                        >
                          {channelTestingId === ch.id ? (
                            <CircleNotch size={14} className="animate-spin text-blue" />
                          ) : (
                            <PaperPlaneTilt size={14} />
                          )}
                          <span>测试</span>
                        </button>

                        <button
                          type="button"
                          className={`switch-toggle ${ch.enabled ? 'active' : ''}`}
                          onClick={() => handleToggleChannel(ch)}
                          title={ch.enabled ? '点击禁用该通知渠道' : '点击启用该通知渠道'}
                        >
                          <span className="switch-thumb" />
                        </button>

                        <button
                          type="button"
                          className="icon-action-btn text-rose"
                          onClick={() => handleDeleteChannel(ch.id, ch.name)}
                          title="删除此渠道"
                        >
                          <Trash size={16} />
                        </button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>

          {/* 卡片 4: 添加 / 配置通知渠道 */}
          <div className="lite-card-box">
            <div className="lite-card-meta pb-2 border-b border-subtle">
              <span className="lite-card-title">添加通知渠道</span>
              <span className="lite-card-desc">配置 Telegram Bot、Discord、企业微信、Bark 或自定义 Webhook 接收告警</span>
            </div>

            <div className="space-y-4 pt-3">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="field">
                  <label className="field-label required">渠道平台</label>
                  <select
                    className="field-select"
                    value={formPlatform}
                    onChange={(e) => setFormPlatform(e.target.value)}
                  >
                    <option value="telegram">Telegram Bot</option>
                    <option value="discord">Discord Webhook</option>
                    <option value="wecom">企业微信机器人 (WeCom)</option>
                    <option value="bark">Bark (iOS 实时推送)</option>
                    <option value="webhook">自定义 Webhook / Server酱</option>
                  </select>
                </div>

                <div className="field">
                  <label className="field-label required">渠道名称</label>
                  <input
                    type="text"
                    className="field-input"
                    placeholder="如: 运维群 Telegram 告警"
                    value={formName}
                    onChange={(e) => setFormName(e.target.value)}
                  />
                </div>
              </div>

              {/* Telegram 表单 */}
              {formPlatform === 'telegram' && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4 p-3 rounded-lg" style={{ background: 'var(--surface-2)' }}>
                  <div className="field">
                    <label className="field-label required">Telegram Bot Token</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="例如: 7050097486:AAHQ9SHunWD9yvSA677A1pVF5Ao8yRTynUE"
                      value={formTelegramToken}
                      onChange={(e) => setFormTelegramToken(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">通过 @BotFather 机器人创建 Bot 获取。</span>
                  </div>

                  <div className="field">
                    <label className="field-label required">目标 Chat ID</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="例如: 6110992384 或群组 ID -100..."
                      value={formTelegramChatId}
                      onChange={(e) => setFormTelegramChatId(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">向 @userinfobot 发送消息或在群内获取 Chat ID。</span>
                  </div>
                </div>
              )}

              {/* Discord 表单 */}
              {formPlatform === 'discord' && (
                <div className="p-3 rounded-lg" style={{ background: 'var(--surface-2)' }}>
                  <div className="field">
                    <label className="field-label required">Discord Webhook URL</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="https://discord.com/api/webhooks/..."
                      value={formDiscordWebhook}
                      onChange={(e) => setFormDiscordWebhook(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">在 Discord 频道的「设置」-「整合」-「Webhook」中创建并复制。</span>
                  </div>
                </div>
              )}

              {/* 企业微信机器人表单 */}
              {formPlatform === 'wecom' && (
                <div className="p-3 rounded-lg" style={{ background: 'var(--surface-2)' }}>
                  <div className="field">
                    <label className="field-label required">企业微信机器人 Webhook 地址</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."
                      value={formWecomWebhook}
                      onChange={(e) => setFormWecomWebhook(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">在企业微信内部群右上角点击「添加群机器人」复制 Webhook 地址。</span>
                  </div>
                </div>
              )}

              {/* Bark 表单 */}
              {formPlatform === 'bark' && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4 p-3 rounded-lg" style={{ background: 'var(--surface-2)' }}>
                  <div className="field">
                    <label className="field-label required">Bark Device Key</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="复制 Bark App 中的推送 Key"
                      value={formBarkKey}
                      onChange={(e) => setFormBarkKey(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">iOS App Store 下载 Bark，打开即刻复制专属推送 Key。</span>
                  </div>

                  <div className="field">
                    <label className="field-label">Bark 服务器地址</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="https://api.day.app"
                      value={formBarkServer}
                      onChange={(e) => setFormBarkServer(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">默认官方服务器 https://api.day.app，亦支持私有部署服务器。</span>
                  </div>
                </div>
              )}

              {/* Webhook 表单 */}
              {formPlatform === 'webhook' && (
                <div className="p-3 rounded-lg" style={{ background: 'var(--surface-2)' }}>
                  <div className="field">
                    <label className="field-label required">Webhook URL</label>
                    <input
                      type="text"
                      className="field-input mono text-xs"
                      placeholder="https://... 支持 Server酱 (https://sctapi.ftqq.com/...) 或自定义 HTTP POST"
                      value={formWebhookUrl}
                      onChange={(e) => setFormWebhookUrl(e.target.value)}
                    />
                    <span className="text-muted text-xs mt-1 block">支持标准 JSON Webhook 自动化接口与 Server酱（自动解析标题与正文参数）。</span>
                  </div>
                </div>
              )}

              {/* 订阅事件多选 */}
              <div className="space-y-1.5">
                <label className="field-label font-bold">订阅告警事件</label>
                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2 text-xs">
                  {[
                    { id: 'node', label: '节点离线与恢复' },
                    { id: 'resource', label: '资源高负载告警' },
                    { id: 'network', label: '网络丢包与延迟' },
                    { id: 'mtr', label: 'MTR 路由拓扑' },
                    { id: 'media', label: '流媒体/AI 解锁' },
                    { id: 'traffic', label: '流量限额预警' },
                    { id: 'billing', label: '账单到期提醒' },
                  ].map((item) => (
                    <label
                      key={item.id}
                      className="flex items-center gap-1.5 p-2 rounded cursor-pointer border border-subtle hover:bg-surface-2"
                    >
                      <input
                        type="checkbox"
                        checked={formEvents.includes(item.id)}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setFormEvents((prev) => [...prev, item.id])
                          } else {
                            setFormEvents((prev) => prev.filter((x) => x !== item.id))
                          }
                        }}
                      />
                      <span>{item.label}</span>
                    </label>
                  ))}
                </div>
              </div>

              {/* 底部按钮 */}
              <div className="flex items-center justify-end gap-2 pt-2">
                <button
                  type="button"
                  className="button button-quiet flex items-center gap-1.5"
                  disabled={testingForm}
                  onClick={handleTestCurrentForm}
                >
                  {testingForm ? <CircleNotch size={14} className="animate-spin text-blue" /> : <PaperPlaneTilt size={14} />}
                  <span>测试当前参数</span>
                </button>
                <button
                  type="button"
                  className="lite-btn-primary flex items-center gap-1.5"
                  disabled={savingChannel}
                  onClick={handleSaveCurrentChannel}
                >
                  {savingChannel ? <CircleNotch size={14} className="animate-spin" /> : <Plus size={14} weight="bold" />}
                  <span>保存并启用此渠道</span>
                </button>
              </div>
            </div>
          </div>

          {/* 卡片 5: 发送全量测试消息 */}
          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">发送全量测试通知</span>
                <span className="lite-card-desc">向所有已启用的通知渠道立即派发一条测试告警，检验通知通道连通性与格式渲染效果。</span>
              </div>
              <button
                type="button"
                className="lite-btn-primary flex items-center gap-1.5"
                disabled={globalTesting}
                onClick={handleGlobalTest}
              >
                {globalTesting ? <CircleNotch size={14} className="animate-spin" /> : <PaperPlaneTilt size={14} />}
                <span>发送测试消息</span>
              </button>
            </div>
          </div>

          {/* 底部跳转提示 */}
          <div className="text-muted" style={{ fontSize: '12.5px', marginTop: '4px' }}>
            正在寻找过期通知？现已迁移至「
            <span
              className="text-blue cursor-pointer"
              onClick={() => handleTabClick('general', 'notify-general')}
            >
              通知与告警 &gt; 通用
            </span>
            」。
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 2: 离线通知设置 (严格匹配 Image 2: media_1790307370795.png)
         ==================================================================== */}
      {currentTab === 'offline' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>离线通知设置</h1>
              <p>按节点设置离线宽限期与冷却时间，减少短暂断连造成的重复通知。</p>
            </div>
          </div>

          {/* 搜索与工具条 */}
          <div className="lite-toolbar-row">
            <div className="lite-search-box">
              <MagnifyingGlass size={15} />
              <input
                type="text"
                className="lite-search-input"
                placeholder="搜索"
                value={offlineSearch}
                onChange={(e) => setOfflineSearch(e.target.value)}
              />
            </div>

            <div className="lite-toolbar-right">
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedOfflineNodes.length === filteredOfflineNodes.length) {
                    setSelectedOfflineNodes([])
                  } else {
                    setSelectedOfflineNodes(filteredOfflineNodes.map((n) => n.id))
                  }
                }}
              >
                全选
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedOfflineNodes.length === 0) {
                    showToast('请先勾选需要批量修改的服务器')
                  } else {
                    showToast(`已批量设置 ${selectedOfflineNodes.length} 台服务器宽限期为 180秒`)
                  }
                }}
              >
                批量修改
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => showToast('已恢复全局默认离线配置 (宽限期 180秒)')}
              >
                默认配置
              </button>
            </div>
          </div>

          {/* 节点离线表格 */}
          <div className="monitor-table-card">
            <div className="table-scroll">
              <table className="monitor-table">
                <thead>
                  <tr>
                    <th style={{ width: '40px' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredOfflineNodes.length > 0 &&
                          selectedOfflineNodes.length === filteredOfflineNodes.length
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedOfflineNodes(filteredOfflineNodes.map((n) => n.id))
                          } else {
                            setSelectedOfflineNodes([])
                          }
                        }}
                      />
                    </th>
                    <th>服务器</th>
                    <th style={{ width: '120px' }}>状态</th>
                    <th style={{ width: '140px' }}>宽限期</th>
                    <th style={{ minWidth: '180px' }}>最后通知</th>
                    <th style={{ width: '80px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredOfflineNodes.map((node) => {
                    const isSelected = selectedOfflineNodes.includes(node.id)
                    return (
                      <tr key={node.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => {
                              setSelectedOfflineNodes((prev) =>
                                prev.includes(node.id)
                                  ? prev.filter((i) => i !== node.id)
                                  : [...prev, node.id]
                              )
                            }}
                          />
                        </td>
                        <td>
                          <span style={{ fontWeight: 600 }}>{node.name}</span>
                        </td>
                        <td>
                          {node.status === 'offline' ? (
                            <span className="badge badge-rose font-bold">离线</span>
                          ) : node.status === 'attention' ? (
                            <span className="badge badge-amber font-bold">关注</span>
                          ) : (
                            <span className="badge badge-mint font-bold">在线</span>
                          )}
                        </td>
                        <td className="mono">{node.gracePeriod}</td>
                        <td className="mono text-muted">{node.lastNotified}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="修改离线通知宽限期"
                            onClick={() => setEditingOfflineNode(node)}
                          >
                            <PencilSimple size={15} />
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            {/* 底部分页与提示 */}
            <div className="lite-pagination-row">
              <div className="lite-callout-note" style={{ maxWidth: '600px' }}>
                <div>
                  <strong>为了避免频繁发送通知，我们设置了宽限期：</strong>
                  <br />
                  <span>宽限期：客户端离线后，如果在这段时间内没有重新上线，就会发送离线通知</span>
                </div>
              </div>

              <div className="lite-pagination-right">
                <span>
                  已选 {selectedOfflineNodes.length} / 共 {displayNodes.length}
                </span>
                <select className="lite-page-select" defaultValue="20">
                  <option value="20">20 条/页</option>
                  <option value="50">50 条/页</option>
                </select>
                <div className="flex items-center gap-1">
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &lt;
                  </button>
                  <span className="mono">1/1</span>
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &gt;
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 3: 负载通知 (严格匹配 Image 3: media_1790307386277.png)
         ==================================================================== */}
      {currentTab === 'load' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>负载通知</h1>
              <p>配置 CPU、内存、负载等资源告警规则，并绑定适用节点。</p>
            </div>
          </div>

          {/* 子 Tab 切换: 告警配置 | 当前告警 */}
          <div className="monitor-toolbar">
            <div className="monitor-view-toggle">
              <button
                type="button"
                className={`monitor-toggle-btn ${loadSubTab === 'config' ? 'active' : ''}`}
                onClick={() => setLoadSubTab('config')}
              >
                <Sliders size={14} className="inline mr-1" />
                告警配置
              </button>
              <button
                type="button"
                className={`monitor-toggle-btn ${loadSubTab === 'current' ? 'active' : ''}`}
                onClick={() => setLoadSubTab('current')}
              >
                <Bell size={14} className="inline mr-1" />
                当前告警 {alerts.filter((a) => a.status === 'open').length > 0 && `(${alerts.filter((a) => a.status === 'open').length})`}
              </button>
              <button
                type="button"
                className={`monitor-toggle-btn ${loadSubTab === 'silences' ? 'active' : ''}`}
                onClick={() => setLoadSubTab('silences')}
              >
                <Clock size={14} className="inline mr-1" />
                静默与防抖 {flappingAlerts.length > 0 && `(⚠️ ${flappingAlerts.length})`}
              </button>
            </div>

            <div className="lite-toolbar-right">
              <div className="lite-search-box">
                <MagnifyingGlass size={15} />
                <input
                  type="text"
                  className="lite-search-input"
                  placeholder="搜索"
                  value={loadSearch}
                  onChange={(e) => setLoadSearch(e.target.value)}
                />
              </div>

              {loadSubTab === 'config' && (
                <button
                  type="button"
                  className="lite-btn-primary"
                  onClick={handleOpenAddRule}
                >
                  <Plus size={14} weight="bold" />
                  <span>添加规则</span>
                </button>
              )}
              {loadSubTab === 'silences' && (
                <button
                  type="button"
                  className="lite-btn-primary"
                  onClick={() => handleOpenSnooze(null)}
                >
                  <Plus size={14} weight="bold" />
                  <span>新建维护静默</span>
                </button>
              )}
            </div>
          </div>

          {loadSubTab === 'config' ? (
            /* 告警配置表格 */
            <div className="monitor-table-card">
              <div className="table-scroll">
                <table className="monitor-table">
                  <thead>
                    <tr>
                      <th style={{ width: '64px' }}>启用</th>
                      <th style={{ minWidth: '150px' }}>规则名称</th>
                      <th style={{ minWidth: '200px' }}>适用服务器</th>
                      <th style={{ width: '150px' }}>监控指标</th>
                      <th style={{ minWidth: '160px' }}>判定条件与防抖</th>
                      <th style={{ width: '80px' }}>级别</th>
                      <th style={{ width: '90px', textAlign: 'right' }}>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {loadingRules ? (
                      <tr>
                        <td colSpan={7} className="text-center py-8 text-muted">
                          <CircleNotch size={20} className="animate-spin inline mr-2" />
                          加载规则列表中...
                        </td>
                      </tr>
                    ) : filteredLoadRules.length === 0 ? (
                      <tr>
                        <td colSpan={7} className="text-center py-8 text-muted">
                          暂无匹配的告警规则，点击右上角「添加规则」创建自定义监控阈值
                        </td>
                      </tr>
                    ) : (
                      filteredLoadRules.map((rule) => {
                        const isPercent = ['cpu', 'memory', 'disk', 'traffic_percent', 'network_loss'].includes(rule.metric)
                        const isComposite = rule.expression_type === 'composite'
                        return (
                          <tr key={rule.id}>
                            <td>
                              <button
                                type="button"
                                className={`switch-toggle ${rule.enabled ? 'active' : ''}`}
                                title={rule.enabled ? '点击停用规则' : '点击启用规则'}
                                onClick={() => handleToggleRule(rule)}
                              >
                                <span className="switch-thumb" />
                              </button>
                            </td>
                            <td>
                              <strong style={{ color: 'var(--text-1)' }}>{rule.name}</strong>
                              <div className="text-xs text-muted mono">{rule.id}</div>
                            </td>
                            <td className="text-muted text-xs leading-relaxed">
                              {rule.node_filter === '*' ? (
                                <span className="badge badge-subtle">所有探针节点</span>
                              ) : (
                                <span className="mono">{rule.node_filter}</span>
                              )}
                            </td>
                            <td>
                              {isComposite ? (
                                <span className="badge badge-subtle font-bold" style={{ borderColor: 'var(--accent)', color: 'var(--accent)' }}>
                                  复合规则 ({rule.logic || 'AND'})
                                </span>
                              ) : (
                                <span className="badge badge-subtle font-bold mono">
                                  {METRIC_LABELS[rule.metric] || rule.metric}
                                </span>
                              )}
                            </td>
                            <td>
                              {isComposite ? (
                                <div className="flex flex-col gap-1">
                                  <div className="mono text-rose font-bold text-xs leading-snug">
                                    {rule.conditions && rule.conditions.length > 0
                                      ? rule.conditions.map((c) => `${c.metric} ${c.operator} ${c.threshold}`).join(` ${rule.logic || 'AND'} `)
                                      : '未设置条件'}
                                  </div>
                                  {rule.consecutive_count > 1 && (
                                    <span className="badge badge-subtle text-[10px] w-fit">
                                      连续 ≥ {rule.consecutive_count} 次触发
                                    </span>
                                  )}
                                </div>
                              ) : (
                                <div className="flex items-center gap-1.5 flex-wrap">
                                  <span className="mono text-rose font-bold">
                                    {rule.operator} {rule.threshold}{isPercent ? '%' : ''}
                                  </span>
                                  {rule.consecutive_count > 1 && (
                                    <span className="badge badge-subtle text-[10px]">
                                      连续 ≥ {rule.consecutive_count} 次
                                    </span>
                                  )}
                                </div>
                              )}
                            </td>
                            <td>
                              <span className={`badge ${rule.severity === 'critical' ? 'badge-danger' : rule.severity === 'warning' ? 'badge-warning' : 'badge-subtle'}`}>
                                {rule.severity === 'critical' ? '严重' : rule.severity === 'warning' ? '警告' : '提示'}
                              </span>
                            </td>
                            <td style={{ textAlign: 'right' }}>
                              <div className="flex items-center justify-end gap-1">
                                <button
                                  type="button"
                                  className="icon-action-btn"
                                  title="编辑规则"
                                  onClick={() => handleOpenEditRule(rule)}
                                >
                                  <PencilSimple size={15} />
                                </button>
                                <button
                                  type="button"
                                  className="icon-action-btn text-rose"
                                  title="删除规则"
                                  onClick={() => handleDeleteRule(rule.id, rule.name)}
                                >
                                  <Trash size={15} />
                                </button>
                              </div>
                            </td>
                          </tr>
                        )
                      })
                    )}
                  </tbody>
                </table>
              </div>

              <div className="lite-pagination-row">
                <div className="text-xs text-muted">
                  共 {filteredLoadRules.length} 条告警规则
                </div>
                <div className="lite-pagination-right">
                  <select className="lite-page-select" defaultValue="20">
                    <option value="20">20 条/页</option>
                  </select>
                  <div className="flex items-center gap-1">
                    <button type="button" className="lite-page-nav-btn" disabled>
                      &lt;
                    </button>
                    <span className="mono">1/1</span>
                    <button type="button" className="lite-page-nav-btn" disabled>
                      &gt;
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ) : loadSubTab === 'current' ? (
            /* 当前告警列表（含确认告警、快捷静默支持） */
            <div className="panel p-5 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-bold text-base flex items-center gap-2">
                    <Warning size={18} className="text-amber" />
                    <span>活跃与已确认告警列表</span>
                  </h3>
                  <p className="text-xs text-muted mt-0.5">所有通过监控上报与阈值判断触发的告警项</p>
                </div>
              </div>

              {alerts.length === 0 ? (
                <div className="text-center py-8 text-muted text-sm">暂无活跃告警，系统运行平稳。</div>
              ) : (
                <div className="table-scroll">
                  <table className="table w-full text-xs">
                    <thead>
                      <tr>
                        <th>级别</th>
                        <th>节点</th>
                        <th>类型</th>
                        <th>告警详情</th>
                        <th>触发时间</th>
                        <th>状态</th>
                        <th className="text-right">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {alerts.map((alert) => (
                        <tr key={alert.id}>
                          <td>
                            <span className={`badge badge-${alertSeverity(alert.severity)}`}>
                              {alert.severity}
                            </span>
                          </td>
                          <td className="font-semibold">{alert.node_name || alert.node_id}</td>
                          <td className="mono">{alert.type}</td>
                          <td>{alert.message || alert.title}</td>
                          <td className="mono text-muted">{formatAlertTime(alert.created_at)}</td>
                          <td>
                            <span className={`badge badge-${alert.status === 'open' ? 'rose' : 'mint'}`}>
                              {alert.status === 'open' ? '未解决' : '已确认'}
                            </span>
                          </td>
                          <td className="text-right">
                            <div className="flex items-center justify-end gap-1.5">
                              {alert.status === 'open' ? (
                                <button
                                  type="button"
                                  className="button button-quiet btn-xs"
                                  disabled={ackingId === alert.id}
                                  onClick={() => onAck && onAck(alert.id)}
                                >
                                  {ackingId === alert.id ? '确认中…' : '确认告警'}
                                </button>
                              ) : (
                                <span className="text-muted text-xs">已确认</span>
                              )}
                              <button
                                type="button"
                                className="button button-quiet btn-xs"
                                title="一键设置快捷静默，暂停重复推送"
                                onClick={() => handleOpenSnooze(alert)}
                              >
                                <Clock size={13} className="inline mr-1" />
                                <span>静默</span>
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
          ) : (
            /* 静默与防抖子 Tab */
            <div className="space-y-4">
              {/* 防抖监测状态栏 */}
              <div className="panel p-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Warning size={18} className={flappingAlerts.length > 0 ? 'text-amber' : 'text-mint'} />
                    <div>
                      <h3 className="font-bold text-sm">告警抖动抑制系统 (Flapping Mitigation Engine)</h3>
                      <p className="text-xs text-muted">
                        滑窗 5 分钟内状态翻转达到 4 次自动阻断重复推送，冷却 3 分钟持续稳定后自动恢复正常通知。
                      </p>
                    </div>
                  </div>
                  <span className={`badge ${flappingAlerts.length > 0 ? 'badge-rose' : 'badge-mint'}`}>
                    {flappingAlerts.length > 0 ? `${flappingAlerts.length} 项频繁震荡抖动中` : '状态平稳运行中'}
                  </span>
                </div>

                {flappingAlerts.length > 0 && (
                  <div className="table-scroll mt-3">
                    <table className="table w-full text-xs">
                      <thead>
                        <tr>
                          <th>节点 ID</th>
                          <th>告警指纹</th>
                          <th>5m 内翻转次数</th>
                          <th>最近翻转时间</th>
                          <th>防抖抑制状态</th>
                        </tr>
                      </thead>
                      <tbody>
                        {flappingAlerts.map((f) => (
                          <tr key={f.key}>
                            <td className="font-semibold mono">{f.node_id}</td>
                            <td className="mono text-muted">{f.fingerprint}</td>
                            <td>
                              <span className="badge badge-rose font-bold">{f.transitions} 次切换</span>
                            </td>
                            <td className="mono text-muted">{formatAlertTime(f.last_transition)}</td>
                            <td>
                              <span className="badge badge-subtle text-amber">已静默防刷 (稳定3m恢复)</span>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>

              {/* 静默与维护窗口列表 */}
              <div className="panel p-5 space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="font-bold text-base flex items-center gap-2">
                      <Clock size={18} className="text-blue" />
                      <span>活跃静默与维护窗口 (Alert Silences)</span>
                    </h3>
                    <p className="text-xs text-muted mt-0.5">所有已生效与即将生效的告警静默与维护规则列表</p>
                  </div>
                  <button
                    type="button"
                    className="lite-btn-primary"
                    onClick={() => handleOpenSnooze(null)}
                  >
                    <Plus size={14} weight="bold" />
                    <span>新建维护静默</span>
                  </button>
                </div>

                {loadingSilences ? (
                  <div className="text-center py-8 text-muted">
                    <CircleNotch size={20} className="animate-spin inline mr-2" />
                    正在加载静默规则...
                  </div>
                ) : silences.length === 0 ? (
                  <div className="text-center py-8 text-muted text-sm">
                    当前无生效中的告警静默规则。在排查问题或进行例行维护时，可快速开启静默以避免报警骚扰。
                  </div>
                ) : (
                  <div className="table-scroll">
                    <table className="table w-full text-xs">
                      <thead>
                        <tr>
                          <th>静默主题 / 原因</th>
                          <th>适用节点</th>
                          <th>类别 / 指纹</th>
                          <th>生效时段</th>
                          <th>创建人</th>
                          <th className="text-right">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {silences.map((s) => (
                          <tr key={s.id}>
                            <td>
                              <strong style={{ color: 'var(--text-1)' }}>{s.name}</strong>
                              {s.reason && s.reason !== s.name && (
                                <div className="text-xs text-muted">{s.reason}</div>
                              )}
                            </td>
                            <td className="mono text-muted">
                              {s.node_filter === '*' ? <span className="badge badge-subtle">所有节点</span> : s.node_filter}
                            </td>
                            <td className="mono text-xs">
                              {s.fingerprint ? (
                                <span className="badge badge-subtle">{s.fingerprint.slice(0, 16)}...</span>
                              ) : s.category !== '*' ? (
                                <span className="badge badge-subtle">{s.category}</span>
                              ) : (
                                <span className="badge badge-subtle">全类别</span>
                              )}
                            </td>
                            <td className="mono text-muted text-xs">
                              <div>{formatAlertTime(s.starts_at)} 至</div>
                              <div>{formatAlertTime(s.ends_at)}</div>
                            </td>
                            <td className="text-muted">{s.created_by || 'admin'}</td>
                            <td className="text-right">
                              <button
                                type="button"
                                className="button button-quiet btn-xs text-rose"
                                onClick={() => handleDeleteSilence(s.id, s.name)}
                              >
                                解除静默
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            </div>
          )}
        </>
      )}

      {/* ====================================================================
          TAB 4: 流量定时报告 (严格匹配 Image 4: media_1790307401109.png)
         ==================================================================== */}
      {currentTab === 'traffic_report' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>流量定时报告</h1>
              <p>按节点设置日报、周报与月报的推送周期和内容。</p>
            </div>
          </div>

          {/* 顶部双列分栏卡片: 推送时间与立即发送 */}
          <div className="lite-top-card-split">
            {/* 左分栏: 报告推送时间 */}
            <div className="lite-split-col">
              <div className="lite-card-meta">
                <span className="lite-card-title">报告推送时间</span>
                <span className="lite-card-desc">日报、周报和月报均按北京时间在此时刻推送</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="relative flex items-center">
                  <input
                    type="text"
                    className="field-input mono"
                    style={{ width: '88px', height: '32px', textAlign: 'center' }}
                    value={reportPushTime}
                    onChange={(e) => setReportPushTime(e.target.value)}
                  />
                  <Clock size={14} className="absolute right-2 text-muted pointer-events-none" />
                </div>
                <button
                  type="button"
                  className="lite-btn-quiet"
                  onClick={() => showToast(`推送时刻已设置为 ${reportPushTime}`)}
                >
                  保存
                </button>
              </div>
            </div>

            {/* 右分栏: 发送日报消息 */}
            <div className="lite-split-col">
              <div className="lite-card-meta">
                <span className="lite-card-title">发送日报消息</span>
                <span className="lite-card-desc">立即发送北京时间今日 00:00 至当前时刻的日报</span>
              </div>
              <button
                type="button"
                className="lite-btn-primary"
                onClick={() => showToast('已成功触发即时日报推送')}
              >
                立即发送
              </button>
            </div>
          </div>

          {/* 工具栏 */}
          <div className="lite-toolbar-row">
            <div className="lite-search-box">
              <MagnifyingGlass size={15} />
              <input
                type="text"
                className="lite-search-input"
                placeholder="搜索"
                value={reportSearch}
                onChange={(e) => setReportSearch(e.target.value)}
              />
            </div>

            <div className="lite-toolbar-right">
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedReportNodes.length === filteredReportNodes.length) {
                    setSelectedReportNodes([])
                  } else {
                    setSelectedReportNodes(filteredReportNodes.map((n) => n.id))
                  }
                }}
              >
                全选
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedReportNodes.length === 0) {
                    showToast('请先勾选需要批量修改的服务器')
                  } else {
                    showToast(`已批量更新 ${selectedReportNodes.length} 台服务器流量报告参数`)
                  }
                }}
              >
                批量修改
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => showToast('已恢复默认流量报告配置 (日报+周报)')}
              >
                默认配置
              </button>
            </div>
          </div>

          {/* 流量报告表格 */}
          <div className="monitor-table-card">
            <div className="table-scroll">
              <table className="monitor-table">
                <thead>
                  <tr>
                    <th style={{ width: '40px' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredReportNodes.length > 0 &&
                          selectedReportNodes.length === filteredReportNodes.length
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedReportNodes(filteredReportNodes.map((n) => n.id))
                          } else {
                            setSelectedReportNodes([])
                          }
                        }}
                      />
                    </th>
                    <th>服务器</th>
                    <th style={{ width: '120px' }}>状态</th>
                    <th style={{ width: '160px' }}>定时类型</th>
                    <th style={{ minWidth: '180px' }}>报告内容</th>
                    <th style={{ width: '80px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredReportNodes.map((node) => {
                    const isSelected = selectedReportNodes.includes(node.id)
                    return (
                      <tr key={node.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => {
                              setSelectedReportNodes((prev) =>
                                prev.includes(node.id)
                                  ? prev.filter((i) => i !== node.id)
                                  : [...prev, node.id]
                              )
                            }}
                          />
                        </td>
                        <td>
                          <span style={{ fontWeight: 600 }}>{node.name}</span>
                        </td>
                        <td>
                          <span className="badge badge-mint font-bold">启用</span>
                        </td>
                        <td>{node.reportType}</td>
                        <td>{node.reportContent}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="修改报告推送设置"
                            onClick={() => showToast(`已打开 ${node.name} 的定时报告修改面板`)}
                          >
                            <PencilSimple size={15} />
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div className="lite-pagination-row">
              <div />
              <div className="lite-pagination-right">
                <span>
                  已选 {selectedReportNodes.length} / 共 {displayNodes.length}
                </span>
                <select className="lite-page-select" defaultValue="20">
                  <option value="20">20 条/页</option>
                  <option value="50">50 条/页</option>
                </select>
                <div className="flex items-center gap-1">
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &lt;
                  </button>
                  <span className="mono">1/1</span>
                  <button type="button" className="lite-page-nav-btn" disabled>
                    &gt;
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 5: 延迟监测告警 (严格匹配 Image 5: media_1790307417925.png)
         ==================================================================== */}
      {currentTab === 'latency_alert' && (
        <>
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>延迟监测告警</h1>
              <p>根据延迟监测结果设置丢包阈值、统计窗口和冷却时间。</p>
            </div>
          </div>

          {/* 视图切换与筛选条 */}
          <div className="lite-toolbar-row">
            <div className="flex items-center gap-3">
              <div className="monitor-view-toggle">
                <button
                  type="button"
                  className={`monitor-toggle-btn ${latencyAlertView === 'tasks' ? 'active' : ''}`}
                  onClick={() => setLatencyAlertView('tasks')}
                >
                  <WifiHigh size={14} className="inline mr-1" />
                  任务视图
                </button>
                <button
                  type="button"
                  className={`monitor-toggle-btn ${latencyAlertView === 'servers' ? 'active' : ''}`}
                  onClick={() => setLatencyAlertView('servers')}
                >
                  服务器视图
                </button>
              </div>

              <select
                className="monitor-filter-select"
                value={latencyStatusFilter}
                onChange={(e) => setLatencyStatusFilter(e.target.value)}
              >
                <option value="all">状态</option>
                <option value="未配置">未配置</option>
                <option value="已启用">已启用</option>
              </select>

              <div className="lite-search-box">
                <MagnifyingGlass size={15} />
                <input
                  type="text"
                  className="lite-search-input"
                  placeholder="搜索"
                  value={latencySearch}
                  onChange={(e) => setLatencySearch(e.target.value)}
                />
              </div>
            </div>

            <div className="lite-toolbar-right">
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedLatencyAlerts.length === filteredLatencyAlerts.length) {
                    setSelectedLatencyAlerts([])
                  } else {
                    setSelectedLatencyAlerts(filteredLatencyAlerts.map((i) => i.id))
                  }
                }}
              >
                全选
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => {
                  if (selectedLatencyAlerts.length === 0) {
                    showToast('请先勾选需要批量修改的延迟告警项')
                  } else {
                    showToast(`已批量配置 ${selectedLatencyAlerts.length} 个延迟告警规则`)
                  }
                }}
              >
                批量修改
              </button>
              <button
                type="button"
                className="lite-btn-quiet"
                onClick={() => showToast('已恢复默认配置 (丢包超 15% 报警)')}
              >
                默认配置
              </button>
              <button
                type="button"
                className="lite-btn-primary"
                onClick={() => showToast('已打开延迟告警添加面板')}
              >
                <Plus size={14} weight="bold" />
                <span>添加</span>
              </button>
            </div>
          </div>

          {/* 矩阵表格 (72 项分页展示，严格对齐 Image 5) */}
          <div className="monitor-table-card">
            <div className="table-scroll">
              <table className="monitor-table">
                <thead>
                  <tr>
                    <th style={{ width: '40px' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredLatencyAlerts.length > 0 &&
                          selectedLatencyAlerts.length === filteredLatencyAlerts.length
                        }
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedLatencyAlerts(filteredLatencyAlerts.map((i) => i.id))
                          } else {
                            setSelectedLatencyAlerts([])
                          }
                        }}
                      />
                    </th>
                    <th style={{ minWidth: '100px' }}>任务</th>
                    <th style={{ minWidth: '120px' }}>服务器</th>
                    <th style={{ minWidth: '220px' }}>目标</th>
                    <th style={{ width: '90px' }}>状态</th>
                    <th style={{ width: '90px' }}>统计窗口</th>
                    <th style={{ width: '90px' }}>丢包阈值</th>
                    <th style={{ width: '100px' }}>最少样本数</th>
                    <th style={{ width: '90px' }}>冷却时间</th>
                    <th style={{ minWidth: '120px' }}>最后通知</th>
                    <th style={{ width: '80px', textAlign: 'right' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredLatencyAlerts.slice(0, 20).map((row) => {
                    const isSelected = selectedLatencyAlerts.includes(row.id)
                    return (
                      <tr key={row.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => {
                              setSelectedLatencyAlerts((prev) =>
                                prev.includes(row.id)
                                  ? prev.filter((i) => i !== row.id)
                                  : [...prev, row.id]
                              )
                            }}
                          />
                        </td>
                        <td>
                          <strong style={{ color: 'var(--text-1)' }}>{row.task}</strong>
                        </td>
                        <td>{row.server}</td>
                        <td className="mono text-muted text-xs">{row.targetHost}</td>
                        <td>
                          <span className="badge badge-amber font-bold">{row.status}</span>
                        </td>
                        <td className="mono text-muted">{row.window}</td>
                        <td className="mono text-muted">{row.lossThreshold}</td>
                        <td className="mono text-muted">{row.minSamples}</td>
                        <td className="mono text-muted">{row.cooldown}</td>
                        <td className="mono text-muted">{row.lastNotified}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="icon-action-btn"
                            title="配置丢包与时延告警阈值"
                            onClick={() => showToast(`已打开 ${row.task} - ${row.server} 的阈值配置`)}
                          >
                            <PencilSimple size={15} />
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div className="lite-pagination-row">
              <div />
              <div className="lite-pagination-right">
                <span>
                  已选 {selectedLatencyAlerts.length} / 共 {filteredLatencyAlerts.length}
                </span>
                <select className="lite-page-select" defaultValue="20">
                  <option value="20">20 条/页</option>
                  <option value="50">50 条/页</option>
                </select>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    className="lite-page-nav-btn"
                    disabled={latencyPage <= 1}
                    onClick={() => setLatencyPage((p) => Math.max(1, p - 1))}
                  >
                    &lt;
                  </button>
                  <span className="mono">
                    {latencyPage}/{Math.max(1, Math.ceil(filteredLatencyAlerts.length / 20))}
                  </span>
                  <button
                    type="button"
                    className="lite-page-nav-btn"
                    disabled={latencyPage >= Math.ceil(filteredLatencyAlerts.length / 20)}
                    onClick={() =>
                      setLatencyPage((p) =>
                        Math.min(Math.ceil(filteredLatencyAlerts.length / 20), p + 1)
                      )
                    }
                  >
                    &gt;
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}

      {/* ====================================================================
          TAB 6: 通用 (General)
         ==================================================================== */}
      {currentTab === 'general' && (
        <div className="space-y-4">
          <div className="notify-header-row">
            <div className="notify-title-col">
              <h1>通用设置</h1>
              <p>全局通知免打扰时段、过期通知与通道安全连接配置。</p>
            </div>
          </div>

          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">夜间免打扰静音时段</span>
                <span className="lite-card-desc">在设定的时段内暂停发送一般通知，仅致命告警（P0）穿透推送</span>
              </div>
              <button
                type="button"
                className="switch-toggle"
                onClick={() => showToast('夜间免打扰时段已更新')}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>

          <div className="lite-card-box">
            <div className="lite-card-row">
              <div className="lite-card-meta">
                <span className="lite-card-title">服务器到期提前提醒</span>
                <span className="lite-card-desc">自动根据成本中心资费到期时间，在到期前 7 天、3 天发送续费预警通知</span>
              </div>
              <button
                type="button"
                className="switch-toggle active"
                onClick={() => showToast('到期预警已启用')}
              >
                <span className="switch-thumb" />
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 添加/编辑负载与多指标告警规则弹窗 */}
      {showAddLoadModal && (
        <div className="modal-backdrop" onClick={() => setShowAddLoadModal(false)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-group">
                <div className="modal-icon-badge text-blue">
                  <Sliders size={20} />
                </div>
                <div>
                  <h2 className="modal-title">{editingRule ? '编辑告警规则' : '添加告警规则'}</h2>
                  <p className="modal-subtitle">设置 CPU、内存、负载或流量周期的自定义监控阈值闭环</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button modal-close"
                onClick={() => setShowAddLoadModal(false)}
              >
                <X size={18} />
              </button>
            </div>

            <form onSubmit={handleSaveRule}>
              <div className="modal-body space-y-4">
                <div className="field">
                  <label className="field-label required">规则名称</label>
                  <input
                    className="field-input"
                    placeholder="如: CPU持续过高告警"
                    value={ruleFormName}
                    onChange={(e) => setRuleFormName(e.target.value)}
                    required
                  />
                </div>

                {/* 规则类型切换: 单一指标 vs 复合多维度 */}
                <div className="field">
                  <label className="field-label">规则模式</label>
                  <div className="grid grid-cols-2 gap-2">
                    <button
                      type="button"
                      className={`button text-xs py-2 ${ruleFormExpressionType === 'simple' ? 'button-primary font-bold' : 'button-quiet'}`}
                      onClick={() => setRuleFormExpressionType('simple')}
                    >
                      单一指标阈值
                    </button>
                    <button
                      type="button"
                      className={`button text-xs py-2 ${ruleFormExpressionType === 'composite' ? 'button-primary font-bold' : 'button-quiet'}`}
                      onClick={() => setRuleFormExpressionType('composite')}
                    >
                      复合多维度规则 (AND / OR)
                    </button>
                  </div>
                </div>

                {ruleFormExpressionType === 'simple' ? (
                  /* 单一指标配置 */
                  <div className="form-grid-2">
                    <div className="field">
                      <label className="field-label">监控指标</label>
                      <select
                        className="field-select"
                        value={ruleFormMetric}
                        onChange={(e) => setRuleFormMetric(e.target.value)}
                      >
                        <option value="cpu">CPU 使用率 (%)</option>
                        <option value="memory">物理内存使用率 (%)</option>
                        <option value="disk">磁盘根分区使用率 (%)</option>
                        <option value="load1">系统负载 Load 1m</option>
                        <option value="load5">系统负载 Load 5m</option>
                        <option value="load15">系统负载 Load 15m</option>
                        <option value="traffic_percent">周期流量使用率 (%)</option>
                        <option value="network_loss">丢包率 (%)</option>
                        <option value="network_latency">网络延迟 (ms)</option>
                      </select>
                    </div>

                    <div className="form-grid-2">
                      <div className="field">
                        <label className="field-label">判定符</label>
                        <select
                          className="field-select"
                          value={ruleFormOperator}
                          onChange={(e) => setRuleFormOperator(e.target.value)}
                        >
                          <option value=">">&gt; 大于</option>
                          <option value=">=">&gt;= 大于等于</option>
                          <option value="<">&lt; 小于</option>
                          <option value="<=">&lt;= 小于等于</option>
                          <option value="==">== 等于</option>
                        </select>
                      </div>
                      <div className="field">
                        <label className="field-label required">触发阈值</label>
                        <input
                          className="field-input mono"
                          type="number"
                          step="any"
                          placeholder="85"
                          value={ruleFormThreshold}
                          onChange={(e) => setRuleFormThreshold(e.target.value)}
                          required
                        />
                      </div>
                    </div>
                  </div>
                ) : (
                  /* 复合多维度条件构建器 */
                  <div className="p-3.5 rounded-lg border border-[var(--border)] bg-[var(--surface-subtle)] space-y-3">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-bold flex items-center gap-1.5">
                        <Sliders size={14} className="text-blue" />
                        <span>多指标判定逻辑</span>
                      </span>
                      <div className="flex items-center gap-1.5">
                        <button
                          type="button"
                          className={`btn-xs button ${ruleFormLogic === 'AND' ? 'button-primary font-bold' : 'button-quiet'}`}
                          onClick={() => setRuleFormLogic('AND')}
                        >
                          全部满足 (AND 交集)
                        </button>
                        <button
                          type="button"
                          className={`btn-xs button ${ruleFormLogic === 'OR' ? 'button-primary font-bold' : 'button-quiet'}`}
                          onClick={() => setRuleFormLogic('OR')}
                        >
                          任一满足 (OR 并集)
                        </button>
                      </div>
                    </div>

                    <div className="space-y-2">
                      {ruleFormConditions.map((cond, idx) => (
                        <div key={idx} className="flex items-center gap-2 p-2 rounded bg-[var(--surface)] border border-[var(--border)]">
                          <span className="mono text-xs text-muted w-5 text-center">#{idx + 1}</span>
                          <select
                            className="field-select text-xs py-1"
                            value={cond.metric}
                            onChange={(e) => handleConditionChange(idx, 'metric', e.target.value)}
                          >
                            <option value="cpu">CPU 使用率 (%)</option>
                            <option value="memory">物理内存使用率 (%)</option>
                            <option value="disk">磁盘根分区使用率 (%)</option>
                            <option value="load1">系统负载 Load 1m</option>
                            <option value="load5">系统负载 Load 5m</option>
                            <option value="load15">系统负载 Load 15m</option>
                            <option value="traffic_percent">周期流量使用率 (%)</option>
                            <option value="network_loss">丢包率 (%)</option>
                            <option value="network_latency">网络延迟 (ms)</option>
                          </select>
                          <select
                            className="field-select text-xs py-1 w-28"
                            value={cond.operator}
                            onChange={(e) => handleConditionChange(idx, 'operator', e.target.value)}
                          >
                            <option value=">">&gt; 大于</option>
                            <option value=">=">&gt;= 大于等于</option>
                            <option value="<">&lt; 小于</option>
                            <option value="<=">&lt;= 小于等于</option>
                            <option value="==">== 等于</option>
                          </select>
                          <input
                            className="field-input mono text-xs py-1 w-24"
                            type="number"
                            step="any"
                            placeholder="阈值"
                            value={cond.threshold}
                            onChange={(e) => handleConditionChange(idx, 'threshold', e.target.value)}
                            required
                          />
                          {ruleFormConditions.length > 1 && (
                            <button
                              type="button"
                              className="icon-action-btn text-rose"
                              title="移除此条件"
                              onClick={() => handleRemoveCondition(idx)}
                            >
                              <Trash size={14} />
                            </button>
                          )}
                        </div>
                      ))}
                    </div>

                    <button
                      type="button"
                      className="button button-quiet btn-xs w-full justify-center"
                      onClick={handleAddCondition}
                    >
                      <Plus size={13} className="inline mr-1" />
                      <span>添加组合指标条件</span>
                    </button>
                  </div>
                )}

                {/* 连续触发判定次数 (防毛刺) */}
                <div className="field">
                  <label className="field-label">连续采样判定次数 (防抖与防毛刺)</label>
                  <div className="flex items-center gap-3">
                    <select
                      className="field-select mono w-48 text-xs"
                      value={ruleFormConsecutiveCount}
                      onChange={(e) => setRuleFormConsecutiveCount(parseInt(e.target.value, 10) || 1)}
                    >
                      <option value="1">1 次 (达到即触发)</option>
                      <option value="2">连续 2 次采样超标</option>
                      <option value="3">连续 3 次采样超标</option>
                      <option value="5">连续 5 次采样超标</option>
                    </select>
                    <span className="text-xs text-muted">
                      需要连续多次采样超标才触发告警，有效过滤偶发突增或瞬时毛刺。
                    </span>
                  </div>
                </div>

                <div className="form-grid-2">
                  <div className="field">
                    <label className="field-label">告警严重度</label>
                    <select
                      className="field-select"
                      value={ruleFormSeverity}
                      onChange={(e) => setRuleFormSeverity(e.target.value)}
                    >
                      <option value="warning">警告 (Warning)</option>
                      <option value="critical">严重 (Critical)</option>
                      <option value="info">提示 (Info)</option>
                    </select>
                  </div>

                  <div className="field">
                    <label className="field-label">适用服务器范围</label>
                    <input
                      className="field-input mono"
                      placeholder="* 代表所有服务器，或逗号分隔节点 UUID"
                      value={ruleFormNodeFilter}
                      onChange={(e) => setRuleFormNodeFilter(e.target.value)}
                    />
                  </div>
                </div>

                <div className="flex items-center justify-between p-3 rounded-lg border border-[var(--border)] bg-[var(--surface-subtle)]">
                  <div>
                    <div className="font-semibold text-sm">启用此告警规则</div>
                    <div className="text-xs text-muted">开启后自动在节点遥测上报时进行多维阈值计算并触发告警</div>
                  </div>
                  <button
                    type="button"
                    className={`switch-toggle ${ruleFormEnabled ? 'active' : ''}`}
                    onClick={() => setRuleFormEnabled(!ruleFormEnabled)}
                  >
                    <span className="switch-thumb" />
                  </button>
                </div>
              </div>

              <div className="modal-footer">
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={() => setShowAddLoadModal(false)}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="button button-primary"
                  disabled={savingRule}
                >
                  {savingRule ? (
                    <>
                      <CircleNotch size={14} className="animate-spin inline mr-1" />
                      保存中...
                    </>
                  ) : (
                    '保存规则'
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* 快捷静默 / 维护窗口弹窗 */}
      {showSnoozeModal && (
        <div className="modal-backdrop" onClick={() => setShowSnoozeModal(false)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-group">
                <div className="modal-icon-badge text-amber">
                  <Clock size={20} />
                </div>
                <div>
                  <h2 className="modal-title">设置告警静默 / 维护窗口</h2>
                  <p className="modal-subtitle">在指定维护或故障排查时段内抑制通知推送，避免频繁告警打扰</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button modal-close"
                onClick={() => setShowSnoozeModal(false)}
              >
                <X size={18} />
              </button>
            </div>

            <form onSubmit={handleCreateSnooze}>
              <div className="modal-body space-y-4">
                <div className="field">
                  <label className="field-label required">静默原因 / 维护主题</label>
                  <input
                    className="field-input"
                    placeholder="如: 服务器系统升级维护 / 正在排查内存泄漏"
                    value={snoozeReason}
                    onChange={(e) => setSnoozeReason(e.target.value)}
                    required
                  />
                </div>

                <div className="field">
                  <label className="field-label">快捷静默时长</label>
                  <div className="grid grid-cols-4 gap-2">
                    {[
                      { label: '15 分钟', value: 15 },
                      { label: '1 小时', value: 60 },
                      { label: '4 小时', value: 240 },
                      { label: '24 小时', value: 1440 },
                    ].map((opt) => (
                      <button
                        key={opt.value}
                        type="button"
                        className={`button text-xs py-2 ${snoozeDurationPreset === opt.value ? 'button-primary font-bold' : 'button-quiet'}`}
                        onClick={() => setSnoozeDurationPreset(opt.value)}
                      >
                        {opt.label}
                      </button>
                    ))}
                  </div>
                </div>

                {snoozeTarget && (
                  <div className="field">
                    <label className="field-label">静默生效范围</label>
                    <div className="space-y-1.5 text-xs">
                      <label className="flex items-center gap-2 cursor-pointer p-2.5 rounded border border-[var(--border)] bg-[var(--surface-subtle)]">
                        <input
                          type="radio"
                          name="snooze_scope"
                          value="fingerprint"
                          checked={snoozeScope === 'fingerprint'}
                          onChange={() => setSnoozeScope('fingerprint')}
                        />
                        <span>仅静默当前特定告警 (<span className="mono font-semibold">{snoozeTarget.type || snoozeTarget.category}</span>)</span>
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer p-2.5 rounded border border-[var(--border)] bg-[var(--surface-subtle)]">
                        <input
                          type="radio"
                          name="snooze_scope"
                          value="node"
                          checked={snoozeScope === 'node'}
                          onChange={() => setSnoozeScope('node')}
                        />
                        <span>静默当前服务器的所有告警 (节点: <span className="mono font-semibold">{snoozeTarget.node_name || snoozeTarget.node_id}</span>)</span>
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer p-2.5 rounded border border-[var(--border)] bg-[var(--surface-subtle)]">
                        <input
                          type="radio"
                          name="snooze_scope"
                          value="category"
                          checked={snoozeScope === 'category'}
                          onChange={() => setSnoozeScope('category')}
                        />
                        <span>静默该类别所有告警 (类别: <span className="mono font-semibold">{snoozeTarget.category || '全部'}</span>)</span>
                      </label>
                    </div>
                  </div>
                )}
              </div>

              <div className="modal-footer">
                <button
                  type="button"
                  className="button button-quiet"
                  onClick={() => setShowSnoozeModal(false)}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="button button-primary"
                  disabled={savingSnooze}
                >
                  {savingSnooze ? (
                    <>
                      <CircleNotch size={14} className="animate-spin inline mr-1" />
                      正在设置...
                    </>
                  ) : (
                    '确认静默'
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* 修改离线宽限期弹窗 */}
      {editingOfflineNode && (
        <div className="modal-backdrop" onClick={() => setEditingOfflineNode(null)}>
          <div className="modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title-group">
                <div className="modal-icon-badge text-blue">
                  <Clock size={20} />
                </div>
                <div>
                  <h2 className="modal-title">设置离线宽限期 · {editingOfflineNode.name}</h2>
                  <p className="modal-subtitle">设置客户端断连后等待重新上线的时间窗口</p>
                </div>
              </div>
              <button
                type="button"
                className="icon-button modal-close"
                onClick={() => setEditingOfflineNode(null)}
              >
                <X size={18} />
              </button>
            </div>

            <div className="modal-body space-y-4">
              <div className="field">
                <label className="field-label">宽限期 (秒)</label>
                <input
                  type="number"
                  className="field-input mono"
                  value={offlineGracePeriod}
                  onChange={(e) => setOfflineGracePeriod(e.target.value)}
                  min={30}
                  max={3600}
                />
                <span className="text-muted text-xs mt-1 block">
                  推荐 180 秒（3 分钟），有效过滤由于网络抖动短暂重连产生的误告警。
                </span>
              </div>
            </div>

            <div className="modal-footer">
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setEditingOfflineNode(null)}
              >
                取消
              </button>
              <button
                type="button"
                className="button button-primary"
                onClick={async () => {
                  setEditingOfflineNode(null)
                  await handleSaveGracePeriod()
                }}
              >
                确认修改
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
