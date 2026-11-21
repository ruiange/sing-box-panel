import { useSyncExternalStore } from 'react'

export const pageNames = {
  overview: '运行概览',
  devices: '设备管理',
  traffic: '流量统计',
  analysis: '流量分析',
  access: '网站设置',
}

export type View = keyof typeof pageNames
type PanelMode = 'desktop' | 'mobile'

function isView(value: string): value is View {
  return Object.hasOwn(pageNames, value)
}

function parseLocation({ pathname, search, hash }: Pick<Location, 'pathname' | 'search' | 'hash'>) {
  const path = pathname.replace(/\/+$/, '')
  const mobile = path === '/m' || path.startsWith('/m/')
  const pathView = path.slice(mobile ? 3 : 1)
  const hashRoute = hash.replace(/^#\/?/, '')
  const hashView = hashRoute.split('?')[0]
  const params = new URLSearchParams(search)
  if (isView(pathView)) return { view: pathView, params, mobile, hasPathView: true }
  if (!isView(hashView)) return { view: 'overview' as const, params, mobile, hasPathView: false }
  const hashQuery = hashRoute.slice(hashView.length)
  for (const [key, value] of new URLSearchParams(hashQuery)) params.set(key, value)
  return { view: hashView, params, mobile, hasPathView: false }
}

function panelURL(mode: PanelMode, view: View, params: URLSearchParams) {
  const query = params.toString()
  return `${mode === 'mobile' ? '/m/' : '/#'}${view}${query ? '?' + query : ''}`
}

function prefersMobile() {
  const { userAgent, maxTouchPoints, platform } = navigator
  return /Android|iPhone|iPad|iPod|IEMobile|Opera Mini|Mobile/i.test(userAgent) ||
    (platform === 'MacIntel' && maxTouchPoints > 1) ||
    window.matchMedia('(max-width: 767px)').matches
}

export function initializePanelLocation(): PanelMode {
  const { view, params, mobile, hasPathView } = parseLocation(window.location)
  const mode = mobile || prefersMobile() ? 'mobile' : 'desktop'
  if (mode === 'mobile') {
    const atHome = !hasPathView && !window.location.hash && view === 'overview'
    window.history.replaceState(window.history.state, '', atHome ? '/m' + window.location.search : panelURL(mode, view, params))
  } else if (hasPathView) {
    window.history.replaceState(window.history.state, '', panelURL(mode, view, params))
  }
  return mode
}

function readURL() {
  return window.location.href
}

function subscribe(notify: () => void) {
  window.addEventListener('hashchange', notify)
  window.addEventListener('popstate', notify)
  return () => {
    window.removeEventListener('hashchange', notify)
    window.removeEventListener('popstate', notify)
  }
}

export function readPanelLocation(url = window.location.href) {
  const { view, params } = parseLocation(new URL(url))
  return { view, scope: view === 'traffic' ? params.get('deviceId') ?? '' : '' }
}

export function usePanelLocation() {
  return readPanelLocation(useSyncExternalStore(subscribe, readURL))
}

export function navigatePanel(mode: PanelMode, view: View, deviceId = '', replace = false) {
  const params = new URLSearchParams()
  if (view === 'traffic' && deviceId) params.set('deviceId', deviceId)
  const url = panelURL(mode, view, params)
  if (new URL(url, window.location.href).href === window.location.href) return
  if (replace) window.history.replaceState(window.history.state, '', url)
  else window.history.pushState(window.history.state, '', url)
  window.dispatchEvent(new PopStateEvent('popstate', { state: window.history.state }))
}
