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
