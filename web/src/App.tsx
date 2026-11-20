import {
  ApiOutlined, DashboardOutlined, LineChartOutlined, LogoutOutlined, MobileOutlined, PieChartOutlined,
  PlusOutlined, ReloadOutlined, SettingOutlined,
} from '@ant-design/icons'
import { PageContainer, ProCard, ProLayout } from '@ant-design/pro-components'
import { Alert, App as AntApp, Badge, Button, Input, Select, Spin, Tooltip } from 'antd'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { api, APIError, errorMessage, type Device, type LiveRatePoint, type SiteSettings as SiteSettingsResponse } from './api'
import DeviceDialogs, { type DeviceDialog } from './components/DeviceDialogs'
import DeviceTable from './components/DeviceTable'
import TrafficPanels, { liveLabels, type HistoryRange } from './components/TrafficPanels'
import PanelLogin, { PanelBrand } from './pages/PanelLogin'
import SiteSettings from './pages/SiteSettings'
import { useLiveTraffic } from './useLiveTraffic'
import { usePanelData } from './usePanelData'

const TrafficAnalysis = lazy(() => import('./pages/TrafficAnalysis'))

const pages = {
  overview: { path: '/overview', name: '运行概览', icon: <DashboardOutlined /> },
  devices: { path: '/devices', name: '设备管理', icon: <MobileOutlined /> },
  traffic: { path: '/traffic', name: '流量统计', icon: <LineChartOutlined /> },
  analysis: { path: '/analysis', name: '流量分析', icon: <PieChartOutlined /> },
  access: { path: '/access', name: '网站设置', icon: <SettingOutlined /> },
}
const layoutRoute = {
  path: '/',
  routes: [...Object.values(pages), { path: '/docs', name: '接口文档', icon: <ApiOutlined /> }],
}
const statusOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'enabled', label: '已启用' },
  { value: 'disabled', label: '已停用' },
]
const defaultSiteTitle = document.title
const sessionExpiredMessage = '登录已过期，请重新登录。'
const emptyLiveHistory: LiveRatePoint[] = []

type View = keyof typeof pages
type DeviceFilter = 'all' | 'enabled' | 'disabled'

function subscribeToLocation(notify: () => void) {
  window.addEventListener('hashchange', notify)
  return () => window.removeEventListener('hashchange', notify)
}

function readHash() {
  return window.location.hash
}

function readLocation(hash: string): { view: View; scope: string } {
  const [view, search] = hash.slice(1).split('?')
  if (view === 'traffic') return { view, scope: new URLSearchParams(search).get('deviceId') ?? '' }
  if (view === 'devices' || view === 'analysis' || view === 'access') return { view, scope: '' }
  return { view: 'overview', scope: '' }
}

function showDeviceTraffic(deviceId: string) {
  window.location.hash = deviceId ? 'traffic?deviceId=' + encodeURIComponent(deviceId) : 'traffic'
}

