import assert from 'node:assert'
import {
  normalizeServerBilling,
  normalizeBillingCycle,
  getNodeBilling,
  getNodeTrafficConfig,
  setCachedServerBilling,
  getCachedServerBilling,
  fetchAndCacheNodeBilling,
  fetchAllNodesBilling,
  saveNodeBillingRemote,
  migrateLocalStorageBillingToBackend,
  calculateRemainingValue,
  generateForumSalesPost,
  READONLY_CACHE_KEY,
  MIGRATION_FLAG_KEY,
} from '../src/lib/billing.js'

console.log('=== Starting ProbeWatch Billing Backend Unification Contract Tests ===\n')

// Mock storage environment for Node.js
class MockLocalStorage {
  constructor() {
    this.store = {}
  }
  getItem(key) {
    return this.store[key] ?? null
  }
  setItem(key, value) {
    this.store[key] = String(value)
  }
  removeItem(key) {
    delete this.store[key]
  }
  clear() {
    this.store = {}
  }
}

globalThis.localStorage = new MockLocalStorage()
globalThis.window = {
  localStorage: globalThis.localStorage,
  dispatchEvent: (event) => {
    // Event dispatcher mock
  },
}
globalThis.Event = class Event {
  constructor(name) {
    this.name = name
  }
}

// -----------------------------------------------------------------------------
// Scenario 1: normalizeServerBilling parses backend CycleTrafficInfo (snake_case)
// -----------------------------------------------------------------------------
{
  const backendData = {
    node_id: 'test-node-1',
    reset_day: 15,
    reset_time: '00:00:00',
    accounting_method: 'max',
    included_interfaces: 'eth0',
    cycle_rx_bytes: 1073741824,
    cycle_tx_bytes: 2147483648,
    cycle_used_bytes: 2147483648,
    traffic_quota_bytes: 5368709120,
    bonus_quota_bytes: 1073741824,
    total_quota_bytes: 6442450944,
    used_percent: 33.33,
    merchant: 'DMIT',
    price: 39.9,
    currency: 'USD',
    cycle: 'annual',
    start_date: '2026-01-01',
    due_date: '2027-01-01',
    auto_renew: true,
  }

  const normalized = normalizeServerBilling(backendData)
  assert.strictEqual(normalized.merchant, 'DMIT', 'merchant must match')
  assert.strictEqual(normalized.price, 39.9, 'price must match float')
  assert.strictEqual(normalized.currency, 'USD', 'currency must match')
  assert.strictEqual(normalized.cycle, 'annual', 'cycle must match')
  assert.strictEqual(normalized.resetDay, 15, 'resetDay must be 15')
  assert.strictEqual(normalized.trafficQuotaBytes, 5368709120, 'traffic quota must be parsed')
  assert.strictEqual(normalized.bonusQuotaBytes, 1073741824, 'bonus quota must be parsed')
  assert.strictEqual(normalized.totalQuotaBytes, 6442450944, 'total quota must be parsed')
  assert.strictEqual(normalized.accountingMethod, 'max', 'accountingMethod must match')
  assert.strictEqual(normalized.dueDate, '2027-01-01', 'dueDate must match')
  assert.strictEqual(normalized.autoRenew, true, 'autoRenew must be true')
  assert.strictEqual(normalized.configured, true, 'configured must be true')
  console.log('✓ Scenario 1: normalizeServerBilling correctly maps backend snake_case structure')
}

// -----------------------------------------------------------------------------
// Scenario 2: Unconfigured node returns clean defaults without synthetic numbers
// -----------------------------------------------------------------------------
{
  const emptyNodeBilling = getNodeBilling('unconfigured-uuid-999')
  assert.strictEqual(emptyNodeBilling.price, null, 'Unconfigured price must be null')
  assert.strictEqual(emptyNodeBilling.merchant, '', 'Unconfigured merchant must be empty')
  assert.strictEqual(emptyNodeBilling.dueDate, '', 'Unconfigured dueDate must be empty')
  assert.strictEqual(emptyNodeBilling.configured, false, 'Unconfigured configured must be false')
  console.log('✓ Scenario 2: Unconfigured node returns neutral defaults without fake prices')
}

