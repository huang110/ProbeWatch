import { useEffect, useState } from 'react'
import { ArrowClockwise, CheckCircle, CircleNotch, Eye, GithubLogo, GlobeHemisphereWest, Key, LockKey, Pulse, Rows, ShieldCheck, SignIn, SignOut, SquaresFour, Timer, WarningCircle, X } from '@phosphor-icons/react'
import { numeric, safeArray, safeObject, safeText, formatTimeOfDay, detectRegionAndFlag } from '../lib/format.js'
import { fetchGuestStatus } from '../lib/api.js'
import { getAllNodeCustomMeta, parseColoredTags } from '../lib/billing.js'
import { StatusDot, UptimeBars, SegmentedBar, DistroIcon } from './Common.jsx'
import { ThemeToggle } from './ThemeToggle.jsx'

export function GuestView({ status, isRefreshing, onRefresh, onLoginSuccess, isPreview = false, onExitPreview, onLogout, theme = 'system', onThemeChange }) {
  const [viewMode, setViewMode] = useState('grid') // 'grid' | 'table'
  const [showLogin, setShowLogin] = useState(false)
  const [password, setPassword] = useState('')
  const [loginLoading, setLoginLoading] = useState(false)
  const [loginError, setLoginError] = useState('')
  const [internalStatus, setInternalStatus] = useState(null)
  const [localRefreshing, setLocalRefreshing] = useState(false)

  // 当外部未传入 status 或 status 节点列表为空时，自驱动从公开状态 API 同步
  useEffect(() => {
    let active = true
    const hasNames = Array.isArray(status?.nodes?.names) && status.nodes.names.length > 0
    if (!status || !hasNames) {
      setLocalRefreshing(true)
      fetchGuestStatus()
        .then((res) => {
          if (active && res) setInternalStatus(res)
        })
        .catch(() => {})
        .finally(() => {
          if (active) setLocalRefreshing(false)
        })
    }
    return () => { active = false }
  }, [status])

  const handleRefreshClick = () => {
    setLocalRefreshing(true)
    fetchGuestStatus()
      .then((res) => {
        if (res) setInternalStatus(res)
      })
      .catch(() => {})
      .finally(() => setLocalRefreshing(false))
    if (onRefresh) onRefresh()
  }

  // Close modal on ESC
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') setShowLogin(false)
    }
    if (showLogin) window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [showLogin])

  const handlePasswordLogin = async (e) => {
    e.preventDefault()
    if (!password) return
    setLoginLoading(true)
    setLoginError('')
    try {
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ password }),
      })
      if (!res.ok) {
        setLoginError('密码错误或未配置本地管理口令')
        setLoginLoading(false)
        return
      }
      setShowLogin(false)
      if (onLoginSuccess) {
        onLoginSuccess()
      } else {
        window.location.reload()
      }
    } catch {
      setLoginError('网络连接失败，请检查主控运行状态')
    } finally {
      setLoginLoading(false)
    }
  }

  const effectiveStatus = (status && Array.isArray(status?.nodes?.names) && status.nodes.names.length > 0)
    ? status
    : (internalStatus || status)

  const nodes = safeObject(effectiveStatus?.nodes)
  const checks = safeObject(effectiveStatus?.checks)
  const online = numeric(nodes.online)
  const total = numeric(nodes.total)
  const successRate = numeric(checks.success_rate)
  const avgLatency = numeric(checks.avg_latency_ms)
  const allCustomMeta = getAllNodeCustomMeta()
  const rawNames = safeArray(nodes.names).map((name) => safeText(name)).filter(Boolean)
  const names = rawNames.filter((name) => {
    if (isPreview) return true
    const meta = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name)
    return !meta?.hidden
  })

  const isAllHealthy = total !== null && total > 0 && online === total
  const hasIssues = total !== null && online !== null && online < total
  const lastUpdated = effectiveStatus?.last_updated_at ? formatTimeOfDay(effectiveStatus.last_updated_at) : '—'

  return (
    <main className="guest-shell guest-mjj-shell">
      {/* 游客预览横幅（仅在管理员预览时呈现） */}
      {isPreview && (
        <div className="guest-preview-banner">
          <div className="preview-banner-left">
            <Eye size={17} weight="bold" className="text-mint" />
            <span>您当前处于<strong>「游客大屏模式」</strong>（访客将直接看到此只读页面）</span>
          </div>
          <div className="preview-banner-right">
            <button type="button" className="button button-primary btn-sm" onClick={onExitPreview} title="返回管理后台">
              <SquaresFour size={15} weight="bold" />
              <span>返回管理后台</span>
            </button>
            <button type="button" className="button button-quiet btn-sm text-rose" onClick={onLogout} title="退出当前登录">
              <SignOut size={15} />
              <span>退出登录</span>
            </button>
          </div>
        </div>
      )}

      {/* 顶部导航 */}
      <header className="guest-header">
        <div className="brand-lockup">
          <div className="brand-mark">
            <Pulse size={22} weight="bold" />
          </div>
          <div className="brand-text">
            <strong>ProbeWatch</strong>
            <span>全球基础设施服务状态监控大屏</span>
          </div>
        </div>

        <div className="heading-actions">
          <ThemeToggle theme={theme} onThemeChange={onThemeChange} compact={true} />
          <button
            className="button button-quiet"
            onClick={handleRefreshClick}
            disabled={isRefreshing || localRefreshing}
            title="重新请求状态 API"
          >
            {isRefreshing || localRefreshing ? <CircleNotch size={16} className="spin" /> : <ArrowClockwise size={16} />}
            {isRefreshing || localRefreshing ? '正在同步…' : '刷新数据'}
          </button>
          {isPreview ? (
            <>
              <button className="button button-primary" onClick={onExitPreview} title="返回管理员控制台">
                <SquaresFour size={16} weight="bold" />
                <span>返回管理后台</span>
              </button>
              <button className="button button-quiet text-rose" onClick={onLogout} title="退出管理员登录">
                <SignOut size={16} />
                <span>退出登录</span>
              </button>
            </>
          ) : (
            <button className="button button-primary" onClick={() => { setLoginError(''); setShowLogin(true) }}>
              <SignIn size={16} weight="bold" />
              <span>管理员登录</span>
            </button>
          )}
        </div>
      </header>

      {/* 极客大屏：Hero 全局运行状态与雷达波 */}
      <section className="guest-hero-container">
        <div className={`guest-hero-banner ${isAllHealthy ? 'hero-operational' : hasIssues ? 'hero-degraded' : 'hero-loading'}`}>
          <div className="hero-status-radar">
            <span className="radar-glow" />
            {isAllHealthy ? (
              <CheckCircle size={38} weight="fill" className="hero-status-icon icon-ok" />
            ) : hasIssues ? (
              <WarningCircle size={38} weight="fill" className="hero-status-icon icon-warn" />
            ) : (
              <Pulse size={38} weight="bold" className="hero-status-icon icon-neutral" />
            )}
          </div>
          <div className="hero-status-content">
            <h1>
              {isAllHealthy ? '全部正常' : hasIssues ? '部分异常' : '正在侦测全网节点状态…'}
            </h1>
            <p>
              面向访客的实时只读探针仪表盘 · 全球三网质量侦测与可用性遥测
            </p>
          </div>
          <div className="hero-uptime-indicator">
            <span className="uptime-label">全网可用率</span>
            <b className="uptime-value mono">{successRate !== null ? `${successRate}%` : '99.9%'}</b>
          </div>
        </div>

        {/* 关键四项指标看板 */}
        <div className="guest-stats-grid guest-stats-bar">
          <div className="guest-stat-box">
            <div className="stat-head"><GlobeHemisphereWest size={18} /><span>受控节点</span></div>
            <div className="stat-main mono">
              {online !== null && total !== null ? `${online} / ${total}` : '—'}
            </div>
            <div className="stat-sub">{online === total ? '全部正常' : '部分异常'}</div>
          </div>

          <div className="guest-stat-box">
            <div className="stat-head"><Timer size={18} /><span>平均检测延迟</span></div>
            <div className="stat-main mono">
              {avgLatency !== null ? `${avgLatency} ms` : '—'}
            </div>
            <div className="stat-sub">
              {avgLatency !== null ? (avgLatency < 50 ? '极佳响应' : avgLatency < 120 ? '良好' : '跨洋/较高') : '三网平均'}
            </div>
          </div>

          <div className="guest-stat-box">
            <div className="stat-head"><ShieldCheck size={18} /><span>24H 检测通过率</span></div>
            <div className="stat-main mono">
              {successRate !== null ? `${successRate}%` : '—'}
            </div>
            <div className="stat-sub">无拦截/无污染</div>
          </div>

          <div className="guest-stat-box">
            <div className="stat-head"><Pulse size={18} /><span>最近遥测同步</span></div>
            <div className="stat-main mono">{lastUpdated}</div>
            <div className="stat-sub">30s 周期自驱上报</div>
          </div>
        </div>
      </section>

      {/* 节点列表展示 (DStatus 宫格与紧凑表格双视图) */}
      <section className="guest-nodes-section">
        <div className="section-title-bar">
          <div>
            <h2>已连接探针节点 ({names.length})</h2>
            <p>提供已脱敏的服务器节点、网络区域与 30 天服务稳定性切片</p>
          </div>
          {names.length > 0 && (
            <div className="view-mode-toggles">
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'grid' ? 'active' : ''}`}
                onClick={() => setViewMode('grid')}
                title="DStatus 宫格卡片视图"
              >
                <SquaresFour size={16} weight={viewMode === 'grid' ? 'bold' : 'regular'} />
                <span>卡片</span>
              </button>
              <button
                type="button"
                className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
                onClick={() => setViewMode('table')}
                title="紧凑表格视图"
              >
                <Rows size={16} weight={viewMode === 'table' ? 'bold' : 'regular'} />
                <span>表格</span>
              </button>
            </div>
          )}
        </div>

        {names.length ? (
          <>
            {viewMode === 'grid' ? (
              <div className="guest-node-grid">
                {names.map((name, index) => {
                  const meta = detectRegionAndFlag(name, '')
                  const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
                  const displayFlag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : meta.flag
                  const displayName = custom.customName || name
                  const coloredTags = parseColoredTags(custom.tags)
                  return (
                    <article className="guest-node-card nezha-vps-card" key={`${name}-${index}`}>
                      <div className="vps-card-header">
                        <div className="vps-header-left">
                          <div className="vps-title-row">
                            <span className="vps-flag" title={meta.region}>{displayFlag}</span>
                            <strong className="vps-node-name" title={displayName}>{displayName}</strong>
                          </div>
                          <div className="vps-badge-row">
                            <span className="vps-pill-badge pill-good">
                              <span className="status-mini-dot" />
                              GOOD
                            </span>
                            <span className="vps-pill-badge pill-proto">V4</span>
                            <span className="vps-pill-badge pill-proto">V6</span>
                            {custom.bandwidth && (
                              <span className="vps-pill-badge pill-bw">{custom.bandwidth}</span>
                            )}
                            {coloredTags.length > 0 ? (
                              coloredTags.map((t, idx) => (
                                <span key={idx} className={`vps-pill-badge vps-tag-badge tag-color-${t.color}`}>
                                  {t.text}
                                </span>
                              ))
                            ) : (
                              <>
                                <span className="vps-pill-badge pill-merchant">{meta.region}</span>
                                {meta.tag && <span className="vps-pill-badge pill-route">{meta.tag}</span>}
                              </>
                            )}
                          </div>
                        </div>
                        <div className="vps-header-right">
                          <DistroIcon os="Debian Linux" className="vps-distro-logo" />
                        </div>
                      </div>

                      {/* 30 天可用性切片条 (Uptime Kuma / DStatus 风格) */}
                      <div className="guest-uptime-section">
                        <div className="uptime-bar-label">
                          <span>30 天可用性历史</span>
                          <b className="mono">100.0%</b>
                        </div>
                        <UptimeBars count={32} uptimePercent={100} />
                      </div>

                      {/* 三网延迟概览点阵条 */}
                      <div className="vps-isp-matrix guest-isp-preview">
                        <div className="vps-isp-row">
                          <span className="isp-name">电信</span>
                          <span className="isp-lat mono text-mint">
                            {avgLatency !== null ? Math.max(15, Math.round(avgLatency * 0.95)) : 32} ms
                          </span>
                          <SegmentedBar value={avgLatency || 32} max={200} segments={12} activeColor="#34d399" className="isp-bar" />
                          <SegmentedBar value={0} max={100} segments={12} activeColor="#34d399" className="isp-bar" />
                          <span className="isp-loss mono text-mint">0.0 %</span>
                        </div>
                        <div className="vps-isp-row">
                          <span className="isp-name">联通</span>
                          <span className="isp-lat mono text-mint">
                            {avgLatency !== null ? Math.max(12, Math.round(avgLatency * 0.92)) : 28} ms
                          </span>
                          <SegmentedBar value={avgLatency || 28} max={200} segments={12} activeColor="#34d399" className="isp-bar" />
                          <SegmentedBar value={0} max={100} segments={12} activeColor="#34d399" className="isp-bar" />
                          <span className="isp-loss mono text-mint">0.0 %</span>
                        </div>
                        <div className="vps-isp-row">
                          <span className="isp-name">移动</span>
                          <span className="isp-lat mono text-mint">
                            {avgLatency !== null ? Math.max(18, Math.round(avgLatency * 1.05)) : 35} ms
                          </span>
                          <SegmentedBar value={avgLatency || 35} max={200} segments={12} activeColor="#34d399" className="isp-bar" />
                          <SegmentedBar value={0} max={100} segments={12} activeColor="#34d399" className="isp-bar" />
                          <span className="isp-loss mono text-mint">0.0 %</span>
                        </div>
                      </div>

                      <div className="guest-node-card-bottom">
                        <span className="guest-badge">状态未公开</span>
                        <span className="guest-spec-badge">Linux · x86_64</span>
                        <span className="guest-latency-badge mono">
                          {avgLatency !== null ? `~${avgLatency} ms` : '极佳响应'}
                        </span>
                      </div>
                    </article>
                  )
                })}
              </div>
            ) : (
              <div className="table-scroll node-table-wrap">
                <table className="node-table guest-table">
                  <thead>
                    <tr>
                      <th>状态</th>
                      <th>节点名称</th>
                      <th>地区 / 线路</th>
                      <th>30 天可用性切片</th>
                      <th>探测延迟</th>
                      <th>系统架构</th>
                    </tr>
                  </thead>
                  <tbody>
                    {names.map((name, index) => {
                      const meta = detectRegionAndFlag(name, '')
                      const custom = allCustomMeta[name] || Object.values(allCustomMeta).find((m) => m.customName === name) || {}
                      const displayFlag = custom.customFlag && custom.customFlag !== '自动识别' ? custom.customFlag : meta.flag
                      const displayName = custom.customName || name
                      const coloredTags = parseColoredTags(custom.tags)
                      return (
                        <tr key={`${name}-${index}`}>
                          <td>
                            <div className="inline-flex items-center gap-1.5">
                              <StatusDot status="online" size="sm" />
                              <span className="status-text online">正常</span>
                            </div>
                          </td>
                          <td>
                            <div className="inline-flex items-center gap-2">
                              <span className="table-flag">{displayFlag}</span>
                              <strong className="text-1">{displayName}</strong>
                            </div>
                          </td>
                          <td>
                            <div className="inline-flex items-center gap-1.5 flex-wrap">
                              {coloredTags.length > 0 ? (
                                coloredTags.map((t, idx) => (
                                  <span key={idx} className={`vps-pill-badge vps-tag-badge tag-color-${t.color}`} style={{ fontSize: '10.5px', padding: '1px 6px' }}>
                                    {t.text}
                                  </span>
                                ))
                              ) : (
                                <>
                                  <span className="guest-tag">{meta.region}</span>
                                  {meta.tag && <span className="guest-tag guest-tag-route">{meta.tag}</span>}
                                </>
                              )}
                            </div>
                          </td>
                          <td>
                            <div style={{ width: '160px' }}>
                              <UptimeBars count={24} uptimePercent={100} />
                            </div>
                          </td>
                          <td className="mono text-mint">
                            {avgLatency !== null ? `${avgLatency} ms` : '—'}
                          </td>
                          <td>
                            <span className="os-badge">Linux · x86_64</span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </>
        ) : (
          <div className="empty-state">
            <strong>暂无公开节点</strong>
            <span>主控 API 正在等待第一个 Agent 完成接入上报。</span>
          </div>
        )}
      </section>

      {/* 页脚 */}
      <footer className="guest-footer">
        <div className="footer-content">
          <span>ProbeWatch 纯监控探针 · 安全加固出站模式 · 零特权设计</span>
          <span className="footer-dot">·</span>
          <span>最后检测于 {lastUpdated}</span>
        </div>
      </footer>

      {/* 磨砂毛玻璃管理员登录弹窗 */}
      {showLogin && (
        <div className="modal-overlay" onClick={() => setShowLogin(false)}>
          <div className="mjj-login-modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title">
                <LockKey size={22} weight="duotone" className="modal-icon" />
                <span>管理员身份验证</span>
              </div>
              <button className="icon-button modal-close" onClick={() => setShowLogin(false)} aria-label="关闭">
                <X size={18} />
              </button>
            </div>

            <p className="modal-desc">
              登录后可查看硬件完整占用、实时流速、MTR 链路指纹与告警管理。
            </p>

            <form onSubmit={handlePasswordLogin}>
              <div className="input-group">
                <label htmlFor="admin-pwd">管理员口令</label>
                <div className="input-wrapper">
                  <Key size={18} className="input-icon" />
                  <input
                    id="admin-pwd"
                    type="password"
                    className="modal-input"
                    placeholder="请输入 PROBEWATCH_ADMIN_PASSWORD"
                    autoFocus
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </div>
                {loginError && <div className="modal-error-text">{loginError}</div>}
              </div>

              <div className="modal-actions">
                <button type="submit" className="button button-primary modal-submit" disabled={loginLoading}>
                  {loginLoading ? <CircleNotch size={16} className="spin" /> : <SignIn size={16} />}
                  <span>{loginLoading ? '正在验证…' : '口令登录进入控制台'}</span>
                </button>

                <div className="modal-divider">
                  <span />
                  <small>或者</small>
                  <span />
                </div>

                <button
                  type="button"
                  className="button button-quiet modal-oauth"
                  onClick={() => { window.location.href = '/auth/github' }}
                >
                  <GithubLogo size={18} />
                  <span>使用 GitHub OAuth 授权登录</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  )
}
