import { useEffect, useRef, useState } from 'react'

// Keeps live pages responsive without polling hidden tabs or waiting for the
// next interval after the browser reconnects to the network.
export function useLivePolling(load, { interval = 15000, enabled = true, restartKey = '' } = {}) {
  const loadRef = useRef(load)
  const [status, setStatus] = useState(enabled ? 'connecting' : 'paused')

  useEffect(() => {
    loadRef.current = load
  }, [load])

  useEffect(() => {
    if (!enabled) { setStatus('paused'); return undefined }
    let timer
    let disposed = false
    let inFlight = false
    let failures = 0
    const isActive = () => !disposed && document.visibilityState === 'visible' && navigator.onLine !== false
    const run = async (manual = false) => {
      if (!isActive() || inFlight) return
      inFlight = true
      setStatus('loading')
      try {
        const result = await loadRef.current(manual)
        if (result === false) { failures = Math.min(failures + 1, 4); setStatus('retrying') }
        else { failures = 0; setStatus('online') }
      } catch (error) {
        if (error?.name !== 'AbortError') { failures = Math.min(failures + 1, 4); setStatus('retrying') }
      } finally {
        inFlight = false
        schedule()
      }
    }
    const schedule = () => {
      window.clearTimeout(timer)
      if (!isActive() || inFlight) return
      timer = window.setTimeout(() => {
        run(false)
      }, Math.min(interval * 2 ** failures, 120000))
    }
    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        failures = 0
        window.clearTimeout(timer)
        run(false)
      } else { window.clearTimeout(timer); setStatus('paused') }
    }
    const onOnline = () => {
      failures = 0
      window.clearTimeout(timer)
      run(false)
    }
    const onOffline = () => { window.clearTimeout(timer); setStatus('offline') }
    run(false)
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('online', onOnline)
    window.addEventListener('offline', onOffline)
    return () => {
      disposed = true
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
      window.removeEventListener('online', onOnline)
      window.removeEventListener('offline', onOffline)
    }
  }, [interval, enabled, restartKey])
  return status
}
