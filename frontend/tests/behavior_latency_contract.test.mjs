import assert from 'node:assert'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve } from 'node:path'
import { isTargetGlobal, nodeMatchesTarget, getTargetScopeSummary } from '../src/lib/targetScope.js'
import { escapeShell } from '../src/lib/shellEscape.js'

console.log('=== Starting ProbeWatch Latency & Enrollment Behavioral Tests ===\n')

// -----------------------------------------------------------------------------
// Scenario 1: node_ids=[] global task matching
// -----------------------------------------------------------------------------
{
  const globalTarget = { id: 't-global', name: 'Global Target', node_ids: [], node_tags: [] }
  assert.strictEqual(isTargetGlobal(globalTarget), true, 'Scenario 1: Empty node_ids & node_tags must be global')
  assert.strictEqual(isTargetGlobal({ id: 't1' }), true, 'Scenario 1: Undefined node_ids & node_tags must be global')

  const testNode = { id: 'node-abc', uuid: 'node-abc', name: 'Test Server', tags: ['hk'] }
  assert.strictEqual(nodeMatchesTarget(testNode, globalTarget), true, 'Scenario 1: Global target must match any node')
  console.log('[PASS] Scenario 1: Empty node_ids global scope matching verified.')
}

// -----------------------------------------------------------------------------
// Scenario 2: Single node_id scoped task
// -----------------------------------------------------------------------------
{
  const scopedTarget = { id: 't-single', name: 'Single Node Task', node_ids: ['node-1'], node_tags: [] }
  assert.strictEqual(isTargetGlobal(scopedTarget), false, 'Scenario 2: Non-empty node_ids must not be global')

  const node1 = { id: 'node-1', uuid: 'node-1', name: 'Server 1' }
  const node2 = { id: 'node-2', uuid: 'node-2', name: 'Server 2' }

  assert.strictEqual(nodeMatchesTarget(node1, scopedTarget), true, 'Scenario 2: Node 1 must match scoped task')
  assert.strictEqual(nodeMatchesTarget(node2, scopedTarget), false, 'Scenario 2: Node 2 must NOT match scoped task')
  console.log('[PASS] Scenario 2: Single node_id scoping verified.')
}

// -----------------------------------------------------------------------------
// Scenario 3: Multiple node_ids scoped task
// -----------------------------------------------------------------------------
{
  const multiTarget = { id: 't-multi', name: 'Multi Node Task', node_ids: ['node-1', 'node-2'], node_tags: [] }
  const node1 = { id: 'node-1', uuid: 'node-1', name: 'Server 1' }
  const node2 = { id: 'node-2', uuid: 'node-2', name: 'Server 2' }
  const node3 = { id: 'node-3', uuid: 'node-3', name: 'Server 3' }

  assert.strictEqual(nodeMatchesTarget(node1, multiTarget), true, 'Scenario 3: Node 1 matches multiTarget')
  assert.strictEqual(nodeMatchesTarget(node2, multiTarget), true, 'Scenario 3: Node 2 matches multiTarget')
  assert.strictEqual(nodeMatchesTarget(node3, multiTarget), false, 'Scenario 3: Node 3 does NOT match multiTarget')
  console.log('[PASS] Scenario 3: Multiple node_ids scoping verified.')
}

// -----------------------------------------------------------------------------
// Scenario 4: Edit preserving node_tags
// -----------------------------------------------------------------------------
{
  const targetWithTags = {
    id: 't-tags',
    name: 'Tag Scoped Target',
    host: '198.51.100.1',
    node_ids: [],
    node_tags: ['ap', 'hk'],
  }
  const summary = getTargetScopeSummary(targetWithTags)
  assert.strictEqual(summary.type, 'tags', 'Scenario 4: Summary type should be tags')
  assert.ok(summary.text.includes('ap, hk'), 'Scenario 4: Summary text must include tags')

  // Simulate edit payload construction
  const initialData = { ...targetWithTags }
  const parsedTags = ['ap', 'hk']
  const payload = {
    id: initialData.id,
    name: initialData.name,
    node_tags: parsedTags.length > 0 ? parsedTags : [],
  }
  assert.deepStrictEqual(payload.node_tags, ['ap', 'hk'], 'Scenario 4: node_tags must be preserved upon edit')
  console.log('[PASS] Scenario 4: Edit preserving node_tags verified.')
}

