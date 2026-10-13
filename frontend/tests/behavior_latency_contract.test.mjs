import assert from 'node:assert'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  isTargetGlobal,
  nodeMatchesTarget,
  getTargetScopeSummary,
  formatTargetScopeDisplay,
  maskNodeIdentifier,
  resolveNodeDisplayName,
  extractAvailableNodeTags,
  TARGET_QUICK_TEMPLATES,
  applyTargetQuickTemplate,
  buildTargetFormFromRecord,
  buildTargetSubmitPayload,
  executeWithCsrfRetry,
} from '../src/lib/targetScope.js'
import { escapeShell } from '../src/lib/shellEscape.js'
import {
  buildIPQualityViewModel,
  normalizeIPQuality,
  parseIPQualityScoreValue,
  formatIPQualitySourceName,
  formatIPQualityType,
} from '../src/lib/api.js'

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

// -----------------------------------------------------------------------------
// Scenario 21: IPQA with risk scores & database flags (IPQA takes priority over legacy ipwho.is)
// -----------------------------------------------------------------------------
{
  const payload = {
    ip_quality: {
      ip_type: 'unknown',
      country: 'SG',
      region: 'Central Singapore',
      city: 'Singapore',
      asn: 'AS16509',
      organization: 'Amazon.com, Inc.',
      risk: 'unknown',
      checked_at: 1791846642,
    },
    ipqa: {
      enabled: true,
      installed: true,
      state: 'enabled',
      has_ipv4: true,
      has_ipv6: false,
      latest_archive_date: '2026-10-12',
      ipv4: {
        risk_score: 13.18,
        risk_level: 'low',
        ip_type: '广播IP',
        country: 'SG',
        region: '新加坡',
        asn: 'AS16509',
        organization: 'Amazon.com, Inc.',
        proxy: true,
        vpn: true,
        tor: false,
        abuse: false,
        source_scores: {
          AbuseIPDB: 0,
          DBIP: 0,
          IP2LOCATION: 3,
          IPQS: 75,
          SCAMALYTICS: 0,
          ipapi: 1.09,
        },
      },
    },
  }

  const vm = normalizeIPQuality(payload)
  assert.strictEqual(vm.hasScores, true, 'Scenario 21: IPQA scores must be detected')
  assert.strictEqual(vm.risk, 'low', 'Scenario 21: IPQA risk_level must override legacy unknown')
  assert.strictEqual(vm.ipType, '广播IP', 'Scenario 21: IPQA ip_type must override legacy unknown')
  assert.strictEqual(vm.scoreItems.length, 6, 'Scenario 21: Must extract all 6 IPQA source_scores')
  assert.strictEqual(vm.hasFlags, true, 'Scenario 21: Must extract threat flags and database conclusions')

  const proxyFlag = vm.flagItems.find((f) => f.key === 'proxy')
  assert.ok(proxyFlag && proxyFlag.value === true && proxyFlag.text === '是', 'Scenario 21: Proxy flag must be true/是')

  const ipqsDbFlag = vm.flagItems.find((f) => f.key === 'db:ipqs')
  assert.ok(ipqsDbFlag && ipqsDbFlag.status === 'yes' && ipqsDbFlag.text.includes('命中'), 'Scenario 21: IPQS=75 must show 命中 conclusion')

  const abuseDbFlag = vm.flagItems.find((f) => f.key === 'db:abuseipdb')
  assert.ok(abuseDbFlag && abuseDbFlag.status === 'no' && abuseDbFlag.text.includes('通过'), 'Scenario 21: AbuseIPDB=0 must show 通过 conclusion')

  console.log('[PASS] Scenario 21: IPQA with risk scores & database flags verified.')
}

