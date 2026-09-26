import { useCallback, useEffect, useState } from 'react'
import {
  ArrowsClockwise,
  Certificate,
  CheckCircle,
  Clock,
  Globe,
  MagnifyingGlass,
  ShieldCheck,
  ShieldWarning,
  Warning,
  WarningCircle,
  WarningOctagon,
  X,
  XCircle,
} from '@phosphor-icons/react'
import { fetchCertificates, fetchDNSMatrix } from '../lib/api.js'

export function CertificatesAndDNSView({ initialTab = 'certificates' }) {
  const [activeTab, setActiveTab] = useState(initialTab) // 'certificates' | 'dns'
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState(null)

  // Certificate state
  const [certData, setCertData] = useState({ total: 0, valid_count: 0, expiring_soon: 0, expired_count: 0, certificates: [] })
  const [certFilter, setCertFilter] = useState('all') // 'all' | 'expiring' | 'expired' | 'valid'
  const [certSearch, setCertSearch] = useState('')
  const [selectedCert, setSelectedCert] = useState(null)

  // DNS matrix state
  const [dnsData, setDnsData] = useState({ total_targets: 0, consistent_count: 0, divergent_count: 0, matrix: [] })
  const [dnsFilter, setDnsFilter] = useState('all') // 'all' | 'divergent' | 'consistent'
  const [dnsSearch, setDnsSearch] = useState('')

  const loadData = useCallback(async (isManual = false) => {
    if (isManual) setRefreshing(true)
    setError(null)
    try {
      const [certs, dns] = await Promise.all([
        fetchCertificates().catch(() => ({ total: 0, valid_count: 0, expiring_soon: 0, expired_count: 0, certificates: [] })),
        fetchDNSMatrix().catch(() => ({ total_targets: 0, consistent_count: 0, divergent_count: 0, matrix: [] })),
      ])
      setCertData(certs || { total: 0, valid_count: 0, expiring_soon: 0, expired_count: 0, certificates: [] })
      setDnsData(dns || { total_targets: 0, consistent_count: 0, divergent_count: 0, matrix: [] })
    } catch (err) {
      setError(err.message || '加载合成探针数据失败')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    const timer = setInterval(() => loadData(false), 30000)
    return () => clearInterval(timer)
  }, [loadData])

  // Filtered certificates
  const filteredCerts = (certData.certificates || []).filter((cert) => {
    if (certFilter === 'expiring' && !cert.expiring_soon && cert.days_left > 14) return false
    if (certFilter === 'expired' && !cert.is_expired && cert.days_left > 0) return false
    if (certFilter === 'valid' && (cert.is_expired || cert.expiring_soon || cert.days_left <= 14)) return false
    if (certSearch) {
      const q = certSearch.toLowerCase()
      const matchHost = (cert.host || '').toLowerCase().includes(q)
      const matchSub = (cert.subject || '').toLowerCase().includes(q)
      const matchName = (cert.target_name || '').toLowerCase().includes(q)
      const matchIssuer = (cert.issuer || '').toLowerCase().includes(q)
      const matchSans = (cert.dns_names || []).some((s) => s.toLowerCase().includes(q))
      if (!matchHost && !matchSub && !matchName && !matchIssuer && !matchSans) return false
    }
    return true
  })

  // Filtered DNS targets
  const filteredDns = (dnsData.matrix || []).filter((item) => {
    if (dnsFilter === 'divergent' && item.is_consistent) return false
    if (dnsFilter === 'consistent' && !item.is_consistent) return false
    if (dnsSearch) {
      const q = dnsSearch.toLowerCase()
      const matchHost = (item.host || '').toLowerCase().includes(q)
      const matchName = (item.target_name || '').toLowerCase().includes(q)
      if (!matchHost && !matchName) return false
    }
    return true
  })

  const formatDate = (unix) => {
    if (!unix) return '—'
    const d = new Date(unix * 1000)
    return d.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
  }

  const formatDateTime = (unix) => {
    if (!unix) return '—'
    const d = new Date(unix * 1000)
    return d.toLocaleString('zh-CN', { hour12: false })
  }

  return (
    <div className="synthetic-probing-view" style={{ padding: '0 0 40px' }}>
      {/* Top Banner and Navigation Tabs */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '20px', flexWrap: 'wrap', gap: '12px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <button
            type="button"
            className={`button ${activeTab === 'certificates' ? 'button-primary' : 'button-quiet'}`}
            onClick={() => setActiveTab('certificates')}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Certificate size={18} />
            <span>SSL/TLS 证书生命周期巡检</span>
            {certData.expiring_soon > 0 && (
              <span style={{ background: '#f59e0b', color: '#fff', fontSize: '11px', borderRadius: '10px', padding: '1px 6px', fontWeight: 600 }}>
                {certData.expiring_soon}
              </span>
            )}
            {certData.expired_count > 0 && (
              <span style={{ background: '#ef4444', color: '#fff', fontSize: '11px', borderRadius: '10px', padding: '1px 6px', fontWeight: 600 }}>
                {certData.expired_count}
              </span>
            )}
          </button>
          <button
            type="button"
            className={`button ${activeTab === 'dns' ? 'button-primary' : 'button-quiet'}`}
            onClick={() => setActiveTab('dns')}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <Globe size={18} />
            <span>DNS 多节点解析矩阵</span>
            {dnsData.divergent_count > 0 && (
              <span style={{ background: '#ef4444', color: '#fff', fontSize: '11px', borderRadius: '10px', padding: '1px 6px', fontWeight: 600 }}>
                {dnsData.divergent_count} 分歧
              </span>
            )}
          </button>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <button
            type="button"
            className="button button-secondary"
            onClick={() => loadData(true)}
            disabled={refreshing}
            style={{ display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            <ArrowsClockwise size={16} className={refreshing ? 'spin' : ''} />
            <span>{refreshing ? '正在巡检...' : '立即刷新'}</span>
          </button>
        </div>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', background: '#fef2f2', border: '1px solid #fee2e2', borderRadius: '8px', color: '#b91c1c', marginBottom: '20px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <WarningCircle size={20} />
          <span>{error}</span>
        </div>
      )}

      {/* TAB 1: SSL/TLS Certificates */}
      {activeTab === 'certificates' && (
        <div>
          {/* Stat Cards */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '16px', marginBottom: '24px' }}>
            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: 'var(--text-muted)', fontSize: '13px' }}>
                <span>受监测证书</span>
                <Certificate size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px' }}>{certData.total}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>覆盖 HTTPS & TLS 探针目标</div>
            </div>

            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: '#10b981', fontSize: '13px' }}>
                <span>有效期正常</span>
                <ShieldCheck size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#10b981' }}>{certData.valid_count}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>剩余有效期大于 14 天</div>
            </div>

            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: '#f59e0b', fontSize: '13px' }}>
                <span>即将到期预警</span>
                <ShieldWarning size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#f59e0b' }}>{certData.expiring_soon}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>剩余有效期 &le; 14 天 (触发通知)</div>
            </div>

            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: '#ef4444', fontSize: '13px' }}>
                <span>已过期证书</span>
                <WarningOctagon size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#ef4444' }}>{certData.expired_count}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>已超期失效，急需续签</div>
            </div>
          </div>

          {/* Filter Bar */}
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', marginBottom: '16px', flexWrap: 'wrap' }}>
            <div style={{ display: 'flex', gap: '6px' }}>
              {[
                { id: 'all', label: `全部 (${certData.total})` },
                { id: 'expiring', label: `即将到期 (${certData.expiring_soon})` },
                { id: 'expired', label: `已过期 (${certData.expired_count})` },
                { id: 'valid', label: `正常 (${certData.valid_count})` },
              ].map((f) => (
                <button
                  key={f.id}
                  type="button"
                  className={`button button-small ${certFilter === f.id ? 'button-primary' : 'button-quiet'}`}
                  onClick={() => setCertFilter(f.id)}
                >
                  {f.label}
                </button>
              ))}
            </div>

            <div style={{ position: 'relative', width: '280px' }}>
              <MagnifyingGlass size={16} style={{ position: 'absolute', left: '10px', top: '10px', color: 'var(--text-muted)' }} />
              <input
                type="text"
                className="input input-small"
                style={{ paddingLeft: '32px', width: '100%' }}
                placeholder="搜索域名、颁发机构、SAN..."
                value={certSearch}
                onChange={(e) => setCertSearch(e.target.value)}
              />
            </div>
          </div>

          {/* Certificate Inventory List */}
          {loading ? (
            <div className="panel" style={{ padding: '40px', textAlign: 'center', color: 'var(--text-muted)' }}>
              <ArrowsClockwise size={24} className="spin" style={{ marginBottom: '8px' }} />
              <div>正在加载全网证书状态...</div>
            </div>
          ) : filteredCerts.length === 0 ? (
            <div className="panel" style={{ padding: '40px', textAlign: 'center', color: 'var(--text-muted)' }}>
              <Certificate size={32} style={{ marginBottom: '8px', opacity: 0.6 }} />
              <div style={{ fontWeight: 600 }}>暂无匹配的证书记录</div>
              <div style={{ fontSize: '13px', marginTop: '4px' }}>配置 HTTPS 检测目标后，探针将在下次周期巡检自动提取 SSL 证书。</div>
            </div>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(360px, 1fr))', gap: '16px' }}>
              {filteredCerts.map((cert) => {
                const isExp = cert.is_expired || cert.days_left <= 0
                const isSoon = !isExp && (cert.expiring_soon || cert.days_left <= 14)
                const isCrit = !isExp && cert.days_left <= 3
                const badgeColor = isExp ? '#ef4444' : isCrit ? '#ef4444' : isSoon ? '#f59e0b' : '#10b981'
                const badgeBg = isExp ? 'rgba(239, 68, 68, 0.1)' : isSoon ? 'rgba(245, 158, 11, 0.1)' : 'rgba(16, 185, 129, 0.1)'
                const progressPct = Math.max(0, Math.min(100, Math.round((cert.days_left / 90) * 100)))

                return (
                  <div
                    key={cert.target_id}
                    className="panel"
                    style={{
                      padding: '16px',
                      display: 'flex',
                      flexDirection: 'column',
                      justifyContent: 'space-between',
                      borderLeft: `4px solid ${badgeColor}`,
                    }}
                  >
                    <div>
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '8px', marginBottom: '8px' }}>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: '15px' }}>{cert.target_name || cert.host}</div>
                          <div style={{ fontSize: '12px', color: 'var(--text-muted)', fontFamily: 'monospace', marginTop: '2px' }}>
                            {cert.host}:{cert.port}
                          </div>
                        </div>
                        <span
                          style={{
                            padding: '3px 8px',
                            borderRadius: '12px',
                            fontSize: '12px',
                            fontWeight: 600,
                            color: badgeColor,
                            background: badgeBg,
                            border: `1px solid ${badgeColor}33`,
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {isExp ? '已过期' : isSoon ? `剩余 ${cert.days_left} 天` : `剩余 ${cert.days_left} 天`}
                        </span>
                      </div>

                      {/* Expiration Progress Bar */}
                      <div style={{ margin: '12px 0 16px' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                          <span>有效期倒计时</span>
                          <span>{formatDate(cert.not_after)} 到期</span>
                        </div>
                        <div style={{ width: '100%', height: '6px', background: 'var(--bg-muted, #f1f5f9)', borderRadius: '3px', overflow: 'hidden' }}>
                          <div
                            style={{
                              width: `${progressPct}%`,
                              height: '100%',
                              background: badgeColor,
                              borderRadius: '3px',
                              transition: 'width 0.3s ease',
                            }}
                          />
                        </div>
                      </div>

                      {/* Details Grid */}
                      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px', fontSize: '12px', marginBottom: '14px' }}>
                        <div>
                          <span style={{ color: 'var(--text-muted)' }}>颁发机构：</span>
                          <span style={{ fontWeight: 500 }}>{cert.issuer || '—'}</span>
                        </div>
                        <div>
                          <span style={{ color: 'var(--text-muted)' }}>协议版本：</span>
                          <span style={{ fontWeight: 500 }}>{cert.protocol || 'TLS 1.3'}</span>
                        </div>
                        <div style={{ gridColumn: 'span 2' }}>
                          <span style={{ color: 'var(--text-muted)' }}>通用名称 (CN)：</span>
                          <span style={{ fontFamily: 'monospace', fontWeight: 500 }}>{cert.subject || cert.host}</span>
                        </div>
                        <div style={{ gridColumn: 'span 2' }}>
                          <span style={{ color: 'var(--text-muted)' }}>巡检节点：</span>
                          <span>{cert.node_name || cert.node_id}</span>
                          <span style={{ color: 'var(--text-muted)', marginLeft: '8px' }}>({formatDateTime(cert.checked_at)})</span>
                        </div>
                      </div>
                    </div>

                    <div style={{ borderTop: '1px solid var(--border-color, #e2e8f0)', paddingTop: '10px', display: 'flex', justifyContent: 'flex-end' }}>
                      <button
                        type="button"
                        className="button button-small button-quiet"
                        onClick={() => setSelectedCert(cert)}
                        style={{ fontSize: '12px' }}
                      >
                        查看详细指纹与 SANs &rarr;
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* TAB 2: DNS Matrix */}
      {activeTab === 'dns' && (
        <div>
          {/* Stat Cards */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '16px', marginBottom: '24px' }}>
            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: 'var(--text-muted)', fontSize: '13px' }}>
                <span>解析域名总数</span>
                <Globe size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px' }}>{dnsData.total_targets}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>多节点分布式解析监测</div>
            </div>

            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: '#10b981', fontSize: '13px' }}>
                <span>跨节点一致</span>
                <CheckCircle size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#10b981' }}>{dnsData.consistent_count}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>各地域节点解析记录完全吻合</div>
            </div>

            <div className="panel" style={{ padding: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', color: '#ef4444', fontSize: '13px' }}>
                <span>存在分歧 / 疑似投毒</span>
                <Warning size={20} />
              </div>
              <div style={{ fontSize: '28px', fontWeight: 700, marginTop: '8px', color: '#ef4444' }}>{dnsData.divergent_count}</div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>节点间返回 IP 集合不一致</div>
            </div>
          </div>

          {/* Filter Bar */}
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', marginBottom: '16px', flexWrap: 'wrap' }}>
            <div style={{ display: 'flex', gap: '6px' }}>
              {[
                { id: 'all', label: `全部 (${dnsData.total_targets})` },
                { id: 'divergent', label: `存在分歧 (${dnsData.divergent_count})` },
                { id: 'consistent', label: `一致 (${dnsData.consistent_count})` },
              ].map((f) => (
                <button
                  key={f.id}
                  type="button"
                  className={`button button-small ${dnsFilter === f.id ? 'button-primary' : 'button-quiet'}`}
                  onClick={() => setDnsFilter(f.id)}
                >
                  {f.label}
                </button>
              ))}
            </div>

            <div style={{ position: 'relative', width: '280px' }}>
              <MagnifyingGlass size={16} style={{ position: 'absolute', left: '10px', top: '10px', color: 'var(--text-muted)' }} />
              <input
                type="text"
                className="input input-small"
                style={{ paddingLeft: '32px', width: '100%' }}
                placeholder="搜索域名..."
                value={dnsSearch}
                onChange={(e) => setDnsSearch(e.target.value)}
              />
            </div>
          </div>

          {/* DNS Matrix List */}
          {loading ? (
            <div className="panel" style={{ padding: '40px', textAlign: 'center', color: 'var(--text-muted)' }}>
              <ArrowsClockwise size={24} className="spin" style={{ marginBottom: '8px' }} />
              <div>正在聚合各节点 DNS 解析矩阵...</div>
            </div>
          ) : filteredDns.length === 0 ? (
            <div className="panel" style={{ padding: '40px', textAlign: 'center', color: 'var(--text-muted)' }}>
              <Globe size={32} style={{ marginBottom: '8px', opacity: 0.6 }} />
              <div style={{ fontWeight: 600 }}>暂无 DNS 探针记录</div>
              <div style={{ fontSize: '13px', marginTop: '4px' }}>配置 DNS 检测目标后，各节点将定期上报解析耗时与 IP 集合。</div>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              {filteredDns.map((item) => (
                <div key={item.target_id} className="panel" style={{ padding: '18px' }}>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '14px', flexWrap: 'wrap', gap: '10px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                      <span style={{ fontSize: '16px', fontWeight: 600 }}>{item.target_name || item.host}</span>
                      <span style={{ fontFamily: 'monospace', fontSize: '13px', color: 'var(--text-muted)' }}>{item.host}</span>
                      <span style={{ fontSize: '11px', background: 'var(--bg-muted, #f1f5f9)', padding: '2px 6px', borderRadius: '4px', fontWeight: 600 }}>
                        {item.dns_type || 'A'}
                      </span>
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                      <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                        平均时延：<strong style={{ color: item.avg_query_time_ms > 100 ? '#f59e0b' : '#10b981' }}>{item.avg_query_time_ms} ms</strong>
                      </span>
                      {item.is_consistent ? (
                        <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: '#10b981', fontSize: '13px', fontWeight: 600 }}>
                          <CheckCircle size={16} />
                          跨节点完全一致
                        </span>
                      ) : (
                        <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: '#ef4444', fontSize: '13px', fontWeight: 600 }}>
                          <Warning size={16} />
                          检测到 {item.unique_sets_count} 组分歧记录
                          {item.divergent_nodes?.length > 0 && ` (${item.divergent_nodes.join(', ')})`}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Node breakdown table */}
                  <div style={{ overflowX: 'auto' }}>
                    <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
                      <thead>
                        <tr style={{ borderBottom: '1px solid var(--border-color, #e2e8f0)', color: 'var(--text-muted)', textAlign: 'left' }}>
                          <th style={{ padding: '8px 12px' }}>监测节点</th>
                          <th style={{ padding: '8px 12px' }}>解析结果 (IP 集合)</th>
                          <th style={{ padding: '8px 12px' }}>Nameserver</th>
                          <th style={{ padding: '8px 12px' }}>解析时延</th>
                          <th style={{ padding: '8px 12px', textAlign: 'right' }}>最近上报</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(item.nodes || []).map((node) => (
                          <tr key={node.node_id} style={{ borderBottom: '1px solid var(--border-color, #f1f5f9)' }}>
                            <td style={{ padding: '10px 12px', fontWeight: 500 }}>{node.node_name || node.node_id}</td>
                            <td style={{ padding: '10px 12px' }}>
                              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
                                {(node.records || []).map((ip) => (
                                  <span
                                    key={ip}
                                    style={{
                                      fontFamily: 'monospace',
                                      fontSize: '12px',
                                      padding: '2px 6px',
                                      borderRadius: '4px',
                                      background: 'var(--bg-muted, #f1f5f9)',
                                      border: '1px solid var(--border-color, #e2e8f0)',
                                    }}
                                  >
                                    {ip}
                                  </span>
                                ))}
                                {(!node.records || node.records.length === 0) && (
                                  <span style={{ color: 'var(--text-muted)' }}>无解析结果</span>
                                )}
                              </div>
                            </td>
                            <td style={{ padding: '10px 12px', fontFamily: 'monospace', fontSize: '12px', color: 'var(--text-muted)' }}>
                              {node.nameserver || '系统默认'}
                            </td>
                            <td style={{ padding: '10px 12px' }}>
                              <span style={{ fontWeight: 600, color: node.query_time_ms > 100 ? '#f59e0b' : '#10b981' }}>
                                {node.query_time_ms} ms
                              </span>
                            </td>
                            <td style={{ padding: '10px 12px', textAlign: 'right', color: 'var(--text-muted)', fontSize: '12px' }}>
                              {formatDateTime(node.checked_at)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Certificate Details Modal */}
      {selectedCert && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0, 0, 0, 0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
            padding: '20px',
          }}
          onClick={() => setSelectedCert(null)}
        >
          <div
            className="panel"
            style={{
              maxWidth: '600px',
              width: '100%',
              maxHeight: '90vh',
              overflowY: 'auto',
              padding: '24px',
              boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.2)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <Certificate size={22} style={{ color: 'var(--color-primary, #3b82f6)' }} />
                <h3 style={{ margin: 0, fontSize: '18px' }}>证书完整元数据</h3>
              </div>
              <button
                type="button"
                className="button button-quiet"
                onClick={() => setSelectedCert(null)}
                style={{ padding: '4px' }}
              >
                <X size={18} />
              </button>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '14px', fontSize: '13px' }}>
              <div>
                <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>通用名称 (Common Name)</div>
                <div style={{ fontWeight: 600, fontFamily: 'monospace' }}>{selectedCert.subject}</div>
              </div>

              <div>
                <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>颁发者 (Issuer)</div>
                <div style={{ fontWeight: 600 }}>{selectedCert.issuer}</div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>生效起始时间</div>
                  <div>{formatDateTime(selectedCert.not_before)}</div>
                </div>
                <div>
                  <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>到期失效时间</div>
                  <div style={{ color: selectedCert.is_expired ? '#ef4444' : selectedCert.expiring_soon ? '#f59e0b' : 'inherit', fontWeight: 600 }}>
                    {formatDateTime(selectedCert.not_after)}
                  </div>
                </div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                <div>
                  <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>TLS 协议版本</div>
                  <div>{selectedCert.protocol || 'TLS 1.3'}</div>
                </div>
                <div>
                  <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>加密套件 (Cipher Suite)</div>
                  <div style={{ fontFamily: 'monospace', fontSize: '12px' }}>{selectedCert.cipher_suite || '—'}</div>
                </div>
              </div>

              <div>
                <div style={{ color: 'var(--text-muted)', marginBottom: '6px' }}>使用者备选名称 (Subject Alternative Names, SANs)</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', maxHeight: '120px', overflowY: 'auto', padding: '8px', background: 'var(--bg-muted, #f1f5f9)', borderRadius: '6px' }}>
                  {(selectedCert.dns_names || [selectedCert.host]).map((san) => (
                    <span
                      key={san}
                      style={{
                        fontFamily: 'monospace',
                        fontSize: '12px',
                        padding: '2px 8px',
                        borderRadius: '4px',
                        background: '#ffffff',
                        border: '1px solid var(--border-color, #e2e8f0)',
                      }}
                    >
                      {san}
                    </span>
                  ))}
                </div>
              </div>

              <div>
                <div style={{ color: 'var(--text-muted)', marginBottom: '2px' }}>巡检上报节点</div>
                <div>{selectedCert.node_name} ({selectedCert.node_id}) · {formatDateTime(selectedCert.checked_at)}</div>
              </div>
            </div>

            <div style={{ marginTop: '20px', display: 'flex', justifyContent: 'flex-end' }}>
              <button
                type="button"
                className="button button-primary"
                onClick={() => setSelectedCert(null)}
              >
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
