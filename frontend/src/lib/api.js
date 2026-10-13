export const IP_QUALITY_SOURCE_MAP = {
  risk_score: '综合风险',
  fraud_score: '欺诈风险',
  abuse_score: '滥用风险',
  threat_score: '威胁分',
  scamalytics: 'Scamalytics',
  ip2location: 'IP2Location',
  abuseipdb: 'AbuseIPDB',
  ipqs: 'IPQS',
  dbip: 'DB-IP',
  'db-ip': 'DB-IP',
  ipapi: 'ipapi',
  ipinfo: 'IPinfo',
  ipregistry: 'ipregistry',
  ipdata: 'ipdata',
  maxmind: 'MaxMind',
  spur: 'Spur',
  cloudflare: 'Cloudflare',
}

const AGGREGATE_SCORE_KEYS = new Set([
  'risk_score',
  'riskscore',
  'fraud_score',
  'fraudscore',
  'abuse_score',
  'abusescore',
  'threat_score',
  'threatscore',
])

export function formatIPQualitySourceName(key) {
  if (!key) return '—'
  const raw = String(key).trim()
  if (!raw) return '—'
  const suffixMatch = raw.match(/^(.*?)\s*\((IPv[46])\)$/i)
  if (suffixMatch) {
    const baseName = formatIPQualitySourceName(suffixMatch[1])
    const fam = suffixMatch[2].toUpperCase().replace('IPV', 'IPv')
    return `${baseName} (${fam})`
  }
  if (IP_QUALITY_SOURCE_MAP[raw]) return IP_QUALITY_SOURCE_MAP[raw]
  const lower = raw.toLowerCase()
  if (IP_QUALITY_SOURCE_MAP[lower]) return IP_QUALITY_SOURCE_MAP[lower]
  return raw.replace(/_/g, ' ')
}

export function formatIPQualityType(type) {
  if (!type) return '未知 / 未识别'
  const t = String(type).trim().toLowerCase()
  if (!t || t === 'unknown') return '未知 / 未识别'
  if (t === 'hosting' || t === 'datacenter') return '机房'
  if (t === 'isp') return 'ISP'
  if (t === 'residential') return '家宽'
  return String(type).trim()
}

export function parseIPQualityScoreValue(rawVal) {
  if (rawVal === null || rawVal === undefined || rawVal === '') {
    return {
      value: null,
      displayValue: '未知',
      status: 'missing',
      tone: 'unknown',
      pct: 0,
      hasConclusion: false,
    }
  }
  if (typeof rawVal === 'string') {
    const trimmed = rawVal.trim()
    if (!trimmed) {
      return {
        value: null,
        displayValue: '未知',
        status: 'missing',
        tone: 'unknown',
        pct: 0,
        hasConclusion: false,
      }
    }
    const lower = trimmed.toLowerCase()
    if (lower === 'unknown' || lower === '未知' || lower === 'n/a' || lower === 'na' || lower === 'null' || lower === 'none') {
      return {
        value: null,
        displayValue: '未知',
        status: 'unknown',
        tone: 'unknown',
        pct: 0,
        hasConclusion: false,
      }
    }
    const parsed = Number(trimmed)
    if (Number.isFinite(parsed)) {
      const clamped = Math.min(100, Math.max(0, Math.round(parsed * 100) / 100))
      const tone = clamped < 25 ? 'low' : clamped < 75 ? 'medium' : 'high'
      return {
        value: clamped,
        displayValue: String(clamped),
        status: 'valid',
        tone,
        pct: clamped,
        hasConclusion: true,
      }
    }
    return {
      value: null,
      displayValue: '未知',
      status: 'unknown',
      tone: 'unknown',
      pct: 0,
      hasConclusion: false,
    }
  }
  if (typeof rawVal === 'number' && Number.isFinite(rawVal)) {
    const clamped = Math.min(100, Math.max(0, Math.round(rawVal * 100) / 100))
    const tone = clamped < 25 ? 'low' : clamped < 75 ? 'medium' : 'high'
    return {
      value: clamped,
      displayValue: String(clamped),
      status: 'valid',
      tone,
      pct: clamped,
      hasConclusion: true,
    }
  }
  return {
    value: null,
    displayValue: '未知',
    status: 'unknown',
    tone: 'unknown',
    pct: 0,
    hasConclusion: false,
  }
}

function parseBoolConclusion(val) {
  if (typeof val === 'boolean') return val
  if (typeof val === 'string') {
    const lower = val.trim().toLowerCase()
    if (lower === 'true' || lower === 'yes' || lower === '是' || lower === 'hit' || lower === '命中') return true
    if (lower === 'false' || lower === 'no' || lower === '否' || lower === 'pass' || lower === 'clean' || lower === '通过') return false
  }
  return null
}

function extractScoreEntriesFromMap(scoreMap, family = null) {
  if (!scoreMap || typeof scoreMap !== 'object' || Array.isArray(scoreMap)) {
    return []
  }
  const list = []
  for (const [rawKey, rawVal] of Object.entries(scoreMap)) {
    if (!rawKey) continue
    const parsed = parseIPQualityScoreValue(rawVal)
    if (parsed.status === 'valid' || parsed.status === 'unknown') {
      const baseLabel = formatIPQualitySourceName(rawKey)
      list.push({
        key: family ? `${family}:${rawKey}` : rawKey,
        sourceKey: rawKey,
        family: family || null,
        baseLabel,
        label: baseLabel,
        familyLabel: family ? `${baseLabel} (${family})` : baseLabel,
        value: parsed.value,
        displayValue: parsed.displayValue,
        status: parsed.status,
        tone: parsed.tone,
        pct: parsed.pct,
        hasConclusion: parsed.hasConclusion,
      })
    }
  }
  return list
}