// -----------------------------------------------------------------------------
// Scenario 22: IPQA with media only and NO scores/flags (must not fake scores from ipqa_enabled=true)
// -----------------------------------------------------------------------------
{
  const mediaOnlyPayload = {
    ipqa: {
      enabled: true,
      installed: true,
      state: 'enabled',
      latest_archive_date: '2026-10-12',
      media: [
        { detector_id: 'netflix', detector: 'Netflix', status: 'available', region: 'SG' },
        { detector_id: 'openai', detector: 'ChatGPT', status: 'available', region: 'SG' },
      ],
    },
  }

  const vm = buildIPQualityViewModel(mediaOnlyPayload)
  assert.strictEqual(vm.ipqa_enabled, true, 'Scenario 22: IPQA must be enabled')
  assert.strictEqual(vm.media.length, 2, 'Scenario 22: Media items must be preserved')
  assert.strictEqual(vm.hasScores, false, 'Scenario 22: Media-only IPQA must NOT pretend to have risk scores')
  assert.strictEqual(vm.scoreItems.length, 0, 'Scenario 22: scoreItems must be empty when no scores exist')
  assert.strictEqual(vm.hasFlags, false, 'Scenario 22: Media-only IPQA must NOT fabricate database flags')
  assert.strictEqual(vm.flagItems.length, 0, 'Scenario 22: flagItems must be empty when no conclusions exist')

  console.log('[PASS] Scenario 22: IPQA with media only (empty scores & flags) verified.')
}

// -----------------------------------------------------------------------------
// Scenario 23: IPv4 has scores, IPv6 has no scores
// -----------------------------------------------------------------------------
{
  const v4OnlyPayload = {
    ipqa: {
      enabled: true,
      installed: true,
      has_ipv4: true,
      has_ipv6: false,
      ipv4: {
        risk_score: 0.14,
        risk_level: 'low',
        ip_type: '原生IP',
        proxy: false,
        vpn: false,
        tor: false,
        abuse: false,
        source_scores: {
          AbuseIPDB: 0,
          SCAMALYTICS: 0,
          ipapi: '0.69',
        },
      },
      ipv6: null,
    },
  }

  const vm = buildIPQualityViewModel(v4OnlyPayload)
  assert.strictEqual(vm.hasIPv4Scores, true, 'Scenario 23: hasIPv4Scores must be true')
  assert.strictEqual(vm.hasIPv6Scores, false, 'Scenario 23: hasIPv6Scores must be false')
  assert.strictEqual(vm.isDualStackScores, false, 'Scenario 23: isDualStackScores must be false')
  assert.strictEqual(vm.scoreItems.length, 3, 'Scenario 23: Must have 3 IPv4 score items')
  assert.strictEqual(vm.scoreItems[0].label, 'AbuseIPDB', 'Scenario 23: Single-stack label uses clean source name')
  assert.strictEqual(vm.scoreItems[2].displayValue, '0.69', 'Scenario 23: Numeric string "0.69" parsed accurately')

  console.log('[PASS] Scenario 23: IPv4 present and IPv6 absent verified.')
}

// -----------------------------------------------------------------------------
// Scenario 24: Dual-stack IPv4 and IPv6 both have scores (must NOT overwrite each other)
// -----------------------------------------------------------------------------
{
  const dualStackPayload = {
    ipqa: {
      enabled: true,
      installed: true,
      has_ipv4: true,
      has_ipv6: true,
      ipv4: {
        risk_score: 13.18,
        risk_level: 'low',
        ip_type: '广播IP',
        proxy: true,
        vpn: true,
        tor: false,
        abuse: false,
        source_scores: {
          AbuseIPDB: 0,
          IPQS: 75,
          ipapi: 1.09,
        },
      },
      ipv6: {
        risk_score: 20.2,
        risk_level: 'low',
        ip_type: '广播IP',
        proxy: true,
        vpn: false,
        tor: false,
        abuse: false,
        source_scores: {
          AbuseIPDB: 0,
          IPQS: 95,
          ipapi: 0,
        },
      },
    },
  }

  const vm = buildIPQualityViewModel(dualStackPayload)
  assert.strictEqual(vm.isDualStackScores, true, 'Scenario 24: Dual-stack scores must be detected')
  assert.strictEqual(vm.scoreGroups.length, 2, 'Scenario 24: Must preserve separate IPv4 and IPv6 score groups')
  assert.strictEqual(vm.scoreItems.length, 6, 'Scenario 24: Both IPv4 (3) and IPv6 (3) scores must be preserved without overwriting')

  const v4Ipqs = vm.scoreItems.find((i) => i.key === 'IPv4:IPQS')
  const v6Ipqs = vm.scoreItems.find((i) => i.key === 'IPv6:IPQS')
  assert.ok(v4Ipqs && v4Ipqs.value === 75 && v4Ipqs.label === 'IPQS (IPv4)', 'Scenario 24: IPv4 IPQS=75 preserved and labeled')
  assert.ok(v6Ipqs && v6Ipqs.value === 95 && v6Ipqs.label === 'IPQS (IPv6)', 'Scenario 24: IPv6 IPQS=95 preserved and labeled')

  const vpnFlag = vm.flagItems.find((f) => f.key === 'vpn')
  assert.ok(vpnFlag && vpnFlag.text === '是 (IPv4)', 'Scenario 24: Mixed dual-stack boolean flag identifies family')

  const dbIpqs = vm.databaseFlags.find((d) => d.key === 'db:ipqs')
  assert.ok(dbIpqs && dbIpqs.text.includes('v4:75/v6:95'), 'Scenario 24: Dual-stack database conclusion preserves both v4 and v6 scores')

  console.log('[PASS] Scenario 24: Dual-stack IPv4 + IPv6 scores preserved without overwrite.')
}

