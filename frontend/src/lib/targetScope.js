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