function sanitizeFamilyObject(fam) {
  if (!fam || typeof fam !== 'object') return null
  const riskScoreParsed = parseIPQualityScoreValue(fam.risk_score ?? fam.riskScore)
  const rawScores = fam.source_scores || fam.sourceScores || fam.scores || null
  const cleanScores = {}
  if (rawScores && typeof rawScores === 'object' && !Array.isArray(rawScores)) {
    for (const [k, v] of Object.entries(rawScores)) {
      const p = parseIPQualityScoreValue(v)
      if (p.status === 'valid') cleanScores[k] = p.value
      else if (p.status === 'unknown') cleanScores[k] = 'unknown'
    }
  }
  return {
    risk_score: riskScoreParsed.status === 'valid' ? riskScoreParsed.value : null,
    riskScore: riskScoreParsed.status === 'valid' ? riskScoreParsed.value : null,
    risk_level: typeof (fam.risk_level || fam.riskLevel) === 'string' ? (fam.risk_level || fam.riskLevel) : '',
    riskLevel: typeof (fam.risk_level || fam.riskLevel) === 'string' ? (fam.risk_level || fam.riskLevel) : '',
    ip_type: typeof (fam.ip_type || fam.ipType) === 'string' ? (fam.ip_type || fam.ipType) : '',
    ipType: typeof (fam.ip_type || fam.ipType) === 'string' ? (fam.ip_type || fam.ipType) : '',
    country: typeof fam.country === 'string' ? fam.country : '',
    region: typeof fam.region === 'string' ? fam.region : '',
    asn: typeof fam.asn === 'string' ? fam.asn : '',
    organization: typeof (fam.organization || fam.org) === 'string' ? (fam.organization || fam.org) : '',
    proxy: parseBoolConclusion(fam.proxy ?? fam.is_proxy),
    vpn: parseBoolConclusion(fam.vpn ?? fam.is_vpn),
    tor: parseBoolConclusion(fam.tor ?? fam.is_tor),
    abuse: parseBoolConclusion(fam.abuse ?? fam.is_abuser ?? fam.abuser),
    hosting: parseBoolConclusion(fam.hosting ?? fam.is_server ?? fam.server),
    robot: parseBoolConclusion(fam.robot ?? fam.is_robot),
    source_scores: cleanScores,
  }
}