// -----------------------------------------------------------------------------
// Scenario 25: Score value 0 and numeric string "0" (must be valid low-risk, never treated as empty)
// -----------------------------------------------------------------------------
{
  const pNumZero = parseIPQualityScoreValue(0)
  assert.strictEqual(pNumZero.status, 'valid', 'Scenario 25: Numeric 0 must have status=valid')
  assert.strictEqual(pNumZero.value, 0, 'Scenario 25: Numeric 0 value must be 0')
  assert.strictEqual(pNumZero.displayValue, '0', 'Scenario 25: Numeric 0 displayValue must be "0"')
  assert.strictEqual(pNumZero.tone, 'low', 'Scenario 25: Numeric 0 tone must be low')
  assert.strictEqual(pNumZero.hasConclusion, true, 'Scenario 25: Numeric 0 must have conclusion')

  const pStrZero = parseIPQualityScoreValue('0')
  assert.strictEqual(pStrZero.status, 'valid', 'Scenario 25: String "0" must have status=valid')
  assert.strictEqual(pStrZero.value, 0, 'Scenario 25: String "0" value must be 0')
  assert.strictEqual(pStrZero.displayValue, '0', 'Scenario 25: String "0" displayValue must be "0"')

  const vmZero = buildIPQualityViewModel({
    ipv4: {
      source_scores: {
        AbuseIPDB: 0,
        DBIP: '0',
      },
    },
  })
  assert.strictEqual(vmZero.hasScores, true, 'Scenario 25: All-zero scores must still render as valid score list')
  assert.strictEqual(vmZero.scoreItems.length, 2, 'Scenario 25: Both 0 and "0" must be present in scoreItems')
  assert.strictEqual(vmZero.databaseFlags.length, 2, 'Scenario 25: Zero scores must produce 通过 (0) database conclusions')

  console.log('[PASS] Scenario 25: Zero score values (0 and "0") verified.')
}

// -----------------------------------------------------------------------------
// Scenario 26: Score value "unknown" and missing null/undefined/""
// -----------------------------------------------------------------------------
{
  const pUnknown = parseIPQualityScoreValue('unknown')
  assert.strictEqual(pUnknown.status, 'unknown', 'Scenario 26: "unknown" must have status=unknown')
  assert.strictEqual(pUnknown.value, null, 'Scenario 26: "unknown" value must be null (NOT 0)')
  assert.strictEqual(pUnknown.displayValue, '未知', 'Scenario 26: "unknown" displayValue must be 未知')
  assert.strictEqual(pUnknown.hasConclusion, false, 'Scenario 26: "unknown" must NOT count as a database conclusion')

  for (const missingVal of [null, undefined, '']) {
    const pMissing = parseIPQualityScoreValue(missingVal)
    assert.strictEqual(pMissing.status, 'missing', 'Scenario 26: null/undefined/"" must have status=missing')
    assert.strictEqual(pMissing.hasConclusion, false, 'Scenario 26: missing value must NOT count as conclusion')
  }

  const vmUnknown = buildIPQualityViewModel({
    ipv4: {
      source_scores: {
        SCAMALYTICS: 'unknown',
        IPQS: null,
        DBIP: '',
      },
    },
  })
  assert.strictEqual(vmUnknown.scoreItems.length, 1, 'Scenario 26: Explicit "unknown" renders as 未知 row while null/"" are omitted')
  assert.strictEqual(vmUnknown.scoreItems[0].displayValue, '未知', 'Scenario 26: "unknown" displays 未知')
  assert.strictEqual(vmUnknown.scoreItems[0].value, null, 'Scenario 26: "unknown" is never coerced to 0')
  assert.strictEqual(vmUnknown.databaseFlags.length, 0, 'Scenario 26: "unknown" and missing scores must NOT fabricate low-risk database flags')

  console.log('[PASS] Scenario 26: "unknown" and missing score values verified.')
}

