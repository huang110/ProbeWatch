/**
 * Target scoping and server association utilities
 * Strictly mirrors the Go control plane logic in internal/api/agent_config.go
 */

/**
 * Checks whether a target has no scope restrictions, meaning it matches all current and future nodes.
 * In backend agent_config.go: !hasTags && !hasIDs => matches all nodes.
 */
export function isTargetGlobal(target) {
  if (!target) return true
  const hasIDs = Array.isArray(target.node_ids) && target.node_ids.length > 0
  const hasTags = Array.isArray(target.node_tags) && target.node_tags.length > 0
  return !hasIDs && !hasTags
}

/**
 * Evaluates whether a given node matches a target's distribution scope.
 * Mirrors nodeMatchesTarget(node db.Node, config agentTargetPayload) bool in internal/api/agent_config.go.
 */
export function nodeMatchesTarget(node, target) {
  if (!node || !target) return false

  const hasIDs = Array.isArray(target.node_ids) && target.node_ids.length > 0
  const hasTags = Array.isArray(target.node_tags) && target.node_tags.length > 0

  // 1. If target has no tags and no IDs, it matches all nodes globally
  if (!hasIDs && !hasTags) {
    return true
  }

  const nodeUuid = String(node.uuid || node.id || '').trim().toLowerCase()
  const nodeId = String(node.id || '').trim().toLowerCase()

  // 2. Check explicit NodeIDs match (case-insensitive UUID or ID match)
  if (hasIDs) {
    for (const rawId of target.node_ids) {
      const cleanId = String(rawId || '').trim().toLowerCase()
      if (cleanId && (cleanId === nodeUuid || cleanId === nodeId)) {
        return true
      }
    }
  }

  // 3. Check NodeTags match (node.tags parsed as comma/space/array tags)
  if (hasTags) {
    let nodeTags = []
    if (Array.isArray(node.tags)) {
      nodeTags = node.tags.map((t) => String(t || '').trim().toLowerCase())
    } else if (typeof node.tags === 'string' && node.tags.trim()) {
      nodeTags = node.tags
        .split(/[,\s]+/)
        .map((t) => t.trim().toLowerCase())
        .filter(Boolean)
    }

    for (const rawTag of target.node_tags) {
      const cleanReqTag = String(rawTag || '').trim().toLowerCase()
      if (cleanReqTag && nodeTags.includes(cleanReqTag)) {
        return true
      }
    }
  }

  return false
}

/**
 * Returns a human-friendly scope summary description for UI display.
 */
export function getTargetScopeSummary(target, nodes = []) {
  if (!target) return { type: 'global', text: '全部服务器 (包含未来新增)' }

  const hasIDs = Array.isArray(target.node_ids) && target.node_ids.length > 0
  const hasTags = Array.isArray(target.node_tags) && target.node_tags.length > 0

  if (!hasIDs && !hasTags) {
    return { type: 'global', text: '全部服务器 (包含未来新增)' }
  }

  if (hasTags && !hasIDs) {
    return {
      type: 'tags',
      text: `标签限定: ${target.node_tags.join(', ')}`,
      tags: target.node_tags,
    }
  }

  if (hasTags && hasIDs) {
    return {
      type: 'hybrid',
      text: `指定 ${target.node_ids.length} 台服务器 + 标签: ${target.node_tags.join(', ')}`,
      count: target.node_ids.length,
      tags: target.node_tags,
    }
  }

  return {
    type: 'scoped',
    text: `指定 ${target.node_ids.length} 台服务器`,
    count: target.node_ids.length,
  }
}

/**
 * Masks any raw UUID or node ID so full UUIDs are never exposed in UI tables or tooltips.
 */
export function maskNodeIdentifier(rawId) {
  const s = String(rawId || '').trim()
  if (!s) return '未知节点'
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s)) {
    return `节点 #${s.slice(0, 6)}`
  }
  if (s.length > 16) {
    return `${s.slice(0, 8)}…`
  }
  return s
}