export function buildIPQualityViewModel(raw) {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  if (raw._isIPQualityViewModel === true) {
    return raw
  }

  const legacyRaw = (raw.ip_quality && typeof raw.ip_quality === 'object')
    ? raw.ip_quality
    : (raw.ipQuality && typeof raw.ipQuality === 'object')
      ? raw.ipQuality
      : (!raw.ipqa && !raw.ipv4 && !raw.ipv6 && raw.ipqa_enabled === undefined && raw.enabled === undefined ? raw : null)

  const data = legacyRaw || raw

  const ipqaRaw = (raw.ipqa && typeof raw.ipqa === 'object')
    ? raw.ipqa
    : (data.ipqa && typeof data.ipqa === 'object')
      ? data.ipqa
      : (typeof raw === 'object' ? raw : data)

  const v4Raw = (ipqaRaw?.ipv4 && typeof ipqaRaw.ipv4 === 'object')
    ? ipqaRaw.ipv4
    : (raw?.ipv4 && typeof raw.ipv4 === 'object')
      ? raw.ipv4
      : (data?.ipv4 && typeof data.ipv4 === 'object')
        ? data.ipv4
        : null

  const v6Raw = (ipqaRaw?.ipv6 && typeof ipqaRaw.ipv6 === 'object')
    ? ipqaRaw.ipv6
    : (raw?.ipv6 && typeof raw.ipv6 === 'object')
      ? raw.ipv6
      : (data?.ipv6 && typeof data.ipv6 === 'object')
        ? data.ipv6
        : null

  const ipv4Clean = sanitizeFamilyObject(v4Raw)
  const ipv6Clean = sanitizeFamilyObject(v6Raw)

  // 1. Score extraction with IPQA IPv4/IPv6 priority and legacy fallback
  const v4ScoreItems = extractScoreEntriesFromMap(v4Raw?.source_scores || v4Raw?.sourceScores || v4Raw?.scores, 'IPv4')
  const v6ScoreItems = extractScoreEntriesFromMap(v6Raw?.source_scores || v6Raw?.sourceScores || v6Raw?.scores, 'IPv6')

  let legacyScoreMap = null
  if (v4ScoreItems.length === 0 && v6ScoreItems.length === 0) {
    const candidateMaps = [
      raw?.source_scores,
      raw?.scores,
      data?.source_scores,
      data?.scores,
      ipqaRaw?.source_scores,
      ipqaRaw?.scores,
      (!Array.isArray(data?.sources) && typeof data?.sources === 'object') ? data.sources : null,
      (!Array.isArray(raw?.sources) && typeof raw?.sources === 'object') ? raw.sources : null,
    ]
    for (const m of candidateMaps) {
      if (m && typeof m === 'object' && !Array.isArray(m) && Object.keys(m).length > 0) {
        legacyScoreMap = m
        break
      }
    }
    if (!legacyScoreMap) {
      const explicitScores = {}
      for (const k of ['fraud_score', 'abuse_score', 'threat_score']) {
        const val = data?.[k] ?? raw?.[k] ?? ipqaRaw?.[k]
        if (val !== undefined && val !== null && val !== '') {
          explicitScores[k] = val
        }
      }
      if (Object.keys(explicitScores).length > 0) {
        legacyScoreMap = explicitScores
      }
    }
  }
  const legacyScoreItems = extractScoreEntriesFromMap(legacyScoreMap, null)

  const hasIPv4Scores = v4ScoreItems.length > 0
  const hasIPv6Scores = v6ScoreItems.length > 0
  const isDualStackScores = hasIPv4Scores && hasIPv6Scores

  let scoreGroups = []
  let scoreItems = []
  const scores = {}

  if (isDualStackScores) {
    const v4Labeled = v4ScoreItems.map((item) => ({
      ...item,
      label: item.familyLabel,
    }))
    const v6Labeled = v6ScoreItems.map((item) => ({
      ...item,
      label: item.familyLabel,
    }))
    scoreGroups = [
      {
        family: 'IPv4',
        label: 'IPv4',
        riskScore: ipv4Clean?.riskScore ?? null,
        riskLevel: ipv4Clean?.riskLevel || '',
        items: v4Labeled,
      },
      {
        family: 'IPv6',
        label: 'IPv6',
        riskScore: ipv6Clean?.riskScore ?? null,
        riskLevel: ipv6Clean?.riskLevel || '',
        items: v6Labeled,
      },
    ]
    scoreItems = [...v4Labeled, ...v6Labeled]
    for (const item of v4Labeled) {
      scores[`${item.sourceKey} (IPv4)`] = item.status === 'valid' ? item.value : 'unknown'
    }
    for (const item of v6Labeled) {
      scores[`${item.sourceKey} (IPv6)`] = item.status === 'valid' ? item.value : 'unknown'
    }
  } else if (hasIPv4Scores) {
    scoreGroups = [
      {
        family: 'IPv4',
        label: 'IPv4',
        riskScore: ipv4Clean?.riskScore ?? null,
        riskLevel: ipv4Clean?.riskLevel || '',
        items: v4ScoreItems,
      },
    ]
    scoreItems = v4ScoreItems
    for (const item of v4ScoreItems) {
      scores[item.sourceKey] = item.status === 'valid' ? item.value : 'unknown'
    }
  } else if (hasIPv6Scores) {
    scoreGroups = [
      {
        family: 'IPv6',
        label: 'IPv6',
        riskScore: ipv6Clean?.riskScore ?? null,
        riskLevel: ipv6Clean?.riskLevel || '',
        items: v6ScoreItems,
      },
    ]
    scoreItems = v6ScoreItems
    for (const item of v6ScoreItems) {
      scores[item.sourceKey] = item.status === 'valid' ? item.value : 'unknown'
    }
  } else if (legacyScoreItems.length > 0) {
    scoreGroups = [
      {
        family: null,
        label: '',
        riskScore: null,
        riskLevel: '',
        items: legacyScoreItems,
      },
    ]
    scoreItems = legacyScoreItems
    for (const item of legacyScoreItems) {
      scores[item.sourceKey] = item.status === 'valid' ? item.value : 'unknown'
    }
  }

  // 2. Resolve IPType, Risk, Location, ASN with IPQA priority over legacy fallback
  const pickNonUnknown = (...candidates) => {
    for (const c of candidates) {
      if (typeof c === 'string') {
        const t = c.trim()
        if (t && t.toLowerCase() !== 'unknown') return t
      }
    }
    for (const c of candidates) {
      if (typeof c === 'string' && c.trim().toLowerCase() === 'unknown') return 'unknown'
    }
    return ''
  }

  const ipType = pickNonUnknown(
    v4Raw?.ip_type,
    v4Raw?.ipType,
    v6Raw?.ip_type,
    v6Raw?.ipType,
    ipqaRaw?.ip_type,
    ipqaRaw?.ipType,
    data?.ipType,
    data?.ip_type,
    raw?.ipType,
    raw?.ip_type
  )

  const riskLevel = pickNonUnknown(
    v4Raw?.risk_level,
    v4Raw?.riskLevel,
    v6Raw?.risk_level,
    v6Raw?.riskLevel,
    ipqaRaw?.risk_level,
    ipqaRaw?.riskLevel,
    data?.riskLevel,
    data?.risk_level,
    raw?.riskLevel,
    raw?.risk_level
  )

  const risk = pickNonUnknown(
    riskLevel,
    data?.risk,
    raw?.risk
  )

  const country = data?.country || v4Raw?.country || v6Raw?.country || ipqaRaw?.country || raw?.country || ''
  const region = data?.region || v4Raw?.region || v6Raw?.region || ipqaRaw?.region || raw?.region || ''
  const city = data?.city || raw?.city || ''
  const asn = data?.asn || v4Raw?.asn || v6Raw?.asn || ipqaRaw?.asn || raw?.asn || ''
  const organization = data?.organization || data?.org || v4Raw?.organization || v6Raw?.organization || ipqaRaw?.organization || raw?.organization || raw?.org || ''

  // 3. Resolve Threat/Factor Flags & Database Conclusions (only items with actual conclusions)
  const rawFlags = (data?.flags && typeof data.flags === 'object') ? data.flags : ((raw?.flags && typeof raw.flags === 'object') ? raw.flags : {})
  const resolveFlag = (key, altKey) => {
    const b4 = parseBoolConclusion(v4Raw?.[key] ?? (altKey ? v4Raw?.[altKey] : undefined) ?? v4Raw?.flags?.[key])
    const b6 = parseBoolConclusion(v6Raw?.[key] ?? (altKey ? v6Raw?.[altKey] : undefined) ?? v6Raw?.flags?.[key])
    if (b4 !== null && b6 !== null) {
      if (b4 === b6) {
        return { value: b4, status: b4 ? 'yes' : 'no', text: b4 ? '是' : '否' }
      }
      return { value: true, status: 'yes', text: b4 ? '是 (IPv4)' : '是 (IPv6)' }
    }
    if (b4 !== null) {
      return { value: b4, status: b4 ? 'yes' : 'no', text: b4 ? '是' : '否' }
    }
    if (b6 !== null) {
      return { value: b6, status: b6 ? 'yes' : 'no', text: b6 ? '是' : '否' }
    }
    const bFallback = parseBoolConclusion(
      rawFlags[key] ??
      ipqaRaw?.flags?.[key] ??
      ipqaRaw?.[key] ??
      (altKey ? ipqaRaw?.[altKey] : undefined) ??
      data?.[key] ??
      (altKey ? data?.[altKey] : undefined) ??
      raw?.[key] ??
      (altKey ? raw?.[altKey] : undefined)
    )
    if (bFallback !== null) {
      return { value: bFallback, status: bFallback ? 'yes' : 'no', text: bFallback ? '是' : '否' }
    }
    return { value: null, status: 'unknown', text: '未知' }
  }

  const proxyRes = resolveFlag('proxy', 'is_proxy')
  const vpnRes = resolveFlag('vpn', 'is_vpn')
  const torRes = resolveFlag('tor', 'is_tor')
  const abuseRes = resolveFlag('abuse', 'is_abuser')
  let hostingRes = resolveFlag('hosting', 'is_server')
  const robotRes = resolveFlag('robot', 'is_robot')

  const lowerIpType = (ipType || '').toLowerCase()
  if (hostingRes.value === null) {
    if (lowerIpType === 'hosting' || lowerIpType === 'datacenter' || ipType === '机房') {
      hostingRes = { value: true, status: 'yes', text: '是' }
    } else if (lowerIpType === 'isp' || lowerIpType === 'residential' || ipType === '家宽') {
      hostingRes = { value: false, status: 'no', text: '否' }
    }
  }

  const flags = {
    proxy: proxyRes.value,
    vpn: vpnRes.value,
    tor: torRes.value,
    abuse: abuseRes.value,
    hosting: hostingRes.value,
  }

  const flagItems = []
  if (proxyRes.value !== null) {
    flagItems.push({ key: 'proxy', label: '代理', value: proxyRes.value, status: proxyRes.status, text: proxyRes.text, category: 'factor' })
  }
  if (vpnRes.value !== null) {
    flagItems.push({ key: 'vpn', label: 'VPN', value: vpnRes.value, status: vpnRes.status, text: vpnRes.text, category: 'factor' })
  }
  if (torRes.value !== null) {
    flagItems.push({ key: 'tor', label: 'Tor', value: torRes.value, status: torRes.status, text: torRes.text, category: 'factor' })
  }
  if (abuseRes.value !== null) {
    flagItems.push({ key: 'abuse', label: '滥用', value: abuseRes.value, status: abuseRes.status, text: abuseRes.text, category: 'factor' })
  }
  if (hostingRes.value !== null) {
    flagItems.push({ key: 'hosting', label: '机房', value: hostingRes.value, status: hostingRes.status, text: hostingRes.text, category: 'factor' })
  }
  if (robotRes.value !== null) {
    flagItems.push({ key: 'robot', label: '机器人', value: robotRes.value, status: robotRes.status, text: robotRes.text, category: 'factor' })
  }

  // Explicit IP / usage / company type conclusion (when non-unknown and not already covered by hosting boolean)
  if (ipType && lowerIpType !== 'unknown' && lowerIpType !== 'hosting' && lowerIpType !== 'datacenter') {
    flagItems.push({
      key: 'ip_type',
      label: 'IP 属性',
      value: ipType,
      status: 'no',
      text: formatIPQualityType(ipType),
      category: 'type',
    })
  }
  const usageType = pickNonUnknown(v4Raw?.usage_type, v6Raw?.usage_type, ipqaRaw?.usage_type, data?.usage_type, raw?.usage_type)
  if (usageType) {
    flagItems.push({
      key: 'usage_type',
      label: '使用类型',
      value: usageType,
      status: 'no',
      text: usageType,
      category: 'type',
    })
  }
  const companyType = pickNonUnknown(v4Raw?.company_type, v6Raw?.company_type, ipqaRaw?.company_type, data?.company_type, raw?.company_type)
  if (companyType) {
    flagItems.push({
      key: 'company_type',
      label: '机构类型',
      value: companyType,
      status: 'no',
      text: companyType,
      category: 'type',
    })
  }

  // Per-database conclusions (only databases with actual score/flag conclusions)
  const databaseFlags = []
  const seenDbKeys = new Set()

  // Explicit database_flags if provided
  const rawDbFlags = v4Raw?.database_flags || v6Raw?.database_flags || ipqaRaw?.database_flags || data?.database_flags || raw?.database_flags || null
  if (rawDbFlags && typeof rawDbFlags === 'object' && !Array.isArray(rawDbFlags)) {
    for (const [dbName, dbVal] of Object.entries(rawDbFlags)) {
      if (!dbName || dbVal === null || dbVal === undefined || dbVal === '') continue
      const b = parseBoolConclusion(dbVal)
      if (b !== null) {
        const canonical = dbName.toLowerCase()
        seenDbKeys.add(canonical)
        const item = {
          key: `db:${canonical}`,
          label: formatIPQualitySourceName(dbName),
          value: b,
          status: b ? 'yes' : 'no',
          text: b ? '命中' : '通过',
          category: 'database',
        }
        databaseFlags.push(item)
        flagItems.push(item)
      } else {
        const sParsed = parseIPQualityScoreValue(dbVal)
        if (sParsed.hasConclusion) {
          const canonical = dbName.toLowerCase()
          seenDbKeys.add(canonical)
          const s = sParsed.value
          const status = s >= 75 ? 'yes' : s >= 25 ? 'warn' : 'no'
          const verdict = s >= 75 ? '命中' : s >= 25 ? '中危' : s === 0 ? '通过' : '低危'
          const item = {
            key: `db:${canonical}`,
            label: formatIPQualitySourceName(dbName),
            value: s,
            status,
            text: `${verdict} (${s})`,
            category: 'database',
          }
          databaseFlags.push(item)
          flagItems.push(item)
        }
      }
    }
  }

  // Derive per-database conclusions from real database scores (excluding generic aggregate score keys)
  const dbMap = new Map()
  for (const item of v4ScoreItems) {
    const canonical = item.sourceKey.toLowerCase()
    if (! item.hasConclusion || AGGREGATE_SCORE_KEYS.has(canonical) || seenDbKeys.has(canonical)) continue
    if (!dbMap.has(canonical)) dbMap.set(canonical, { rawKey: item.sourceKey, v4: null, v6: null, leg: null })
    dbMap.get(canonical).v4 = item.value
  }
  for (const item of v6ScoreItems) {
    const canonical = item.sourceKey.toLowerCase()
    if (!item.hasConclusion || AGGREGATE_SCORE_KEYS.has(canonical) || seenDbKeys.has(canonical)) continue
    if (!dbMap.has(canonical)) dbMap.set(canonical, { rawKey: item.sourceKey, v4: null, v6: null, leg: null })
    dbMap.get(canonical).v6 = item.value
  }
  for (const item of legacyScoreItems) {
    const canonical = item.sourceKey.toLowerCase()
    if (!item.hasConclusion || AGGREGATE_SCORE_KEYS.has(canonical) || seenDbKeys.has(canonical)) continue
    if (!dbMap.has(canonical)) dbMap.set(canonical, { rawKey: item.sourceKey, v4: null, v6: null, leg: null })
    dbMap.get(canonical).leg = item.value
  }

  for (const [canonical, entry] of dbMap.entries()) {
    const label = formatIPQualitySourceName(entry.rawKey)
    if (entry.v4 !== null && entry.v6 !== null) {
      const maxS = Math.max(entry.v4, entry.v6)
      const status = maxS >= 75 ? 'yes' : maxS >= 25 ? 'warn' : 'no'
      const verdict = maxS >= 75 ? '命中' : maxS >= 25 ? '中危' : maxS === 0 ? '通过' : '低危'
      const scoreTag = entry.v4 === entry.v6 ? `${entry.v4}` : `v4:${entry.v4}/v6:${entry.v6}`
      const dbItem = {
        key: `db:${canonical}`,
        label,
        value: maxS,
        status,
        text: `${verdict} (${scoreTag})`,
        category: 'database',
      }
      databaseFlags.push(dbItem)
      flagItems.push(dbItem)
    } else {
      const s = entry.v4 !== null ? entry.v4 : entry.v6 !== null ? entry.v6 : entry.leg
      if (s !== null) {
        const famPrefix = isDualStackScores ? (entry.v4 !== null ? 'v4:' : 'v6:') : ''
        const status = s >= 75 ? 'yes' : s >= 25 ? 'warn' : 'no'
        const verdict = s >= 75 ? '命中' : s >= 25 ? '中危' : s === 0 ? '通过' : '低危'
        const dbItem = {
          key: `db:${canonical}`,
          label,
          value: s,
          status,
          text: `${verdict} (${famPrefix}${s})`,
          category: 'database',
        }
        databaseFlags.push(dbItem)
        flagItems.push(dbItem)
      }
    }
  }

  // 4. IPQA metadata & timestamps
  const enabled = Boolean(raw.ipqa_enabled ?? raw.ipqaEnabled ?? data.ipqaEnabled ?? data.ipqa_enabled ?? ipqaRaw?.enabled)
  const installed = Boolean(raw.ipqa_installed ?? raw.ipqaInstalled ?? data.ipqaInstalled ?? data.ipqa_installed ?? ipqaRaw?.installed)
  const state = raw.ipqa_state || raw.ipqaState || data.ipqaState || data.ipqa_state || ipqaRaw?.state || (installed ? (enabled ? 'enabled' : 'installed_disabled') : 'not_installed')
  const alertCount = Number(data.alertCount ?? data.alert_count ?? ipqaRaw?.alertCount ?? ipqaRaw?.alert_count ?? 0)
  const criticalCount = Number(data.criticalCount ?? data.critical_count ?? ipqaRaw?.criticalCount ?? ipqaRaw?.critical_count ?? 0)
  const warningCount = Number(data.warningCount ?? data.warning_count ?? ipqaRaw?.warningCount ?? ipqaRaw?.warning_count ?? 0)
  const lastChangeAt = data.lastChangeAt ?? data.last_change_at ?? data.lastCheckedAt ?? data.last_checked_at ?? ipqaRaw?.lastChangeAt ?? ipqaRaw?.last_change_at ?? ipqaRaw?.last_checked_at ?? null
  const checkedAt = data.checkedAt ?? data.checked_at ?? lastChangeAt ?? null
  const highestSeverity = data.highestSeverity || data.highest_severity || ipqaRaw?.highestSeverity || ipqaRaw?.highest_severity || ''
  const hasRecentChanges = Boolean(data.hasRecentChanges ?? data.has_recent_changes ?? ipqaRaw?.hasRecentChanges ?? (Array.isArray(ipqaRaw?.changes) && ipqaRaw.changes.length > 0))
  const recentChangeSummary = data.recentChangeSummary || data.recent_change_summary || ipqaRaw?.recentChangeSummary || (Array.isArray(ipqaRaw?.changes) && ipqaRaw.changes.length > 0 ? (ipqaRaw.changes[0].after || ipqaRaw.changes[0].category || '') : '')
  const latestArchiveDate = data.latestArchiveDate || data.latest_archive_date || ipqaRaw?.latestArchiveDate || ipqaRaw?.latest_archive_date || ''
  const sources = Array.isArray(data.sources) ? data.sources : (Array.isArray(ipqaRaw?.sources) ? ipqaRaw.sources : (Array.isArray(raw.sources) ? raw.sources : []))

  const riskScoreCandidates = [
    ipv4Clean?.riskScore,
    ipv6Clean?.riskScore,
    data?.riskScore,
    data?.risk_score,
    ipqaRaw?.riskScore,
    ipqaRaw?.risk_score,
    raw?.riskScore,
    raw?.risk_score,
  ]
  let riskScore = null
  for (const c of riskScoreCandidates) {
    const p = parseIPQualityScoreValue(c)
    if (p.status === 'valid') {
      riskScore = p.value
      break
    }
  }

  const hasIPv4 = v4Raw !== null ? true : Boolean(data.hasIPv4 ?? data.has_ipv4 ?? ipqaRaw?.hasIPv4 ?? ipqaRaw?.has_ipv4 ?? (v6Raw ? false : true))
  const hasIPv6 = v6Raw !== null ? true : Boolean(data.hasIPv6 ?? data.has_ipv6 ?? ipqaRaw?.hasIPv6 ?? ipqaRaw?.has_ipv6 ?? false)
  const stale = Boolean(data.stale ?? raw.stale)
  const source = (enabled && installed) ? 'IPQA' : (data.source || raw.source || 'ipwho.is')

  const media = Array.isArray(data.media) && data.media.length > 0
    ? data.media
    : Array.isArray(ipqaRaw?.media) && ipqaRaw.media.length > 0
      ? ipqaRaw.media
      : Array.isArray(raw.media) && raw.media.length > 0
        ? raw.media
        : Array.isArray(v4Raw?.media) && v4Raw.media.length > 0
          ? v4Raw.media
          : Array.isArray(v6Raw?.media) && v6Raw.media.length > 0
            ? v6Raw.media
            : []

  const ipQualityObj = {
    ipType,
    country,
    region,
    city,
    asn,
    organization,
    risk,
    checkedAt,
    scores,
    scoreItems,
    scoreGroups,
    hasScores: scoreItems.length > 0,
    hasIPv4Scores,
    hasIPv6Scores,
    isDualStackScores,
    flags,
    flagItems,
    databaseFlags,
    hasFlags: flagItems.length > 0,
    stale,
    source,
    ipv4: ipv4Clean,
    ipv6: ipv6Clean,
  }

  const ipqaObj = {
    enabled,
    installed,
    state,
    alertCount,
    criticalCount,
    warningCount,
    lastChangeAt,
    highestSeverity,
    hasRecentChanges,
    recentChangeSummary,
    latestArchiveDate,
    sources,
    riskScore,
    riskLevel,
    hasIPv4,
    hasIPv6,
    ipv4: ipv4Clean,
    ipv6: ipv6Clean,
  }

  return {
    _isIPQualityViewModel: true,
    ipQuality: ipQualityObj,
    ipqa: ipqaObj,
    ...ipQualityObj,
    ip_type: ipType,
    checked_at: checkedAt,
    ipqa_enabled: enabled,
    ipqa_installed: installed,
    ipqa_state: state,
    alert_count: alertCount,
    critical_count: criticalCount,
    warning_count: warningCount,
    last_checked_at: lastChangeAt,
    highest_severity: highestSeverity,
    has_recent_changes: hasRecentChanges,
    recent_change_summary: recentChangeSummary,
    latest_archive_date: latestArchiveDate,
    latestArchiveDate,
    sources,
    risk_score: riskScore,
    riskScore,
    risk_level: riskLevel,
    riskLevel,
    has_ipv4: hasIPv4,
    hasIPv4,
    has_ipv6: hasIPv6,
    hasIPv6,
    proxy: flags.proxy,
    vpn: flags.vpn,
    tor: flags.tor,
    abuse: flags.abuse,
    hosting: flags.hosting,
    media,
  }
}