export default function App() {
  const { message } = AntApp.useApp()
  const hash = useSyncExternalStore(subscribeToLocation, readHash)
  const { view, scope } = readLocation(hash)
  const [stage, setStage] = useState<'checking' | 'login' | 'ready'>('checking')
  const [loginError, setLoginError] = useState('')
  const [loggingOut, setLoggingOut] = useState(false)
  const [range, setRange] = useState<HistoryRange>('days=7')
  const [analysisRange, setAnalysisRange] = useState<HistoryRange>('days=7')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<DeviceFilter>('all')
  const [dialog, setDialog] = useState<DeviceDialog>()
  const [siteTitle, setSiteTitle] = useState<string>()
  const [titleLoading, setTitleLoading] = useState(true)
  const [titleError, setTitleError] = useState('')
  const titleRequest = useRef<AbortController>()
  const focusLiveHeading = useRef(false)

  const showLogin = useCallback((reason = '') => {
    setDialog(undefined)
    setLoginError(reason)
    setStage('login')
  }, [])
  const onExpired = useCallback(() => showLogin(sessionExpiredMessage), [showLogin])
  const onMissingDevice = useCallback((deviceId: string) => {
    if (readLocation(readHash()).scope !== deviceId) return
    window.history.replaceState(window.history.state, '', '#traffic')
    window.dispatchEvent(new Event('hashchange'))
    void message.info('所选设备已不存在，已切换到全部设备')
  }, [message])

  const ready = stage === 'ready'
  const requestedRange = view === 'analysis' ? analysisRange : range
  const { devices, loading, deviceError, traffic, scopedTraffic, refresh, updateDevice } = usePanelData(
    ready, scope, requestedRange, onExpired, onMissingDevice,
  )
  const live = useLiveTraffic(ready, scope, onExpired)
  const selectedDevice = devices.find((device) => device.id === scope)
  const visibleTraffic = scope ? (scopedTraffic?.deviceId === scope ? scopedTraffic : undefined) : traffic
  const chartData = visibleTraffic?.range === requestedRange ? visibleTraffic.traffic : undefined
  const analysisTrafficError = traffic?.errorRange === analysisRange ? traffic.error : undefined
  const activeCount = devices.filter((device) => device.enabled).length
  const currentHistory = live.snapshot && live.snapshot.deviceId === scope ? live.snapshot.history : emptyLiveHistory
  const rows = useMemo(() => {
    const usage = new Map(traffic?.traffic?.devices.map((entry) => [entry.id, entry]))
    return devices.map((device) => ({ ...device, usage: usage.get(device.id) }))
  }, [devices, traffic?.traffic])
  const visibleRows = useMemo(() => {
    if (view === 'overview') return rows.slice(0, 5)
    if (view !== 'devices') return rows
    const search = query.trim().toLocaleLowerCase()
    return rows.filter((device) => device.name.toLocaleLowerCase().includes(search) &&
      (filter === 'all' || device.enabled === (filter === 'enabled')))
  }, [view, rows, query, filter])

  const loadSiteTitle = useCallback(async () => {
    titleRequest.current?.abort()
    const request = new AbortController()
    titleRequest.current = request
    setTitleLoading(true)
    setTitleError('')
    try {
      const current = await api<SiteSettingsResponse>('/site', 'GET', undefined, request.signal)
      if (!request.signal.aborted) setSiteTitle(current.title)
    } catch (error) {
      if (!request.signal.aborted) setTitleError(errorMessage(error))
    } finally {
      if (!request.signal.aborted) setTitleLoading(false)
    }
  }, [])

  const applySiteTitle = useCallback((title: string) => {
    titleRequest.current?.abort()
    setSiteTitle(title)
    setTitleError('')
    setTitleLoading(false)
  }, [])

  useEffect(() => {
    const request = new AbortController()
    void loadSiteTitle()
    void api<{ authenticated: boolean }>('/auth/session', 'GET', undefined, request.signal).then(
      (session) => {
        if (request.signal.aborted) return
        if (session?.authenticated) setStage('ready')
        else showLogin()
      },
      (error) => {
        if (!request.signal.aborted) showLogin(errorMessage(error))
      },
    )
    return () => {
      request.abort()
      titleRequest.current?.abort()
    }
  }, [loadSiteTitle, showLogin])

  const displayTitle = siteTitle ?? defaultSiteTitle
  useEffect(() => { document.title = displayTitle }, [displayTitle])

  useEffect(() => {
    if (!scope || !focusLiveHeading.current) return
    focusLiveHeading.current = false
    document.getElementById('live-heading')?.focus()
  }, [scope])

  async function login(password: string) {
    setLoginError('')
    try {
      const result = await api<{ authenticated: boolean }>('/auth/login', 'POST', { password })
      if (!result.authenticated) throw new Error('密码验证失败。')
      setStage('ready')
      return true
    } catch (error) {
      setLoginError(errorMessage(error))
      return false
    }
  }

  async function logout() {
    if (loggingOut) return
    setLoggingOut(true)
    try {
      await api('/auth/logout', 'POST')
      showLogin()
    } catch (error) {
      if (error instanceof APIError && error.status === 401) showLogin()
      else void message.error(errorMessage(error))
    } finally {
      setLoggingOut(false)
    }
  }

  function openDeviceTraffic(device: Device) {
    if (scope === device.id) {
      document.getElementById('live-heading')?.focus()
      return
    }
    focusLiveHeading.current = true
    showDeviceTraffic(device.id)
  }

  function openCreate() {
    setDialog({ type: 'create' })
  }

  function deviceSaved(device: Device, notice: string) {
    updateDevice(device)
    void message.success(notice)
    void refresh()
  }

  if (!ready) {
    return <PanelLogin siteTitle={displayTitle} checking={stage === 'checking'} error={loginError} onLogin={login} />
  }

  const collectionFailed = Boolean(traffic?.error) || traffic?.traffic?.collectorStatus === 'error'
  const collecting = traffic?.traffic?.collectorStatus === 'ok' && !collectionFailed
  const collectionLabel = collectionFailed ? '采集异常' : collecting ? '采集正常' : '等待采集'
  const createButton = <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新增设备</Button>
  const renderHomeLink = () => (
    <a className="panel-home-link" href="#overview" aria-label={`${displayTitle} · 面板概览`}>
      <PanelBrand />
      <span className="panel-home-title" title={displayTitle}>{displayTitle}</span>
    </a>
  )
  const scopeOptions = [
    { value: '', label: '全部设备' },
    ...(scope && !selectedDevice ? [{ value: scope, label: '所选设备' }] : []),
    ...devices.map((device) => ({ value: device.id, label: device.name + (device.enabled ? '' : '（已停用）') })),
  ]
  const pageAction = view === 'traffic' ? (
    <label className="panel-scope">
      <span>统计设备</span>
      <Select aria-label="统计设备" value={scope} options={scopeOptions} onChange={showDeviceTraffic} />
    </label>
  ) : view === 'overview' || view === 'devices' ? createButton : undefined

  return (
    <ProLayout
      className="panel-layout"
      title={displayTitle}
      logo={<PanelBrand />}
      layout="mix"
      contentWidth="Fluid"
      fixedHeader
      fixSiderbar
      siderWidth={216}
      contentStyle={{ padding: 0 }}
      route={layoutRoute}
      location={{ pathname: pages[view].path }}
      pageTitleRender={false}
      breadcrumbRender={false}
      headerTitleRender={renderHomeLink}
      menuHeaderRender={(_logo, _title, props) => props && !props.isMobile ? null : renderHomeLink()}
      menu={{ locale: false }}
      token={{
        header: { colorBgHeader: '#ffffff' },
        sider: {
          colorMenuBackground: '#ffffff',
          colorBgMenuItemSelected: '#eaf2ff',
          colorTextMenuSelected: '#145abb',
        },
        pageContainer: { paddingBlockPageContainerContent: 12, paddingInlinePageContainerContent: 24 },
      }}
      menuItemRender={(item, dom) => item.path && (
        <a href={item.path === '/docs' ? '/docs' : '#' + item.path.slice(1)}
          aria-current={item.path === pages[view].path ? 'page' : undefined}>{dom}</a>
      )}
      actionsRender={() => [
        view !== 'access' && <span key="collection" className="panel-collection" title={collectionLabel} aria-label={collectionLabel}>
          <Badge status={collectionFailed ? 'error' : collecting ? 'success' : 'default'} text={collectionLabel} />
        </span>,
        view !== 'access' && <Tooltip key="refresh" title="刷新数据">
          <Button type="text" aria-label="刷新数据" icon={<ReloadOutlined />} loading={loading} onClick={() => void refresh()} />
        </Tooltip>,
        <Tooltip key="logout" title="退出登录">
          <Button type="text" aria-label="退出登录" icon={<LogoutOutlined />} loading={loggingOut} onClick={() => void logout()} />
        </Tooltip>,
      ]}
    >
      <PageContainer
        title={<h1 className="panel-page-title">{pages[view].name}</h1>}
        extra={pageAction}
      >
        {view === 'access' ? (
          <SiteSettings
            siteTitle={siteTitle} titleLoading={titleLoading} titleError={titleError}
            onLoadTitle={loadSiteTitle} onTitleSaved={applySiteTitle} onExpired={onExpired}
          />
        ) : view === 'analysis' ? (
          <Suspense fallback={(
            <div style={{ display: 'grid', placeItems: 'center', minHeight: 180 }} role="status">
              <Spin tip="正在加载流量分析…"><div style={{ height: 64 }} /></Spin>
            </div>
          )}>
            <TrafficAnalysis
              range={analysisRange}
              onRangeChange={setAnalysisRange}
              chartData={chartData}
              traffic={traffic?.traffic}
              devices={devices}
              presence={live.snapshot?.presence}
              liveState={live.state}
              loading={loading || (!chartData && !analysisTrafficError)}
              trafficError={analysisTrafficError}
              deviceError={deviceError}
            />
          </Suspense>
        ) : (
          <div className="panel-stack">
            {deviceError && <Alert type="error" showIcon message={deviceError} />}
            {view !== 'devices' && visibleTraffic?.error && <Alert type="error" showIcon message={visibleTraffic.error} />}

            {view !== 'devices' && <TrafficPanels
              traffic={visibleTraffic?.traffic}
              chartData={chartData}
              range={range}
              onRangeChange={setRange}
              loading={loading}
              device={selectedDevice}
              deviceId={scope}
              liveState={live.state}
              rate={selectedDevice?.enabled ? live.snapshot?.devices[scope] : undefined}
              history={currentHistory}
              liveEnd={live.snapshot?.at ? Date.parse(live.snapshot.at) : Date.now()}
            />}

            <ProCard
              bordered
              title={<h2 className="panel-section-title">{view === 'traffic' ? '全部设备用量' : '设备列表'}</h2>}
              extra={view === 'overview' ? <Button type="link" href="#devices">管理设备</Button> : undefined}
            >
              <div className="panel-device-meta">
                {deviceError ? '设备列表暂时不可用' : `已启用 ${activeCount} 台 · 共 ${devices.length} 台`}
                {!deviceError && devices.length > 0 && <> · {liveLabels[live.state]}</>}
              </div>
              {view === 'devices' && (
                <div className="panel-filters">
                  <Input.Search aria-label="搜索设备名称" placeholder="搜索设备名称" allowClear
                    value={query} onChange={(event) => setQuery(event.target.value)} />
                  <Select<DeviceFilter> aria-label="按设备启用状态筛选" value={filter} options={statusOptions} onChange={setFilter} />
                </div>
              )}
              <DeviceTable
                devices={visibleRows} rates={live.snapshot?.devices} presence={live.snapshot?.presence}
                lastActiveAt={live.lastActiveAt} timeZone={traffic?.traffic?.timeZone} selectedDeviceId={scope}
                loading={loading}
                emptyLabel={deviceError ? '设备列表加载失败' : devices.length ? '没有符合条件的设备' : '还没有设备'}
                emptyAction={!loading && !devices.length && !deviceError ? createButton : undefined}
                onSubscription={(device) => setDialog({ type: 'subscription', device })}
                onRename={(device) => setDialog({ type: 'rename', device })}
                onToggle={(device) => setDialog({ type: 'toggle', device })}
                onTraffic={openDeviceTraffic}
                onDestinations={(device) => setDialog({ type: 'destinations', device })}
              />
            </ProCard>
          </div>
        )}
      </PageContainer>
      {dialog && <DeviceDialogs
        key={dialog.type === 'create' ? 'create' : `${dialog.type}-${dialog.device.id}`}
        dialog={dialog}
        devices={devices}
        timeZone={traffic?.traffic?.timeZone}
        onClose={() => setDialog(undefined)}
        onExpired={onExpired}
        onSaved={deviceSaved}
      />}
    </ProLayout>
  )
}