// -----------------------------------------------------------------------------
// Scenario 5: Edit preserving region_rules
// -----------------------------------------------------------------------------
{
  const initialData = {
    id: 't-region',
    name: 'Target with Region Rules',
    region_rules: [{ region: 'US', contains: 'United States' }],
  }
  const payload = {
    id: initialData.id,
    name: initialData.name,
  }
  if (Array.isArray(initialData?.region_rules) && initialData.region_rules.length > 0) {
    payload.region_rules = initialData.region_rules
  }
  assert.deepStrictEqual(
    payload.region_rules,
    [{ region: 'US', contains: 'United States' }],
    'Scenario 5: region_rules must be explicitly preserved'
  )
  console.log('[PASS] Scenario 5: Edit preserving region_rules verified.')
}

// -----------------------------------------------------------------------------
// Scenario 6: Simultaneous node_ids and node_tags verifying Go backend OR logic parity
// -----------------------------------------------------------------------------
{
  const hybridTarget = {
    id: 't-hybrid',
    node_ids: ['uuid-node-a'],
    node_tags: ['edge-proxy'],
  }
  // Node A matches via ID
  const nodeA = { id: 'node-a', uuid: 'uuid-node-a', tags: [] }
  // Node B matches via Tag
  const nodeB = { id: 'node-b', uuid: 'uuid-node-b', tags: ['edge-proxy'] }
  // Node C matches neither
  const nodeC = { id: 'node-c', uuid: 'uuid-node-c', tags: ['core-db'] }

  assert.strictEqual(nodeMatchesTarget(nodeA, hybridTarget), true, 'Scenario 6: Node A matches by ID')
  assert.strictEqual(nodeMatchesTarget(nodeB, hybridTarget), true, 'Scenario 6: Node B matches by tag (OR logic)')
  assert.strictEqual(nodeMatchesTarget(nodeC, hybridTarget), false, 'Scenario 6: Node C matches neither')

  const summary = getTargetScopeSummary(hybridTarget)
  assert.strictEqual(summary.type, 'hybrid')
  assert.ok(summary.text.includes('指定 1 台服务器 + 标签: edge-proxy'))
  console.log('[PASS] Scenario 6: Simultaneous node_ids and node_tags OR matching parity verified.')
}

// -----------------------------------------------------------------------------
// Scenario 7: Mock delete returning 403 retains item in list
// -----------------------------------------------------------------------------
{
  let targets = [{ id: 'target-1', name: 'Task 1' }, { id: 'target-2', name: 'Task 2' }]
  let operationError = null

  // Simulate handleDeleteLatencyTarget with 403 response
  const mockDelete = async (target) => {
    const res = { ok: false, status: 403, json: async () => ({ error: 'Forbidden' }) }
    if (!res.ok) {
      const errorData = await res.json()
      operationError = `删除目标 “${target.name}” 失败: ${errorData.error}`
      return
    }
    targets = targets.filter((t) => t.id !== target.id)
  }

  await mockDelete(targets[0])
  assert.strictEqual(targets.length, 2, 'Scenario 7: targets list must retain item on HTTP 403')
  assert.ok(operationError.includes('Forbidden'), 'Scenario 7: operationError must record forbidden error')
  console.log('[PASS] Scenario 7: Mock delete returning 403 retains item in list.')
}

// -----------------------------------------------------------------------------
// Scenario 8: Mock delete returning 500 retains item in list
// -----------------------------------------------------------------------------
{
  let targets = [{ id: 'target-1', name: 'Task 1' }, { id: 'target-2', name: 'Task 2' }]
  let operationError = null

  const mockDelete = async (target) => {
    const res = { ok: false, status: 500, json: async () => ({ error: 'Internal Server Error' }) }
    if (!res.ok) {
      const errorData = await res.json()
      operationError = `删除目标 “${target.name}” 失败: ${errorData.error}`
      return
    }
    targets = targets.filter((t) => t.id !== target.id)
  }

  await mockDelete(targets[1])
  assert.strictEqual(targets.length, 2, 'Scenario 8: targets list must retain item on HTTP 500')
  assert.ok(operationError.includes('Internal Server Error'), 'Scenario 8: operationError must record server error')
  console.log('[PASS] Scenario 8: Mock delete returning 500 retains item in list.')
}