export function normalizeIPQuality(raw) {
  return buildIPQualityViewModel(raw)
}

export async function fetchGuestStatus(signal) {
  try {
    const response = await fetch('/api/public/status', { credentials: 'same-origin', signal })
    if (!response.ok) return null
    const json = await response.json()
    if (json && typeof json === 'object' && !Array.isArray(json)) {
      if (Array.isArray(json.nodes?.telemetry)) {
        json.nodes.telemetry.forEach((t) => {
          if (t && (t.ip_quality || t.ipQuality)) {
            const normalized = normalizeIPQuality(t.ip_quality || t.ipQuality)
            t.ipQuality = normalized
            t.ip_quality = normalized
          }
        })
      }
      return json
    }
    return null
  } catch (error) {
    if (error?.name === 'AbortError') throw error
    return null
  }
}

export function formatVisitorTimestamp(rawTime) {
  const text = String(rawTime || '').trim()
  const parsed = text ? new Date(text) : new Date()
  const d = Number.isNaN(parsed.getTime()) ? new Date() : parsed
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function buildVisitorCardViewModel(raw) {
  const info = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {}
  const ip = String(info.ip || '').trim()
  const rawConn = String(info.connection || '').trim().toUpperCase()
  const connection = rawConn === 'IPV6' || (!rawConn && ip.includes(':')) ? 'IPv6' : 'IPv4'
  const country = String(info.country || '').trim()
  const region = String(info.region || '').trim()
  const city = String(info.city || '').trim()
  const geoParts = []
  for (const part of [country, region, city]) {
    if (!part) continue
    if (!geoParts.some((existing) => existing.toLowerCase() === part.toLowerCase())) {
      geoParts.push(part)
    }
  }
  const location = String(info.location || '').trim() || geoParts.join(' · ')
  const locationText = location || '位置未知'
  const isp = String(info.isp || '').trim()
  const ispText = isp || '运营商未知'
  const rawAsn = String(info.asn || '').trim()
  const asn = rawAsn ? (/^\d+$/.test(rawAsn) ? `AS${rawAsn}` : rawAsn.toUpperCase().startsWith('AS') ? `AS${rawAsn.slice(2)}` : rawAsn) : ''
  const platform = String(info.platform || '').trim() || '未知系统'
  const browser = String(info.browser || '').trim() || '未知浏览器'
  const browserVersion = String(info.browser_version ?? info.browserVersion ?? '').trim()
  const browserText = browserVersion && !browser.includes(browserVersion) ? `${browser} ${browserVersion}` : browser
  const visitedAt = String(info.visited_at ?? info.visitedAt ?? '').trim()
  const visitedAtText = formatVisitorTimestamp(visitedAt)

  return {
    title: '访客访问',
    ip,
    ipText: ip || '未知 IP',
    hasIp: Boolean(ip),
    connection,
    country,
    region,
    city,
    location,
    locationText,
    hasLocation: Boolean(location),
    isp,
    ispText,
    hasIsp: Boolean(isp),
    asn,
    asnText: asn,
    hasAsn: Boolean(asn),
    platform,
    platformText: platform,
    browser,
    browser_version: browserVersion,
    browserVersion,
    browserText,
    visited_at: visitedAt,
    visitedAt,
    visitedAtText,
    sourceLabel: '数据来源：请求连接信息',
  }
}

export async function fetchPublicClientInfo(signal) {
  try {
    const response = await fetch('/api/public/client-info', { credentials: 'same-origin', signal })
    if (!response.ok) return null
    const json = await response.json()
    return json && typeof json === 'object' && !Array.isArray(json) ? buildVisitorCardViewModel(json) : null
  } catch (error) {
    if (error?.name === 'AbortError') throw error
    return null
  }
}


// 管理页写操作（POST/PATCH/DELETE）先取一次性 CSRF Token，与 ackAlert / TOTP 同一模式。
export async function fetchCsrfToken() {
  const response = await fetch('/api/csrf', { credentials: 'same-origin' })
  if (response.status === 401) throw new Error('auth')
  if (!response.ok) throw new Error('csrf')
  const token = response.headers.get('X-CSRF-Token')
  if (token) return token
  try {
    const data = await response.json()
    if (data?.token) return data.token
  } catch {}
  throw new Error('csrf')
}

export async function performLogout() {
  try {
    const csrfToken = await fetchCsrfToken()
    await fetch('/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrfToken,
      },
      body: '{}',
    })
  } catch (error) {
    console.error('Logout error:', error)
  }
  window.location.href = '/'
}

