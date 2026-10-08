// Core live polling controller shared between React hook and test suite.
// Manages timer, deduplication, exponential backoff, lifecycle events (online/offline, visibility),
// and guarantees status integrity even when in-flight requests resolve after network or visibility state changes.

export class LivePollingController {
  constructor(load, { interval = 15000, enabled = true, onStatusChange } = {}) {
    this.load = load
    this.interval = interval
    this.enabled = enabled
    this.onStatusChange = onStatusChange
    this.status = enabled ? 'connecting' : 'paused'
    this.timer = null
    this.disposed = false
    this.inFlight = false
    this.failures = 0
    this.calls = 0

    this.onVisibility = () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
        this.failures = 0
        this.clearTimer()
        this.run(false)
      } else {
        this.clearTimer()
        this.updateStatus('paused')
      }
    }

    this.onOnline = () => {
      this.failures = 0
      this.clearTimer()
      this.run(false)
    }

    this.onOffline = () => {
      this.clearTimer()
      this.updateStatus('offline')
    }

    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', this.onVisibility)
    }
    if (typeof window !== 'undefined') {
      window.addEventListener('online', this.onOnline)
      window.addEventListener('offline', this.onOffline)
    }
  }

  start() {
    if (this.disposed) return
    if (!this.enabled) {
      this.updateStatus('paused')
      return
    }
    this.run(false)
  }

  isActive() {
    return (
      !this.disposed &&
      (typeof document === 'undefined' || document.visibilityState === 'visible') &&
      (typeof navigator === 'undefined' || navigator.onLine !== false)
    )
  }

  updateStatus(next) {
    if (this.disposed) return
    this.status = next
    if (this.onStatusChange) {
      this.onStatusChange(next)
    }
  }

  clearTimer() {
    if (this.timer) {
      if (typeof window !== 'undefined' && window.clearTimeout) {
        window.clearTimeout(this.timer)
      } else {
        clearTimeout(this.timer)
      }
      this.timer = null
    }
  }

  schedule() {
    this.clearTimer()
    if (!this.isActive() || this.inFlight || this.disposed || !this.enabled) return
    const delay = Math.min(this.interval * 2 ** this.failures, 120000)
    const setTimer = typeof window !== 'undefined' && window.setTimeout ? window.setTimeout : setTimeout
    this.timer = setTimer(() => {
      this.run(false)
    }, delay)
  }

  async run(manual = false) {
    if (!this.isActive() || this.inFlight || this.disposed || !this.enabled) return
    this.inFlight = true
    this.calls++
    this.updateStatus('loading')
    try {
      const result = await this.load(manual)
      if (this.disposed) return
      if (result === false) {
        this.failures = Math.min(this.failures + 1, 4)
        // If while in-flight, network went offline or tab became hidden: do not overwrite status
        if (!this.isActive()) return
        this.updateStatus('retrying')
      } else {
        this.failures = 0
        // If while in-flight, network went offline or tab became hidden: do not overwrite status
        if (!this.isActive()) return
        this.updateStatus('online')
      }
    } catch (error) {
      if (this.disposed) return
      if (error?.name !== 'AbortError') {
        this.failures = Math.min(this.failures + 1, 4)
        if (this.isActive()) {
          this.updateStatus('retrying')
        }
      }
    } finally {
      if (!this.disposed) {
        this.inFlight = false
        this.schedule()
      }
    }
  }

  updateConfig({ load, interval, enabled, onStatusChange } = {}) {
    if (this.disposed) return
    if (load !== undefined) this.load = load
    if (onStatusChange !== undefined) this.onStatusChange = onStatusChange

    let intervalChanged = false
    if (interval !== undefined && interval !== this.interval) {
      this.interval = interval
      intervalChanged = true
    }

    if (enabled !== undefined && enabled !== this.enabled) {
      this.enabled = enabled
      if (!enabled) {
        this.clearTimer()
        this.updateStatus('paused')
      } else {
        this.failures = 0
        this.clearTimer()
        this.run(false)
      }
    } else if (intervalChanged && this.enabled && !this.inFlight) {
      this.schedule()
    }
  }

  dispose() {
    this.disposed = true
    this.inFlight = false
    this.clearTimer()
    if (typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', this.onVisibility)
    }
    if (typeof window !== 'undefined') {
      window.removeEventListener('online', this.onOnline)
      window.removeEventListener('offline', this.onOffline)
    }
  }
}
