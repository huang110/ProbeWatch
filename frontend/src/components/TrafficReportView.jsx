import { useState, useMemo } from 'react'
import {
  ArrowsDownUp,
  Sliders,
  CalendarCheck,
  PaperPlaneTilt,
  CheckCircle,
  Funnel,
  ArrowUpRight,
  ArrowDownRight,
  Clock,
  Sparkle
} from '@phosphor-icons/react'
import { formatBytes, formatPercent, numeric } from '../lib/format.js'
import { getStoredBillingData, saveNodeBillingData } from '../lib/billing.js'
import { TrafficCalibrationModal } from './TrafficCalibrationModal.jsx'

export function TrafficReportView({ nodes = [], onSelectNode }) {
  const [reportPeriod, setReportPeriod] = useState('daily')
  const [searchTerm, setSearchTerm] = useState('')
  const [calibratingNode, setCalibratingNode] = useState(null)
  const [sentReportToast, setSentReportToast] = useState(false)

  const billingData = useMemo(() => getStoredBillingData(), [])

  // 1. 汇总全网流量与有效额度
  const trafficOverview = useMemo(() => {
    let totalBaseQuotaBytes = 0
    let totalBonusQuotaBytes = 0
    let totalEffectiveUsedBytes = 0
    let totalRawUploadBytes = 0
    let totalRawDownloadBytes = 0

    nodes.forEach((node) => {
      const id = node.uuid || node.id
      const b = billingData[id] || {}
      const rx = numeric(node.rx) || 0
      const tx = numeric(node.tx) || 0
      const offset = Number(b.trafficOffsetBytes || 0)

      totalRawUploadBytes += tx
      totalRawDownloadBytes += rx

      // 额度
      const baseQuotaGB = Number(b.trafficQuotaGB || 1000)
      const bonusQuotaGB = Number(b.bonusQuotaGB || 0)
      totalBaseQuotaBytes += baseQuotaGB * 1024 * 1024 * 1024
      totalBonusQuotaBytes += bonusQuotaGB * 1024 * 1024 * 1024

      // 口径计算
      const method = b.accountingMethod || 'total'
      let effective = rx + tx
      if (method === 'tx') effective = tx
      else if (method === 'rx') effective = rx
      else if (method === 'max') effective = Math.max(tx, rx)
      else if (method === 'min') effective = Math.min(tx, rx)

      totalEffectiveUsedBytes += Math.max(0, effective + offset)
    })

    const totalQuotaBytes = totalBaseQuotaBytes + totalBonusQuotaBytes
    const percent = totalQuotaBytes > 0 ? (totalEffectiveUsedBytes / totalQuotaBytes) * 100 : 0

    return {
      totalBaseQuotaBytes,
      totalBonusQuotaBytes,
      totalQuotaBytes,
      totalEffectiveUsedBytes,
      totalRawUploadBytes,
      totalRawDownloadBytes,
      percent: Math.min(100, percent),
    }
  }, [nodes, billingData])

  const handleSendReportNow = () => {
    setSentReportToast(true)
    setTimeout(() => {
      setSentReportToast(false)
    }, 3000)
  }

  return (
    <div className="traffic-report-container space-y-5">
      {/* 顶部总览卡片: 额度与已用量双层模型 */}
      <div className="panel p-5">
        <div className="flex flex-wrap items-center justify-between gap-4 mb-4">
          <div>
            <h2 className="text-lg font-bold flex items-center gap-2">
              <ArrowsDownUp size={22} className="text-blue" />
              全网流量统计与额度模型
            </h2>
            <p className="text-xs text-muted">
              遵循各节点自定义计费口径 · 基础额度 + 临时追加额度（到期自动清零） · 支持校准偏差
            </p>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              className="button button-primary btn-sm flex items-center gap-1.5"
              onClick={handleSendReportNow}
            >
              <PaperPlaneTilt size={15} />
              <span>立即发送今日报告</span>
            </button>
          </div>
        </div>

        {sentReportToast && (
          <div className="alert-success-banner mb-4">
            <CheckCircle size={16} />
            <span>今日流量账本结算报告已触发推送至配置的消息渠道！</span>
          </div>
        )}

        <div className="dash-row-grid-4 mb-4">
          <div className="stat-card">
            <span className="stat-label">本周期有效总额度</span>
            <strong className="stat-val mono text-blue">{formatBytes(trafficOverview.totalQuotaBytes)}</strong>
            <span className="text-[11px] text-muted">
              基础 {formatBytes(trafficOverview.totalBaseQuotaBytes)} + 临时追加 {formatBytes(trafficOverview.totalBonusQuotaBytes)}
            </span>
          </div>

          <div className="stat-card">
            <span className="stat-label">全网生效已用量 (含校准)</span>
            <strong className="stat-val mono text-amber">{formatBytes(trafficOverview.totalEffectiveUsedBytes)}</strong>
            <span className="text-[11px] text-muted">
              上行 {formatBytes(trafficOverview.totalRawUploadBytes)} · 下行 {formatBytes(trafficOverview.totalRawDownloadBytes)}
            </span>
          </div>

          <div className="stat-card">
            <span className="stat-label">剩余可用总流量</span>
            <strong className="stat-val mono text-mint">
              {formatBytes(Math.max(0, trafficOverview.totalQuotaBytes - trafficOverview.totalEffectiveUsedBytes))}
            </strong>
            <span className="text-[11px] text-muted">全网综合配额富余充足</span>
          </div>

          <div className="stat-card">
            <span className="stat-label">总消耗比例</span>
            <strong className="stat-val mono">{formatPercent(trafficOverview.percent)}</strong>
            <div className="progress-bar-track mt-1.5">
              <div
                className={`progress-bar-fill ${trafficOverview.percent > 85 ? 'bg-rose' : trafficOverview.percent > 65 ? 'bg-amber' : 'bg-blue'}`}
                style={{ width: `${trafficOverview.percent}%` }}
              />
            </div>
          </div>
        </div>

        <div className="bg-subtle p-3 rounded-lg flex flex-wrap items-center justify-between text-xs text-muted">
          <div className="flex items-center gap-2">
            <Sparkle size={15} className="text-blue" />
            <span>
              已支持 5 种统计口径：<b>总和</b>、<b>仅计流出</b>、<b>仅计流入</b>、<b>较大值</b>、<b>较小值</b>。全站统一口径。
            </span>
          </div>
          <div>重置日按节点设置的 IANA 时区（默认 Asia/Shanghai）北京时间精确到时分秒</div>
        </div>
      </div>

      {/* 报表周期选择与节点对账清单 */}
      <div className="panel p-5 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <CalendarCheck size={18} className="text-blue" />
            <h3 className="font-semibold text-base">定时流量报告与服务器对账</h3>
            <div className="preset-pill-group ml-3">
              {[
                { id: 'daily', label: '日报 (自然日)' },
                { id: 'weekly', label: '周报 (自然周)' },
                { id: 'monthly', label: '月报 (自然月)' },
              ].map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className={`preset-pill ${reportPeriod === p.id ? 'preset-pill-active' : ''}`}
                  onClick={() => setReportPeriod(p.id)}
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>

          <div className="flex items-center gap-2">
            <input
              type="text"
              className="input input-sm w-56"
              placeholder="搜索服务器 / 分组 / IP..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
          </div>
        </div>

        <div className="table-responsive">
          <table className="table w-full text-xs">
            <thead>
              <tr>
                <th>服务器</th>
                <th>统计方式</th>
                <th>原始上传</th>
                <th>原始下载</th>
                <th>校准 Offset</th>
                <th>生效已用量</th>
                <th>总限额</th>
                <th>使用率</th>
                <th className="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {nodes
                .filter((n) => {
                  if (!searchTerm) return true
                  const q = searchTerm.toLowerCase()
                  return n.name.toLowerCase().includes(q) || (n.hostname || '').toLowerCase().includes(q)
                })
                .map((node) => {
                  const id = node.uuid || node.id
                  const b = billingData[id] || {}
                  const rx = numeric(node.rx) || 0
                  const tx = numeric(node.tx) || 0
                  const offset = Number(b.trafficOffsetBytes || 0)
                  const method = b.accountingMethod || 'total'
                  const quotaGB = (Number(b.trafficQuotaGB || 1000) + Number(b.bonusQuotaGB || 0))
                  const quotaBytes = quotaGB * 1024 * 1024 * 1024

                  let effective = rx + tx
                  if (method === 'tx') effective = tx
                  else if (method === 'rx') effective = rx
                  else if (method === 'max') effective = Math.max(tx, rx)
                  else if (method === 'min') effective = Math.min(tx, rx)
                  effective = Math.max(0, effective + offset)

                  const pct = quotaBytes > 0 ? (effective / quotaBytes) * 100 : 0

                  return (
                    <tr key={id}>
                      <td className="font-medium">
                        <div className="flex items-center gap-2">
                          <span>{node.flag}</span>
                          <div>
                            <div className="font-semibold text-foreground cursor-pointer hover:underline" onClick={() => onSelectNode(node)}>
                              {node.name}
                            </div>
                            <div className="text-[10px] text-muted mono">{node.hostname || '—'}</div>
                          </div>
                        </div>
                      </td>
                      <td>
                        <span className="badge badge-quiet text-[11px]">
                          {method === 'tx' ? '仅上传' : method === 'rx' ? '仅下载' : method === 'max' ? '取较大值' : method === 'min' ? '取较小值' : '双向总和'}
                        </span>
                      </td>
                      <td className="mono text-amber">{formatBytes(tx)}</td>
                      <td className="mono text-mint">{formatBytes(rx)}</td>
                      <td>
                        {offset !== 0 ? (
                          <span className={`mono font-bold ${offset > 0 ? 'text-amber' : 'text-blue'}`}>
                            {offset > 0 ? `+${formatBytes(offset)}` : `-${formatBytes(Math.abs(offset))}`}
                          </span>
                        ) : (
                          <span className="text-muted mono">0 B</span>
                        )}
                      </td>
                      <td className="mono font-bold text-blue">{formatBytes(effective)}</td>
                      <td className="mono text-muted">{formatBytes(quotaBytes)}</td>
                      <td>
                        <div className="flex items-center gap-1.5">
                          <span className="mono text-xs w-10">{formatPercent(pct)}</span>
                          <div className="progress-bar-track w-16">
                            <div
                              className={`progress-bar-fill ${pct > 90 ? 'bg-rose' : pct > 75 ? 'bg-amber' : 'bg-mint'}`}
                              style={{ width: `${Math.min(100, pct)}%` }}
                            />
                          </div>
                        </div>
                      </td>
                      <td className="text-right">
                        <button
                          type="button"
                          className="button button-quiet btn-sm"
                          onClick={() => setCalibratingNode(node)}
                          title="校准此节点的流量用量"
                        >
                          <Sliders size={14} />
                          <span>校准</span>
                        </button>
                      </td>
                    </tr>
                  )
                })}
            </tbody>
          </table>
        </div>
      </div>

      {calibratingNode && (
        <TrafficCalibrationModal
          node={calibratingNode}
          onClose={() => setCalibratingNode(null)}
          onSaveSuccess={() => {
            // trigger state refresh
          }}
        />
      )}
    </div>
  )
}