// -----------------------------------------------------------------------------
// Scenario 3: fetchAndCacheNodeBilling caches server response in memory and read-only cache
// -----------------------------------------------------------------------------
{
  const testNodeId = 'test-node-cached-uuid'
  const mockApi = async (uuid) => {
    assert.strictEqual(uuid, testNodeId)
    return {
      node_id: uuid,
      reset_day: 1,
      accounting_method: 'total',
      merchant: 'BandwagonHost',
      price: 49.99,
      currency: 'USD',
      cycle: 'annual',
      due_date: '2027-05-01',
      auto_renew: true,
      traffic_quota_bytes: 1099511627776,
    }
  }

  await fetchAndCacheNodeBilling(testNodeId, mockApi)

  // Verify memory cache
  const cached = getNodeBilling(testNodeId)
  assert.strictEqual(cached.merchant, 'BandwagonHost')
  assert.strictEqual(cached.price, 49.99)
  assert.strictEqual(cached.dueDate, '2027-05-01')
  assert.strictEqual(cached.configured, true)

  // Verify read-only cache in localStorage
  const readOnlyRaw = globalThis.localStorage.getItem(READONLY_CACHE_KEY)
  assert.ok(readOnlyRaw, 'Read-only cache must be persisted in localStorage')
  const readOnly = JSON.parse(readOnlyRaw)
  assert.strictEqual(readOnly[testNodeId].merchant, 'BandwagonHost')
  console.log('✓ Scenario 3: fetchAndCacheNodeBilling correctly populates in-memory & read-only cache')
}

// -----------------------------------------------------------------------------
// Scenario 4: saveNodeBillingRemote sends structured payload to backend API
// -----------------------------------------------------------------------------
{
  const saveNodeId = 'node-save-remote-1'
  let capturedPayload = null

  const mockSave = async (uuid, payload) => {
    assert.strictEqual(uuid, saveNodeId)
    capturedPayload = payload
    return {
      node_id: uuid,
      reset_day: payload.reset_day,
      reset_time: payload.reset_time,
      accounting_method: payload.accounting_method,
      traffic_quota_bytes: payload.traffic_quota_bytes,
      bonus_quota_bytes: payload.bonus_quota_bytes,
      merchant: payload.merchant,
      price: payload.price,
      currency: payload.currency,
      cycle: payload.cycle,
      start_date: payload.start_date,
      due_date: payload.due_date,
      auto_renew: payload.auto_renew,
      used_percent: 0,
    }
  }

  const result = await saveNodeBillingRemote(
    saveNodeId,
    {
      price: 88.5,
      currency: 'EUR',
      cycle: 'annual',
      dueDate: '2028-10-14',
      autoRenew: true,
      merchant: 'Hetzner',
      resetDay: 10,
      trafficQuotaBytes: 2199023255552,
    },
    mockSave
  )

  assert.ok(capturedPayload, 'Payload must be sent to backend')
  assert.strictEqual(capturedPayload.merchant, 'Hetzner')
  assert.strictEqual(capturedPayload.price, 88.5)
  assert.strictEqual(capturedPayload.currency, 'EUR')
  assert.strictEqual(capturedPayload.cycle, 'annual')
  assert.strictEqual(capturedPayload.due_date, '2028-10-14')
  assert.strictEqual(capturedPayload.auto_renew, true)
  assert.strictEqual(capturedPayload.reset_day, 10)
  assert.strictEqual(capturedPayload.traffic_quota_bytes, 2199023255552)

  // Verify memory cache updated from backend return
  const current = getNodeBilling(saveNodeId)
  assert.strictEqual(current.merchant, 'Hetzner')
  assert.strictEqual(current.price, 88.5)
  assert.strictEqual(current.currency, 'EUR')
  console.log('✓ Scenario 4: saveNodeBillingRemote converts and sends structured NodeBillingSettings to server')
}

