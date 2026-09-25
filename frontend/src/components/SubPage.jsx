import { AlertCenterView } from './AlertCenterView.jsx'
import { MediaMatrix } from './MediaMatrix.jsx'
import { TOTPSettingsCard } from './TOTPSettingsCard.jsx'
import { TargetManage } from './TargetManage.jsx'
import { NodeEnroll } from './NodeEnroll.jsx'
import { BillingCenter } from './BillingCenter.jsx'
import { MonitoringView } from './MonitoringView.jsx'
import { DashboardView } from './DashboardView.jsx'
import { ServerManageView } from './ServerManageView.jsx'
import { TrafficReportView } from './TrafficReportView.jsx'
import { LogsView } from './LogsView.jsx'
import { Pulse } from '@phosphor-icons/react'

export function SubPage({
  page,
  data = [],
  alerts = [],
  onAck,
  ackingId,
  onBack,
  onSelectNode,
  onNavigate,
  rates = {},
  lossRates = {},
  refreshInterval = 30,
  onIntervalChange,
  theme = 'system',
  onThemeChange,
  overview = null,
  onRefresh,
}) {
  const pages = {
    dashboard: ['仪表盘', '值守巡检中枢：异常Top排行榜、今日与30天流量、回程与被墙监测、成本中心与主控健康。'],
    servers: ['服务器管理', '全网 VPS 实例维护：5 种流量口径、流量校准 Offset、网卡过滤与 24h 平滑 Token 重置。'],
    nodes: ['服务器管理', '全网 VPS 实例维护：5 种流量口径、流量校准 Offset、网卡过滤与 24h 平滑 Token 重置。'],
    billing: ['成本中心', '多币种实时汇率折算、费用趋势柱状图、本月构成、综合资产台账与二手出鸡发帖器。'],
    monitoring: ['监测与回程', '三大运营商精品骨干线路指纹识别 (CN2 GIA/GT, 9929, CMIN2) 与 IP 疑似被墙交叉判定。'],
    network: ['网络检测', '网络连通性只读列表（TCP / HTTP / HTTPS / DNS）。'],
    mtr: ['回程追踪', '回程路由检测目标只读列表（MTR）。'],
    enroll: ['节点接入', '生成单节点一键接入指令、有效期限与平滑轮换。'],
    traffic: ['流量与报告', '双层额度模型、5 种统计口径全网统一、流量校准中心与日报/周报/月报自动推送。'],
    notifications: ['通知与告警', 'Telegram 结构化带图卡片与深层直达排障链接、单机单规则临时静音 (Snooze) 与新机规则继承。'],
    alerts: ['通知与告警', 'Telegram 结构化带图卡片与深层直达排障链接、单机单规则临时静音 (Snooze) 与新机规则继承。'],
    logs: ['系统日志', '系统运行、管理操作行为、网络事件与安全拦截全量日志 · 本地时区显示 · 支持详情展开。'],
    media: ['流媒体矩阵', '全球主流流媒体与 AI 服务（Netflix, YouTube, OpenAI 等）解锁能力矩阵。'],
    targets: ['检测目标', '管理全部检测目标：新建、启停或删除探测项。'],
    settings: ['系统设置', '管理控制台安全选项、外观主题与全局采样时序时间。'],
  }

  const [title, description] = pages[page] || pages.dashboard

  return (
    <section className="subpage">
      <div className="subpage-heading">
        <div>
          <div className="eyebrow">Lite 架构管理中心</div>
          <h1>
            {title}
            <span className="heading-period">。</span>
          </h1>
          <p>{description}</p>
        </div>
        <button type="button" className="button button-quiet" onClick={onBack}>
          返回仪表盘
        </button>
      </div>

      {page === 'dashboard' || page === 'overview' ? (
        <DashboardView
          nodes={data}
          overview={overview}
          alerts={alerts}
          rates={rates}
          lossRates={lossRates}
          onNavigate={onNavigate || onBack}
          onSelectNode={onSelectNode}
          refreshInterval={refreshInterval}
          onRefresh={onRefresh}
        />
      ) : page === 'servers' || page === 'nodes' ? (
        <ServerManageView
          nodes={data}
          rates={rates}
          lossRates={lossRates}
          onSelectNode={onSelectNode}
        />
      ) : page === 'billing' ? (
        <BillingCenter nodes={data} />
      ) : page === 'monitoring' ? (
        <MonitoringView nodes={data} readOnly={true} />
      ) : page === 'network' ? (
        <TargetManage readOnly kinds={['tcp', 'http', 'https', 'dns']} title="网络检测目标" />
      ) : page === 'mtr' ? (
        <TargetManage readOnly kinds={['mtr']} title="回程追踪目标" />
      ) : page === 'enroll' ? (
        <NodeEnroll />
      ) : page === 'traffic' ? (
        <TrafficReportView nodes={data} onSelectNode={onSelectNode} />
      ) : page === 'notifications' || page === 'alerts' ? (
        <AlertCenterView alerts={alerts} onAck={onAck} ackingId={ackingId} />
      ) : page === 'logs' ? (
        <LogsView />
      ) : page === 'media' ? (
        <MediaMatrix nodes={data} />
      ) : page === 'targets' ? (
        <TargetManage />
      ) : page === 'settings' ? (
        <TOTPSettingsCard
          interval={refreshInterval}
          onIntervalChange={onIntervalChange}
          theme={theme}
          onThemeChange={onThemeChange}
        />
      ) : (
        <div className="panel placeholder-panel">
          <div className="placeholder-icon">
            <Pulse size={22} />
          </div>
          <h2>{title}数据面板</h2>
          <p>暂无可由当前 API 支撑的数据。</p>
        </div>
      )}
    </section>
  )
}
