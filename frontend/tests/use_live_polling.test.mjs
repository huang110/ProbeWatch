// Tests for LivePollingController and live polling lifecycle.
// Imports and tests the actual production controller directly (no duplicate copy).

import { LivePollingController } from '../src/lib/LivePollingController.js'

export async function runUseLivePollingTests() {
  const listeners = new Map()
  let visibilityState = 'visible'
  let onLineState = true

  // Set up mock browser environment on globalThis
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

  if (typeof globalThis.navigator === 'undefined') {
    globalThis.navigator = {
      get onLine() { return onLineState },
    }
  } else {
    Object.defineProperty(globalThis.navigator, 'onLine', {
      get: () => onLineState,
      configurable: true,
    })
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
      for (const fn of Array.from(set)) fn()
    }
  }

  let assertions = 0

  // 1. Initial run & status transitions (connecting -> loading -> online)
  {
    visibilityState = 'visible'
    onLineState = true
    const statuses = []
    let callCount = 0
    const ctrl = new LivePollingController(async () => {
      callCount++
      return true
    }, { interval: 1000, enabled: true, onStatusChange: (s) => statuses.push(s) })

    ctrl.start()
    await new Promise((r) => setTimeout(r, 20))

    if (ctrl.status !== 'online') throw new Error(`Expected online, got ${ctrl.status}`)
    if (callCount !== 1) throw new Error(`Expected 1 call, got ${callCount}`)
    if (!statuses.includes('loading') || !statuses.includes('online')) {
      throw new Error(`Missing transition statuses: ${statuses.join('->')}`)
    }
    ctrl.dispose()
    assertions += 3
  }

  // 2. In-flight request deduplication
  {
    visibilityState = 'visible'
    onLineState = true
    let running = 0
    let maxConcurrent = 0
    let totalInvocations = 0

    const ctrl = new LivePollingController(async () => {
      totalInvocations++
      running++
      maxConcurrent = Math.max(maxConcurrent, running)
      await new Promise((r) => setTimeout(r, 40))
      running--
      return true
    }, { interval: 5000, enabled: true })

    ctrl.start()
    // Trigger additional runs while the first is in-flight
    ctrl.run(false)
    ctrl.run(true)
    await new Promise((r) => setTimeout(r, 70))

    if (maxConcurrent !== 1) throw new Error(`Expected maxConcurrent 1, got ${maxConcurrent}`)
    if (totalInvocations !== 1) throw new Error(`Expected in-flight deduplication to 1 invocation, got ${totalInvocations}`)
    ctrl.dispose()
    assertions += 2
  }

  // 3. Failure, retry backoff calculation
  {
    visibilityState = 'visible'
    onLineState = true
    const statuses = []
    let failCalls = 0
    const ctrl = new LivePollingController(async () => {
      failCalls++
      return false
    }, { interval: 100, enabled: true, onStatusChange: (s) => statuses.push(s) })

    ctrl.start()
    await new Promise((r) => setTimeout(r, 20))

    if (ctrl.status !== 'retrying') throw new Error(`Expected status retrying, got ${ctrl.status}`)
    if (ctrl.failures !== 1) throw new Error(`Expected failures count 1, got ${ctrl.failures}`)
    if (!statuses.includes('retrying')) throw new Error('Status callback missed retrying')
    ctrl.dispose()
    assertions += 3
  }

  // 4. Exception (non-AbortError) transitions to retrying
  {
    visibilityState = 'visible'
    onLineState = true
    const ctrl = new LivePollingController(async () => {
      throw new Error('network-interrupted')
    }, { interval: 100, enabled: true })

    ctrl.start()
    await new Promise((r) => setTimeout(r, 20))

    if (ctrl.status !== 'retrying') throw new Error(`Expected status retrying on throw, got ${ctrl.status}`)
    ctrl.dispose()
    assertions += 1
  }

  // 5. CRITICAL: In-flight request completing after network goes offline must NOT overwrite status to online!
  {
    visibilityState = 'visible'
    onLineState = true
    let resolveRequest
    const ctrl = new LivePollingController(() => {
      return new Promise((resolve) => {
        resolveRequest = resolve
      })
    }, { interval: 5000, enabled: true })

    ctrl.start()
    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'loading') throw new Error(`Expected loading while in-flight, got ${ctrl.status}`)

    // Network goes offline while request is still awaiting response
    onLineState = false
    emit('offline')
    if (ctrl.status !== 'offline') throw new Error(`Expected status offline upon offline event, got ${ctrl.status}`)

    // Now resolve the in-flight request
    resolveRequest(true)
    await new Promise((r) => setTimeout(r, 20))

    // Must remain offline! Not overwritten to online!
    if (ctrl.status !== 'offline') {
      throw new Error(`CRITICAL REGRESSION: In-flight completion overwrote offline status to ${ctrl.status}!`)
    }
    ctrl.dispose()
    assertions += 3
  }

  // 6. CRITICAL: In-flight request completing after tab hidden must NOT overwrite status to online!
  {
    visibilityState = 'visible'
    onLineState = true
    let resolveRequest
    const ctrl = new LivePollingController(() => {
      return new Promise((resolve) => {
        resolveRequest = resolve
      })
    }, { interval: 5000, enabled: true })

    ctrl.start()
    await new Promise((r) => setTimeout(r, 10))
    if (ctrl.status !== 'loading') throw new Error(`Expected loading while in-flight, got ${ctrl.status}`)

    // Tab is hidden while request is still awaiting response
    visibilityState = 'hidden'
    emit('visibilitychange')
    if (ctrl.status !== 'paused') throw new Error(`Expected status paused upon tab hide, got ${ctrl.status}`)

    // Now resolve the in-flight request
    resolveRequest(true)
    await new Promise((r) => setTimeout(r, 20))

    // Must remain paused! Not overwritten to online!
    if (ctrl.status !== 'paused') {
      throw new Error(`CRITICAL REGRESSION: In-flight completion overwrote paused status to ${ctrl.status}!`)
    }
    ctrl.dispose()
    assertions += 3
  }

  // 7. Network recovery: offline -> online triggers fetch and recovers to online
  {
    visibilityState = 'visible'
    onLineState = false
    const ctrl = new LivePollingController(async () => true, { interval: 5000, enabled: true })
    ctrl.start()
    await new Promise((r) => setTimeout(r, 10))
    emit('offline')
    if (ctrl.status !== 'offline') throw new Error(`Expected offline, got ${ctrl.status}`)

    onLineState = true
    emit('online')
    await new Promise((r) => setTimeout(r, 20))

    if (ctrl.status !== 'online') throw new Error(`Expected recovery to online, got ${ctrl.status}`)
    ctrl.dispose()
    assertions += 2
  }

  // 8. Visibility recovery: hidden -> visible triggers fetch and recovers to online
  {
    visibilityState = 'hidden'
    onLineState = true
    const ctrl = new LivePollingController(async () => true, { interval: 5000, enabled: true })
    ctrl.start()
    await new Promise((r) => setTimeout(r, 10))
    emit('visibilitychange')
    if (ctrl.status !== 'paused') throw new Error(`Expected paused, got ${ctrl.status}`)

    visibilityState = 'visible'
    emit('visibilitychange')
    await new Promise((r) => setTimeout(r, 20))

    if (ctrl.status !== 'online') throw new Error(`Expected recovery to online, got ${ctrl.status}`)
    ctrl.dispose()
    assertions += 2
  }

  // 9. Dynamic configuration changes (enabled toggling & interval adjustment)
  {
    visibilityState = 'visible'
    onLineState = true
    let runs = 0
    const ctrl = new LivePollingController(async () => {
      runs++
      return true
    }, { interval: 50, enabled: true })

    ctrl.start()
    await new Promise((r) => setTimeout(r, 20))
    const runsBeforePause = runs

    // Pause polling dynamically
    ctrl.updateConfig({ enabled: false })
    if (ctrl.status !== 'paused') throw new Error(`Expected paused when disabled, got ${ctrl.status}`)

    await new Promise((r) => setTimeout(r, 120))
    if (runs !== runsBeforePause) throw new Error(`Expected no runs while paused, got ${runs} vs ${runsBeforePause}`)

    // Resume polling dynamically
    ctrl.updateConfig({ enabled: true })
    await new Promise((r) => setTimeout(r, 20))
    if (ctrl.status !== 'online') throw new Error(`Expected online after resume, got ${ctrl.status}`)
    if (runs <= runsBeforePause) throw new Error('Expected new run after resuming enabled')

    ctrl.dispose()
    assertions += 4
  }

  // 10. Disposal stops timers, cleans up event listeners, and prevents post-disposal status updates
  {
    let afterDisposedUpdated = false
    let ctrl
    ctrl = new LivePollingController(async () => {
      await new Promise((r) => setTimeout(r, 30))
      return true
    }, {
      interval: 100,
      enabled: true,
      onStatusChange: () => {
        if (ctrl?.disposed) afterDisposedUpdated = true
      },
    })

    ctrl.start()
    ctrl.dispose()
    await new Promise((r) => setTimeout(r, 60))

    if (afterDisposedUpdated) throw new Error('Status was updated after controller was disposed')
    assertions += 1
  }

  console.log(`[PASS] Live polling behavioral tests passed: ${assertions} assertions verified on production controller.`)
  return assertions
}

await runUseLivePollingTests()