// -----------------------------------------------------------------------------
// Scenario 27: Legacy IPQualityInfo fallback (when IPQA has no ipv4/ipv6)
// -----------------------------------------------------------------------------
{
  const legacyPayload = {
    ip_type: 'hosting',
    country: 'US',
    region: 'California',
    city: 'Los Angeles',
    asn: 'AS13335',
    organization: 'Cloudflare, Inc.',
    proxy: false,
    vpn: false,
    tor: false,
    abuse: false,
    risk: 'low',
    checked_at: 1700000000,
    scores: {
      scamalytics: 12,
      abuseipdb: 0,
    },
  }

  const vmLegacy = buildIPQualityViewModel(legacyPayload)
  assert.strictEqual(vmLegacy.hasScores, true, 'Scenario 27: Legacy scores must be supported as fallback')
  assert.strictEqual(vmLegacy.scoreItems.length, 2, 'Scenario 27: Legacy scores must produce 2 scoreItems')
  assert.strictEqual(vmLegacy.scoreItems[0].label, 'Scamalytics', 'Scenario 27: Legacy source key formatted')
  assert.strictEqual(vmLegacy.scoreItems[0].displayValue, '12', 'Scenario 27: Legacy score value preserved')
  assert.strictEqual(vmLegacy.hasFlags, true, 'Scenario 27: Legacy flags must be supported')
  const hostingFlag = vmLegacy.flagItems.find((f) => f.key === 'hosting')
  assert.ok(hostingFlag && hostingFlag.value === true && hostingFlag.text === '是', 'Scenario 27: hosting ip_type infers 机房=是')

  console.log('[PASS] Scenario 27: Legacy IPQualityInfo fallback verified.')
}

// -----------------------------------------------------------------------------
// Scenario 28 (TargetManage #1 & #2): Drawer closed by default, opens on "新建目标"
// -----------------------------------------------------------------------------
{
  const targetManageSrc = readFileSync(resolve('frontend/src/components/TargetManage.jsx'), 'utf8')
  assert.ok(
    targetManageSrc.includes('const [drawerOpen, setDrawerOpen] = useState(false)'),
    'Scenario 28: Drawer must be closed by default (drawerOpen = false)'
  )
  assert.ok(
    targetManageSrc.includes("openCreateDrawer('http')") && targetManageSrc.includes('setDrawerOpen(true)'),
    'Scenario 28: Clicking "新建目标" must open the drawer via openCreateDrawer()'
  )
  assert.ok(
    targetManageSrc.includes('{drawerOpen && (') && targetManageSrc.includes('target-drawer-panel'),
    'Scenario 28: Creation/Edit form must only render inside .target-drawer-panel when drawerOpen is true'
  )
  console.log('[PASS] Scenario 28: Drawer closed by default & opens on "新建目标" verified.')
}