/**
 * Resolves a node UUID/ID to its human-readable node name (never returning a full UUID).
 */
export function resolveNodeDisplayName(rawId, nodes = []) {
  const clean = String(rawId || '').trim().toLowerCase()
  if (!clean) return '未知节点'
  if (Array.isArray(nodes)) {
    const found = nodes.find((n) => {
      if (!n) return false
      const u = String(n.uuid || '').trim().toLowerCase()
      const i = String(n.id || '').trim().toLowerCase()
      return (u && u === clean) || (i && i === clean)
    })
    if (found && (found.name || found.hostname)) {
      return String(found.name || found.hostname).trim()
    }
  }
  return maskNodeIdentifier(rawId)
}

/**
 * Implements Section 三 execution scope display rules:
 * 1. node_ids & node_tags both empty -> "全部节点"
 * 2. only node_ids -> "指定 N 台节点" (hover/click shows resolved node names, never full UUID)
 * 3. only node_tags -> "标签：tag1、tag2"
 * 4. both node_ids & node_tags -> "指定 N 台 + 标签"
 * 5. region_rules non-empty -> adds "区域规则" badge
 * 6. Never exposes full UUIDs.
 */
export function formatTargetScopeDisplay(target, nodes = []) {
  const t = target && typeof target === 'object' ? target : {}
  const nodeIds = Array.isArray(t.node_ids)
    ? t.node_ids.filter(Boolean)
    : Array.isArray(t.nodeIds)
      ? t.nodeIds.filter(Boolean)
      : []
  const nodeTags = Array.isArray(t.node_tags)
    ? t.node_tags.filter(Boolean)
    : Array.isArray(t.nodeTags)
      ? t.nodeTags.filter(Boolean)
      : []
  const regionRules = Array.isArray(t.region_rules)
    ? t.region_rules
    : Array.isArray(t.regionRules)
      ? t.regionRules
      : []

  const hasIDs = nodeIds.length > 0
  const hasTags = nodeTags.length > 0
  const hasRegionRules = regionRules.length > 0
  const nodeNames = nodeIds.map((id) => resolveNodeDisplayName(id, nodes))
  const tagsJoined = nodeTags.join('、')

  let type = 'global'
  let label = '全部节点'
  let detail = '在所有在线及未来新增节点上执行探测'

  if (hasIDs && !hasTags) {
    type = 'nodes'
    label = `指定 ${nodeIds.length} 台节点`
    detail = `指定节点：${nodeNames.join('、')}`
  } else if (!hasIDs && hasTags) {
    type = 'tags'
    label = `标签：${tagsJoined}`
    detail = `按标签匹配：${tagsJoined}`
  } else if (hasIDs && hasTags) {
    type = 'hybrid'
    label = `指定 ${nodeIds.length} 台 + 标签`
    detail = `指定节点（${nodeNames.join('、')}）或标签（${tagsJoined}），按 OR 规则匹配`
  }

  return {
    type,
    label,
    detail,
    nodeCount: nodeIds.length,
    nodeNames,
    tags: nodeTags,
    tagsJoined,
    hasRegionRules,
    regionRulesCount: regionRules.length,
    regionBadge: hasRegionRules ? '区域规则' : null,
  }
}

/**
 * Extracts deduplicated real tags from the current nodes list.
 */
export function extractAvailableNodeTags(nodes = []) {
  const set = new Set()
  if (!Array.isArray(nodes)) return []
  for (const n of nodes) {
    if (!n) continue
    if (Array.isArray(n.tags)) {
      for (const raw of n.tags) {
        const clean = String(raw || '').trim()
        if (clean) set.add(clean)
      }
    } else if (typeof n.tags === 'string' && n.tags.trim()) {
      for (const raw of n.tags.split(/[,\s]+/)) {
        const clean = raw.trim()
        if (clean) set.add(clean)
      }
    }
  }
  return Array.from(set)
}

/**
 * Quick templates for the empty state (only pre-fills drawer form, never writes directly to DB).
 */