export async function fetchNotificationChannels() {
  const res = await fetch('/api/alerts/channels', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch channels')
  return await res.json()
}

export async function createNotificationChannel(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/alerts/channels', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.message || 'Failed to create channel')
  }
  return await res.json()
}

export async function updateNotificationChannel(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/channels/${encodeURIComponent(id)}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.message || 'Failed to update channel')
  }
  return await res.json()
}

export async function deleteNotificationChannel(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/channels/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) throw new Error('Failed to delete channel')
  return true
}

export async function testNotificationChannel(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/channels/${encodeURIComponent(id)}/test`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.message || 'Test failed')
  return data
}

export async function testNotificationDirect(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/alerts/test', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload || {}),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.message || 'Test failed')
  return data
}

export async function fetchAlertSettings() {
  const res = await fetch('/api/alerts/settings', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch settings')
  return await res.json()
}

export async function saveAlertSettings(settings) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/alerts/settings', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(settings),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.message || 'Failed to save settings')
  }
  return await res.json()
}

export async function fetchAlertRules() {
  const res = await fetch('/api/alerts/rules', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch alert rules')
  return await res.json()
}

export async function createAlertRule(rule) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/alerts/rules', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(rule),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create alert rule')
  }
  return await res.json()
}

export async function updateAlertRule(id, rule) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/rules/${encodeURIComponent(id)}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(rule),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update alert rule')
  }
  return await res.json()
}

export async function deleteAlertRule(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/rules/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete alert rule')
  }
  return await res.json()
}

export async function toggleAlertRule(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/rules/${encodeURIComponent(id)}/toggle`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to toggle alert rule')
  }
  return await res.json()
}