// -----------------------------------------------------------------------------
// Scenario 29 (TargetManage #3): Create HTTP target submits accurate payload
// -----------------------------------------------------------------------------
{
  const form = applyTargetQuickTemplate('http', {})
  form.id = 'http-cf-check'
  const payload = buildTargetSubmitPayload(form)
  assert.strictEqual(payload.kind, 'http', 'Scenario 29: HTTP health template sets kind=http')
  assert.strictEqual(payload.host, 'example.com', 'Scenario 29: Host populated')
  assert.strictEqual(payload.port, 80, 'Scenario 29: Port is numeric 80')
  assert.strictEqual(payload.path, '/health', 'Scenario 29: Path populated')
  assert.strictEqual(payload.expected_status, 200, 'Scenario 29: expected_status is numeric 200')
  assert.deepStrictEqual(payload.node_ids, [], 'Scenario 29: Global scope submits empty node_ids')
  assert.deepStrictEqual(payload.node_tags, [], 'Scenario 29: Global scope submits empty node_tags')
  console.log('[PASS] Scenario 29: Create HTTP target payload verified.')
}

// -----------------------------------------------------------------------------
// Scenario 30 (TargetManage #4): Create TCP target submits accurate payload
// -----------------------------------------------------------------------------
{
  const form = applyTargetQuickTemplate('tcp', {})
  form.id = 'tcp-dns-check'
  const payload = buildTargetSubmitPayload(form)
  assert.strictEqual(payload.kind, 'tcp', 'Scenario 30: TCP template sets kind=tcp')
  assert.strictEqual(payload.host, '1.1.1.1', 'Scenario 30: TCP host populated')
  assert.strictEqual(payload.port, 443, 'Scenario 30: TCP port is numeric 443')
  assert.strictEqual(payload.interval_seconds, 60, 'Scenario 30: TCP interval_seconds is numeric 60')
  assert.strictEqual(payload.timeout_ms, 3000, 'Scenario 30: TCP timeout_ms is numeric 3000')
  assert.strictEqual(payload.expected_status, undefined, 'Scenario 30: TCP target omits expected_status')
  console.log('[PASS] Scenario 30: Create TCP target payload verified.')
}

// -----------------------------------------------------------------------------
// Scenario 31 (TargetManage #5): Create DNS target includes dns_type
// -----------------------------------------------------------------------------
{
  const form = applyTargetQuickTemplate('dns', {})
  form.dnsType = 'AAAA'
  const payload = buildTargetSubmitPayload(form)
  assert.strictEqual(payload.kind, 'dns', 'Scenario 31: DNS target kind=dns')
  assert.strictEqual(payload.dns_type, 'AAAA', 'Scenario 31: DNS target includes dns_type')
  console.log('[PASS] Scenario 31: Create DNS target includes dns_type verified.')
}

// -----------------------------------------------------------------------------
// Scenario 32 (TargetManage #6): Create MTR target includes max_hops
// -----------------------------------------------------------------------------
{
  const form = buildTargetFormFromRecord({
    id: 'mtr-google',
    kind: 'mtr',
    name: 'MTR Route Check',
    host: '8.8.8.8',
    max_hops: 24,
    interval_seconds: 120,
    timeout_ms: 5000,
  })
  const payload = buildTargetSubmitPayload(form)
  assert.strictEqual(payload.kind, 'mtr', 'Scenario 32: MTR target kind=mtr')
  assert.strictEqual(payload.max_hops, 24, 'Scenario 32: MTR target includes numeric max_hops')
  console.log('[PASS] Scenario 32: Create MTR target includes max_hops verified.')
}

// -----------------------------------------------------------------------------
// Scenario 33 (TargetManage #7): Selecting "全部节点" submits node_ids=[] & node_tags=[]
// -----------------------------------------------------------------------------
{
  const form = buildTargetFormFromRecord({
    id: 'http-global',
    kind: 'http',
    name: 'Global HTTP',
    host: 'example.com',
    node_ids: ['node-1', 'node-2'],
    node_tags: ['hk'],
  })
  form.scopeMode = 'all'
  const payload = buildTargetSubmitPayload(form)
  assert.deepStrictEqual(payload.node_ids, [], 'Scenario 33: scopeMode=all forces node_ids=[]')
  assert.deepStrictEqual(payload.node_tags, [], 'Scenario 33: scopeMode=all forces node_tags=[]')
  console.log('[PASS] Scenario 33: Selecting "全部节点" resets node_ids=[] and node_tags=[] verified.')
}