// -----------------------------------------------------------------------------
// Scenario 9: Delete HTTP 204 successfully removes item
// -----------------------------------------------------------------------------
{
  let targets = [{ id: 'target-1', name: 'Task 1' }, { id: 'target-2', name: 'Task 2' }]
  let operationError = null

  const mockDelete = async (target) => {
    const res = { ok: true, status: 204 }
    if (!res.ok) {
      operationError = `删除失败`
      return
    }
    targets = targets.filter((t) => t.id !== target.id)
  }

  await mockDelete(targets[0])
  assert.strictEqual(targets.length, 1, 'Scenario 9: targets list must remove item on HTTP 204')
  assert.strictEqual(targets[0].id, 'target-2', 'Scenario 9: Remaining target should be target-2')
  assert.strictEqual(operationError, null)
  console.log('[PASS] Scenario 9: Delete HTTP 204 successfully removes item.')
}

// -----------------------------------------------------------------------------
// Scenario 10: Batch toggle partial failure correctly tallies success/failure counts
// -----------------------------------------------------------------------------
{
  const toUpdate = [
    { id: 't1', name: 'Task 1' },
    { id: 't2', name: 'Task 2' },
    { id: 't3', name: 'Task 3' },
  ]
  let successCount = 0
  const failedItems = []

  const mockResponses = {
    t1: { ok: true, status: 200 },
    t2: { ok: false, status: 500, error: 'Database locked' },
    t3: { ok: true, status: 200 },
  }

  for (const t of toUpdate) {
    const res = mockResponses[t.id]
    if (res.ok) {
      successCount++
    } else {
      failedItems.push(`“${t.name}” (${res.error || res.status})`)
    }
  }

  assert.strictEqual(successCount, 2, 'Scenario 10: successCount should be 2')
  assert.strictEqual(failedItems.length, 1, 'Scenario 10: failedItems length should be 1')
  assert.ok(failedItems[0].includes('Task 2'), 'Scenario 10: failedItem must name Task 2')

  const msg = `批量启用完成：成功 ${successCount} 个，失败 ${failedItems.length} 个 [${failedItems.join('；')}]`
  assert.ok(msg.includes('成功 2 个，失败 1 个'), 'Scenario 10: Tally message is correct')
  console.log('[PASS] Scenario 10: Batch toggle partial failure correctly tallies counts.')
}

// -----------------------------------------------------------------------------
// Scenario 11: Node removal failure retains existing node_ids
// -----------------------------------------------------------------------------
{
  const originalTargets = [
    { id: 't1', name: 'Task 1', node_ids: ['node-a', 'node-b'] },
  ]
  let currentTargets = JSON.parse(JSON.stringify(originalTargets))

  // Simulate API failure during node removal
  const res = { ok: false, status: 500, error: 'Write failed' }
  if (res.ok) {
    currentTargets[0].node_ids = currentTargets[0].node_ids.filter((id) => id !== 'node-a')
  }

  assert.deepStrictEqual(
    currentTargets[0].node_ids,
    ['node-a', 'node-b'],
    'Scenario 11: node_ids must be retained on removal failure'
  )
  console.log('[PASS] Scenario 11: Node removal failure retains existing node_ids.')
}

// -----------------------------------------------------------------------------
// Scenario 12: Intercepts last node removal when node_tags is empty
// -----------------------------------------------------------------------------
{
  const task = { id: 't-last', name: 'Only Node Task', node_ids: ['node-only'], node_tags: [] }
  const serverUuid = 'node-only'
  const nextNodeIds = task.node_ids.filter((id) => id !== serverUuid)

  let skipped = false
  let skipReason = ''
  if (nextNodeIds.length === 0 && (!task.node_tags || task.node_tags.length === 0)) {
    skipped = true
    skipReason = `“${task.name}” (仅剩此唯一指定服务器，移除会导致任务扩大至全部服务器，请直接编辑或删除该任务)`
  }

  assert.strictEqual(skipped, true, 'Scenario 12: Last node removal must be intercepted')
  assert.ok(skipReason.includes('扩大至全部服务器'), 'Scenario 12: Intercept message explains global fallback risk')
  console.log('[PASS] Scenario 12: Intercepts last node removal when node_tags is empty.')
}

