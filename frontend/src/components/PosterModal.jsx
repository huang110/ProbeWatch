import { useEffect, useRef, useState } from 'react'
import {
  Check,
  ClipboardText,
  Copy,
  DownloadSimple,
  Eye,
  EyeSlash,
  FilmStrip,
  Globe,
  HardDrive,
  Lightning,
  Rows,
  ShareNetwork,
  Sparkle,
  WifiHigh,
  X,
} from '@phosphor-icons/react'
import { formatBytes, formatLatency, formatRate, numeric, safeArray, safeObject, safeText } from '../lib/format.js'
import { calculateRemainingValue, getNodeBilling, getNodeCustomMeta } from '../lib/billing.js'

function maskIpString(ip) {
  if (!ip) return '—'
  const parts = String(ip).trim().split('.')
  if (parts.length === 4) {
    return `${parts[0]}.${parts[1]}.*.*`
  }
  if (ip.includes(':')) {
    const colons = ip.split(':')
    return `${colons.slice(0, 2).join(':')}:****:****`
  }
  return ip
}

function getRating(latency, loss) {
  if (latency === null && loss === null) return { text: '未知', tone: 'muted' }
  const lat = latency ?? 999
  const lss = loss ?? 100
  if (lat < 100 && lss <= 0.5) return { text: '优秀', tone: 'mint' }
  if (lat < 200 && lss <= 2) return { text: '良好', tone: 'blue' }
  if (lat < 300 && lss <= 5) return { text: '一般', tone: 'amber' }
  if (lat < 450 || lss <= 15) return { text: '较差', tone: 'orange' }
  return { text: '严重异常', tone: 'rose' }
}