// -----------------------------------------------------------------------------
// Scenario 5: saveNodeBillingRemote error handling preserves existing data
// -----------------------------------------------------------------------------
{
  const errNodeId = 'node-save-err-1'
  setCachedServerBilling(errNodeId, {
    merchant: 'ReliableServer',
    price: 15.0,
    currency: 'USD',
    cycle: 'month',
    due_date: '2026-12-01',
  })

  const failingSave = async () => {
    const err = new Error('401 Unauthorized: please log in')
    err.status = 401
    throw err
  }

  let caught = null
  try {
    await saveNodeBillingRemote(
      errNodeId,
      {
        merchant: 'CorruptedServer',
        price: 999.0,
      },
      failingSave
    )
  } catch (e) {
    caught = e
  }

  assert.ok(caught, 'Must throw error when save fails')
  assert.ok(caught.message.includes('401'), 'Error message must preserve failure details')

  // Cache must NOT be overwritten on failure
  const intact = getNodeBilling(errNodeId)
  assert.strictEqual(intact.merchant, 'ReliableServer', 'Cache must remain unchanged on failure')
  assert.strictEqual(intact.price, 15.0, 'Price must remain unchanged on failure')
  console.log('✓ Scenario 5: Save failure does not corrupt cache and throws descriptive error')
}

// -----------------------------------------------------------------------------
// Scenario 6: migrateLocalStorageBillingToBackend one-time migration
// -----------------------------------------------------------------------------
{
  const legacyStorageKey = 'probewatch_node_billing_v1'
  const legacyNodeId = 'legacy-node-migrate-1'

  // Clear migration flag and setup legacy data
  globalThis.localStorage.removeItem(MIGRATION_FLAG_KEY)
  globalThis.localStorage.setItem(
    legacyStorageKey,
    JSON.stringify({
      [legacyNodeId]: {
        merchant: 'RackNerd',
        price: 12.88,
        currency: 'USD',
        cycle: 'annual',
        dueDate: '2026-11-20',
        autoRenew: true,
      },
    })
  )

  let migratedCall = null
  const mockMigrateSave = async (uuid, payload) => {
    migratedCall = { uuid, payload }
    return {
      node_id: uuid,
      merchant: payload.merchant,
      price: payload.price,
      currency: payload.currency,
      cycle: payload.cycle,
      due_date: payload.due_date,
      auto_renew: payload.auto_renew,
    }
  }

  const summary = await migrateLocalStorageBillingToBackend([{ uuid: legacyNodeId }], {
    saveImpl: mockMigrateSave,
  })

  assert.strictEqual(summary.migrated, 1, 'Must migrate 1 item')
  assert.ok(migratedCall, 'Must call backend save for legacy item')
  assert.strictEqual(migratedCall.uuid, legacyNodeId)
  assert.strictEqual(migratedCall.payload.merchant, 'RackNerd')
  assert.strictEqual(migratedCall.payload.price, 12.88)

  // Verify migration flag is set
  assert.strictEqual(globalThis.localStorage.getItem(MIGRATION_FLAG_KEY), 'true')
  // Verify legacy raw key removed
  assert.strictEqual(globalThis.localStorage.getItem(legacyStorageKey), null)

  // Verify second run is skipped immediately
  const secondRun = await migrateLocalStorageBillingToBackend([{ uuid: legacyNodeId }])
  assert.strictEqual(secondRun.alreadyMigrated, true, 'Subsequent runs must be skipped')
  console.log('✓ Scenario 6: migrateLocalStorageBillingToBackend successfully migrates and cleans up legacy storage')
}

