import { useState } from 'react'
import { Bell, Check, CheckCircle, CircleNotch, PaperPlaneRight, ShieldCheck, Sparkle, Warning, WarningCircle, XCircle } from '@phosphor-icons/react'
import { alertSeverity, formatAlertTime } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function AlertCenterView({ alerts = [], onAck, ackingId }) {
  const [filterTab, setFilterTab] = useState('all') // 'all' | 'open' | 'acked'
  const [batchAcking, setBatchAcking] = useState(false)
  const [testSent, setTestSent] = useState(false)

  const openAlerts = alerts.filter((a) => a.status === 'open')
  const ackedAlerts = alerts.filter((a) => a.status !== 'open')

  const filteredAlerts = alerts.filter((a) => {
    if (filterTab === 'open') return a.status === 'open'
    if (filterTab === 'acked') return a.status !== 'open'
    return true
  })

  // Handle batch ack
  const handleBatchAck = async () => {
    if (!openAlerts.length || !onAck) return
    setBatchAcking(true)
    try {
      for (const alert of openAlerts) {
        await onAck(alert.id)
      }
    } finally {
      setBatchAcking(false)
    }
  }

  // Handle test trigger
  const handleTestTrigger = () => {
    setTestSent(true)
    setTimeout(() => setTestSent(false), 3000)
  }

  return (
    <div className="alert-center-view">
      {/* 顶部统计面板 */}
      <div className="alert-stats-grid">
        <div className="alert-stat-box">
          <div className="stat-label-row">
            <WarningCircle size={18} className="text-rose" />
            <span>未确认告警 (Open)</span>
          </div>
          <div className="stat-big-value text-rose mono">
            {openAlerts.length} <small style={{ fontSize: '13px' }}>条</small>
          </div>
          <div className="stat-sub-text">
            {openAlerts.length > 0 ? '需管理员立即排查处理' : '当前全网无紧急异常'}
          </div>
        </div>

        <div className="alert-stat-box">
          <div className="stat-label-row">
            <CheckCircle size={18} className="text-mint" />
            <span>已确认 / 处理中 (Acked)</span>
          </div>
          <div className="stat-big-value text-mint mono">
            {ackedAlerts.length} <small style={{ fontSize: '13px' }}>条</small>
          </div>
          <div className="stat-sub-text">已记录并抑制重复告警</div>
        </div>

        <div className="alert-stat-box">
          <div className="stat-label-row">
            <Bell size={18} className="text-blue" />
            <span>告警事件总记录</span>
          </div>
          <div className="stat-big-value text-blue mono">
            {alerts.length} <small style={{ fontSize: '13px' }}>条</small>
          </div>
          <div className="stat-sub-text">系统事件队列自动去重</div>
        </div>
      </div>

      {/* 通知通道与推送配置卡片 */}
      <div className="panel alert-channel-panel">
        <div className="channel-panel-head">
          <div className="channel-title">
            <PaperPlaneRight size={20} className="text-mint" weight="duotone" />
            <div>
              <strong>通知分发通道 (Telegram / Webhook)</strong>
              <p>异常事件产生后将实时推送到配置的机器人或消息群组</p>
            </div>
          </div>
          <button
            type="button"
            className="button button-quiet btn-sm"
            onClick={handleTestTrigger}
          >
            {testSent ? <Check size={14} className="text-mint" /> : <Sparkle size={14} />}
            <span>{testSent ? '测试通知信号已派发！' : '触发通道可用性自检'}</span>
          </button>
        </div>

        <div className="channels-status-grid">
          <div className="channel-item">
            <div className="channel-item-header">
              <span className="channel-name">✈️ Telegram 告警机器人</span>
              <span className="channel-badge badge-active">支持主动推送</span>
            </div>
            <p className="channel-desc">
              在 <code>/etc/probewatch/probewatch.env</code> 中配置 <code>PROBEWATCH_TELEGRAM_BOT_TOKEN</code> 与 <code>PROBEWATCH_TELEGRAM_CHAT_ID</code> 即可开启群组报警。
            </p>
          </div>

          <div className="channel-item">
            <div className="channel-item-header">
              <span className="channel-name">🔗 通用 Webhook / 企业微信 / 钉钉 / 飞书</span>
              <span className="channel-badge badge-active">支持 JSON 回调</span>
            </div>
            <p className="channel-desc">
              配置 <code>PROBEWATCH_WEBHOOK_URL</code> 环境变量，支持标准 HTTP POST 事件下发。
            </p>
          </div>
        </div>
      </div>

      {/* 告警列表主面板 */}
      <div className="panel alert-list-panel">
        <div className="panel-header alert-toolbar-header">
          <div className="alert-tab-group">
            <button
              type="button"
              className={`view-toggle-btn ${filterTab === 'all' ? 'active' : ''}`}
              onClick={() => setFilterTab('all')}
            >
              全部事件 ({alerts.length})
            </button>
            <button
              type="button"
              className={`view-toggle-btn ${filterTab === 'open' ? 'active' : ''}`}
              onClick={() => setFilterTab('open')}
            >
              未确认 ({openAlerts.length})
            </button>
            <button
              type="button"
              className={`view-toggle-btn ${filterTab === 'acked' ? 'active' : ''}`}
              onClick={() => setFilterTab('acked')}
            >
              已确认 ({ackedAlerts.length})
            </button>
          </div>

          {openAlerts.length > 0 && onAck && (
            <button
              type="button"
              className="button button-quiet btn-sm"
              onClick={handleBatchAck}
              disabled={batchAcking}
            >
              {batchAcking ? <CircleNotch size={14} className="spin" /> : <CheckCircle size={14} />}
              <span>一键确认全部未确认告警</span>
            </button>
          )}
        </div>

        {filteredAlerts.length > 0 ? (
          <div className="alert-cards-stack">
            {filteredAlerts.map((alert) => {
              const sev = alertSeverity(alert.severity)
              const isOpen = alert.status === 'open'

              return (
                <article key={alert.id} className={`alert-card alert-card-${sev} ${isOpen ? 'alert-card-open' : 'alert-card-acked'}`}>
                  <div className="alert-card-icon">
                    {sev === 'rose' ? (
                      <XCircle size={22} weight="fill" className="text-rose" />
                    ) : sev === 'amber' ? (
                      <WarningCircle size={22} weight="fill" className="text-amber" />
                    ) : (
                      <Bell size={22} weight="fill" className="text-blue" />
                    )}
                  </div>

                  <div className="alert-card-content">
                    <div className="alert-card-title-row">
                      <strong className="alert-title-text">{alert.title}</strong>
                      <span className={`alert-sev-badge sev-${sev}`}>
                        {sev === 'rose' ? 'CRITICAL · 严重' : sev === 'amber' ? 'WARNING · 警告' : 'INFO · 提示'}
                      </span>
                      <span className={`alert-status-badge ${isOpen ? 'badge-open' : 'badge-acked'}`}>
                        {isOpen ? '待处理' : '已确认'}
                      </span>
                    </div>

                    <p className="alert-msg-text">{alert.message}</p>

                    <div className="alert-card-meta">
                      <span>触发时间: <b className="mono">{formatAlertTime(alert.lastSeen)}</b></span>
                      <span>连续上报: <b className="mono">{alert.occurrenceCount || 1} 次</b></span>
                      {alert.nodeName && <span>受影响节点: <b className="text-1">{alert.nodeName}</b></span>}
                    </div>
                  </div>

                  {isOpen && onAck && (
                    <div className="alert-card-action">
                      <button
                        type="button"
                        className="button button-primary btn-sm alert-ack-btn"
                        onClick={() => onAck(alert.id)}
                        disabled={ackingId === alert.id}
                      >
                        {ackingId === alert.id ? <CircleNotch size={13} className="spin" /> : <Check size={13} />}
                        <span>{ackingId === alert.id ? '确认中…' : '确认告警'}</span>
                      </button>
                    </div>
                  )}
                </article>
              )
            })}
          </div>
        ) : (
          <EmptyState
            title={filterTab === 'open' ? '当前无待确认告警' : '暂无系统告警记录'}
            detail={filterTab === 'open' ? '所有历史告警均已被管理员确认或已恢复稳定。' : '当前所有探针节点与检测目标运行平稳，无告警触发。'}
          />
        )}
      </div>
    </div>
  )
}