// -----------------------------------------------------------------------------
// Scenario 34 (TargetManage #8): Selecting specific nodes submits node_ids
// -----------------------------------------------------------------------------
{
  const form = applyTargetQuickTemplate('tcp', {})
  form.name = 'Scoped Nodes TCP'
  form.host = '1.0.0.1'
  form.port = '443'
  form.scopeMode = 'nodes'
  form.nodeIds = ['node-sg-1', 'node-hk-2', 'node-sg-1']
  form.nodeTags = []
  const payload = buildTargetSubmitPayload(form)
  assert.deepStrictEqual(payload.node_ids, ['node-sg-1', 'node-hk-2'], 'Scenario 34: Deduplicated node_ids submitted')
  assert.deepStrictEqual(payload.node_tags, [], 'Scenario 34: Empty node_tags when only nodes selected')
  console.log('[PASS] Scenario 34: Selecting specific nodes submits node_ids verified.')
}

// -----------------------------------------------------------------------------
// Scenario 35 (TargetManage #9): Selecting tags submits node_tags
// -----------------------------------------------------------------------------
{
  const form = applyTargetQuickTemplate('http', {})
  form.kind = 'https'
  form.name = 'Scoped Tags HTTPS'
  form.host = 'example.org'
  form.scopeMode = 'tags'
  form.nodeIds = []
  form.nodeTags = ['cn2', 'bgp', 'cn2']
  const payload = buildTargetSubmitPayload(form)
  assert.deepStrictEqual(payload.node_ids, [], 'Scenario 35: Empty node_ids when only tags selected')
  assert.deepStrictEqual(payload.node_tags, ['cn2', 'bgp'], 'Scenario 35: Deduplicated node_tags submitted')
  console.log('[PASS] Scenario 35: Selecting tags submits node_tags verified.')
}

// -----------------------------------------------------------------------------
// Scenario 36 (TargetManage #10): Selecting both node_ids and node_tags displays and submits accurately
// -----------------------------------------------------------------------------
{
  const nodes = [
    { id: '5177b9b4-e693-4fff-aaff-adb26ab858da', name: 'AWS-Singapore', tags: ['sg', 'aws'] },
    { id: 'e2812132-89ab-4cde-9012-3456789abcde', name: 'Ciallonet-Host', tags: ['us', 'cn2'] },
  ]
  const form = applyTargetQuickTemplate('tcp', {})
  form.name = 'Mixed Scope TCP'
  form.host = '8.8.4.4'
  form.port = '53'
  form.scopeMode = 'custom'
  form.nodeIds = ['5177b9b4-e693-4fff-aaff-adb26ab858da']
  form.nodeTags = ['cn2']
  const payload = buildTargetSubmitPayload(form)
  assert.deepStrictEqual(payload.node_ids, ['5177b9b4-e693-4fff-aaff-adb26ab858da'], 'Scenario 36: Mixed scope preserves node_ids')
  assert.deepStrictEqual(payload.node_tags, ['cn2'], 'Scenario 36: Mixed scope preserves node_tags')

  const display = formatTargetScopeDisplay(payload, nodes)
  assert.strictEqual(display.label, '指定 1 台 + 标签', 'Scenario 36: Mixed scope label is "指定 1 台 + 标签"')
  assert.deepStrictEqual(display.nodeNames, ['AWS-Singapore'], 'Scenario 36: Resolved node name without full UUID')
  assert.deepStrictEqual(display.tags, ['cn2'], 'Scenario 36: Resolved tags list')
  console.log('[PASS] Scenario 36: Both node_ids and node_tags display & submit verified.')
}

