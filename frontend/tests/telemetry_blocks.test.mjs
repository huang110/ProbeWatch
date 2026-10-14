import assert from 'node:assert'
import {
  getLatencyBlocks,
  getLossBlocks,
  mapLatencyToColor,
  mapLossToColor,
  TELEMETRY_COLORS,
} from '../src/lib/telemetryBlocks.js'

console.log('=== Starting ProbeWatch Telemetry Blocks Dynamic Mapping Unit Tests ===\n')

// -----------------------------------------------------------------------------
// Scenario 1: All Successful Samples (全部成功)
// -----------------------------------------------------------------------------
{
  const perfectLatencies = [15, 28, 42, 55, 60]
  const blocks = getLatencyBlocks(perfectLatencies)
  assert.strictEqual(blocks.length, 5, 'Must have exactly 5 blocks for 5 samples')
  for (const c of blocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.EXCELLENT, 'Latency < 65ms must be green')
  }

  const zeroLossSamples = [0, 0, 0, 0]
  const lossBlocks = getLossBlocks(zeroLossSamples)
  assert.strictEqual(lossBlocks.length, 4, 'Must have exactly 4 blocks')
  for (const c of lossBlocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.EXCELLENT, 'Loss == 0% must be green')
  }
  console.log('✓ Scenario 1: All successful samples map directly to EXCELLENT green')
}

// -----------------------------------------------------------------------------
// Scenario 2: All Dropped / Timeout Samples (全部丢包)
// -----------------------------------------------------------------------------
{
  const highLossSamples = [100, 85, 50, 48.3, 30]
  const lossBlocks = getLossBlocks(highLossSamples)
  assert.strictEqual(lossBlocks.length, 5)
  for (const c of lossBlocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.CRITICAL, 'Loss > 20% must be crimson red')
  }

  const failedProbeSamples = [
    { status: 'failed', latency_ms: 0 },
    { status: 'timeout', latency_ms: 5000 },
    { status: 'down', latency_ms: 0 },
  ]
  const failedBlocks = getLatencyBlocks(failedProbeSamples)
  assert.strictEqual(failedBlocks.length, 3)
  for (const c of failedBlocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.CRITICAL, 'Failed / timeout probes must be critical red')
  }
  console.log('✓ Scenario 2: All dropped / timeout samples map to CRITICAL red')
}

// -----------------------------------------------------------------------------
// Scenario 3: Mixed Data (混合数据)
// -----------------------------------------------------------------------------
{
  // Mixed latencies: excellent (<65), fair (65-130), poor (130-220), critical (>=220)
  const mixedLatencies = [
    { latency_ms: 25 },
    { latency: 95 },
    { result: { latency_ms: 180 } },
    { latency_ms: 310 },
  ]
  const latBlocks = getLatencyBlocks(mixedLatencies)
  assert.strictEqual(latBlocks.length, 4)
  assert.strictEqual(latBlocks[0], TELEMETRY_COLORS.EXCELLENT, '25ms -> Green')
  assert.strictEqual(latBlocks[1], TELEMETRY_COLORS.FAIR, '95ms -> Amber')
  assert.strictEqual(latBlocks[2], TELEMETRY_COLORS.POOR, '180ms -> Rose')
  assert.strictEqual(latBlocks[3], TELEMETRY_COLORS.CRITICAL, '310ms -> Red')

  // Mixed loss: 0%, slight (3%), medium (12%), severe (45%)
  const mixedLoss = [
    { loss: 0 },
    { loss_percent: 3.5 },
    { result: { packet_loss: 15.0 } },
    { packet_loss: 45.0 },
  ]
  const lossBlocks = getLossBlocks(mixedLoss)
  assert.strictEqual(lossBlocks.length, 4)
  assert.strictEqual(lossBlocks[0], TELEMETRY_COLORS.EXCELLENT, '0% -> Green')
  assert.strictEqual(lossBlocks[1], TELEMETRY_COLORS.FAIR, '3.5% -> Amber')
  assert.strictEqual(lossBlocks[2], TELEMETRY_COLORS.POOR, '15% -> Rose')
  assert.strictEqual(lossBlocks[3], TELEMETRY_COLORS.CRITICAL, '45% -> Red')
  console.log('✓ Scenario 3: Mixed data correctly maps across all 4 threshold tiers')
}

