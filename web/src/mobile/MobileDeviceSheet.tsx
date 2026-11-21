import { Button, Popup } from 'antd-mobile'
import { useEffect, useId, useRef, type KeyboardEvent, type ReactNode } from 'react'

interface MobileDeviceSheetProps {
  title: string
  description?: string
  locked?: boolean
  onClose: () => void
  children: ReactNode
}

export default function MobileDeviceSheet({ title, description, locked, onClose, children }: MobileDeviceSheetProps) {
  const titleId = useId()
  const descriptionId = useId()
  const heading = useRef<HTMLHeadingElement>(null)

  useEffect(() => {
    const previous = document.activeElement
    heading.current?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [])

  function handleKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key === 'Escape') {
      event.preventDefault()
      if (!locked) onClose()
    }
    if (event.key !== 'Tab') return
    const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), a[href], [tabindex="0"]',
    )).filter((control) => control.tabIndex >= 0 && control.getClientRects().length > 0)
    const first = controls[0]
    const last = controls.at(-1)
    if (!first || !last) {
      event.preventDefault()
      return
    }
    const active = document.activeElement
    if (event.shiftKey ? active === first || active === heading.current : active === last) {
      event.preventDefault()
      const next = event.shiftKey ? last : first
      next.focus()
    }
  }

  return (
    <Popup visible position="bottom" bodyClassName="mobile-device-sheet">
      <section
        className="mobile-device-sheet-layout"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        onKeyDown={handleKeyDown}
      >
        <header className="mobile-device-sheet-header">
          <div>
            <h2 id={titleId} ref={heading} tabIndex={-1}>{title}</h2>
            {description && <p id={descriptionId}>{description}</p>}
          </div>
          <Button fill="none" color="primary" disabled={locked} onClick={onClose}>关闭</Button>
        </header>
        <div className="mobile-device-sheet-content">{children}</div>
      </section>
    </Popup>
  )
}