export const TARGET_QUICK_TEMPLATES = {
  http: {
    key: 'http',
    title: 'HTTP 健康检查',
    shortLabel: '快速创建 HTTP',
    desc: '预填 HTTP 80 端口与 /health 路径状态码探测',
    form: {
      kind: 'http',
      name: 'HTTP 健康检查',
      id: 'http-health-01',
      host: 'example.com',
      port: '80',
      path: '/health',
      dnsType: 'A',
      intervalSeconds: '60',
      timeoutMs: '3000',
      maxHops: '20',
      expectedStatus: '200',
      scopeMode: 'all',
      nodeIds: [],
      nodeTags: [],
      regionRulesList: [],
    },
  },
  tcp: {
    key: 'tcp',
    title: 'TCP 端口检查',
    shortLabel: '快速创建 TCP',
    desc: '预填 TCP 443 端口连通性与握手延迟探测',
    form: {
      kind: 'tcp',
      name: 'TCP 端口检查',
      id: 'tcp-port-443',
      host: '1.1.1.1',
      port: '443',
      path: '',
      dnsType: 'A',
      intervalSeconds: '60',
      timeoutMs: '3000',
      maxHops: '20',
      expectedStatus: '',
      scopeMode: 'all',
      nodeIds: [],
      nodeTags: [],
      regionRulesList: [],
    },
  },
  dns: {
    key: 'dns',
    title: 'DNS 解析检查',
    shortLabel: '快速创建 DNS',
    desc: '预填 DNS 53 端口 A 记录解析响应延迟探测',
    form: {
      kind: 'dns',
      name: 'DNS 解析检查',
      id: 'dns-resolve-01',
      host: 'one.one.one.one',
      port: '53',
      path: '',
      dnsType: 'A',
      intervalSeconds: '60',
      timeoutMs: '3000',
      maxHops: '20',
      expectedStatus: '',
      scopeMode: 'all',
      nodeIds: [],
      nodeTags: [],
      regionRulesList: [],
    },
  },
}

export function applyTargetQuickTemplate(templateKey, baseForm = {}) {
  const tpl = TARGET_QUICK_TEMPLATES[templateKey]
  if (!tpl) return { ...baseForm }
  return {
    ...baseForm,
    ...tpl.form,
    nodeIds: [...tpl.form.nodeIds],
    nodeTags: [...tpl.form.nodeTags],
    regionRulesList: [...tpl.form.regionRulesList],
  }
}

/**
 * Converts an existing target object into drawer form state, preserving node_ids, node_tags, and region_rules.
 */
export function buildTargetFormFromRecord(target) {
  if (!target || typeof target !== 'object') return null
  const kind = target.kind || 'tcp'
  const nodeIds = Array.isArray(target.node_ids)
    ? [...target.node_ids]
    : Array.isArray(target.nodeIds)
      ? [...target.nodeIds]
      : []
  const nodeTags = Array.isArray(target.node_tags)
    ? [...target.node_tags]
    : Array.isArray(target.nodeTags)
      ? [...target.nodeTags]
      : []
  const rawRules = Array.isArray(target.region_rules)
    ? target.region_rules
    : Array.isArray(target.regionRules)
      ? target.regionRules
      : []
  const regionRulesList = rawRules
    .filter((r) => r && typeof r === 'object')
    .map((r) => ({
      region: String(r.region || '').trim(),
      contains: String(r.contains || '').trim(),
    }))

  let scopeMode = 'all'
  if (nodeIds.length > 0 && nodeTags.length > 0) {
    scopeMode = 'custom'
  } else if (nodeIds.length > 0) {
    scopeMode = 'nodes'
  } else if (nodeTags.length > 0) {
    scopeMode = 'tags'
  }

  const defaultPort = kind === 'https' ? 443 : kind === 'dns' ? 53 : 80

  return {
    kind,
    id: String(target.id || ''),
    name: String(target.name || ''),
    host: String(target.host || ''),
    port: String(target.port ?? defaultPort),
    path: String(target.path || (kind === 'http' || kind === 'https' ? '/health' : '')),
    dnsType: String(target.dns_type || target.dnsType || 'A'),
    intervalSeconds: String(target.interval_seconds ?? target.intervalSeconds ?? 60),
    timeoutMs: String(target.timeout_ms ?? target.timeoutMs ?? 3000),
    maxHops: String(target.max_hops ?? target.maxHops ?? 20),
    expectedStatus:
      target.expected_status !== undefined && target.expected_status !== null && target.expected_status !== 0 && target.expected_status !== ''
        ? String(target.expected_status)
        : target.expectedStatus !== undefined && target.expectedStatus !== null && target.expectedStatus !== 0 && target.expectedStatus !== ''
          ? String(target.expectedStatus)
          : '',
    enabled: target.enabled !== false,
    scopeMode,
    nodeIds,
    nodeTags,
    regionRulesList,
    regionRules: regionRulesList.length > 0 ? JSON.stringify(regionRulesList) : '',
  }
}

