import { AlertList } from './AlertList.jsx'
import { MediaMatrix } from './MediaMatrix.jsx'
import { TOTPSettingsCard } from './TOTPSettingsCard.jsx'
import { NodeTable } from './NodeList.jsx'
import { Pulse } from '@phosphor-icons/react'

export function SubPage({ page, data, alerts, onAck, ackingId, onBack, onSelectNode, rates = {}, lossRates = {} }) {
  const pages = {
    nodes: ['节点', '查看所有已注册节点和探针状态。'],
    network: ['网络检测', '暂无 API 数据 · 等待网络检测结果上报。'],
    mtr: ['MTR 路由', '暂无 API 数据 · 等待 MTR 路由上报。'],
    media: ['流媒体', '按节点查看各流媒体检测器的最新状态。'],
    alerts: ['告警事件', '暂无告警 API 数据。'],
    targets: ['检测目标', '暂无检测目标 API 数据。'],
    settings: ['系统设置', '管理控制台安全选项。'],
  }
  const [title, description] = pages[page] || pages.nodes
  return <section className="subpage">
    <div className="subpage-heading">
      <div><div className="eyebrow">监控模块</div><h1>{title}<span className="heading-period">。</span></h1><p>{description}</p></div>
      <button className="button button-quiet" onClick={onBack}>返回总览</button>
    </div>
    {page === 'nodes' ? <div className="panel"><div className="panel-header"><div><h2>节点清单</h2><p>共 {data.length} 个节点 · 桌面端表格 · 移动端卡片</p></div></div><NodeTable nodes={data} rates={rates} lossRates={lossRates} onSelect={onSelectNode} /></div>
      : page === 'alerts' ? <div className="panel"><div className="panel-header"><div><h2>告警列表</h2><p>共 {alerts.length} 条</p></div></div><AlertList alerts={alerts} onAck={onAck} ackingId={ackingId} /></div>
        : page === 'media' ? <div className="panel"><div className="panel-header"><div><h2>流媒体检测矩阵</h2><p>行 = 节点 · 列 = 检测器 · 单元格 = 最新检测状态</p></div></div><MediaMatrix nodes={data} /></div>
          : page === 'settings' ? <TOTPSettingsCard />
            : <div className="panel placeholder-panel"><div className="placeholder-icon"><Pulse size={22} /></div><h2>{title}数据面板</h2><p>暂无可由当前 API 支撑的数据。</p></div>}
  </section>
}
