import { useEffect, useRef, useState } from 'react'
import { LivePollingController } from './LivePollingController.js'

// Keeps live pages responsive without polling hidden tabs or waiting for the
// next interval after the browser reconnects to the network.
// Status transitions and lifecycle are managed by LivePollingController.
export function useLivePolling(load, { interval = 15000, enabled = true, restartKey = '', onStatusChange } = {}) {
  const [status, setStatus] = useState(enabled ? 'connecting' : 'paused')
  const controllerRef = useRef(null)

  useEffect(() => {
    const controller = new LivePollingController(load, {
      interval,
      enabled,
      onStatusChange: (nextStatus) => {
        setStatus(nextStatus)
        if (onStatusChange) {
          onStatusChange(nextStatus)
        }
      },
    })
    controllerRef.current = controller
    controller.start()

    return () => {
      controller.dispose()
    }
  }, [interval, enabled, restartKey])

  useEffect(() => {
    if (controllerRef.current) {
      controllerRef.current.updateConfig({ load, onStatusChange })
    }
  }, [load, onStatusChange])

  return status
}
