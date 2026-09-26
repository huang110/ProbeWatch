export async function fetchGuestStatus(signal) {
  try {
    const response = await fetch('/api/public/status', { credentials: 'same-origin', signal })
    if (!response.ok) return null
    const json = await response.json()
    return json && typeof json === 'object' && !Array.isArray(json) ? json : null
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

export async function fetchBackups() {
  const res = await fetch('/api/system/backups', { credentials: 'same-origin' })
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

export async function fetchBackupConfig() {
  const res = await fetch('/api/system/backups/config', { credentials: 'same-origin' })
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

export async function fetchTerminalStatus() {
  const res = await fetch('/api/admin/terminal/status', { credentials: 'same-origin' })
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

export async function fetchUsers() {
  const res = await fetch('/api/users', { credentials: 'same-origin' })
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

export async function fetchTokens(mineOnly = false) {
  const url = mineOnly ? '/api/tokens?mine=true' : '/api/tokens'
  const res = await fetch(url, { credentials: 'same-origin' })
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

export async function fetchAuditLogs({ limit = 50, offset = 0, action = '' } = {}) {
  const params = new URLSearchParams()
  if (limit) params.set('limit', String(limit))
  if (offset) params.set('offset', String(offset))
  if (action) params.set('action', action)

  const res = await fetch(`/api/audit-logs?${params.toString()}`, { credentials: 'same-origin' })
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



