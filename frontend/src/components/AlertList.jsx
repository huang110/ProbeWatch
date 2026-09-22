import { Bell } from '@phosphor-icons/react'
import { alertSeverity, formatAlertTime } from '../lib/format.js'
import { EmptyState } from './Common.jsx'

export function AlertList({ alerts, onAck, ackingId }) {
  return alerts.length ? <div className="alert-list">{alerts.map((alert) => <article className={`alert-item alert-item-${alertSeverity(alert.severity)}`} key={alert.id}><span className="alert-icon"><Bell size={15} weight="fill" /></span><div><strong>{alert.title}</strong><p>{alert.message}</p><small>{formatAlertTime(alert.lastSeen)} · {alert.occurrenceCount} 次</small>{alert.status === 'open' && onAck && <button type="button" className="button button-quiet alert-ack-button" onClick={() => onAck(alert.id)} disabled={ackingId === alert.id}>{ackingId === alert.id ? '确认中…' : '确认告警'}</button>}</div></article>)}</div> : <EmptyState title="暂无告警" detail="当前没有待处理或已确认的告警。" />
}

export function RecentAlerts({ alerts, onNavigate }) {
  const recent = alerts.slice(0, 3)
  if (!recent.length) return <EmptyState title="暂无告警" detail="当前没有待处理或已确认的告警。" />
  return <div className="recent-alert-list">{recent.map((alert) => <button type="button" key={alert.id} className={`recent-alert recent-alert-${alertSeverity(alert.severity)}`} onClick={() => onNavigate('alerts')}><span className="alert-icon"><Bell size={14} weight="fill" /></span><span className="recent-alert-body"><strong>{alert.title}</strong><small>{formatAlertTime(alert.lastSeen)}</small></span></button>)}</div>
}