export async function fetchAlertSilences() {
  const res = await fetch('/api/alerts/silences', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch alert silences')
  return await res.json()
}

export async function createAlertSilence(silence) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/alerts/silences', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(silence),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create alert silence')
  }
  return await res.json()
}

export async function deleteAlertSilence(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/alerts/silences/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete alert silence')
  }
  return await res.json()
}

export async function fetchFlappingAlerts() {
  const res = await fetch('/api/alerts/flapping', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch flapping alerts')
  return await res.json()
}

export async function fetchBackups(signal) {
  const res = await fetch('/api/system/backups', { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch backups')
  return await res.json()
}

export async function createBackup() {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/system/backups', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create backup')
  }
  return await res.json()
}

export async function deleteBackup(filename) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/system/backups/${encodeURIComponent(filename)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete backup')
  }
  return true
}

export async function restoreBackup(filename) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/system/backups/${encodeURIComponent(filename)}/restore`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to restore backup')
  }
  return await res.json()
}

export async function uploadBackupFile(file) {
  const csrf = await fetchCsrfToken()
  const formData = new FormData()
  formData.append('file', file)
  const res = await fetch('/api/system/backups/upload', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf },
    body: formData,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to upload backup')
  }
  return await res.json()
}

export async function fetchBackupConfig(signal) {
  const res = await fetch('/api/system/backups/config', { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch backup config')
  return await res.json()
}

export async function saveBackupConfig(config) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/system/backups/config', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(config),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to save backup config')
  }
  return await res.json()
}

export async function testS3Backup(s3Config) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/system/backups/test-s3', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(s3Config),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to test S3 connection')
  }
  return await res.json()
}

export async function testWebDAVBackup(webdavConfig) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/system/backups/test-webdav', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(webdavConfig),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to test WebDAV connection')
  }
  return await res.json()
}

export async function exportBackupNow() {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/system/backups/export-now', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to export backup now')
  }
  return await res.json()
}

export async function verifyBackup(filename) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/system/backups/${encodeURIComponent(filename)}/verify`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to verify backup integrity')
  }
  return await res.json()
}