/**
 * Builds the API POST/PATCH request payload from drawer form state, strictly preserving
 * node_ids, node_tags, and existing region_rules upon edit.
 */
export function buildTargetSubmitPayload(form, initialTarget = null) {
  const kind = form.kind || 'tcp'
  const isAllScope = form.scopeMode === 'all'
  const cleanNodeIds = isAllScope
    ? []
    : Array.from(new Set((Array.isArray(form.nodeIds) ? form.nodeIds : []).map((id) => String(id || '').trim()).filter(Boolean)))
  const cleanNodeTags = isAllScope
    ? []
    : Array.from(new Set((Array.isArray(form.nodeTags) ? form.nodeTags : []).map((t) => String(t || '').trim()).filter(Boolean)))

  const body = {
    id: String(form.id || '').trim(),
    name: String(form.name || '').trim(),
    kind,
    host: String(form.host || '').trim(),
    port: Number(form.port),
    interval_seconds: Number(form.intervalSeconds),
    timeout_ms: Number(form.timeoutMs),
    node_ids: cleanNodeIds,
    node_tags: cleanNodeTags,
  }

  if (typeof form.enabled === 'boolean') {
    body.enabled = form.enabled
  }

  if (kind === 'http' || kind === 'https' || kind === 'media_http') {
    body.path = String(form.path || '').trim()
    const exp = String(form.expectedStatus ?? '').trim()
    if (exp !== '') {
      body.expected_status = Number(exp)
    }
  }
  if (kind === 'dns') {
    body.dns_type = String(form.dnsType || 'A').trim()
  }
  if (kind === 'mtr') {
    body.max_hops = Number(form.maxHops || 20)
  }

  const formRules = Array.isArray(form.regionRulesList)
    ? form.regionRulesList
        .map((r) => ({
          region: String(r?.region || '').trim(),
          contains: String(r?.contains || '').trim(),
        }))
        .filter((r) => r.region && r.contains)
    : []

  const existingRules = Array.isArray(initialTarget?.region_rules)
    ? initialTarget.region_rules
    : Array.isArray(initialTarget?.regionRules)
      ? initialTarget.regionRules
      : []

  if (formRules.length > 0) {
    body.region_rules = formRules
  } else if (existingRules.length > 0 && form.regionRulesCleared !== true) {
    body.region_rules = existingRules
  } else if (existingRules.length > 0 && form.regionRulesCleared === true) {
    body.region_rules = []
  }

  return body
}

/**
 * Executes an authenticated write request with a single automatic CSRF refresh & retry on HTTP 403.
 */
export async function executeWithCsrfRetry(performRequest, getCsrfToken) {
  const firstToken = await getCsrfToken()
  const firstResponse = await performRequest(firstToken)
  if (firstResponse && firstResponse.status === 403) {
    const secondToken = await getCsrfToken()
    return performRequest(secondToken)
  }
  return firstResponse
}
