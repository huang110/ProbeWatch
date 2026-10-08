import { useEffect, useRef, useState } from 'react'

// Keeps live pages responsive without polling hidden tabs or waiting for the
// next interval after the browser reconnects to the network.
export function useLivePolling(load, { interval = 15000, enabled = true, restartKey = '', onStatusChange } = {}) {
  const loadRef = useRef(load)
  const onStatusChangeRef = useRef(onStatusChange)
  const [status, setStatus] = useState(enabled ? 'connecting' : 'paused')

  useEffect(() => {
    loadRef.current = load
  }, [load])

  useEffect(() => {
    onStatusChangeRef.current = onStatusChange
  }, [onStatusChange])

  useEffect(() => {
    let timer
    let disposed = false
    let inFlight = false
    let failures = 0

    const updateStatus = (next) => {
      if (disposed) return
      setStatus(next)
      if (onStatusChangeRef.current) {
        onStatusChangeRef.current(next)
      }
    }

    if (!enabled) {
      updateStatus('paused')
      return undefined
    }

    const isActive = () => !disposed && (typeof document === 'undefined' || document.visibilityState === 'visible') && (typeof navigator === 'undefined' || navigator.onLine !== false)

    const run = async (manual = false) => {
      if (!isActive() || inFlight || disposed) return
      inFlight = true
      updateStatus('loading')
      try {
        const result = await loadRef.current(manual)
        if (disposed) return
        if (result === false) {
          failures = Math.min(failures + 1, 4)
          updateStatus('retrying')
        } else {
          failures = 0
          updateStatus('online')
        }
      } catch (error) {
        if (disposed) return
        if (error?.name !== 'AbortError') {
          failures = Math.min(failures + 1, 4)
          updateStatus('retrying')
        }
      } finally {
        if (!disposed) {
          inFlight = false
          schedule()
        }
      }
    }

    const schedule = () => {
      window.clearTimeout(timer)
      if (!isActive() || inFlight || disposed) return
      timer = window.setTimeout(() => {
        run(false)
      }, Math.min(interval * 2 ** failures, 120000))
    }

    const onVisibility = () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
        failures = 0
        window.clearTimeout(timer)
        run(false)
      } else {
        window.clearTimeout(timer)
        updateStatus('paused')
      }
    }

    const onOnline = () => {
      failures = 0
      window.clearTimeout(timer)
      run(false)
    }

    const onOffline = () => {
      window.clearTimeout(timer)
      updateStatus('offline')
    }

    run(false)

    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', onVisibility)
    }
    if (typeof window !== 'undefined') {
      window.addEventListener('online', onOnline)
      window.addEventListener('offline', onOffline)
    }

    return () => {
      disposed = true
      inFlight = false
      window.clearTimeout(timer)
      if (typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', onVisibility)
      }
      if (typeof window !== 'undefined') {
        window.removeEventListener('online', onOnline)
        window.removeEventListener('offline', onOffline)
      }
    }
  }, [interval, enabled, restartKey])

  return status
}