export async function fetchNodeBilling(uuid) {
  const res = await fetch(`/api/nodes/${encodeURIComponent(uuid)}/billing`, { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch billing info')
  return await res.json()
}

export async function saveNodeBilling(uuid, settings) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/nodes/${encodeURIComponent(uuid)}/billing`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(settings),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to save billing settings')
  }
  return await res.json()
}

export async function resetNodeBilling(uuid) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/nodes/${encodeURIComponent(uuid)}/billing/reset`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to reset billing cycle')
  }
  return await res.json()
}

export async function fetchPublicNodeBilling(uuid) {
  const res = await fetch(`/api/public/nodes/${encodeURIComponent(uuid)}/billing`, { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch public billing info')
  return await res.json()
}

export async function fetchPublicNodeDetail(uuid, signal) {
  const res = await fetch(`/api/public/nodes/${encodeURIComponent(uuid)}/detail`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    const err = new Error(`Failed to fetch public node detail (${res.status})`)
    err.status = res.status
    throw err
  }
  const data = await res.json()
  if (data && typeof data === 'object') {
    if (data.ip_quality || data.ipQuality) {
      const normalized = normalizeIPQuality(data.ip_quality || data.ipQuality)
      data.ipQuality = normalized
      data.ip_quality = normalized
    }
  }
  return data
}

export async function fetchPublicNodeIPQuality(uuid, signal) {
  try {
    const res = await fetch(`/api/public/nodes/${encodeURIComponent(uuid)}/ip-quality`, { credentials: 'same-origin', signal })
    if (!res.ok) return null
    const json = await res.json()
    const quality = json?.ip_quality || json?.ipQuality || json
    return normalizeIPQuality(quality)
  } catch (err) {
    if (err?.name === 'AbortError') throw err
    return null
  }
}

export async function fetchPublicVersion() {
  const res = await fetch('/api/public/version', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch public version')
  return await res.json()
}

export async function fetchAgentUpdateCheck() {
  const res = await fetch('/api/agent/v1/update/check', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to check agent update')
  return await res.json()
}

export async function fetchAIDiagnosis() {
  const res = await fetch('/api/ai/diagnose', { credentials: 'same-origin' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to fetch AI diagnosis')
  }
  return await res.json()
}

export async function sendAIChatPrompt(prompt) {
  const res = await fetch('/api/ai/chat', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to send prompt to AI Copilot')
  }
  return await res.json()
}

export async function fetchAISettings() {
  const res = await fetch('/api/ai/settings', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch AI settings')
  return await res.json()
}

export async function saveAISettings(settings) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/ai/settings', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(settings),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to save AI settings')
  }
  return await res.json()
}

export async function regenerateMCPToken() {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/ai/mcp/token', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: '{}',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to regenerate MCP token')
  }
  return await res.json()
}

export async function fetchMCPConfig() {
  const res = await fetch('/api/mcp/config', { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch MCP config')
  return await res.json()
}

export async function fetchTerminalStatus(signal) {
  const res = await fetch('/api/admin/terminal/status', { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch terminal status')
  return await res.json()
}

export async function execTerminalCommand({ nodeId, command, timeoutSec = 30 }) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/admin/terminal/exec', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify({ node_id: nodeId, command, timeout_sec: timeoutSec }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to execute command')
  }
  return await res.json()
}

export async function fetchUsers(signal) {
  const res = await fetch('/api/users', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to fetch users')
  }
  return await res.json()
}

export async function createUser(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/users', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create user')
  }
  return await res.json()
}

export async function updateUser(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update user')
  }
  return await res.json()
}

export async function deleteUser(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete user')
  }
  return await res.json()
}

export async function fetchTokens(mineOnly = false, signal) {
  const url = mineOnly ? '/api/tokens?mine=true' : '/api/tokens'
  const res = await fetch(url, { credentials: 'same-origin', signal })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to fetch tokens')
  }
  return await res.json()
}

export async function createToken(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/tokens', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create token')
  }
  return await res.json()
}

export async function updateToken(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/tokens/${encodeURIComponent(id)}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update token')
  }
  return await res.json()
}

export async function deleteToken(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/tokens/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete token')
  }
  return await res.json()
}

export async function fetchAuditLogs({ limit = 50, offset = 0, action = '' } = {}, signal) {
  const params = new URLSearchParams()
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (action) params.set('action', action)

  const res = await fetch(`/api/audit-logs?${params.toString()}`, { credentials: 'same-origin', signal })
  if (res.status === 401) throw new Error('auth')
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to fetch audit logs')
  }
  return await res.json()
}

// Status Page & Incidents APIs
export async function fetchPublicStatusPage(signal) {
  const res = await fetch('/api/public/status-page', { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch public status page')
  return await res.json()
}

export async function fetchPublicIncidents({ limit = 20, offset = 0 } = {}) {
  const params = new URLSearchParams()
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))

  const res = await fetch(`/api/public/incidents?${params.toString()}`, { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to fetch public incidents')
  return await res.json()
}

export async function fetchAdminStatusPageConfig() {
  const res = await fetch('/api/admin/status-page', { credentials: 'same-origin' })
  if (res.status === 401) throw new Error('auth')
  if (!res.ok) throw new Error('Failed to fetch status page config')
  return await res.json()
}

export async function updateAdminStatusPageConfig(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/admin/status-page', {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update status page config')
  }
  return await res.json()
}

export async function fetchAdminIncidents({ all = true, limit = 50, offset = 0 } = {}) {
  const params = new URLSearchParams()
  if (all) params.set('all', 'true')
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))

  const res = await fetch(`/api/admin/incidents?${params.toString()}`, { credentials: 'same-origin' })
  if (res.status === 401) throw new Error('auth')
  if (!res.ok) throw new Error('Failed to fetch incidents')
  return await res.json()
}

export async function createAdminIncident(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/admin/incidents', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create incident')
  }
  return await res.json()
}

export async function getAdminIncident(id) {
  const res = await fetch(`/api/admin/incidents/${encodeURIComponent(id)}`, { credentials: 'same-origin' })
  if (!res.ok) throw new Error('Failed to get incident')
  return await res.json()
}