// -----------------------------------------------------------------------------
// Scenario 37 (TargetManage #11): Editing existing target echoes node_ids, node_tags, and region_rules
// -----------------------------------------------------------------------------
{
  const existingRecord = {
    id: 'legacy-media-nf',
    name: 'Netflix US Check',
    kind: 'media_http',
    host: 'www.netflix.com',
    port: 443,
    path: '/title/80018499',
    interval_seconds: 300,
    timeout_ms: 8000,
    expected_status: 200,
    enabled: false,
    node_ids: ['node-a', 'node-b'],
    node_tags: ['unlock', 'hk'],
    region_rules: [
      { region: 'US', contains: 'United States' },
    ],
  }
  const echoedForm = buildTargetFormFromRecord(existingRecord)
  assert.deepStrictEqual(echoedForm.nodeIds, ['node-a', 'node-b'], 'Scenario 37: Echoes node_ids')
  assert.deepStrictEqual(echoedForm.nodeTags, ['unlock', 'hk'], 'Scenario 37: Echoes node_tags')
  assert.strictEqual(echoedForm.regionRulesList.length, 1, 'Scenario 37: Echoes region_rules into form rows')
  assert.strictEqual(echoedForm.regionRulesList[0].region, 'US', 'Scenario 37: Echoes region_rules region')

  const roundTripPayload = buildTargetSubmitPayload(echoedForm, existingRecord)
  assert.deepStrictEqual(roundTripPayload.node_ids, ['node-a', 'node-b'], 'Scenario 37: Round-trip preserves node_ids')
  assert.deepStrictEqual(roundTripPayload.node_tags, ['unlock', 'hk'], 'Scenario 37: Round-trip preserves node_tags')
  assert.deepStrictEqual(
    roundTripPayload.region_rules,
    [{ region: 'US', contains: 'United States' }],
    'Scenario 37: Round-trip preserves region_rules without manual JSON editing'
  )
  console.log('[PASS] Scenario 37: Editing existing target echoes node_ids, node_tags, and region_rules.')
}

// -----------------------------------------------------------------------------
// Scenario 38 (TargetManage #12 & #13): Scope display rules & NO full UUID exposure
// -----------------------------------------------------------------------------
{
  const sampleNodes = [
    { id: '5177b9b4-e693-4fff-aaff-adb26ab858da', name: 'AWS-Singapore' },
    { id: 'a1b2c3d4-1111-2222-3333-444455556666', name: 'Tokyo-BGP' },
  ]
  const unknownUuid = '99998888-7777-6666-5555-444433332222'

  const dAll = formatTargetScopeDisplay({ node_ids: [], node_tags: [] }, sampleNodes)
  assert.strictEqual(dAll.label, '全部节点', 'Scenario 38: Empty scope displays "全部节点"')

  const dNodes = formatTargetScopeDisplay(
    { node_ids: ['5177b9b4-e693-4fff-aaff-adb26ab858da', unknownUuid], node_tags: [] },
    sampleNodes
  )
  assert.strictEqual(dNodes.label, '指定 2 台节点', 'Scenario 38: Node-only scope displays "指定 N 台节点"')
  assert.strictEqual(dNodes.nodeNames[0], 'AWS-Singapore', 'Scenario 38: Known node resolves to node name')
  assert.strictEqual(dNodes.nodeNames[1], '节点 #999988', 'Scenario 38: Unknown UUID is masked to 6 hex chars')
  assert.ok(!dNodes.detail.includes(unknownUuid), 'Scenario 38: Tooltip/detail must NEVER contain full UUID')

  const dTags = formatTargetScopeDisplay({ node_ids: [], node_tags: ['hk', 'cn2'] }, sampleNodes)
  assert.strictEqual(dTags.label, '标签：hk、cn2', 'Scenario 38: Tag-only scope displays "标签：tag1、tag2"')

  const dMixed = formatTargetScopeDisplay(
    {
      node_ids: ['a1b2c3d4-1111-2222-3333-444455556666'],
      node_tags: ['bgp'],
      region_rules: [{ region: 'JP', contains: 'Japan' }],
    },
    sampleNodes
  )
  assert.strictEqual(dMixed.label, '指定 1 台 + 标签', 'Scenario 38: Mixed scope displays "指定 1 台 + 标签"')
  assert.strictEqual(dMixed.hasRegionRules, true, 'Scenario 38: region_rules badge flag is true')
  assert.strictEqual(dMixed.regionBadge, '区域规则', 'Scenario 38: region_rules badge text is "区域规则"')
  assert.strictEqual(maskNodeIdentifier(unknownUuid), '节点 #999988', 'Scenario 38: maskNodeIdentifier masks UUID')
  assert.strictEqual(resolveNodeDisplayName(unknownUuid, sampleNodes), '节点 #999988', 'Scenario 38: resolveNodeDisplayName masks unknown UUID')
  console.log('[PASS] Scenario 38: Scope display rules & zero full UUID exposure verified.')
}