export function PosterModal({ node, mediaData = [], pingHistory = {}, onClose }) {
  const canvasRef = useRef(null)
  const [maskIp, setMaskIp] = useState(true)
  const [maskUuid, setMaskUuid] = useState(true)
  const [maskBilling, setMaskBilling] = useState(false)
  const [includeIsp, setIncludeIsp] = useState(true)
  const [includeMedia, setIncludeMedia] = useState(true)

  const [copiedType, setCopiedType] = useState(null)

  const nodeUuid = node?.uuid || node?.id || ''
  const billing = getNodeBilling(nodeUuid, node?.name)
  const calc = calculateRemainingValue(billing)
  const customMeta = getNodeCustomMeta(nodeUuid, node)

  const displayName = customMeta.customName || node?.name || 'ProbeWatch 节点'
  const displayFlag = customMeta.customFlag || node?.flag || '🌐'
  const displayRegion = node?.region || customMeta.region || '公网'
  const publicIp = node?.ipv4 || node?.ip || node?.public_ip || ''
  const maskedIp = maskIp ? maskIpString(publicIp) : (publicIp || '—')
  const displayUuid = maskUuid ? `${nodeUuid.slice(0, 8)}****` : nodeUuid

  // Telemetry
  const cpuPercent = numeric(node?.cpu_percent ?? node?.cpu)
  const memUsed = numeric(node?.memory_used_bytes ?? node?.memUsed)
  const memTotal = numeric(node?.memory_total_bytes ?? node?.memTotal)
  const diskUsed = numeric(node?.filesystem_used_bytes ?? node?.diskUsed)
  const diskTotal = numeric(node?.filesystem_total_bytes ?? node?.diskTotal)
  const rxBytes = numeric(node?.network_rx_bytes)
  const txBytes = numeric(node?.network_tx_bytes)

  // 3-ISP checks
  const checks = safeArray(node?.checks || node?.telemetry?.checks)
  const findCheck = (name) => checks.find((c) => String(c?.label || c?.name || '').includes(name))
  const cu = findCheck('联通')
  const ct = findCheck('电信')
  const cm = findCheck('移动')

  const cuLat = numeric(cu?.latency_ms)
  const cuLoss = numeric(cu?.loss_rate) !== null ? cu.loss_rate * 100 : null
  const ctLat = numeric(ct?.latency_ms)
  const ctLoss = numeric(ct?.loss_rate) !== null ? ct.loss_rate * 100 : null
  const cmLat = numeric(cm?.latency_ms)
  const cmLoss = numeric(cm?.loss_rate) !== null ? cm.loss_rate * 100 : null

  const cuRating = getRating(cuLat, cuLoss)
  const ctRating = getRating(ctLat, ctLoss)
  const cmRating = getRating(cmLat, cmLoss)

  // Media reports
  const mediaReports = safeArray(mediaData).map((item) => {
    const res = safeObject(item.result || item)
    return {
      name: safeText(res.detector || item.detector_id || item.target_id || '').replace(/^media-/, ''),
      status: safeText(res.status),
      region: safeText(res.region),
      latency: numeric(res.latency_ms),
    }
  })

  // Render Canvas
  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    let totalHeight = 365
    if (includeIsp) totalHeight += 135
    if (includeMedia) totalHeight += 135
    totalHeight += 45

    canvas.width = 1000
    canvas.height = totalHeight

    // Background gradient
    const bgGrad = ctx.createLinearGradient(0, 0, 1000, totalHeight)
    bgGrad.addColorStop(0, '#0a0f1d')
    bgGrad.addColorStop(0.5, '#0d1527')
    bgGrad.addColorStop(1, '#080d18')
    ctx.fillStyle = bgGrad
    ctx.fillRect(0, 0, 1000, totalHeight)

    // Border
    ctx.strokeStyle = 'rgba(56, 189, 248, 0.25)'
    ctx.lineWidth = 2
    ctx.strokeRect(1, 1, 998, totalHeight - 2)

    // Top Header bar
    ctx.fillStyle = '#38bdf8'
    ctx.font = 'bold 16px "SF Mono", monospace, sans-serif'
    ctx.fillText('PROBEWATCH · VPS SALE POSTER / 节点出鸡海报', 40, 45)

    ctx.fillStyle = '#64748b'
    ctx.font = '14px sans-serif'
    ctx.fillText(`生成时间: ${new Date().toLocaleString('zh-CN')}`, 680, 45)

    // Node Title & Flag
    ctx.fillStyle = '#f8fafc'
    ctx.font = 'bold 32px sans-serif'
    ctx.fillText(`${displayFlag}  ${displayName}`, 40, 95)

    ctx.fillStyle = '#94a3b8'
    ctx.font = '14px "SF Mono", monospace, sans-serif'
    ctx.fillText(`IP: ${maskedIp}  ·  区域: ${displayRegion}  ·  UUID: ${displayUuid}`, 40, 122)

    // Divider
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.1)'
    ctx.beginPath()
    ctx.moveTo(40, 138)
    ctx.lineTo(960, 138)
    ctx.stroke()

    // Section 1: Hardware Specs Box (Left 440px)
    ctx.fillStyle = 'rgba(255, 255, 255, 0.03)'
    ctx.fillRect(40, 155, 440, 195)
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.08)'
    ctx.strokeRect(40, 155, 440, 195)

    ctx.fillStyle = '#38bdf8'
    ctx.font = 'bold 15px sans-serif'
    ctx.fillText('💻 基础配置与负载', 55, 182)

    ctx.font = '14px sans-serif'
    const hwItems = [
      ['CPU 使用率', cpuPercent !== null ? `${Math.round(cpuPercent)}%` : '暂无数据'],
      ['物理内存', memTotal ? `${formatBytes(memUsed)} / ${formatBytes(memTotal)}` : '暂无数据'],
      ['存储容量', diskTotal ? `${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}` : '暂无数据'],
      ['网络流量', txBytes !== null ? `↑ ${formatBytes(txBytes)} · ↓ ${formatBytes(rxBytes)}` : '暂无数据'],
      ['在线时长', node?.uptime || '正常在线'],
    ]
    hwItems.forEach(([k, v], idx) => {
      const y = 215 + idx * 26
      ctx.fillStyle = '#94a3b8'
      ctx.fillText(k, 55, y)
      ctx.fillStyle = '#f1f5f9'
      ctx.font = 'bold 14px "SF Mono", monospace, sans-serif'
      ctx.fillText(v, 240, y)
      ctx.font = '14px sans-serif'
    })

    // Section 2: Billing & Value Box (Right 440px)
    ctx.fillStyle = 'rgba(255, 255, 255, 0.03)'
    ctx.fillRect(520, 155, 440, 195)
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.08)'
    ctx.strokeRect(520, 155, 440, 195)

    ctx.fillStyle = '#34d399'
    ctx.font = 'bold 15px sans-serif'
    ctx.fillText('💰 账单与出鸡残值', 535, 182)

    const remainingDays = calc.daysRemaining ?? calc.remainingDays
    const price = billing.price ?? billing.amount
    const dueDate = billing.dueDate || billing.expiryDate

    const billItems = maskBilling
      ? [
          ['账单状态', '用户已隐藏成本账单信息'],
          ['剩余价值', '私聊商议'],
          ['续费周期', '私聊商议'],
        ]
      : [
          ['计费周期', billing.cycle === 'free' ? '免费 / 永久' : (billing.cycle ? `${billing.cycle} · $${price || 0}` : '未配置')],
          ['到期时间', dueDate || '未配置'],
          ['剩余天数', (typeof remainingDays === 'number' && Number.isFinite(remainingDays)) ? `${remainingDays} 天` : '未配置'],
          ['折算剩余价值', `¥ ${(calc.remainingValueCNY || 0).toFixed(1)} 元`],
          ['出鸡建议', (remainingDays > 0) ? '建议按剩余价值原价或微溢折价出' : (billing.cycle === 'free' ? '传家宝永久免费鸡' : '账单未配置或已临近到期')],
        ]
    billItems.forEach(([k, v], idx) => {
      const y = 215 + idx * 26
      ctx.fillStyle = '#94a3b8'
      ctx.fillText(k, 535, y)
      ctx.fillStyle = idx === 3 && !maskBilling ? '#10b981' : '#f1f5f9'
      ctx.font = 'bold 14px "SF Mono", monospace, sans-serif'
      ctx.fillText(v, 700, y)
      ctx.font = '14px sans-serif'
    })

    let currentY = 365

    // Section 3: Three-Network Quality (Full width 920px)
    if (includeIsp) {
      ctx.fillStyle = 'rgba(255, 255, 255, 0.03)'
      ctx.fillRect(40, currentY, 920, 120)
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.08)'
      ctx.strokeRect(40, currentY, 920, 120)

      ctx.fillStyle = '#fbbf24'
      ctx.font = 'bold 15px sans-serif'
      ctx.fillText('📶 三网链路质量与回程状态', 55, currentY + 27)

      const isps = [
        { name: '中国电信 (CT)', lat: ctLat, loss: ctLoss, rate: ctRating },
        { name: '中国联通 (CU)', lat: cuLat, loss: cuLoss, rate: cuRating },
        { name: '中国移动 (CM)', lat: cmLat, loss: cmLoss, rate: cmRating },
      ]
      isps.forEach((isp, idx) => {
        const x = 55 + idx * 305
        ctx.fillStyle = '#f8fafc'
        ctx.font = 'bold 14px sans-serif'
        ctx.fillText(isp.name, x, currentY + 60)

        ctx.fillStyle = '#94a3b8'
        ctx.font = '13px "SF Mono", monospace, sans-serif'
        ctx.fillText(`延迟: ${formatLatency(isp.lat)}`, x, currentY + 83)
        ctx.fillText(`丢包: ${isp.loss !== null ? isp.loss.toFixed(1) + '%' : '暂无数据'}`, x, currentY + 103)

        // Rating badge
        ctx.fillStyle = isp.rate.tone === 'mint' ? '#059669' : isp.rate.tone === 'blue' ? '#2563eb' : isp.rate.tone === 'amber' ? '#d97706' : '#dc2626'
        ctx.fillRect(x + 190, currentY + 47, 50, 22)
        ctx.fillStyle = '#ffffff'
        ctx.font = 'bold 12px sans-serif'
        ctx.fillText(isp.rate.text, x + 200, currentY + 62)
      })

      currentY += 135
    }

    // Section 4: Media & AI Unlocks (Bottom 920px)
    if (includeMedia) {
      ctx.fillStyle = 'rgba(255, 255, 255, 0.03)'
      ctx.fillRect(40, currentY, 920, 120)
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.08)'
      ctx.strokeRect(40, currentY, 920, 120)

      ctx.fillStyle = '#a855f7'
      ctx.font = 'bold 15px sans-serif'
      ctx.fillText('🎬 流媒体与 AI 原生解锁矩阵', 55, currentY + 27)

      const mediaList = [
        { id: 'chatgpt', label: 'ChatGPT' },
        { id: 'claude', label: 'Claude AI' },
        { id: 'netflix', label: 'Netflix' },
        { id: 'youtube', label: 'YouTube' },
        { id: 'tiktok', label: 'TikTok' },
        { id: 'disney', label: 'Disney+' },
        { id: 'spotify', label: 'Spotify' },
        { id: 'bilibili', label: 'Bilibili' },
      ]
      mediaList.forEach((m, idx) => {
        const rep = mediaReports.find((r) => r.name.toLowerCase().includes(m.id))
        let icon = '—'
        let text = '暂无数据'
        let tone = '#64748b'
        if (rep && rep.status) {
          if (rep.status === 'available') {
            icon = '✔'
            text = '可用'
            tone = '#10b981'
          } else if (rep.status === 'partial') {
            icon = '⚡'
            text = '部分'
            tone = '#38bdf8'
          } else if (rep.status === 'checking') {
            icon = '⏳'
            text = '检测中'
            tone = '#fbbf24'
          } else if (rep.status === 'unavailable') {
            icon = '✖'
            text = '不可用'
            tone = '#f43f5e'
          }
        }
        const x = 55 + (idx % 4) * 225
        const y = currentY + 62 + Math.floor(idx / 4) * 32

        ctx.fillStyle = tone
        ctx.font = 'bold 13px sans-serif'
        ctx.fillText(`${icon} ${text}`, x, y)

        ctx.fillStyle = (rep?.status === 'available' || rep?.status === 'partial') ? '#f8fafc' : '#94a3b8'
        ctx.font = '13px sans-serif'
        ctx.fillText(`${m.label}${rep?.region ? ` (${rep.region})` : ''}`, x + 65, y)
      })

      currentY += 135
    }

    // Footer signature
    ctx.fillStyle = '#475569'
    ctx.font = '12px "SF Mono", monospace, sans-serif'
    ctx.fillText('Powered by ProbeWatch · 轻量化极客 VPS 监控看板', 40, currentY + 22)
  }, [node, maskIp, maskUuid, maskBilling, includeIsp, includeMedia, mediaData])

  // Copy PNG to Clipboard
  const handleCopyImage = async () => {
    const canvas = canvasRef.current
    if (!canvas) return
    canvas.toBlob(async (blob) => {
      if (!blob) return
      try {
        if (navigator.clipboard?.write && typeof ClipboardItem !== 'undefined') {
          await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })])
          setCopiedType('image')
          setTimeout(() => setCopiedType(null), 2500)
          return
        }
      } catch (err) {
        console.error('Copy image failed, falling back to download:', err)
      }
      handleDownloadImage()
    }, 'image/png')
  }

  // Download PNG
  const handleDownloadImage = () => {
    const canvas = canvasRef.current
    if (!canvas) return
    const url = canvas.toDataURL('image/png')
    const a = document.createElement('a')
    a.href = url
    a.download = `probewatch-poster-${displayName.replace(/\s+/g, '-')}.png`
    a.click()
    setCopiedType('download')
    setTimeout(() => setCopiedType(null), 2500)
  }

  // Copy Markdown (Hostloc / NodeSeek format)
  const handleCopyMarkdown = async () => {
    const lines = [
      `### ${displayFlag} 【出鸡 / 测速】${displayName}`,
      '',
      `**基本信息：**`,
      `- 节点位置：${displayRegion}`,
      `- 公网地址：${maskedIp}`,
      `- 节点标识：${displayUuid}`,
      `- 硬件规格：CPU ${cpuPercent !== null ? Math.round(cpuPercent) + '%' : '—'} · 内存 ${memTotal ? formatBytes(memUsed) + '/' + formatBytes(memTotal) : '—'} · 硬盘 ${diskTotal ? formatBytes(diskUsed) + '/' + formatBytes(diskTotal) : '—'}`,
      `- 累计流量：↑ ${formatBytes(txBytes || 0)} · ↓ ${formatBytes(rxBytes || 0)}`,
      '',
    ]

    if (!maskBilling) {
      const remainingDays = calc.daysRemaining ?? calc.remainingDays
      const price = billing.price ?? billing.amount
      const dueDate = billing.dueDate || billing.expiryDate
      lines.push(
        `**账单信息：**`,
        `- 续费价格：${billing.cycle === 'free' ? '免费永久' : (billing.cycle ? billing.cycle + ' $' + (price || 0) : '未配置')}`,
        `- 到期时间：${dueDate || '未配置'} (剩余 ${remainingDays !== null && remainingDays !== undefined ? remainingDays + ' 天' : '未配置'})`,
        `- 剩余价值：折合约 ¥${(calc.remainingValueCNY || 0).toFixed(1)} 元`,
        ''
      )
    }

    if (includeIsp) {
      lines.push(
        `**三网回程质量：**`,
        `| 运营商 | 延迟 | 丢包率 | 评级 |`,
        `| :--- | :--- | :--- | :--- |`,
        `| 🇨🇳 电信 (CT) | ${formatLatency(ctLat)} | ${ctLoss !== null ? ctLoss.toFixed(1) + '%' : '暂无数据'} | ${ctRating.text} |`,
        `| 🇨🇳 联通 (CU) | ${formatLatency(cuLat)} | ${cuLoss !== null ? cuLoss.toFixed(1) + '%' : '暂无数据'} | ${cuRating.text} |`,
        `| 🇨🇳 移动 (CM) | ${formatLatency(cmLat)} | ${cmLoss !== null ? cmLoss.toFixed(1) + '%' : '暂无数据'} | ${cmRating.text} |`,
        ''
      )
    }

    if (includeMedia) {
      const mediaList = [
        { id: 'chatgpt', label: 'ChatGPT' },
        { id: 'claude', label: 'Claude AI' },
        { id: 'netflix', label: 'Netflix' },
        { id: 'youtube', label: 'YouTube' },
        { id: 'tiktok', label: 'TikTok' },
        { id: 'disney', label: 'Disney+' },
        { id: 'spotify', label: 'Spotify' },
        { id: 'bilibili', label: 'Bilibili' },
      ]
      const badges = mediaList.map((m) => {
        const rep = mediaReports.find((r) => r.name.toLowerCase().includes(m.id))
        let st = '暂无数据'
        let icon = '⚪'
        if (rep && rep.status) {
          if (rep.status === 'available') {
            st = '可用'
            icon = '✅'
          } else if (rep.status === 'partial') {
            st = '部分可用'
            icon = '⚡'
          } else if (rep.status === 'checking') {
            st = '检测中'
            icon = '⏳'
          } else if (rep.status === 'unavailable') {
            st = '不可用'
            icon = '❌'
          }
        }
        return `${icon} ${m.label}: ${st}${rep?.region ? `(${rep.region})` : ''}`
      }).join(' · ')
      lines.push(`**流媒体 & AI 解锁：**`, `> ${badges}`, '')
    }

    lines.push(`> 来自 ProbeWatch 探针 · 生成时间: ${new Date().toLocaleString('zh-CN')}`)
    const text = lines.join('\n')

    try {
      await navigator.clipboard.writeText(text)
      setCopiedType('markdown')
      setTimeout(() => setCopiedType(null), 2500)
    } catch {
      // fallback
    }
  }

  // Copy Plain Text (Telegram / QQ format)
  const handleCopyPlainText = async () => {
    const lines = [
      `【出鸡】${displayFlag} ${displayName}`,
      `地区: ${displayRegion} | IP: ${maskedIp}`,
      `配置: CPU ${cpuPercent !== null ? Math.round(cpuPercent) + '%' : '—'} | 内存: ${memTotal ? formatBytes(memUsed) + '/' + formatBytes(memTotal) : '—'}`,
      `流量: 已跑 ↑${formatBytes(txBytes || 0)} / ↓${formatBytes(rxBytes || 0)}`,
    ]
    if (!maskBilling) {
      const remainingDays = calc.daysRemaining ?? calc.remainingDays
      lines.push(`账单: ${billing.cycle || '未配置'} | 剩余: ${remainingDays !== null && remainingDays !== undefined ? remainingDays + '天' : '未配置'} | 残值: ¥${(calc.remainingValueCNY || 0).toFixed(1)}`)
    }
    if (includeIsp) {
      lines.push(`三网: 电信 ${formatLatency(ctLat)} (丢包: ${ctLoss !== null ? ctLoss.toFixed(1) + '%' : '暂无数据'}) | 联通 ${formatLatency(cuLat)} (丢包: ${cuLoss !== null ? cuLoss.toFixed(1) + '%' : '暂无数据'}) | 移动 ${formatLatency(cmLat)} (丢包: ${cmLoss !== null ? cmLoss.toFixed(1) + '%' : '暂无数据'})`)
    }
    if (includeMedia) {
      const getMediaBadge = (id, shortLabel) => {
        const rep = mediaReports.find((r) => r.name.toLowerCase().includes(id))
        if (!rep || !rep.status) return `${shortLabel}(暂无数据)`
        if (rep.status === 'available') return `${shortLabel}(${rep.region || '可用'})`
        if (rep.status === 'partial') return `${shortLabel}(部分:${rep.region || '可用'})`
        if (rep.status === 'checking') return `${shortLabel}(检测中)`
        if (rep.status === 'unavailable') return `${shortLabel}(不可用)`
        return `${shortLabel}(${rep.status})`
      }
      lines.push(`解锁: ${getMediaBadge('chatgpt', 'GPT')} ${getMediaBadge('claude', 'Claude')} ${getMediaBadge('netflix', 'NF')} ${getMediaBadge('disney', 'Disney')} ${getMediaBadge('youtube', 'YT')}`)
    }
    lines.push(`时间: ${new Date().toLocaleString('zh-CN')} (ProbeWatch)`)

    const text = lines.join('\n')
    try {
      await navigator.clipboard.writeText(text)
      setCopiedType('text')
      setTimeout(() => setCopiedType(null), 2500)
    } catch {}
  }

  return (
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 1200 }}>
      <div className="modal-dialog poster-modal-dialog" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '1040px', width: '95%' }}>
        <div className="modal-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <h3 style={{ margin: 0, display: 'flex', alignItems: 'center', gap: '8px' }}>
              <ShareNetwork size={20} className="text-mint" />
              <span>MJJ 出鸡与测速海报生成器</span>
            </h3>
            <p style={{ margin: '4px 0 0 0', fontSize: '12px', color: 'var(--text-muted)' }}>
              支持一键生成高清晰度出鸡卡片、Hostloc/NodeSeek Markdown 排版及 Telegram 纯文本格式。
            </p>
          </div>
          <button type="button" className="button button-quiet btn-sm" onClick={onClose}>
            <X size={16} />
          </button>
        </div>

        {/* 隐私与展示选项栏 */}
        <div className="poster-toggles-bar" style={{ display: 'flex', flexWrap: 'wrap', gap: '16px', padding: '10px 16px', background: 'var(--bg-subtle, rgba(255,255,255,0.03))', borderRadius: '8px', margin: '12px 0' }}>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={maskIp} onChange={(e) => setMaskIp(e.target.checked)} />
            <span>脱敏完整 IP (推荐)</span>
          </label>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={maskUuid} onChange={(e) => setMaskUuid(e.target.checked)} />
            <span>脱敏节点 UUID</span>
          </label>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={maskBilling} onChange={(e) => setMaskBilling(e.target.checked)} />
            <span>隐藏成本与账单信息</span>
          </label>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={includeIsp} onChange={(e) => setIncludeIsp(e.target.checked)} />
            <span>包含三网测速</span>
          </label>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={includeMedia} onChange={(e) => setIncludeMedia(e.target.checked)} />
            <span>包含流媒体与 AI 解锁</span>
          </label>
        </div>

        {/* 海报 Canvas 预览 */}
        <div className="poster-canvas-preview" style={{ overflowX: 'auto', textAlign: 'center', padding: '10px 0', background: '#050811', borderRadius: '8px' }}>
          <canvas ref={canvasRef} style={{ width: '100%', maxWidth: '980px', height: 'auto', borderRadius: '6px', boxShadow: '0 8px 32px rgba(0,0,0,0.5)' }} />
        </div>

        {/* 底部一键复制与导出按钮组 */}
        <div className="modal-footer" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px', marginTop: '16px' }}>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
            默认已自动保护敏感信息 · 支持全平台论坛粘贴
          </div>
          <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
            <button type="button" className="button button-quiet" onClick={handleCopyPlainText}>
              {copiedType === 'text' ? <Check size={16} className="text-mint" /> : <Copy size={16} />}
              <span>{copiedType === 'text' ? '已复制纯文本' : '复制 TG / 纯文本'}</span>
            </button>
            <button type="button" className="button button-quiet" onClick={handleCopyMarkdown}>
              {copiedType === 'markdown' ? <Check size={16} className="text-mint" /> : <ClipboardText size={16} />}
              <span>{copiedType === 'markdown' ? '已复制 Markdown' : '复制论坛 Markdown'}</span>
            </button>
            <button type="button" className="button button-primary" onClick={handleCopyImage}>
              {copiedType === 'image' ? <Check size={16} /> : <Copy size={16} />}
              <span>{copiedType === 'image' ? '图片已入剪贴板' : '复制海报图片'}</span>
            </button>
            <button type="button" className="button button-primary" onClick={handleDownloadImage}>
              <DownloadSimple size={16} />
              <span>下载 PNG</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