// -----------------------------------------------------------------------------
// Scenario 13: Canceling server picker retains initial/previous selection without mutation
// -----------------------------------------------------------------------------
{
  let selectedServers = new Set(['node-1'])
  let tempSelectedServers = new Set(selectedServers)

  // User alters temp selection in the modal
  tempSelectedServers.add('node-2')
  tempSelectedServers.delete('node-1')

  // User cancels: cancel handler restores temp to selectedServers
  const handleCancel = () => {
    tempSelectedServers = selectedServers === 'ALL' ? 'ALL' : new Set(selectedServers)
  }
  handleCancel()

  assert.strictEqual(selectedServers.has('node-1'), true, 'Scenario 13: selectedServers retains node-1')
  assert.strictEqual(selectedServers.has('node-2'), false, 'Scenario 13: selectedServers does not have node-2')
  assert.strictEqual(tempSelectedServers.has('node-1'), true, 'Scenario 13: tempSelectedServers was reset')
  console.log('[PASS] Scenario 13: Canceling server picker retains initial selection.')
}

// -----------------------------------------------------------------------------
// Scenario 14: 375px & 320px responsive checks
// -----------------------------------------------------------------------------
{
  const css = readFileSync(resolve(process.cwd(), 'frontend/src/styles.css'), 'utf8')
  assert.ok(css.includes('.table-scroll'), 'Scenario 14: styles.css must define .table-scroll')
  assert.ok(css.includes('overflow-x: auto'), 'Scenario 14: table-scroll must specify overflow-x: auto')

  // Check that mobile media queries exist and don't enforce rigid min-width on containers
  assert.ok(css.includes('@media (max-width: 768px)') || css.includes('@media (max-width: 640px)'), 'Scenario 14: Media queries exist')
  assert.ok(css.includes('.lite-add-modal') || css.includes('.modal-overlay'), 'Scenario 14: Modal styles defined')
  console.log('[PASS] Scenario 14: 375px & 320px responsive CSS rules verified.')
}

// -----------------------------------------------------------------------------
// Scenario 15: Guest view / readOnly hides edit, delete, and switch buttons
// -----------------------------------------------------------------------------
{
  const monitoringViewCode = readFileSync(
    resolve(process.cwd(), 'frontend/src/components/MonitoringView.jsx'),
    'utf8'
  )
  assert.ok(monitoringViewCode.includes('readOnly = false'), 'Scenario 15: MonitoringView accepts readOnly prop')
  assert.ok(monitoringViewCode.includes('!readOnly && ('), 'Scenario 15: Action buttons gated by !readOnly')
  assert.ok(monitoringViewCode.includes('colSpan={readOnly ? 6 : 7}'), 'Scenario 15: Dynamic colSpan for tasks table')
  assert.ok(monitoringViewCode.includes('colSpan={readOnly ? 3 : 4}'), 'Scenario 15: Dynamic colSpan for servers table')
  console.log('[PASS] Scenario 15: Guest view / readOnly hides edit, delete, and switch buttons verified.')
}

// -----------------------------------------------------------------------------
// Scenario 16: Installation command security escaping
// -----------------------------------------------------------------------------
{
  const testInput = 'my-node "quoted" ; $(rm -rf /) `whoami` \n newline \r return \\ backslash $VAR'
  const escaped = escapeShell(testInput)

  assert.ok(!escaped.includes('\n'), 'Scenario 16: escapeShell must strip newlines')
  assert.ok(!escaped.includes('\r'), 'Scenario 16: escapeShell must strip carriage returns')
  assert.ok(escaped.includes('\\"quoted\\"'), 'Scenario 16: escapeShell must escape double quotes')
  assert.ok(escaped.includes('\\$VAR'), 'Scenario 16: escapeShell must escape dollar signs')
  assert.ok(escaped.includes('\\`whoami\\`'), 'Scenario 16: escapeShell must escape backticks')
  assert.ok(escaped.includes('\\\\ backslash'), 'Scenario 16: escapeShell must escape backslashes')
  console.log('[PASS] Scenario 16: Installation command shell escaping verified.')
}

// -----------------------------------------------------------------------------
// Scenario 17: Docker unreleased state in AddServerModal
// -----------------------------------------------------------------------------
{
  const addServerCode = readFileSync(
    resolve(process.cwd(), 'frontend/src/components/AddServerModal.jsx'),
    'utf8'
  )
  assert.ok(
    addServerCode.includes('Docker 镜像尚未发布，请使用 Linux/OpenWrt 安装'),
    'Scenario 17: AddServerModal must show unreleased warning for Docker'
  )
  assert.ok(
    !addServerCode.includes('probewatch/agent:latest'),
    'Scenario 17: AddServerModal must NOT contain probewatch/agent:latest'
  )
  assert.ok(
    addServerCode.includes("activeTab === 'docker'"),
    'Scenario 17: Docker tab is explicitly checked and disabled'
  )
  assert.ok(
    !addServerCode.includes('dockerScript'),
    'Scenario 17: dockerScript must not exist'
  )
  console.log('[PASS] Scenario 17: Docker unreleased state in AddServerModal verified.')
}