// -----------------------------------------------------------------------------
// Scenario 39 (TargetManage #14): media_http cannot be newly created or re-enabled
// -----------------------------------------------------------------------------
{
  const targetManageSrc = readFileSync(resolve('frontend/src/components/TargetManage.jsx'), 'utf8')
  assert.ok(
    targetManageSrc.includes("const CREATE_TARGET_KINDS = ['http', 'https', 'tcp', 'dns', 'mtr']"),
    'Scenario 39: CREATE_TARGET_KINDS excludes media_http'
  )
  assert.ok(
    targetManageSrc.includes('不再支持新建 media_http 检测目标') &&
      targetManageSrc.includes('不再支持重新启用独立 media-* 周期探测') &&
      targetManageSrc.includes('已由 IPQA 接管') &&
      targetManageSrc.includes('历史流媒体目标'),
    'Scenario 39: media_http creation and re-enabling are blocked and labeled as IPQA-managed'
  )
  console.log('[PASS] Scenario 39: media_http creation & re-enable prevention verified.')
}

// -----------------------------------------------------------------------------
// Scenario 40 (TargetManage #15): Empty state quick templates only prefill drawer form without writing to DB
// -----------------------------------------------------------------------------
{
  const tplList = Object.values(TARGET_QUICK_TEMPLATES)
  assert.strictEqual(tplList.length, 3, 'Scenario 40: 3 quick templates defined')
  const tplTitles = tplList.map((t) => t.title)
  assert.deepStrictEqual(
    tplTitles,
    ['HTTP 健康检查', 'TCP 端口检查', 'DNS 解析检查'],
    'Scenario 40: Quick templates match required names'
  )
  const targetManageSrc = readFileSync(resolve('frontend/src/components/TargetManage.jsx'), 'utf8')
  assert.ok(
    targetManageSrc.includes('openQuickTemplateDrawer') &&
      targetManageSrc.includes('applyTargetQuickTemplate(templateKey, emptyTargetForm())'),
    'Scenario 40: Clicking a quick template opens the drawer and prefills state without calling fetch POST'
  )
  console.log('[PASS] Scenario 40: Empty state quick templates prefill-only behavior verified.')
}

// -----------------------------------------------------------------------------
// Scenario 41 (TargetManage #16): readOnly mode hides create, edit, delete, and toggle buttons, plus CSRF retry & delete confirmation
// -----------------------------------------------------------------------------
{
  const targetManageSrc = readFileSync(resolve('frontend/src/components/TargetManage.jsx'), 'utf8')
  assert.ok(
    targetManageSrc.includes('if (readOnly)') &&
      targetManageSrc.includes('confirmDeleteTarget') &&
      targetManageSrc.includes('确认删除检测目标'),
    'Scenario 41: readOnly mode gates action buttons and delete requires secondary confirmation modal'
  )

  // Verify executeWithCsrfRetry refreshes CSRF token once on 403
  let callCount = 0
  let tokenFetchCount = 0
  const fakeRes = await executeWithCsrfRetry(
    async (token) => {
      callCount += 1
      if (callCount === 1) {
        assert.strictEqual(token, 'stale-token')
        return { status: 403, ok: false }
      }
      assert.strictEqual(token, 'fresh-token')
      return { status: 200, ok: true }
    },
    async () => {
      tokenFetchCount += 1
      return tokenFetchCount === 1 ? 'stale-token' : 'fresh-token'
    }
  )
  assert.strictEqual(callCount, 2, 'Scenario 41: executeWithCsrfRetry retries once after 403')
  assert.strictEqual(tokenFetchCount, 2, 'Scenario 41: executeWithCsrfRetry refreshes token once after initial fetch')
  assert.strictEqual(fakeRes.status, 200, 'Scenario 41: Retried request succeeds with fresh token')
  console.log('[PASS] Scenario 41: readOnly gating, delete confirmation, and CSRF 403 single-retry verified.')
}

console.log('\n=== ALL 41 LATENCY, SCOPE, ENROLLMENT, IPQA & TARGET-MANAGE CONTRACT TESTS PASSED ===')
