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
