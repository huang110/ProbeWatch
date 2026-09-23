import { AlertCenterView } from './AlertCenterView.jsx'
import { MediaMatrix } from './MediaMatrix.jsx'
import { TOTPSettingsCard } from './TOTPSettingsCard.jsx'
import { NodeTable } from './NodeList.jsx'
import { TargetManage } from './TargetManage.jsx'
import { NodeEnroll } from './NodeEnroll.jsx'
import { BillingCenter } from './BillingCenter.jsx'
import { MTRRouteView } from './MTRRouteView.jsx'
import { NetworkMonitorView } from './NetworkMonitorView.jsx'
import { Pulse } from '@phosphor-icons/react'

export function SubPage({ page, data, alerts, onAck, ackingId, onBack, onSelectNode, rates = {}, lossRates = {}, refreshInterval = 30, onIntervalChange }) {
  const pages = {
    nodes: ['节点', '查看所有已注册节点和探针状态，支持自助接入新节点。'],
    billing: ['账单与价值', '全网 VPS 资产台账、续费周期提醒、剩余天数及二手出鸡指导价实时折算。'],
    network: ['网络检测', '三网连通性与时延探针（TCP / HTTP / HTTPS / DNS），支持自定义目标与预设。'],
    mtr: ['MTR 路由追踪', '基于受控节点发起跳数路由追踪，展现逐跳时延梯级与骨干网链路质量。'],
    media: ['流媒体矩阵', '全球主流流媒体与 AI 服务（Netflix, YouTube, OpenAI 等）解锁能力矩阵。'],
    alerts: ['告警事件中心', '系统异常检测、阈值事件流与 Telegram / Webhook 告警分发管理。'],
    targets: ['检测目标', '管理全部检测目标：新建、启停或删除探测项。'],
    settings: ['系统设置', '管理控制台安全选项与全局采样时序时间。'],
  }
  const [title, description] = pages[page] || pages.nodes
  return <section className="subpage">
    <div className="subpage-heading">
      <div><div className="eyebrow">监控模块</div><h1>{title}<span className="heading-period">。</span></h1><p>{description}</p></div>
      <button className="button button-quiet" onClick={onBack}>返回总览</button>
    </div>
    {page === 'nodes' ? <><div className="panel"><div className="panel-header"><div><h2>节点清单</h2><p>共 {data.length} 个节点 · 桌面端表格 · 移动端卡片</p></div></div><NodeTable nodes={data} rates={rates} lossRates={lossRates} onSelect={onSelectNode} /></div><NodeEnroll /></>
      : page === 'billing' ? <BillingCenter nodes={data} />
      : page === 'alerts' ? <AlertCenterView alerts={alerts} onAck={onAck} ackingId={ackingId} />
      : page === 'media' ? <MediaMatrix nodes={data} />
      : page === 'network' ? <NetworkMonitorView nodes={data} readOnly kinds={['tcp', 'http', 'https', 'dns']} />
      : page === 'mtr' ? <MTRRouteView nodes={data} readOnly kinds={['mtr']} />
      : page === 'targets' ? <TargetManage />
      : page === 'settings' ? <TOTPSettingsCard interval={refreshInterval} onIntervalChange={onIntervalChange} />
      : <div className="panel placeholder-panel"><div className="placeholder-icon"><Pulse size={22} /></div><h2>{title}数据面板</h2><p>暂无可由当前 API 支撑的数据。</p></div>}
  </section>
}