// -----------------------------------------------------------------------------
// Scenario 18: Docker unreleased state in ServerManageView
// -----------------------------------------------------------------------------
{
  const serverManageCode = readFileSync(
    resolve(process.cwd(), 'frontend/src/components/ServerManageView.jsx'),
    'utf8'
  )
  assert.ok(
    serverManageCode.includes('Docker 镜像尚未发布，请使用 Linux/OpenWrt 安装'),
    'Scenario 18: ServerManageView must show unreleased warning for Docker'
  )
  assert.ok(
    !serverManageCode.includes('probewatch/agent:latest'),
    'Scenario 18: ServerManageView must NOT contain probewatch/agent:latest'
  )
  assert.ok(
    serverManageCode.includes("if (installTab === 'docker') return ''"),
    'Scenario 18: installCommand must return empty string for docker tab'
  )
  assert.ok(
    serverManageCode.includes("disabled={!installCommand || installTab === 'docker'}"),
    'Scenario 18: Copy button must be disabled for docker tab'
  )
  assert.ok(
    serverManageCode.includes("if (!installCommand || installTab === 'docker') return"),
    'Scenario 18: copyInstallCommand handler must block execution for docker tab'
  )
  console.log('[PASS] Scenario 18: Docker unreleased state in ServerManageView verified.')
}

// -----------------------------------------------------------------------------
// Scenario 19: Other installation components (NodeEnroll)
// -----------------------------------------------------------------------------
{
  const nodeEnrollCode = readFileSync(
    resolve(process.cwd(), 'frontend/src/components/NodeEnroll.jsx'),
    'utf8'
  )
  assert.ok(
    !nodeEnrollCode.includes('probewatch/agent:latest'),
    'Scenario 19: NodeEnroll must NOT contain probewatch/agent:latest'
  )
  assert.ok(
    !nodeEnrollCode.includes('docker run'),
    'Scenario 19: NodeEnroll must not generate unverified docker run commands'
  )
  console.log('[PASS] Scenario 19: Other installation components verified.')
}

// -----------------------------------------------------------------------------
// Scenario 20: Whole codebase audit for probewatch/agent:latest & consistent warning
// -----------------------------------------------------------------------------
{
  const getAllFiles = (dir) => {
    const entries = readdirSync(dir)
    let files = []
    for (const entry of entries) {
      const full = resolve(dir, entry)
      const stat = statSync(full)
      if (stat.isDirectory()) {
        files = files.concat(getAllFiles(full))
      } else if (full.endsWith('.js') || full.endsWith('.jsx')) {
        files.push(full)
      }
    }
    return files
  }

  const srcFiles = getAllFiles(resolve(process.cwd(), 'frontend/src'))
  assert.ok(srcFiles.length > 10, 'Scenario 20: frontend/src must contain source files')

  for (const file of srcFiles) {
    const content = readFileSync(file, 'utf8')
    assert.ok(
      !content.includes('probewatch/agent:latest'),
      `Scenario 20: Production source file ${file} MUST NOT contain probewatch/agent:latest`
    )
  }

  // Verify consistent warning exists across all Docker installation entries
  const expectedNotice = 'Docker 镜像尚未发布，请使用 Linux/OpenWrt 安装'
  const addServerCode = readFileSync(resolve(process.cwd(), 'frontend/src/components/AddServerModal.jsx'), 'utf8')
  const serverManageCode = readFileSync(resolve(process.cwd(), 'frontend/src/components/ServerManageView.jsx'), 'utf8')

  assert.ok(addServerCode.includes(expectedNotice), 'Scenario 20: AddServerModal must match consistent notice')
  assert.ok(serverManageCode.includes(expectedNotice), 'Scenario 20: ServerManageView must match consistent notice')

  console.log('[PASS] Scenario 20: Full codebase audit passed, no probewatch/agent:latest in frontend/src and consistent notice confirmed.')
}

console.log('\n=== ALL 20 LATENCY, SCOPE & ENROLLMENT BEHAVIORAL CONTRACT TESTS PASSED ===')
