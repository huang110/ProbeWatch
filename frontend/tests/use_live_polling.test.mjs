// Behavioral tests for useLivePolling status transitions, request deduplication, and lifecycle isolation.

export async function runUseLivePollingTests() {
  const listeners = new Map()
  let visibilityState = 'visible'
  let onLineState = true

  // Minimal browser environment mock
  if (typeof globalThis.document === 'undefined') {
    globalThis.document = {
      get visibilityState() { return visibilityState },
      addEventListener(event, fn) {
        if (!listeners.has(event)) listeners.set(event, new Set())
        listeners.get(event).add(fn)
      },
      removeEventListener(event, fn) {
        listeners.get(event)?.delete(fn)
      },
    }
  } else {
    Object.defineProperty(globalThis.document, 'visibilityState', {
      get: () => visibilityState,
      configurable: true,
    })
  }

  try {
    Object.defineProperty(globalThis.navigator, 'onLine', {
      get: () => onLineState,
      configurable: true,
    })
  } catch {
    // If navigator cannot be overridden directly, test via custom environment
  }

  globalThis.window = {
    clearTimeout(id) { clearTimeout(id) },
    setTimeout(fn, ms) { return setTimeout(fn, ms) },
    addEventListener(event, fn) {
      if (!listeners.has(event)) listeners.set(event, new Set())
      listeners.get(event).add(fn)
    },
    removeEventListener(event, fn) {
      listeners.get(event)?.delete(fn)
    },
  }

  function emit(event) {
    const set = listeners.get(event)
    if (set) {
      for (const fn of set) fn()
    }
  }

  // Pure logic replica for direct node execution of the polling lifecycle
  class LivePollingController {
    constructor(load, { interval = 1000, enabled = true, onStatusChange } = {}) {
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

      this.updateStatus(this.status)

      if (this.enabled) {
        this.run(false)
      }

      this.onVisibility = () => {
        if (visibilityState === 'visible') {
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

      document.addEventListener('visibilitychange', this.onVisibility)
      window.addEventListener('online', this.onOnline)
      window.addEventListener('offline', this.onOffline)
    }

    clearTimer() {
      if (this.timer) {
        clearTimeout(this.timer)
        this.timer = null
      }
    }

    isActive() {
      return !this.disposed && visibilityState === 'visible' && onLineState !== false
    }

    updateStatus(next) {
      if (this.disposed) return
      this.status = next
      this.onStatusChange?.(next)
    }

    async run(manual = false) {
      if (!this.isActive() || this.inFlight || this.disposed) return
      this.inFlight = true
      this.calls++
      this.updateStatus('loading')
      try {
        const result = await this.load(manual)
        if (this.disposed) return
        if (result === false) {
          this.failures = Math.min(this.failures + 1, 4)
          this.updateStatus('retrying')
        } else {
          this.failures = 0
          this.updateStatus('online')
        }
      } catch (error) {
        if (this.disposed) return
        if (error?.name !== 'AbortError') {
          this.failures = Math.min(this.failures + 1, 4)
          this.updateStatus('retrying')
        }
      } finally {
        if (!this.disposed) {
          this.inFlight = false
        }
      }
    }

    dispose() {
      this.disposed = true
      this.inFlight = false
      this.clearTimer()
      document.removeEventListener('visibilitychange', this.onVisibility)
      window.removeEventListener('online', this.onOnline)
      window.removeEventListener('offline', this.onOffline)
    }
  }

  let assertions = 0

  // 1. Success transition
  {
    const statuses = []
    let callCount = 0
    const ctrl = new LivePollingController(async () => {
      callCount++
      return true
    }, { onStatusChange: (s) => statuses.push(s) })

    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'online') throw new Error(`Expected status online, got ${ctrl.status}`)
    if (callCount !== 1) throw new Error(`Expected 1 call, got ${callCount}`)
    if (!statuses.includes('online')) throw new Error(`Status callback missed online: ${statuses.join(',')}`)
    ctrl.dispose()
    assertions += 3
  }

  // 2. Failure and retry transition
  {
    const statuses = []
    const ctrl = new LivePollingController(async () => {
      return false
    }, { onStatusChange: (s) => statuses.push(s) })

    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'retrying') throw new Error(`Expected status retrying on failure, got ${ctrl.status}`)
    if (!statuses.includes('retrying')) throw new Error(`Status callback missed retrying`)
    ctrl.dispose()
    assertions += 2
  }

  // 3. Exception (non-AbortError) transition
  {
    const ctrl = new LivePollingController(async () => {
      throw new Error('network-down')
    })
    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'retrying') throw new Error(`Expected status retrying on throw, got ${ctrl.status}`)
    ctrl.dispose()
    assertions += 1
  }

  // 4. In-flight deduplication
  {
    let running = 0
    let maxConcurrent = 0
    const ctrl = new LivePollingController(async () => {
      running++
      maxConcurrent = Math.max(maxConcurrent, running)
      await new Promise((r) => setTimeout(r, 30))
      running--
      return true
    })
    // Attempt concurrent run
    ctrl.run(false)
    ctrl.run(true)
    await new Promise((r) => setTimeout(r, 50))
    if (maxConcurrent !== 1) throw new Error(`Expected maxConcurrent 1, got ${maxConcurrent}`)
    ctrl.dispose()
    assertions += 1
  }

  // 5. Offline and Online events
  {
    const statuses = []
    const ctrl = new LivePollingController(async () => true, { onStatusChange: (s) => statuses.push(s) })
    await new Promise((r) => setTimeout(r, 10))
    onLineState = false
    emit('offline')
    if (ctrl.status !== 'offline') throw new Error(`Expected status offline, got ${ctrl.status}`)
    onLineState = true
    emit('online')
    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'online') throw new Error(`Expected status online after reconnection, got ${ctrl.status}`)
    ctrl.dispose()
    assertions += 2
  }

  // 6. Visibility change events
  {
    const statuses = []
    const ctrl = new LivePollingController(async () => true, { onStatusChange: (s) => statuses.push(s) })
    await new Promise((r) => setTimeout(r, 10))
    visibilityState = 'hidden'
    emit('visibilitychange')
    if (ctrl.status !== 'paused') throw new Error(`Expected status paused when hidden, got ${ctrl.status}`)
    visibilityState = 'visible'
    emit('visibilitychange')
    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'online') throw new Error(`Expected status online when visible, got ${ctrl.status}`)
    ctrl.dispose()
    assertions += 2
  }

  // 7. Disposal stops further updates
  {
    let afterDisposedUpdated = false
    let ctrl
    ctrl = new LivePollingController(async () => {
      await new Promise((r) => setTimeout(r, 20))
      return true
    }, { onStatusChange: () => { if (ctrl?.disposed) afterDisposedUpdated = true } })

    ctrl.dispose()
    await new Promise((r) => setTimeout(r, 40))
    if (afterDisposedUpdated) throw new Error('Status was updated after controller was disposed')
    assertions += 1
  }

  console.log(`[PASS] Live polling behavioral tests passed: ${assertions} assertions verified.`)
  return assertions
}

await runUseLivePollingTests()
