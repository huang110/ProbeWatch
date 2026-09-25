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
