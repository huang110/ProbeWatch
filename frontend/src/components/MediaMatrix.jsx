import { Pulse } from '@phosphor-icons/react'

export function MediaMatrix({ nodes = [], onNavigate, onSelectNode, onBack }) {
  return (
    <div className="panel placeholder-panel media-deprecated-notice-card" style={{ maxWidth: '680px', margin: '40px auto', padding: '36px 24px', textAlign: 'center' }}>
      <div className="placeholder-icon" style={{ margin: '0 auto 16px', background: 'rgba(56, 189, 248, 0.1)', color: '#38bdf8', width: '52px', height: '52px', borderRadius: '12px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Pulse size={28} weight="duotone" />
      </div>
      <h2 style={{ fontSize: '20px', fontWeight: 600, marginBottom: '8px' }}>流媒体与 AI 解锁已统一升级</h2>
      <p style={{ color: 'var(--text-muted, #94a3b8)', lineHeight: 1.6, marginBottom: '24px' }}>
        流媒体与 AI 解锁能力已统一由节点 IPQA 历史归档全面提供。<br />
        独立 <code>media-*</code> 周期探测任务已停用并不再占用节点资源，请前往节点详情页查看真实解锁结果。
      </p>
      <div style={{ display: 'flex', gap: '12px', justifyContent: 'center', flexWrap: 'wrap' }}>
        <button
          type="button"
          className="button button-primary"
          onClick={() => {
            const firstNode = (nodes || [])[0]
            if (firstNode && onSelectNode) onSelectNode(firstNode)
            if (onNavigate) onNavigate('node-detail')
          }}
        >
          查看节点详情（IP 质量与流媒体）
        </button>
        {onBack && (
          <button
            type="button"
            className="button button-quiet"
            onClick={onBack}
          >
            返回仪表盘
          </button>
        )}
      </div>
    </div>
  )
}