// -----------------------------------------------------------------------------
// Scenario 7: calculateRemainingValue & generateForumSalesPost works with server billing
// -----------------------------------------------------------------------------
{
  const node = {
    name: 'HK-BGP-Node',
    region: '香港',
    flag: '🇭🇰',
    os: 'Ubuntu 24.04',
    arch: 'x86_64',
    uptime: '120天',
  }
  const billing = {
    merchant: 'DMIT',
    price: 100,
    currency: 'USD',
    cycle: 'annual',
    dueDate: '2027-10-14',
    autoRenew: true,
  }

  const calc = calculateRemainingValue(billing)
  assert.ok(calc.daysRemaining > 300, 'Must have > 300 days remaining')
  assert.strictEqual(calc.isExpired, false)
  assert.ok(calc.remainingValueCNY > 0, 'Must calculate positive remaining value CNY')

  const post = generateForumSalesPost(node, billing, calc, 30)
  assert.ok(post.includes('[出] DMIT - HK-BGP-Node'), 'Forum post must include merchant and node name')
  assert.ok(post.includes('+¥30'), 'Forum post must include markup tag')
  assert.ok(post.includes('官方剩余价值：'), 'Forum post must include remaining value line')
  console.log('✓ Scenario 7: MJJ sales post and remaining value calculation preserved')
}

// -----------------------------------------------------------------------------
// Scenario 8: normalizeBillingCycle canonical and legacy cycle handling
// -----------------------------------------------------------------------------
{
  // Canonical cycles
  assert.strictEqual(normalizeBillingCycle('month'), 'month')
  assert.strictEqual(normalizeBillingCycle('quarter'), 'quarter')
  assert.strictEqual(normalizeBillingCycle('semiannual'), 'semiannual')
  assert.strictEqual(normalizeBillingCycle('annual'), 'annual')
  assert.strictEqual(normalizeBillingCycle('biennial'), 'biennial')
  assert.strictEqual(normalizeBillingCycle('triennial'), 'triennial')
  assert.strictEqual(normalizeBillingCycle('free'), 'free')

  // Legacy and alias cycles
  assert.strictEqual(normalizeBillingCycle('monthly'), 'month')
  assert.strictEqual(normalizeBillingCycle('quarterly'), 'quarter')
  assert.strictEqual(normalizeBillingCycle('half_year'), 'semiannual')
  assert.strictEqual(normalizeBillingCycle('half-year'), 'semiannual')
  assert.strictEqual(normalizeBillingCycle('semi-annual'), 'semiannual')
  assert.strictEqual(normalizeBillingCycle('year'), 'annual')
  assert.strictEqual(normalizeBillingCycle('yearly'), 'annual')
  assert.strictEqual(normalizeBillingCycle('two_year'), 'biennial')
  assert.strictEqual(normalizeBillingCycle('2year'), 'biennial')
  assert.strictEqual(normalizeBillingCycle('three_year'), 'triennial')
  assert.strictEqual(normalizeBillingCycle('3year'), 'triennial')
  assert.strictEqual(normalizeBillingCycle('one_time'), 'onetime')
  assert.strictEqual(normalizeBillingCycle('onetime'), 'onetime')

  // Case insensitivity & trimming
  assert.strictEqual(normalizeBillingCycle('  Annual  '), 'annual')
  assert.strictEqual(normalizeBillingCycle('MONTHLY'), 'month')

  // Unknown cycles return original trimmed string
  assert.strictEqual(normalizeBillingCycle('custom_cycle'), 'custom_cycle')
  assert.strictEqual(normalizeBillingCycle(''), '')

  // calculateRemainingValue with normalized cycles
  const annualCalc = calculateRemainingValue({ price: 120, cycle: 'annual', dueDate: '2027-01-01' })
  assert.strictEqual(annualCalc.cycleDays, 365)
  const biennialCalc = calculateRemainingValue({ price: 240, cycle: 'biennial', dueDate: '2027-01-01' })
  assert.strictEqual(biennialCalc.cycleDays, 730)
  const triennialCalc = calculateRemainingValue({ price: 360, cycle: 'triennial', dueDate: '2027-01-01' })
  assert.strictEqual(triennialCalc.cycleDays, 1095)
  const freeCalc = calculateRemainingValue({ price: 0, cycle: 'free', dueDate: '2099-01-01' })
  assert.strictEqual(freeCalc.isFree, true)
  assert.strictEqual(freeCalc.remainingValueCNY, 0)

  console.log('✓ Scenario 8: normalizeBillingCycle handles canonical, legacy, and unknown cycles properly')
}

console.log('\n=== All Billing Backend Unification Contract Tests Passed! ===')
