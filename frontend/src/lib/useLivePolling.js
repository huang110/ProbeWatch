import { useEffect, useRef } from 'react'

// Keeps live pages responsive without polling hidden tabs or waiting for the
// next interval after the browser reconnects to the network.
export function useLivePolling(load, { interval = 15000, enabled = true } = {}) {
  const loadRef = useRef(load)

  useEffect(() => {
    loadRef.current = load
  }, [load])

  useEffect(() => {
    if (!enabled) return undefined
    let timer
    let disposed = false
    const run = (manual = false) => {
      if (disposed || document.visibilityState !== 'visible') return
      loadRef.current(manual)
    }
    const schedule = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        run(false)
        schedule()
      }, interval)
    }
    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        run(true)
        schedule()
      }
    }
    const onOnline = () => {
      run(true)
      schedule()
    }
    run(false)
    schedule()
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('online', onOnline)
    return () => {
      disposed = true
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
      window.removeEventListener('online', onOnline)
    }
  }, [interval, enabled])
}
