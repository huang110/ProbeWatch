// Ephemeral UI preferences.  Keep node identifiers in memory only: browser
// storage survives logout and can expose fleet topology on shared machines.
const favoriteNodes = new Set()
const recentNodes = []

export function getFavoriteNodeIds() {
  return new Set(favoriteNodes)
}

export function toggleFavoriteNode(nodeId) {
  const id = String(nodeId || '').trim()
  if (!id) return false
  if (favoriteNodes.has(id)) favoriteNodes.delete(id)
  else favoriteNodes.add(id)
  return favoriteNodes.has(id)
}

export function recordRecentNode(nodeId, max = 10) {
  const id = String(nodeId || '').trim()
  if (!id) return
  const index = recentNodes.indexOf(id)
  if (index >= 0) recentNodes.splice(index, 1)
  recentNodes.unshift(id)
  recentNodes.splice(Math.max(1, Number(max) || 10))
}

export function getRecentNodeIds() {
  return recentNodes.slice()
}