// -----------------------------------------------------------------------------
// Scenario 4: Empty Array & Null State (空数组)
// -----------------------------------------------------------------------------
{
  assert.deepStrictEqual(getLatencyBlocks([]), [], 'Empty latency array must return empty array')
  assert.deepStrictEqual(getLossBlocks([]), [], 'Empty loss array must return empty array')
  assert.deepStrictEqual(getLatencyBlocks(null), [], 'null must return empty array')
  assert.deepStrictEqual(getLossBlocks(null), [], 'null must return empty array')
  assert.deepStrictEqual(getLatencyBlocks(undefined), [], 'undefined must return empty array')
  assert.deepStrictEqual(getLossBlocks(undefined), [], 'undefined must return empty array')
  assert.deepStrictEqual(getLatencyBlocks(''), [], 'empty string must return empty array')
  assert.deepStrictEqual(getLossBlocks(''), [], 'empty string must return empty array')
  console.log('✓ Scenario 4: Empty arrays and nullish values return empty array for "暂无采样"')
}

// -----------------------------------------------------------------------------
// Scenario 5: Missing Fields (缺失字段)
// -----------------------------------------------------------------------------
{
  const missingFieldSamples = [
    {},
    { unknown_field: 123 },
    { latency: null },
    { result: {} },
    null,
    undefined,
  ]
  const blocks = getLatencyBlocks(missingFieldSamples)
  assert.strictEqual(blocks.length, 6)
  for (const c of blocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.MISSING, 'Missing fields must display gray')
  }

  const missingLossSamples = [
    {},
    { loss: null },
    { result: { other: 1 } },
  ]
  const lBlocks = getLossBlocks(missingLossSamples)
  assert.strictEqual(lBlocks.length, 3)
  for (const c of lBlocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.MISSING, 'Missing loss fields must display gray')
  }
  console.log('✓ Scenario 5: Missing fields and null items reliably display MISSING gray')
}

// -----------------------------------------------------------------------------
// Scenario 6: Illegal Values (非法数值)
// -----------------------------------------------------------------------------
{
  const illegalLatency = ['invalid-text', NaN, Infinity, -100, -0.5]
  const blocks = getLatencyBlocks(illegalLatency)
  assert.strictEqual(blocks.length, 5)
  for (const c of blocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.MISSING, 'Illegal latency must display gray')
  }

  const illegalLoss = ['not-a-number', NaN, -5, Infinity]
  const lBlocks = getLossBlocks(illegalLoss)
  assert.strictEqual(lBlocks.length, 4)
  for (const c of lBlocks) {
    assert.strictEqual(c, TELEMETRY_COLORS.MISSING, 'Illegal loss must display gray')
  }
  console.log('✓ Scenario 6: Non-numeric, negative, and infinite values display MISSING gray')
}

// -----------------------------------------------------------------------------
// Scenario 7: Zero Synthetic Data (不伪造历史数据)
// -----------------------------------------------------------------------------
{
  // When 1 single value is passed, it returns exactly 1 block, NEVER 16 fake blocks!
  const single191ms = getLatencyBlocks(191)
  assert.strictEqual(single191ms.length, 1, 'Passing 1 value must NOT fabricate 16 screenshot blocks')
  assert.strictEqual(single191ms[0], TELEMETRY_COLORS.POOR, '191ms maps to POOR')

  const single48loss = getLossBlocks(48.3)
  assert.strictEqual(single48loss.length, 1, 'Passing 1 loss value must NOT fabricate 16 screenshot blocks')
  assert.strictEqual(single48loss[0], TELEMETRY_COLORS.CRITICAL, '48.3% maps to CRITICAL')
  console.log('✓ Scenario 7: Zero synthetic screenshot data generation verified')
}

console.log('\n=== All Telemetry Blocks Dynamic Mapping Unit Tests Passed! ===')