export async function updateAdminIncident(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/admin/incidents/${encodeURIComponent(id)}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update incident')
  }
  return await res.json()
}

export async function addAdminIncidentUpdate(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/admin/incidents/${encodeURIComponent(id)}/updates`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to add incident update')
  }
  return await res.json()
}

export async function deleteAdminIncident(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/admin/incidents/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete incident')
  }
  return await res.json()
}

export async function fetchCertificates(signal) {
  const res = await fetch('/api/certificates', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/certificates', { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch certificates')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchDNSMatrix(signal) {
  const res = await fetch('/api/dns-matrix', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/dns-matrix', { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch DNS matrix')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchMeshMatrix(signal) {
  const res = await fetch('/api/mesh-matrix', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/mesh-matrix', { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch mesh matrix')
    return await pubRes.json()
  }
  return await res.json()
}

export async function updateNode(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/nodes/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update node')
  }
  return await res.json()
}

export async function fetchSpeedtestTasks(signal) {
  const res = await fetch('/api/speedtest/tasks', { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch speedtest tasks')
  return await res.json()
}

export async function createSpeedtestTask(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/speedtest/tasks', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create speedtest task')
  }
  return await res.json()
}

export async function updateSpeedtestTask(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/speedtest/tasks/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update speedtest task')
  }
  return await res.json()
}

export async function deleteSpeedtestTask(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/speedtest/tasks/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete speedtest task')
  }
  return await res.json()
}

export async function fetchSpeedtestResults(signal) {
  const res = await fetch('/api/speedtest/results', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/speedtest/results', { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch speedtest results')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchSpeedtestHistory(nodeId, taskId, limit = 100, signal) {
  const params = new URLSearchParams()
  if (nodeId) params.set('node_id', nodeId)
  if (taskId) params.set('task_id', taskId)
  if (limit) params.set('limit', limit)
  const res = await fetch(`/api/speedtest/history?${params.toString()}`, { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch speedtest history')
  return await res.json()
}

export async function runSpeedtestNow(payload = {}) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/speedtest/run', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to trigger speedtest')
  }
  return await res.json()
}

// Synthetic Probing & Multi-Protocol SLA Engine API
export async function fetchSyntheticTargets(signal) {
  const res = await fetch('/api/synthetic/targets', { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch synthetic targets')
  return await res.json()
}

export async function getSyntheticTarget(id, signal) {
  const res = await fetch(`/api/synthetic/targets/${encodeURIComponent(id)}`, { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to get synthetic target')
  return await res.json()
}

export async function createSyntheticTarget(payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/synthetic/targets', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create synthetic target')
  }
  return await res.json()
}

export async function updateSyntheticTarget(id, payload) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/synthetic/targets/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update synthetic target')
  }
  return await res.json()
}

export async function deleteSyntheticTarget(id) {
  const csrf = await fetchCsrfToken()
  const res = await fetch(`/api/synthetic/targets/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete synthetic target')
  }
  return true
}

export async function fetchSyntheticResults(signal) {
  const res = await fetch('/api/synthetic/results', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/synthetic/results', { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch synthetic results')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchSyntheticHistory(targetId, nodeId, limit = 100, signal) {
  const params = new URLSearchParams()
  if (targetId) params.set('target_id', targetId)
  if (nodeId) params.set('node_id', nodeId)
  if (limit) params.set('limit', limit)
  const res = await fetch(`/api/synthetic/history?${params.toString()}`, { credentials: 'same-origin', signal })
  if (!res.ok) throw new Error('Failed to fetch synthetic history')
  return await res.json()
}

export async function testSyntheticTarget(payload = {}) {
  const csrf = await fetchCsrfToken()
  const res = await fetch('/api/synthetic/test', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to test synthetic probe')
  }
  return await res.json()
}

export async function fetchFleetContainerOverview(signal) {
  const res = await fetch('/api/containers/overview', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/containers/overview', { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch fleet container overview')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchNodeContainers(nodeUuid, signal) {
  const res = await fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/containers`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch(`/api/public/nodes/${encodeURIComponent(nodeUuid)}/containers`, { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch node containers')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchNodeProcesses(nodeUuid, signal) {
  const res = await fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/processes`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch(`/api/public/nodes/${encodeURIComponent(nodeUuid)}/processes`, { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch node processes')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchNodes(signal) {
  const res = await fetch('/api/nodes', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const guest = await fetchGuestStatus(signal).catch(() => null)
    if (guest && guest.nodes) return guest.nodes
    throw new Error('Failed to fetch nodes')
  }
  return await res.json()
}

// System Events & Log Streaming API
export async function fetchEventsOverview(signal) {
  const res = await fetch('/api/events/overview', { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch('/api/public/events/overview', { credentials: 'same-origin', signal })
    if (res.status === 401 || pubRes.status === 401) throw new Error('auth')
    if (!pubRes.ok) throw new Error('Failed to fetch events overview')
    return await pubRes.json()
  }
  return await res.json()
}

export async function fetchFleetEvents({ category = '', severity = '', nodeId = '', limit = 100 } = {}, signal) {
  const params = new URLSearchParams()
  if (category) params.set('category', category)
  if (severity) params.set('severity', severity)
  if (nodeId) params.set('node_id', nodeId)
  if (limit) params.set('limit', String(limit))
  const res = await fetch(`/api/events?${params.toString()}`, { credentials: 'same-origin', signal })
  if (res.status === 401) throw new Error('auth')
  if (!res.ok) throw new Error('Failed to fetch fleet events')
  return await res.json()
}

export async function fetchNodeEvents(nodeUuid, { category = '', severity = '', limit = 100 } = {}, signal) {
  const params = new URLSearchParams()
  if (category) params.set('category', category)
  if (severity) params.set('severity', severity)
  if (limit) params.set('limit', String(limit))
  const res = await fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/events?${params.toString()}`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    const pubRes = await fetch(`/api/public/nodes/${encodeURIComponent(nodeUuid)}/events?${params.toString()}`, { credentials: 'same-origin', signal })
    if (!pubRes.ok) throw new Error('Failed to fetch node events')
    return await pubRes.json()
  }
  return await res.json()
}

export async function queryNodeLogs(nodeUuid, { unit = '', priority = '', grep = '', lines = 100, since = '' } = {}, signal) {
  const params = new URLSearchParams()
  if (unit) params.set('unit', unit)
  if (priority) params.set('priority', priority)
  if (grep) params.set('grep', grep)
  if (lines) params.set('lines', String(lines))
  if (since) params.set('since', since)
  const res = await fetch(`/api/nodes/${encodeURIComponent(nodeUuid)}/logs/query?${params.toString()}`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to query node logs')
  }
  return await res.json()
}








