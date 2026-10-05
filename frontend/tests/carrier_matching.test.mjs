import { identifyTargetCarrier } from '../src/lib/format.js'

export function runCarrierMatchingTests() {
  const cases = [
    // 基础要求用例 (用户核心要求覆盖项)
    ['route-ct-cn2', '电信'],
    ['route-cu-9929', '联通'],
    ['route-cm-cmin2', '移动'],
    ['未知目标', '未知'],

    // 对象形式 target_id
    [{ target_id: 'route-ct-cn2' }, '电信'],
    [{ target_id: 'route-cu-9929' }, '联通'],
    [{ target_id: 'route-cm-cmin2' }, '移动'],
    [{ target_id: 'unknown-route' }, '未知'],

    // 显式 isp / operator / carrier 优先
    [{ isp: 'ct', name: 'custom-node' }, '电信'],
    [{ operator: 'China Unicom', host: '1.2.3.4' }, '联通'],
    [{ carrier: 'mobile', host: '1.2.3.4' }, '移动'],
    [{ isp: 'cn2' }, '电信'],
    [{ isp: '9929' }, '联通'],
    [{ isp: 'cmin2' }, '移动'],

    // 中文与常见骨干网命名
    [{ name: '上海电信 CN2 GIA' }, '电信'],
    [{ name: '北京联通 9929' }, '联通'],
    [{ name: '广州移动 CMIN2' }, '移动'],

    // 边界与误报防护 (不应误匹配含 ct/cu/cm 的普通英文单词)
    ['custom', '未知'],
    ['action', '未知'],
    ['document', '未知'],
    ['route-bgp-lax', '未知'],
    ['', '未知'],
    [null, '未知'],
    [undefined, '未知'],
  ]

  let passed = 0
  for (const [input, expected] of cases) {
    const actual = identifyTargetCarrier(input)
    if (actual !== expected) {
      throw new Error(`Carrier matching failed for input ${JSON.stringify(input)}: expected '${expected}', got '${actual}'`)
    }
    passed++
  }

  console.log(`[PASS] Carrier matching tests passed: ${passed} assertions verified.`)
  return passed
}

runCarrierMatchingTests()
