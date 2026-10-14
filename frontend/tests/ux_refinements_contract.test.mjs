import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const containerViewPath = path.resolve(__dirname, '../src/components/ContainerProcessView.jsx')
const containerViewSource = fs.readFileSync(containerViewPath, 'utf8')

const alertCenterViewPath = path.resolve(__dirname, '../src/components/AlertCenterView.jsx')
const alertCenterSource = fs.readFileSync(alertCenterViewPath, 'utf8')

test('Stage 4 UX Contract: ContainerProcessView fleet process empty state', () => {
  // 1. Must contain clear guidance when no node is selected in process tab
  assert.ok(
    containerViewSource.includes('请在右上角选择具体服务器节点以查看宿主机 Top 进程'),
    'Must display clear node selection guidance prompt in fleet process view'
  )

  // 2. Empty state must be guarded by !selectedNode
  assert.ok(
    containerViewSource.includes('!selectedNode ?'),
    'Must explicitly branch on !selectedNode for host processes'
  )

  // 3. Process sorting controls must only be visible when a specific node is selected
  assert.ok(
    containerViewSource.includes("activeTab === 'processes' && selectedNode"),
    'Process sorting controls should be scoped to when a node is selected'
  )
})

test('Stage 4 UX Contract: AlertCenter notification toggle persistence / honest per-channel control', () => {
  // 1. Misleading local-only channelEnabled state must be completely removed
  assert.equal(
    alertCenterSource.includes('channelEnabled'),
    false,
    'Misleading fake master toggle state channelEnabled must be eliminated'
  )

  // 2. Clear status explanation must be displayed informing users that channels are independently managed
  assert.ok(
    alertCenterSource.includes('通知分发状态说明'),
    'Must display notification channel status explanation'
  )
  assert.ok(
    alertCenterSource.includes('ProbeWatch 采用多通道独立控制机制'),
    'Must clarify that channels are independently controlled'
  )

  // 3. Global test action must be available
  assert.ok(
    alertCenterSource.includes('handleGlobalTest'),
    'Must provide global test notification trigger'
  )

  // 4. Per-channel toggle and test must remain active
  assert.ok(
    alertCenterSource.includes('handleToggleChannel'),
    'Per-channel toggle handler must remain functional'
  )
  assert.ok(
    alertCenterSource.includes('handleTestChannel'),
    'Per-channel test handler must remain functional'
  )
})
