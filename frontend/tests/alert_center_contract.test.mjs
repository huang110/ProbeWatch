import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const alertCenterViewPath = path.resolve(__dirname, '../src/components/AlertCenterView.jsx')
const alertCenterSource = fs.readFileSync(alertCenterViewPath, 'utf8')

test('Stage 8 Alert Center Contract: Removal of synthetic mocks and fake toasts', () => {
  // 1. Ensure DEFAULT_TARGET_CONFIGS is completely eliminated
  assert.equal(
    alertCenterSource.includes('DEFAULT_TARGET_CONFIGS'),
    false,
    'DEFAULT_TARGET_CONFIGS mock array must be completely removed'
  )

  // 2. Ensure fake batch toasts are gone
  assert.equal(
    alertCenterSource.includes('已批量设置'),
    false,
    'Fake offline batch toast must be removed'
  )
  assert.equal(
    alertCenterSource.includes('已成功触发即时日报推送'),
    false,
    'Fake instant traffic report toast must be removed'
  )
  assert.equal(
    alertCenterSource.includes('已批量配置'),
    false,
    'Fake latency matrix batch toast must be removed'
  )
  assert.equal(
    alertCenterSource.includes('夜间免打扰时段已更新'),
    false,
    'Fake quiet hours switch toast must be removed'
  )
  assert.equal(
    alertCenterSource.includes('到期预警已启用'),
    false,
    'Fake expiry warning switch toast must be removed'
  )
})

test('Stage 8 Alert Center Contract: Real backend settings integration for offline grace period', () => {
  // Verify offline grace period calls saveAlertSettings with offline_grace_seconds
  assert.ok(
    alertCenterSource.includes("saveAlertSettings({ offline_grace_seconds: String(offlineGracePeriod) })") ||
    alertCenterSource.includes("saveAlertSettings({ offline_grace_seconds:"),
    'Must persist offline grace period via authoritative saveAlertSettings backend API'
  )
})

test('Stage 8 Alert Center Contract: Honest notices for unintegrated features', () => {
  // Traffic report notice
  assert.ok(
    alertCenterSource.includes('流量定时报告功能尚未接入'),
    'Must display honest pending notice for traffic reports'
  )

  // Latency alert integration notice
  assert.ok(
    alertCenterSource.includes('独立延迟矩阵已整合至告警规则中心'),
    'Must display integration notice directing users to multi-metric rules engine'
  )

  // General tab pending notice
  assert.ok(
    alertCenterSource.includes('定时免打扰守护尚未接入'),
    'Must display honest pending notice for quiet hours / auto-expiry daemon'
  )
})

test('Stage 8 Alert Center Contract: Real backend CRUD operations wired up', () => {
  // Notification channels
  assert.ok(alertCenterSource.includes('fetchNotificationChannels'), 'Must call fetchNotificationChannels')
  assert.ok(alertCenterSource.includes('createNotificationChannel'), 'Must call createNotificationChannel')
  assert.ok(alertCenterSource.includes('deleteNotificationChannel'), 'Must call deleteNotificationChannel')
  assert.ok(alertCenterSource.includes('testNotificationDirect'), 'Must call testNotificationDirect')

  // Rules and Silences
  assert.ok(alertCenterSource.includes('fetchAlertRules'), 'Must call fetchAlertRules')
  assert.ok(alertCenterSource.includes('createAlertRule'), 'Must call createAlertRule')
  assert.ok(alertCenterSource.includes('deleteAlertRule'), 'Must call deleteAlertRule')
  assert.ok(alertCenterSource.includes('fetchAlertSilences'), 'Must call fetchAlertSilences')
  assert.ok(alertCenterSource.includes('createAlertSilence'), 'Must call createAlertSilence')
  assert.ok(alertCenterSource.includes('deleteAlertSilence'), 'Must call deleteAlertSilence')
  assert.ok(alertCenterSource.includes('fetchFlappingAlerts'), 'Must call fetchFlappingAlerts')
})
