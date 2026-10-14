// ProbeWatch - Telemetry Block Color Indicators (Latency & Packet Loss Dynamic Mapping)

export const TELEMETRY_COLORS = {
  EXCELLENT: '#34d399', // Green / 优秀 (<65ms, 0% 丢包)
  FAIR: '#fbbf24',      // Amber / 良好 (65-130ms, <=5% 轻微丢包)
  POOR: '#fb7185',      // Rose / 一般 (130-220ms, 5-20% 中等丢包)
  CRITICAL: '#f43f5e',  // Red / 较差 (>=220ms, >20% 严重丢包或探测超时)
  MISSING: '#64748b',   // Gray / 缺失 (null, undefined, 非法数值)
}

export function mapLatencyToColor(latencyMs, status) {
  if (status === 'failed' || status === 'timeout' || status === 'down') {
    return TELEMETRY_COLORS.CRITICAL
  }
  if (latencyMs === null || latencyMs === undefined || latencyMs === '') {
    return TELEMETRY_COLORS.MISSING
  }
  const val = Number(latencyMs)
  if (!Number.isFinite(val) || val < 0) {
    return TELEMETRY_COLORS.MISSING
  }
  if (val < 65) return TELEMETRY_COLORS.EXCELLENT
  if (val < 130) return TELEMETRY_COLORS.FAIR
  if (val < 220) return TELEMETRY_COLORS.POOR
  return TELEMETRY_COLORS.CRITICAL
}

export function getLatencyBlocks(historyOrSamples) {
  if (historyOrSamples === null || historyOrSamples === undefined || historyOrSamples === '') {
    return []
  }
  if (Array.isArray(historyOrSamples)) {
    if (historyOrSamples.length === 0) return []
    return historyOrSamples.map((item) => {
      if (item === null || item === undefined) return TELEMETRY_COLORS.MISSING
      if (typeof item === 'number' || typeof item === 'string') {
        return mapLatencyToColor(item)
      }
      const lat = item.latency_ms ?? item.latency ?? item.result?.latency_ms ?? item.result?.latency
      const status = item.status ?? item.result?.status
      return mapLatencyToColor(lat, status)
    })
  }
  if (typeof historyOrSamples === 'object') {
    if (Array.isArray(historyOrSamples.history)) return getLatencyBlocks(historyOrSamples.history)
    if (Array.isArray(historyOrSamples.samples)) return getLatencyBlocks(historyOrSamples.samples)
    const lat = historyOrSamples.latency_ms ?? historyOrSamples.latency ?? historyOrSamples.result?.latency_ms ?? historyOrSamples.result?.latency
    const status = historyOrSamples.status ?? historyOrSamples.result?.status
    return [mapLatencyToColor(lat, status)]
  }
  const color = mapLatencyToColor(historyOrSamples)
  return color === TELEMETRY_COLORS.MISSING ? [] : [color]
}

export function mapLossToColor(lossPercent, status) {
  if (status === 'failed' || status === 'timeout' || status === 'down') {
    return TELEMETRY_COLORS.CRITICAL
  }
  if (lossPercent === null || lossPercent === undefined || lossPercent === '') {
    return TELEMETRY_COLORS.MISSING
  }
  const val = Number(lossPercent)
  if (!Number.isFinite(val) || val < 0) {
    return TELEMETRY_COLORS.MISSING
  }
  if (val <= 0) return TELEMETRY_COLORS.EXCELLENT
  if (val <= 5) return TELEMETRY_COLORS.FAIR
  if (val <= 20) return TELEMETRY_COLORS.POOR
  return TELEMETRY_COLORS.CRITICAL
}

export function getLossBlocks(historyOrSamples) {
  if (historyOrSamples === null || historyOrSamples === undefined || historyOrSamples === '') {
    return []
  }
  if (Array.isArray(historyOrSamples)) {
    if (historyOrSamples.length === 0) return []
    return historyOrSamples.map((item) => {
      if (item === null || item === undefined) return TELEMETRY_COLORS.MISSING
      if (typeof item === 'number' || typeof item === 'string') {
        return mapLossToColor(item)
      }
      const loss = item.loss_percent ?? item.loss_rate ?? item.loss ?? item.packet_loss ?? item.result?.packet_loss
      const status = item.status ?? item.result?.status
      return mapLossToColor(loss, status)
    })
  }
  if (typeof historyOrSamples === 'object') {
    if (Array.isArray(historyOrSamples.history)) return getLossBlocks(historyOrSamples.history)
    if (Array.isArray(historyOrSamples.samples)) return getLossBlocks(historyOrSamples.samples)
    const loss = historyOrSamples.loss_percent ?? historyOrSamples.loss_rate ?? historyOrSamples.loss ?? historyOrSamples.packet_loss ?? historyOrSamples.result?.packet_loss
    const status = historyOrSamples.status ?? historyOrSamples.result?.status
    return [mapLossToColor(loss, status)]
  }
  const color = mapLossToColor(historyOrSamples)
  return color === TELEMETRY_COLORS.MISSING ? [] : [color]
}
