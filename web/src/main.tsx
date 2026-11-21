import { lazy, StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { initializePanelLocation } from './panelRoutes'

const mode = initializePanelLocation()
document.documentElement.dataset.panelMode = mode

const Panel = mode === 'mobile'
  ? lazy(() => import('./mobile/MobilePanel'))
  : lazy(() => import('./DesktopPanel'))

createRoot(document.getElementById('app')!).render(
  <StrictMode>
    <Suspense fallback={<p role="status" style={{ font: '16px system-ui', padding: 16 }}>正在打开面板…</p>}>
      <Panel />
    </Suspense>
  </StrictMode>,
)
