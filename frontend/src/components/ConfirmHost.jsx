import { useEffect, useState } from 'react'
import { WarningOctagon } from '@phosphor-icons/react'

export function ConfirmHost() {
  const [request, setRequest] = useState(null)

  useEffect(() => {
    window.probewatchConfirm = (message, options = {}) => new Promise((resolve) => {
      setRequest({ message, title: options.title || '确认操作', confirmLabel: options.confirmLabel || '确认', resolve })
    })
    return () => { delete window.probewatchConfirm }
  }, [])

  useEffect(() => {
    if (!request) return undefined
    const onKeyDown = (event) => {
      if (event.key === 'Escape') {
        request.resolve(false)
        setRequest(null)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [request])

  if (!request) return null
  const finish = (result) => {
    request.resolve(result)
    setRequest(null)
  }
  return (
    <div className="modal-backdrop confirm-modal-backdrop" role="presentation" onClick={() => finish(false)}>
      <div className="modal-card confirm-modal-card" role="dialog" aria-modal="true" aria-labelledby="confirm-modal-title" onClick={(event) => event.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title-wrap">
            <span className="modal-icon-badge text-amber"><WarningOctagon size={20} weight="duotone" /></span>
            <div><h3 id="confirm-modal-title">{request.title}</h3><p className="modal-subtitle">此操作可能会影响现有数据或连接。</p></div>
          </div>
        </div>
        <div className="modal-body"><p>{request.message}</p></div>
        <div className="modal-actions">
          <button type="button" className="button button-quiet" onClick={() => finish(false)}>取消</button>
          <button type="button" className="button button-danger" autoFocus onClick={() => finish(true)}>{request.confirmLabel}</button>
        </div>
      </div>
    </div>
  )
}
